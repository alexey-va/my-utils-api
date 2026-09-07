#!/usr/bin/env python3
"""Explicitly delete the four retired RusCrafting alert rule UIDs."""
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

RETIRE_UIDS = (
    "inf00001",
    "inf00002",
    "inf00003",
    "inf00004",
)


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


def list_target_rules() -> dict[str, str]:
    _, rules = request("GET", "/api/v1/provisioning/alert-rules")
    return {
        rule["uid"]: rule.get("title", "")
        for rule in rules
        if rule.get("uid") in RETIRE_UIDS
    }


def main() -> int:
    if sys.argv[1:] != ["--confirm"]:
        print(f"Usage: {sys.argv[0]} --confirm", file=sys.stderr)
        return 2
    if not GRAFANA_TOKEN and not (GRAFANA_USER and GRAFANA_PASSWORD):
        print("Set GRAFANA_SERVICE_ACCOUNT_TOKEN or GRAFANA_USER+GRAFANA_PASSWORD", file=sys.stderr)
        return 1

    deleted = 0
    try:
        targets = list_target_rules()
        for uid in RETIRE_UIDS:
            title = targets.get(uid)
            if title is None:
                print(f"absent: {uid}")
                continue
            status, _ = request("DELETE", f"/api/v1/provisioning/alert-rules/{uid}")
            print(f"deleted: {title} ({uid}) HTTP {status}")
            deleted += 1
    except urllib.error.HTTPError as error:
        print(f"Grafana request failed: HTTP {error.code} {error.reason}", file=sys.stderr)
        return 1

    print(f"done: removed {deleted} legacy rule(s)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
