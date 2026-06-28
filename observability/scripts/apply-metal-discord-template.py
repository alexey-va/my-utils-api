#!/usr/bin/env python3
"""Apply Metal Status Discord notification templates to Grafana."""
from __future__ import annotations

import base64
import json
import os
import sys
import urllib.error
import urllib.request
from pathlib import Path

GRAFANA_URL = os.environ.get("GRAFANA_URL", "http://127.0.0.1:3500/grafana")
GRAFANA_USER = os.environ.get("GRAFANA_USER", "")
GRAFANA_PASSWORD = os.environ.get("GRAFANA_PASSWORD", "")
GRAFANA_TOKEN = os.environ.get("GRAFANA_SERVICE_ACCOUNT_TOKEN", "")
TEMPLATE_FILE = Path(__file__).resolve().parent.parent / "config" / "grafana-metal-discord-template.txt"
TEMPLATE_NAME = "metal-discord"
CONTACT_POINT_NAME = "discord"


def auth_headers() -> dict[str, str]:
    headers = {
        "Content-Type": "application/json",
        "X-Disable-Provenance": "true",
    }
    if GRAFANA_TOKEN:
        headers["Authorization"] = f"Bearer {GRAFANA_TOKEN}"
    elif GRAFANA_USER and GRAFANA_PASSWORD:
        token = base64.b64encode(f"{GRAFANA_USER}:{GRAFANA_PASSWORD}".encode()).decode()
        headers["Authorization"] = f"Basic {token}"
    return headers


def request(method: str, path: str, body: dict | None = None) -> tuple[int, object]:
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(
        f"{GRAFANA_URL}{path}",
        data=data,
        headers=auth_headers(),
        method=method,
    )
    with urllib.request.urlopen(req) as response:
        raw = response.read()
        return response.status, json.loads(raw) if raw else None


def main() -> int:
    if not GRAFANA_TOKEN and not (GRAFANA_USER and GRAFANA_PASSWORD):
        print("Set GRAFANA_SERVICE_ACCOUNT_TOKEN or GRAFANA_USER+GRAFANA_PASSWORD", file=sys.stderr)
        return 1

    template = TEMPLATE_FILE.read_text(encoding="utf-8")
    try:
        status, _ = request("PUT", f"/api/v1/provisioning/templates/{TEMPLATE_NAME}", {
            "name": TEMPLATE_NAME,
            "template": template,
        })
        print(f"template: HTTP {status}")

        _, contact_points = request("GET", "/api/v1/provisioning/contact-points")
        contact_point = next(cp for cp in contact_points if cp["name"] == CONTACT_POINT_NAME)
        settings = dict(contact_point["settings"])
        settings["title"] = '{{ template "metal.discord.title" . }}'
        settings["message"] = '{{ template "metal.discord.message" . }}'
        settings["use_discord_username"] = True
        settings["avatar_url"] = "https://utils.alexeyav.ru/grafana/public/img/grafana_icon.svg"

        uid = contact_point["uid"]
        status, _ = request("PUT", f"/api/v1/provisioning/contact-points/{uid}", {
            "uid": uid,
            "name": contact_point["name"],
            "type": contact_point["type"],
            "settings": settings,
            "disableResolveMessage": contact_point.get("disableResolveMessage", False),
        })
        print(f"contact point: HTTP {status}")
    except urllib.error.HTTPError as error:
        print(error.read().decode(), file=sys.stderr)
        return 1

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
