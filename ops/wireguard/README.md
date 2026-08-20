# my-utils WireGuard relay

This directory packages a standard WireGuard ingress (`wg-users`) whose client
traffic is routed through two simultaneously connected AmneziaWG exits from
independent providers (`awg-exit` and `awg-exit-b`). A five-second end-to-end
probe verifies both the handshake and observed public egress, prefers the
primary, switches after three failures, and fails back after two successful
primary probes. An optional country router sends validated Russian IPv4
destinations directly through the ordinary `utils` egress; every other
destination remains fail-closed on the selected AWG exit. The preserved
`10.89.0.0/24` client addresses make per-peer accounting possible on all hosts.

All installers default to plan-only output. `--apply` performs host changes.
They refuse unexpected container layouts, unsupported operating systems,
unsafe CIDRs, loose secret-file permissions, and existing configs unless an
explicit replacement mode is available.

## Order of operations

1. Create a relay through the administrator API and save its one-time agent
   token in a mode-600 file.
2. On each fresh exit host, copy `veesp-exit/` into a mode-700 directory and
   run `bootstrap-host.sh` in plan mode and then with `--apply`. Supply only
   known administrator source addresses through repeated `--trusted-ssh-ip`.
3. Generate a different protected utils client identity and a different AWG
   overlay subnet for each provider. Generate/install the server config on the
   matching exit host, then transfer only its protected `client.params` back to
   a mode-600 root file on `utils`. Never print or commit keys or PSKs.
4. Keep the existing primary as `awg-exit`; install the second client as
   `awg-exit-b` through `install-utils-client.sh`. This installer verifies its
   public egress without touching policy table `51889`.
5. Run `install-awg-failover.sh` in plan mode and then with `--apply --replace`.
   It installs the HA policy route, end-to-end timer, managed exit wildcard for
   the API proxy, and an unreachable fallback.
6. Run `install-relay.sh` in plan mode and then with `--apply`.
7. Run `install-geo-routing.sh --client-cidr 10.89.0.0/24
   --ingress-interface wg-users --direct-egress-interface eth0` first in plan
   mode and then with `--apply`.
8. Run `install-client-dns.sh --client-cidr 10.89.0.0/24
   --ingress-interface wg-users --resolver-address 10.89.0.1` in plan mode and
   then with `--apply`. It intercepts only TCP/UDP port 53 from VPN clients, so
   existing profiles that still say `DNS = 1.1.1.1` use the local cache without
   being reissued.
9. Verify both AWG services, the failover and agent timers, `wg show wg-users`,
   priorities `1087` through `1089`, the local DNS listener, and public egress
   from an actual client. Do not paste `showconf`, private keys, PSKs, or agent
   environments into logs.

The policy table always keeps an unreachable fallback. During hysteresis, or if
both exits are down, traffic sourced from the client CIDR is rejected instead
of escaping through the ordinary `utils` default route. The only `utils`
masquerade rule matches the owned `0x51890` mark assigned to destinations in
the validated RU set; each exit host owns final NAT for its AWG traffic.

## Reproducible isolated exit host

`veesp-exit/` is the versioned fresh-host stack. Despite its historical folder
name it is provider-neutral. It builds a pinned
AmneziaWG userspace image and a pinned Alpine image containing tinyproxy, then
runs them on the dedicated `my-utils-awg-exit` bridge (`172.29.172.0/24`, host
bridge name `amn0`). Only UDP `42697` is published. Tinyproxy stays at
`172.29.172.3:8888` inside the bridge and has no host port. The build context is
allowlisted by `.dockerignore`, so generated private keys, PSKs, and configs are
not sent to the Docker builder.

On a fresh host, first bootstrap bounded resources and SSH protection, then
generate protected configs from that host's dedicated utils client public key:

```bash
./generate-config.sh \
  --client-public-key-file client.pub \
  --server-config awg0.conf \
  --client-params client.params \
  --endpoint PUBLIC_IPV4:42697 \
  --server-address 10.8.N.1/24 \
  --client-address 10.8.N.250/32

./install.sh --config ./awg0.conf
./install.sh --config ./awg0.conf --apply
```

The installer is plan-only by default, refuses foreign containers, networks,
routes, and unmanaged install directories, and requires mode 600 for the AWG
config. A deliberate config replacement additionally requires `--replace`.
Use a distinct `N` for every exit. Transfer `client.params` directly to a
mode-600 root-only file on `utils`; do not print it or store it in Git. Install
the second and later clients without changing the active route:

