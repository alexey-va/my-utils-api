# my-utils WireGuard relay

This directory packages a standard WireGuard ingress (`wg-users`) whose client
traffic is routed through a dedicated AmneziaWG egress (`awg-exit`) to the
existing `veesp` Amnezia server. An optional country router sends validated
Russian IPv4 destinations directly through the ordinary `utils` egress; every
other destination remains fail-closed on AWG. The preserved `10.89.0.0/24`
client addresses make per-peer accounting possible on both hosts.

All installers default to plan-only output. `--apply` performs host changes.
They refuse unexpected container layouts, unsupported operating systems,
unsafe CIDRs, loose secret-file permissions, and existing configs unless an
explicit replacement mode is available.

## Order of operations

1. Create a relay through the administrator API and save its one-time agent
   token in a mode-600 file.
2. On `veesp`, run `prepare-veesp-amnezia-peer.sh` first without and then with
   `--apply`. Its output is a secret client artifact; never print or commit it.
3. Transfer that artifact directly to a mode-600 temporary file on `utils`.
4. On `utils`, run `install-amnezia-egress.sh` in plan mode and then with
   `--apply --consume`.
5. Run `install-relay.sh` in plan mode and then with `--apply`.
6. Run `install-geo-routing.sh --client-cidr 10.89.0.0/24
   --ingress-interface wg-users --direct-egress-interface eth0` first in plan
   mode and then with `--apply`.
7. Verify `systemctl status my-utils-awg-exit`, `wg show wg-users`,
   `awg show awg-exit`, the agent timer, the source policy rule, and public
   egress from an actual client. Do not paste `showconf`, private keys, PSKs, or
   the agent environment into logs.

The policy table always keeps an unreachable fallback. If `awg-exit` is down,
traffic sourced from the client CIDR is rejected instead of escaping through
the ordinary `utils` default route. The only `utils` masquerade rule matches
the owned `0x51890` mark assigned to destinations in the validated RU set;
Veesp owns final NAT for all AWG traffic.

## RU-direct routing safety

`render-geo-prefixes.py` accepts only strict, public IPv4 networks, rejects
default/private/loopback/link-local/multicast/reserved ranges, and requires an
expected list size. `update-geo-routing.sh` downloads the aggregated IPdeny RU
zone over HTTPS, validates it, checks the nftables transaction, and loads the
whole set atomically. A failed download, validation, or nft load leaves the
previous set untouched. On a first-boot failure the set stays empty, so no
traffic gains the direct mark and the AWG-only rule remains in force.

The owned rule order is:

- priority `1088`: mark `0x51890` uses `main` and `eth0`;
- priority `1089`: unmarked `10.89.0.0/24` uses table `51889` and `awg-exit`.

The updater runs daily through `my-utils-geo-routing-update.timer`. Its
non-secret status file is read by the existing agent and shown in the admin UI.
Country selection is IP-based: CDN placement can differ from a domain's
business country.

## Traffic and route quality metrics

The relay agent records interval byte counters per peer in two route groups:
validated RU destination prefixes routed directly through `eth0`, and all
other destinations routed through `awg-exit`. Download classification uses the
actual return interface. These are forwarded IP bytes, so their sum can differ
slightly from WireGuard's encrypted interface counters because tunnel overhead
is not included.

The systemd timer reports counters every 15 seconds. This keeps live rates
useful without running a permanent daemon; the administrator page polls the
lightweight relay/peer snapshot every five seconds and refreshes historical
previews once per minute.

The agent also sends a current route-quality snapshot on every heartbeat. The
direct probe pings `77.88.8.8` through `eth0`; the Veesp probe pings the public
endpoint reported by `awg show awg-exit endpoints`, also through `eth0`. The
second value measures the underlying path from `utils` to Veesp, not end-to-end
loss between a client device and `wg-users`. Targets and interfaces can be
overridden with `WIREGUARD_DIRECT_PROBE_TARGET`,
`WIREGUARD_DIRECT_INTERFACE`, and `WIREGUARD_AWG_INTERFACE` in the agent
environment.

The production API also needs a base64-encoded 32-byte
`WIREGUARD_CREDENTIALS_ENCRYPTION_KEY`. Losing it does not stop existing peers,
but prevents recovery of stored client configs; rotate affected peers instead
of attempting to reconstruct the key.

## API outbound proxy routing

`api-proxy-routing.sh` keeps the API container's configured HTTP proxy reachable
when the ordinary `utils` to Veesp path is unavailable. It marks only TCP
traffic from Docker private IPv4 networks to `185.242.106.81:8888`, sends that
mark through the existing fail-closed table `51889`, and SNATs it to the owned
`wg-users` address `10.89.0.1`. Telegram and OpenRouter therefore share the
existing AWG egress without moving either secret into host routing files.

Install the script as `/usr/local/libexec/my-utils-api-proxy-routing` and the
unit as `/etc/systemd/system/my-utils-api-proxy-routing.service`, then enable the
unit only after `my-utils-wireguard-routing.service` is active. The unit owns
priority `1087`, mark `0x51891`, and the `MYUTILS-API-PROXY*` iptables chains.
