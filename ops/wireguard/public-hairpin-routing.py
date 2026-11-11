#!/usr/bin/env python3
"""Send VPN requests to this relay's public IPv4 address through its existing exit."""

import fcntl
import ipaddress
import json
import os
import subprocess
import sys
from pathlib import Path


def ip(*args):
    return subprocess.run(["ip", *args], check=True, text=True, capture_output=True, timeout=10).stdout


def network(rule, key):
    value = rule.get(key, "all")
    if value == "all":
        return "0.0.0.0/0"
    length = rule.get(key + "len", 32)
    return str(ipaddress.IPv4Network(value if "/" in value else f"{value}/{length}"))


def signature(rule):
    known = {"priority", "src", "srclen", "dst", "dstlen", "iif", "table", "action", "protocol"}
    return (
        rule["priority"], network(rule, "src"), network(rule, "dst"), rule.get("iif"),
        str(rule.get("table", "")), rule.get("action", "lookup"), str(rule.get("protocol", "")),
        json.dumps({k: v for k, v in rule.items() if k not in known}, sort_keys=True),
    )


def rules():
    return json.loads(ip("-j", "-details", "-4", "rule", "show"))


def main():
    mode = sys.argv[1] if len(sys.argv) == 2 else "plan"
    if mode not in {"plan", "start", "stop"} or len(sys.argv) > 2:
        raise ValueError("Usage: public-hairpin-routing.py [plan|start|stop]")
    address = str(ipaddress.IPv4Address(os.environ["WIREGUARD_PUBLIC_ADDRESS"]))
    clients = str(ipaddress.IPv4Network(os.environ.get("WIREGUARD_CLIENT_CIDR", "10.89.0.0/24")))
    ingress = os.environ.get("WIREGUARD_INGRESS_INTERFACE", "wg-users")
    table = int(os.environ.get("WIREGUARD_ROUTE_TABLE", "51889"))
    if table <= 0 or table in {253, 254, 255} or table > 2**32 - 1:
        raise ValueError("An existing dedicated VPN routing table is required")

    state_path = Path(os.environ.get("WIREGUARD_HAIRPIN_STATE_FILE", "/run/my-utils-wireguard-public-hairpin.json"))
    identity = {"address": address, "clients": clients, "ingress": ingress, "table": table}
    if state_path.exists() and json.loads(state_path.read_text()) != identity:
        raise ValueError("Existing public-address routing belongs to a different configuration")

    # Keep the local lookup kernel-owned so networkd cannot purge all local delivery.
    # The root-only state file records ownership of its otherwise generic rule.
    selector = ["from", clients, "to", address + "/32", "iif", ingress]
    definitions = {
        "original": (["priority", "0", "from", "all", "lookup", "local", "protocol", "kernel"],
                     {"priority": 0, "src": "all", "table": "local", "protocol": "kernel"}),
        "lookup": (["priority", "0", *selector, "lookup", str(table), "protocol", "242"],
                   {"priority": 0, "src": clients, "dst": address, "iif": ingress,
                    "table": str(table), "protocol": "242"}),
        "closed": (["unreachable", "priority", "1", *selector, "protocol", "242"],
                   {"priority": 1, "src": clients, "dst": address, "iif": ingress,
                    "action": "unreachable", "protocol": "242"}),
        "local": (["priority", "2", "from", "all", "lookup", "local", "protocol", "kernel"],
                  {"priority": 2, "src": "all", "table": "local", "protocol": "kernel"}),
    }
    expected = {name: signature(definition[1]) for name, definition in definitions.items()}

    def inspect():
        current = rules()
        for rule in current:
            if rule["priority"] <= 2 and signature(rule) not in expected.values():
                raise ValueError(f"Refusing to replace an unrelated routing rule: {rule}")
        present = {name for name, sig in expected.items() if any(signature(r) == sig for r in current)}
        if "local" in present and not state_path.exists():
            raise ValueError("Refusing an unowned priority 2 local lookup")
        if not present.intersection({"original", "local"}):
            raise ValueError("Neither the original nor the owned backup local lookup exists")
        return present

    def preflight():
        addresses = json.loads(ip("-j", "-4", "address", "show"))
        if not any(a.get("local") == address for link in addresses for a in link.get("addr_info", [])):
            raise ValueError("The public address must belong to this relay")
        ip("link", "show", "dev", ingress)
        routes = json.loads(ip("-j", "-4", "route", "show", "table", str(table)))
        if not any(r.get("type") == "unreachable" and r.get("dst", "default") == "default" for r in routes):
            raise ValueError("The existing VPN table must have an unreachable default")

    if mode == "plan":
        preflight()
        inspect()
        print(f"Plan: {ingress} {clients} -> {address}/32 uses VPN table {table}; other local traffic is unchanged")
        return

    lock_path = os.environ.get("WIREGUARD_HAIRPIN_LOCK_FILE", "/run/my-utils-wireguard-public-hairpin.lock")
    with os.fdopen(os.open(lock_path, os.O_CREAT | os.O_RDWR, 0o600), "w") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        if mode == "start":
            preflight()
        inspect()
        created_state = False
        if mode == "start" and not state_path.exists():
            with os.fdopen(os.open(state_path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600), "w") as state:
                json.dump(identity, state)
            created_state = True
        undo = []

        def change(name, add, record=True):
            current = {signature(rule) for rule in rules()}
            if (expected[name] in current) == add:
                return
            if not add and name in {"original", "local"}:
                other = "local" if name == "original" else "original"
                if expected[other] not in current:
                    raise RuntimeError("Refusing to remove the last verified local lookup")
            action = "add" if add else "del"
            # Record before netlink: a timeout does not prove the mutation failed.
            if record:
                undo.append((name, not add))
            ip("-4", "rule", action, *definitions[name][0])

        try:
            if mode == "start":
                # Keep a local lookup throughout staging, including on command failure.
                change("local", True)
                change("lookup", True)
                change("closed", True)
                change("original", False)
                wanted = {"lookup", "closed", "local"}
            else:
                change("original", True)
                change("lookup", False)
                change("closed", False)
                change("local", False)
                wanted = {"original"}
            if inspect() != wanted:
                raise RuntimeError("Routing readback does not match the requested state")
        except Exception:
            rollback_failed = False
            for name, add in reversed(undo):
                try:
                    change(name, add, record=False)
                except Exception as rollback_error:
                    rollback_failed = True
                    print(f"Rollback needs attention: {rollback_error}", file=sys.stderr)
            if created_state and not rollback_failed:
                state_path.unlink()
            raise
        if mode == "stop" and state_path.exists():
            state_path.unlink()
        print(f"Public-address VPN routing: {mode} verified for {address}/32 via table {table}")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, OSError, RuntimeError, subprocess.SubprocessError) as error:
        print(f"Public-address VPN routing failed: {error}", file=sys.stderr)
        sys.exit(1)
