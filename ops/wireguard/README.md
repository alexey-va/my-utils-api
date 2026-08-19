# my-utils WireGuard relay

This directory packages a standard WireGuard ingress (`wg-users`) whose client
traffic is routed, without source NAT on `utils`, through a dedicated
AmneziaWG egress (`awg-exit`) to the existing `veesp` Amnezia server. The
preserved `10.89.0.0/24` client addresses make per-peer accounting possible on
both hosts.

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
6. Verify `systemctl status my-utils-awg-exit`, `wg show wg-users`,
   `awg show awg-exit`, the agent timer, the source policy rule, and public
   egress from an actual client. Do not paste `showconf`, private keys, PSKs, or
   the agent environment into logs.

The policy table always keeps an unreachable fallback. If `awg-exit` is down,
traffic sourced from the client CIDR is rejected instead of escaping through
the ordinary `utils` default route. `utils` deliberately has no masquerade rule
for the client CIDR; `veesp` owns the final NAT.

The production API also needs a base64-encoded 32-byte
`WIREGUARD_CREDENTIALS_ENCRYPTION_KEY`. Losing it does not stop existing peers,
but prevents recovery of stored client configs; rotate affected peers instead
of attempting to reconstruct the key.
