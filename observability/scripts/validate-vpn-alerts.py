#!/usr/bin/env python3
"""Validate provisioned VPN rules without contacting Grafana."""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

import yaml


ROOT = Path(__file__).resolve().parent.parent
RULES_FILE = ROOT / "config" / "grafana" / "provisioning" / "alerting" / "vpn-alert-rules.yaml"
EXPECTED_ALERTS = {
    "VPN metrics unavailable": ("myutils_wireguard_collection_success", "2m"),
    "VPN relay unavailable": ("myutils_wireguard_relay_ready", "1m"),
    "VPN agent stale": ("myutils_wireguard_agent_last_seen_timestamp_seconds", "1m"),
    "VPN routing unhealthy": ("myutils_wireguard_routing_healthy", "1m"),
    "VPN all exits down": ("myutils_wireguard_exit_healthy", "30s"),
    "VPN primary exit unhealthy": ("myutils_wireguard_exit_healthy", "2m"),
    "VPN running on reserve": ("myutils_wireguard_exit_selected", "2m"),
    "VPN packet loss high": ("myutils_wireguard_route_packet_loss_percent", "5m"),
}


def duration_seconds(value: str) -> int:
    match = re.fullmatch(r"(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s)?", value)
    if not match or not any(match.groups()):
        raise ValueError(f"invalid duration {value!r}")
    hours, minutes, seconds = (int(part or 0) for part in match.groups())
    return hours * 3600 + minutes * 60 + seconds


def validate(path: Path) -> None:
    document = yaml.safe_load(path.read_text(encoding="utf-8"))
    groups = document.get("groups", []) if isinstance(document, dict) else []
    rules = [rule for group in groups for rule in group.get("rules", [])]
    by_title = {rule.get("title"): rule for rule in rules}
    if set(by_title) != set(EXPECTED_ALERTS):
        missing = sorted(set(EXPECTED_ALERTS) - set(by_title))
        extra = sorted(set(by_title) - set(EXPECTED_ALERTS))
        raise ValueError(f"unexpected VPN alert set; missing={missing}, extra={extra}")
    if any(group.get("interval") != "30s" for group in groups):
        raise ValueError("VPN alert groups must evaluate every 30s")
    for title, (metric, pending) in EXPECTED_ALERTS.items():
        rule = by_title[title]
        expressions = "\n".join(
            str(item.get("model", {}).get("expr", "")) for item in rule.get("data", [])
        )
        if metric not in expressions:
            raise ValueError(f"{title} does not query {metric}")
        if rule.get("for") != pending:
            raise ValueError(f"{title} has wrong anti-flap window")
        if rule.get("labels", {}).get("team") != "vpn":
            raise ValueError(f"{title} is missing team=vpn")
        if rule.get("isPaused") is not False:
            raise ValueError(f"{title} must be enabled")
        notifications = rule.get("notification_settings", {})
        if notifications.get("receiver") != "Metal Discord":
            raise ValueError(f"{title} uses the wrong receiver")
        if duration_seconds(str(notifications.get("repeat_interval", ""))) < 4 * 3600:
            raise ValueError(f"{title} repeat_interval is too short")
        no_data_alerts = {"VPN metrics unavailable", "VPN relay unavailable"}
        if title in no_data_alerts and rule.get("noDataState") != "Alerting":
            raise ValueError(f"{title} must alert on no data")
        if title not in no_data_alerts and rule.get("noDataState") != "OK":
            raise ValueError(f"{title} must defer missing-series handling to the collector or relay alert")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true", help="validate the versioned rule file")
    parser.add_argument("--file", type=Path, default=RULES_FILE)
    args = parser.parse_args()
    try:
        validate(args.file)
    except (OSError, ValueError, yaml.YAMLError) as error:
        print(f"VPN alert validation failed: {error}", file=sys.stderr)
        return 1
    print("VPN alert validation: OK")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
