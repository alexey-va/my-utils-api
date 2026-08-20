#!/usr/bin/env python3
"""Validate the public dual-exit inventory and non-secret Vault example shape."""

from __future__ import annotations

import argparse
import ipaddress
import re
import sys
from pathlib import Path

import yaml


KEY_RE = re.compile(r"^[A-Za-z0-9+/]{43}=$")
UUID_RE = re.compile(r"^[0-9a-fA-F-]{36}$")


def fail(message: str) -> None:
    raise ValueError(message)


def mapping(value: object, label: str) -> dict:
    if not isinstance(value, dict):
        fail(f"{label} must be a mapping")
    return value


def load_yaml(path: Path) -> dict:
    value = yaml.safe_load(path.read_text(encoding="utf-8"))
    return mapping(value, str(path))


def validate_inventory(path: Path) -> tuple[str, list[str]]:
    document = load_yaml(path)
    all_group = mapping(document.get("all"), "all")
    variables = mapping(all_group.get("vars"), "all.vars")
    children = mapping(all_group.get("children"), "all.children")
    relay_hosts = mapping(mapping(children.get("vpn_relay"), "vpn_relay").get("hosts"), "vpn_relay.hosts")
    exit_hosts = mapping(mapping(children.get("vpn_exits"), "vpn_exits").get("hosts"), "vpn_exits.hosts")
    if len(relay_hosts) != 1:
        fail("exactly one vpn_relay host is required")
    if len(exit_hosts) != 2:
        fail("exactly two vpn_exits hosts are required")

    relay_name, relay = next(iter(relay_hosts.items()))
    relay = mapping(relay, f"vpn_relay.{relay_name}")
    for key in ("vpn_api_base_url", "vpn_relay_id", "vpn_public_endpoint", "vpn_direct_egress_interface"):
        if not str(relay.get(key, "")).strip():
            fail(f"{relay_name}.{key} is required")
    if not UUID_RE.fullmatch(str(relay["vpn_relay_id"])):
        fail(f"{relay_name}.vpn_relay_id is invalid")

    expected_roles = {"primary", "secondary"}
    roles: set[str] = set()
    overlays: set[int] = set()
    interfaces: set[str] = set()
    for name, raw in exit_hosts.items():
        host = mapping(raw, f"vpn_exits.{name}")
        role = str(host.get("awg_role", ""))
        interface = str(host.get("awg_interface", ""))
        endpoint = str(host.get("awg_endpoint", ""))
        expected_egress = str(host.get("expected_egress", ""))
        try:
            overlay = int(host.get("awg_overlay_octet"))
        except (TypeError, ValueError):
            fail(f"{name}.awg_overlay_octet is invalid")
        if role not in expected_roles:
            fail(f"{name}.awg_role must be primary or secondary")
        if not re.fullmatch(r"[A-Za-z0-9_.-]{1,15}", interface):
            fail(f"{name}.awg_interface is invalid")
        if not 1 <= overlay <= 254:
            fail(f"{name}.awg_overlay_octet is invalid")
        if not endpoint.endswith(":42697"):
            fail(f"{name}.awg_endpoint must use UDP port 42697")
        try:
            address = ipaddress.ip_address(expected_egress)
        except ValueError:
            fail(f"{name}.expected_egress is invalid")
        if address.version != 4:
            fail(f"{name}.expected_egress must be IPv4")
        roles.add(role)
        overlays.add(overlay)
        interfaces.add(interface)
    if roles != expected_roles:
        fail("vpn_exits must contain one primary and one secondary")
    if len(overlays) != 2 or len(interfaces) != 2:
        fail("vpn_exits must use distinct overlay octets and interfaces")

    primary = str(variables.get("vpn_primary_exit_host", ""))
    secondary = str(variables.get("vpn_secondary_exit_host", ""))
    if primary not in exit_hosts or secondary not in exit_hosts or primary == secondary:
        fail("primary and secondary exit host selectors are invalid")
    if exit_hosts[primary].get("awg_role") != "primary" or exit_hosts[secondary].get("awg_role") != "secondary":
        fail("primary and secondary selectors do not match awg_role")
    return relay_name, list(exit_hosts)


def validate_vault(path: Path, exits: list[str]) -> None:
    text = path.read_text(encoding="utf-8")
    if text.startswith("$ANSIBLE_VAULT;"):
        return
    document = load_yaml(path)
    for key in ("vault_wireguard_agent_token", "vault_wireguard_server_private_key"):
        if key not in document:
            fail(f"{key} is required")
    keys = mapping(document.get("vault_awg_client_private_keys"), "vault_awg_client_private_keys")
    if set(keys) != set(exits):
        fail("vault_awg_client_private_keys must match vpn_exits hosts")
    values = [document["vault_wireguard_server_private_key"], *keys.values()]
    placeholders = any(str(value).startswith("CHANGE_ME_") for value in values)
    if not placeholders and not all(KEY_RE.fullmatch(str(value)) for value in values):
        fail("WireGuard and AWG private keys must be base64 keys")
    token = str(document["vault_wireguard_agent_token"])
    if not token.startswith("CHANGE_ME_") and len(token) < 40:
        fail("vault_wireguard_agent_token is too short")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--inventory", required=True, type=Path)
    parser.add_argument("--vault-vars", required=True, type=Path)
    args = parser.parse_args()
    try:
        _, exits = validate_inventory(args.inventory)
        validate_vault(args.vault_vars, exits)
    except (OSError, ValueError, yaml.YAMLError) as error:
        print(f"Ansible VPN configuration is invalid: {error}", file=sys.stderr)
        return 1
    print("Ansible VPN configuration: OK")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