```bash
sudo ./install-utils-client.sh \
  --interface awg-exit-b \
  --params /root/provider-b.params \
  --private-key-file /root/provider-b-client.key \
  --expected-egress PUBLIC_IPV4 \
  --apply

sudo ./switch-velocity-proxy.sh --expected-egress PUBLIC_IPV4
```

The reserve installer and guarded primary switch both wait for a fresh
handshake and verify public egress. The Velocity switch uses `wg syncconf` and
changes only the proxy host route and owned NAT rules, without taking
`wg-utils` down. Installers restore previous runtime state when validation
fails.

## Failover and runtime health

`my-utils-awg-failover.timer` runs every five seconds. Its status file contains
only non-secret interface, handshake-age, latency, expected/observed egress,
counter and selection data. The relay agent sends that snapshot plus a fresh
policy-routing check to the API. The administrator page therefore reports
`READY`, `DEGRADED`, or `DOWN` from the data plane instead of treating a live
agent heartbeat as proof that VPN traffic works.

A controlled failure test stops only `my-utils-awg-exit.service`. The route
must stay unreachable for the first two failed probes, select `awg-exit-b` on
the third, and return to the primary after it is started and passes two probes.
At both stable selections verify egress from source `10.89.0.1` and from the
API container's forced HTTP proxy. Never disable the unreachable fallback for
testing.

## AWG-independent client DNS

The local dnsmasq instance listens on `10.89.0.1` and loopback only. The owned
`MYUTILS-WG-DNS` NAT chain intercepts TCP and UDP port 53 only when packets
arrive on `wg-users` from `10.89.0.0/24`; it is not a public resolver. Upstream
queries originate from the utils host and use its ordinary main route, so RU
domain resolution remains available during AWG failure. This fixes DNS
dependency, but country selection itself remains IP-prefix based.

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
- priority `1089`: unmarked `10.89.0.0/24` uses table `51889` and its selected managed AWG exit.

The updater runs daily through `my-utils-geo-routing-update.timer`. Its
non-secret status file is read by the existing agent and shown in the admin UI.
Country selection is IP-based: CDN placement can differ from a domain's
business country.

## Traffic and route quality metrics

The relay agent records interval byte counters per peer in two route groups:
validated RU destination prefixes routed directly through `eth0`, and all
other destinations routed through the managed `awg-exit+` pool. Download
classification uses the actual return interface. These are forwarded IP bytes, so their sum can differ
slightly from WireGuard's encrypted interface counters because tunnel overhead
is not included.

The systemd timer reports counters every 15 seconds. This keeps live rates
useful without running a permanent daemon; the administrator page polls the
lightweight relay/peer snapshot every three seconds and refreshes historical
previews once per minute.

The agent also sends a current route-quality snapshot on every heartbeat. The
Internal probe pings `77.88.8.8` through `eth0`; the External probe pings the
public endpoint reported by the currently selected AWG interface, also through
`eth0`. The second value measures the underlying path from `utils` to the active exit, not end-to-end
loss between a client device and `wg-users`. Targets and interfaces can be
overridden with `WIREGUARD_DIRECT_PROBE_TARGET`,
`WIREGUARD_DIRECT_INTERFACE`, and `WIREGUARD_AWG_INTERFACE` in the agent
environment.

The production API also needs a base64-encoded 32-byte
`WIREGUARD_CREDENTIALS_ENCRYPTION_KEY`. Losing it does not stop existing peers,
but prevents recovery of stored client configs; rotate affected peers instead
of attempting to reconstruct the key.

## API outbound proxy routing

`api-proxy-routing.sh` keeps the API container's configured legacy HTTP proxy
address reachable without exposing tinyproxy publicly. It marks only TCP
traffic from Docker private IPv4 networks to `185.242.106.81:8888`, DNATs that
destination to the tunnel-only `172.29.172.3:8888`, sends the mark through the
existing fail-closed table `51889`, and SNATs it to the owned `wg-users`
address `10.89.0.1`. Telegram and OpenRouter therefore share the existing AWG
egress without moving either secret into host routing files.

Install the script as `/usr/local/libexec/my-utils-api-proxy-routing` and the
unit as `/etc/systemd/system/my-utils-api-proxy-routing.service`, then enable the
unit only after `my-utils-wireguard-routing.service` is active. The unit owns
priority `1087`, mark `0x51891`, and the `MYUTILS-API-PROXY*` iptables chains.
