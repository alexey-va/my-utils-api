#!/usr/bin/env python3
"""Apply compact Metal Discord template and retire legacy RusCrafting alerts."""
from __future__ import annotations

import base64
import json
import os
import sys
import urllib.error
import urllib.request
from pathlib import Path

GRAFANA_URL = os.environ.get("GRAFANA_URL", "https://utils.alexeyav.ru/grafana")
GRAFANA_USER = os.environ.get("GRAFANA_USER", os.environ.get("GF_SECURITY_ADMIN_USER", ""))
GRAFANA_PASSWORD = os.environ.get("GRAFANA_PASSWORD", os.environ.get("GF_SECURITY_ADMIN_PASSWORD", ""))
GRAFANA_TOKEN = os.environ.get("GRAFANA_SERVICE_ACCOUNT_TOKEN", "")
TEMPLATE_FILE = Path(__file__).resolve().parent.parent / "config" / "grafana-metal-discord-template.txt"
TEMPLATE_NAME = "metal-discord"
LEGACY_PREFIXES = ("RusCrafting ",)
CONTACT_NAMES = ("Metal Discord", "Discord")


def auth_headers(extra: dict[str, str] | None = None) -> dict[str, str]:
    headers = {"Content-Type": "application/json", "X-Disable-Provenance": "true"}
    if extra:
        headers.update(extra)
    if GRAFANA_TOKEN:
        headers["Authorization"] = f"Bearer {GRAFANA_TOKEN}"
    elif GRAFANA_USER and GRAFANA_PASSWORD:
        token = base64.b64encode(f"{GRAFANA_USER}:{GRAFANA_PASSWORD}".encode()).decode()
        headers["Authorization"] = f"Basic {token}"
    return headers


def request(method: str, path: str, body: dict | None = None) -> tuple[int, object]:
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(
        f"{GRAFANA_URL.rstrip('/')}{path}",
        data=data,
        headers=auth_headers(),
        method=method,
    )
    with urllib.request.urlopen(req) as response:
        raw = response.read()
        return response.status, json.loads(raw) if raw else None


def apply_template() -> None:
    template = TEMPLATE_FILE.read_text(encoding="utf-8")
    status, _ = request("PUT", f"/api/v1/provisioning/templates/{TEMPLATE_NAME}", {
        "name": TEMPLATE_NAME,
        "template": template,
    })
    print(f"template: HTTP {status}")

    _, contact_points = request("GET", "/api/v1/provisioning/contact-points")
    for contact_point in contact_points:
        if contact_point.get("name") not in CONTACT_NAMES and contact_point.get("type") != "discord":
            continue
        settings = dict(contact_point["settings"])
        settings["title"] = '{{ template "metal.discord.title" . }}'
        settings["message"] = '{{ template "metal.discord.message" . }}'
        settings["use_discord_username"] = True
        uid = contact_point["uid"]
        status, _ = request("PUT", f"/api/v1/provisioning/contact-points/{uid}", {
            "uid": uid,
            "name": contact_point["name"],
            "type": contact_point["type"],
            "settings": settings,
            "disableResolveMessage": contact_point.get("disableResolveMessage", False),
        })
        print(f"contact point ({contact_point['name']}): HTTP {status}")


def retire_legacy_rules() -> int:
    _, rules = request("GET", "/api/v1/provisioning/alert-rules")
    deleted = 0
    for rule in rules:
        title = rule.get("title", "")
        if not title.startswith(LEGACY_PREFIXES):
            continue
        status, _ = request("DELETE", f"/api/v1/provisioning/alert-rules/{rule['uid']}")
        print(f"deleted: {title} HTTP {status}")
        deleted += 1
    return deleted


def main() -> int:
    if not GRAFANA_TOKEN and not (GRAFANA_USER and GRAFANA_PASSWORD):
        print("Set GRAFANA_SERVICE_ACCOUNT_TOKEN or GRAFANA_USER+GRAFANA_PASSWORD", file=sys.stderr)
        return 1
    try:
        apply_template()
        deleted = retire_legacy_rules()
        print(f"done: removed {deleted} legacy rule(s)")
    except urllib.error.HTTPError as error:
        print(error.read().decode(), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
