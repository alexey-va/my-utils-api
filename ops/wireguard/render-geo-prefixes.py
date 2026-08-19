#!/usr/bin/env python3
"""Validate an IPv4 country zone and render an atomic nftables set transaction."""

from __future__ import annotations

import argparse
import ipaddress
import pathlib
import sys


TABLE_FAMILY = "ip"
TABLE_NAME = "myutils_wg_geo"
SET_NAME = "ru_ipv4"
CHUNK_SIZE = 500


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--minimum-prefixes", type=int, default=5_000)
    parser.add_argument("--maximum-prefixes", type=int, default=20_000)
    parser.add_argument("--count-file", type=pathlib.Path)
    return parser.parse_args()


def is_safe_public(network: ipaddress.IPv4Network) -> bool:
    return (
        network.prefixlen >= 8
        and network.is_global
        and not network.is_private
        and not network.is_loopback
        and not network.is_link_local
        and not network.is_multicast
        and not network.is_reserved
        and not network.is_unspecified
    )


def load_networks() -> list[ipaddress.IPv4Network]:
    networks: list[ipaddress.IPv4Network] = []
    for line_number, raw in enumerate(sys.stdin, start=1):
        value = raw.strip()
        if not value or value.startswith("#"):
            continue
        try:
            network = ipaddress.ip_network(value, strict=True)
        except ValueError as error:
            raise ValueError(f"line {line_number}: invalid CIDR: {error}") from error
        if not isinstance(network, ipaddress.IPv4Network) or not is_safe_public(network):
            raise ValueError(f"line {line_number}: unsafe IPv4 network: {value}")
        networks.append(network)
    return sorted(ipaddress.collapse_addresses(networks), key=lambda item: (int(item.network_address), item.prefixlen))


def main() -> int:
    args = parse_args()
    if args.minimum_prefixes < 1 or args.maximum_prefixes < args.minimum_prefixes:
        print("invalid prefix count limits", file=sys.stderr)
        return 2
    try:
        networks = load_networks()
    except ValueError as error:
        print(error, file=sys.stderr)
        return 2
    if not args.minimum_prefixes <= len(networks) <= args.maximum_prefixes:
        print(
            f"validated prefix count {len(networks)} is outside "
            f"{args.minimum_prefixes}..{args.maximum_prefixes}",
            file=sys.stderr,
        )
        return 2

    print(f"flush set {TABLE_FAMILY} {TABLE_NAME} {SET_NAME}")
    for start in range(0, len(networks), CHUNK_SIZE):
        values = ", ".join(str(network) for network in networks[start : start + CHUNK_SIZE])
        print(f"add element {TABLE_FAMILY} {TABLE_NAME} {SET_NAME} {{ {values} }}")
    if args.count_file:
        args.count_file.write_text(f"{len(networks)}\n", encoding="ascii")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
