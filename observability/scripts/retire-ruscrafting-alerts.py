#!/usr/bin/env python3
"""Delete legacy RusCrafting alert rules superseded by Metal provisioning."""
from __future__ import annotations

import base64
import json
import os
import sys
import urllib.error
import urllib.request

GRAFANA_URL = os.environ.get("GRAFANA_URL", "http://127.0.0.1:3500/grafana")
GRAFANA_USER = os.environ.get("GRAFANA_USER", os.environ.get("GF_SECURITY_ADMIN_USER", ""))
GRAFANA_PASSWORD = os.environ.get("GRAFANA_PASSWORD", os.environ.get("GF_SECURITY_ADMIN_PASSWORD", ""))
GRAFANA_TOKEN = os.environ.get("GRAFANA_SERVICE_ACCOUNT_TOKEN", "")

LEGACY_PREFIXES = ("RusCrafting ",)


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


def request(method: str, path: str) -> tuple[int, object]:
    req = urllib.request.Request(
        f"{GRAFANA_URL}{path}",
        headers=auth_headers(),
        method=method,
    )
    with urllib.request.urlopen(req) as response:
        raw = response.read()
        return response.status, json.loads(raw) if raw else None


def list_legacy_uids() -> list[tuple[str, str]]:
    _, rules = request("GET", "/api/v1/provisioning/alert-rules")
    legacy = [
        (rule["uid"], rule.get("title", ""))
        for rule in rules
        if rule.get("title", "").startswith(LEGACY_PREFIXES)
    ]
    if legacy:
        return legacy

    _, ruler = request("GET", "/api/ruler/grafana/api/v1/rules")
    for _folder, groups in ruler.items():
        for group in groups:
            for rule in group.get("rules", []):
                alert = rule.get("grafana_alert", {})
                title = alert.get("title", "")
                uid = alert.get("uid", "")
                if title.startswith(LEGACY_PREFIXES) and uid:
                    legacy.append((uid, title))
    return legacy


def main() -> int:
    if not GRAFANA_TOKEN and not (GRAFANA_USER and GRAFANA_PASSWORD):
        print("Set GRAFANA_SERVICE_ACCOUNT_TOKEN or GRAFANA_USER+GRAFANA_PASSWORD", file=sys.stderr)
        return 1

    deleted = 0
    try:
        for uid, title in list_legacy_uids():
            status, _ = request("DELETE", f"/api/v1/provisioning/alert-rules/{uid}")
            print(f"deleted: {title} ({uid}) HTTP {status}")
            deleted += 1
    except urllib.error.HTTPError as error:
        print(error.read().decode(), file=sys.stderr)
        return 1

    print(f"done: removed {deleted} legacy rule(s)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
