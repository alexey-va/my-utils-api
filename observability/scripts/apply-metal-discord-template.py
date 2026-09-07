#!/usr/bin/env python3
"""Apply the Metal Discord template to its explicitly owned contact point."""
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
PROVISIONED_TEMPLATE_FILE = (
    Path(__file__).resolve().parent.parent
    / "config"
    / "grafana"
    / "provisioning"
    / "alerting"
    / "metal-templates.yaml"
)
TEMPLATE_NAME = "metal-discord"
METAL_CONTACT_POINT_UID = "bfmetal6vcguq68c"
DASHBOARD_URLS = (
    "https://utils.alexeyav.ru/grafana/d/rYdddlPWk/metal-status",
)
FORBIDDEN_LINK_MARKERS = (".GeneratorURL", ".ExternalURL", ".SilenceURL", "/alerting/")


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


def provisioned_template() -> str:
    lines = PROVISIONED_TEMPLATE_FILE.read_text(encoding="utf-8").splitlines()
    marker = "    template: |"
    try:
        start = lines.index(marker) + 1
    except ValueError as error:
        raise ValueError(f"missing {marker!r} in {PROVISIONED_TEMPLATE_FILE}") from error

    body: list[str] = []
    for line in lines[start:]:
        if line and not line.startswith("      "):
            raise ValueError(f"unexpected content after template block: {line!r}")
        body.append(line[6:] if line.startswith("      ") else "")
    return "\n".join(body).rstrip() + "\n"


def validate_template_files() -> str:
    template = TEMPLATE_FILE.read_text(encoding="utf-8")
    provisioned = provisioned_template()
    if template.rstrip() != provisioned.rstrip():
        raise ValueError("standalone and provisioned Metal Discord templates differ")

    forbidden = [marker for marker in FORBIDDEN_LINK_MARKERS if marker in template]
    if forbidden:
        raise ValueError(f"alert-management links are forbidden: {', '.join(forbidden)}")

    missing = [url for url in DASHBOARD_URLS if url not in template]
    if missing:
        raise ValueError(f"missing dashboard link(s): {', '.join(missing)}")
    if ".DashboardURL" not in template:
        raise ValueError("per-alert DashboardURL link is required")
    if ".PanelURL" not in template:
        raise ValueError("per-alert PanelURL link is required")
    return template


def apply_template(template: str) -> None:
    status, _ = request("PUT", f"/api/v1/provisioning/templates/{TEMPLATE_NAME}", {
        "name": TEMPLATE_NAME,
        "template": template,
    })
    print(f"template: HTTP {status}")

    _, contact_points = request("GET", "/api/v1/provisioning/contact-points")
    contact_point = next(
        (item for item in contact_points if item.get("uid") == METAL_CONTACT_POINT_UID),
        None,
    )
    if contact_point is None:
        raise ValueError(f"owned Metal contact point {METAL_CONTACT_POINT_UID!r} was not found")

    settings = dict(contact_point.get("settings", {}))
    if not settings.get("url"):
        raise ValueError(f"owned Metal contact point {METAL_CONTACT_POINT_UID!r} has no webhook URL")
    settings.update({
        "httpMethod": "POST",
        "maxAlerts": 5,
        "payload": {"template": '{{ tmpl.Exec "metal.discord.payload" . }}'},
    })
    status, _ = request("PUT", f"/api/v1/provisioning/contact-points/{METAL_CONTACT_POINT_UID}", {
        "uid": METAL_CONTACT_POINT_UID,
        "name": contact_point["name"],
        "type": "webhook",
        "settings": settings,
        "disableResolveMessage": contact_point.get("disableResolveMessage", False),
    })
    print(f"contact point ({contact_point['name']} / {METAL_CONTACT_POINT_UID}): HTTP {status}")


def main() -> int:
    check_only = sys.argv[1:] == ["--check"]
    if sys.argv[1:] and not check_only:
        print(f"Usage: {Path(sys.argv[0]).name} [--check]", file=sys.stderr)
        return 2
    try:
        template = validate_template_files()
    except ValueError as error:
        print(f"template validation failed: {error}", file=sys.stderr)
        return 1
    if check_only:
        print("template validation: OK")
        return 0
    if not GRAFANA_TOKEN and not (GRAFANA_USER and GRAFANA_PASSWORD):
        print("Set GRAFANA_SERVICE_ACCOUNT_TOKEN or GRAFANA_USER+GRAFANA_PASSWORD", file=sys.stderr)
        return 1
    try:
        apply_template(template)
        print("done: template and owned contact point updated")
    except urllib.error.HTTPError as error:
        print(f"Grafana request failed: HTTP {error.code} {error.reason}", file=sys.stderr)
        return 1
    except ValueError as error:
        print(f"apply failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
