# WireGuard Relay Control Plane Design

## Goal

Add an administrator-only WireGuard control plane to my-utils. A dedicated
relay on the utils host accepts standard WireGuard client tunnels and forwards
their IPv4 traffic through an AmneziaWG egress tunnel to the existing `veesp`
server.
Administrators can provision, disable, and inspect client peers from the UI,
including repeatable `.conf` downloads and QR codes.

## Scope

The first version supports:

- one or more independently enrolled relay records, with one relay expected in
  the initial deployment;
- the Ubuntu 24.04 utils relay using systemd, standard `wireguard-tools` for
  ingress, official AmneziaWG tools for egress, `iptables-nft`, `curl`, and
  `jq`;
- the existing `amnezia-awg` container on `veesp` as the only external exit;
- IPv4 client traffic only;
- one dedicated standard WireGuard ingress interface (`wg-users`) and one
  dedicated AmneziaWG egress interface (`awg-exit`) on the utils host;
- administrator-only relay and peer management;
- repeatable client credential delivery as WireGuard config text, `.conf`
  download, and an in-browser QR code;
- relay heartbeat, latest handshake, current kernel counters, and accumulated
  upload/download totals;
- explicit enabled/disabled peer state and agent convergence status.

The first version does not provide IPv6 forwarding, traffic quotas, per-domain
policy, multi-hop selection, billing, or application-managed SSH access to
either host.

## Network Topology

```text
client
  | WireGuard, client address 10.89.0.2/32+
  v
utils host: wg-users (10.89.0.1/24, UDP 51820)
  | source-policy route for 10.89.0.0/24 only; no relay-side NAT
  v
utils host: awg-exit (dedicated 10.8.1.x/32 peer, Table=off)
  | AmneziaWG to veesp:42696
  v
veesp: existing amnezia-awg wg0 (10.8.1.0/24)
  | peer route for 10.89.0.0/24 + IPv4 forwarding + MASQUERADE
  v
internet
```

The relay host itself retains its ordinary default route. Only forwarded
packets whose source is the configured client CIDR use the policy-routing table
for `awg-exit`. The utils relay does not masquerade client addresses: each
`10.89.0.x/32` remains visible through the egress tunnel. The dedicated
AmneziaWG peer on `veesp` owns both its egress tunnel address and the complete
`10.89.0.0/24` client CIDR, which provides the return route.

The policy table contains an unreachable fallback and the relay firewall
rejects client-CIDR forwarding through any interface other than `awg-exit`.
Consequently, an egress failure cannot leak client traffic through the normal
utils default route. Client and ingress MTU are fixed at 1280 to tolerate the
nested WireGuard plus AmneziaWG encapsulation.

## Trust Boundaries

`my-utils-api` remains unprivileged and never receives host network
capabilities, WireGuard server private keys, or SSH credentials. A small root
agent installed on the utils host owns WireGuard changes. It calls a narrow
machine API with an opaque, high-entropy relay token.

The AmneziaWG client private key, preshared key, and obfuscation parameters are
root-only host configuration. They never enter the API, database, repository,
agent heartbeat, or UI. `veesp` configuration is backed up before a dedicated
relay peer or persistent NAT rules are changed.

The relay token is generated once by the API. Only its SHA-256 digest is stored;
the plaintext token is returned once to the administrator for agent setup. The
internal agent API is protected independently from administrator JWTs.

Client key pairs are generated in API memory using X25519. The client private
key is encrypted with AES-256-GCM before persistence; the random nonce and
ciphertext are stored, while the encryption key is supplied separately through
the `WIREGUARD_CREDENTIALS_ENCRYPTION_KEY` production secret. The key is never
stored in the database, repository, application logs, or API responses other
than the administrator credential response. An administrator can therefore
retrieve the same `.conf` and QR code later without keeping plaintext key
material in the database. A database-only leak does not reveal client private
keys.

If the encryption key is missing, peer creation and credential retrieval fail
closed while relay status and existing tunnel operation remain available. If
the key is permanently lost, existing peers keep working in WireGuard but their
configs cannot be recovered and must be rotated.

## Backend Model

### Relay

A relay stores:

- id and administrator-visible name;
- public client endpoint such as `vpn.example.net:51820`;
- private IPv4 client CIDR such as `10.89.0.0/24`;
- client DNS address;
- expected ingress interface name;
- hashed agent token;
- ingress server public key reported by the agent;
- last agent heartbeat and last successfully applied desired revision;
- created and updated timestamps.

Creation returns an enrollment token once. A relay is not ready to provision
client peers until an authenticated heartbeat has supplied the ingress server
public key.

### Peer

A peer stores:

- id, relay id, display name, public key, and allocated `/32` address;
- AES-GCM ciphertext and nonce for the client private key;
- enabled state and desired-state revision;
- latest handshake time;
- the latest raw WireGuard receive/transmit counters;
- accumulated receive/transmit totals that survive interface counter resets;
- last metrics update and created/updated timestamps.

The first host address is reserved for `wg-users`; allocation starts at the
second usable address. Disabled peer addresses remain reserved until the peer
is deleted. Deletion is explicit and permanent.

## HTTP Contract

Administrator JWT and `ROLE_ADMIN` are required for:

- `GET /api/admin/wireguard/relays`
- `POST /api/admin/wireguard/relays`
- `DELETE /api/admin/wireguard/relays/{relayId}` when no peers remain
- `POST /api/admin/wireguard/relays/{relayId}/rotate-token`
- `GET /api/admin/wireguard/relays/{relayId}/peers`
- `POST /api/admin/wireguard/relays/{relayId}/peers`
- `GET /api/admin/wireguard/relays/{relayId}/peers/{peerId}/credentials`
- `PATCH /api/admin/wireguard/relays/{relayId}/peers/{peerId}`
- `DELETE /api/admin/wireguard/relays/{relayId}/peers/{peerId}`

Peer creation returns the persisted peer projection plus `clientConfig` and
`fileName`. The credentials endpoint regenerates the same response from the
encrypted private key and current relay connection parameters. Secret-bearing
responses use `Cache-Control: no-store`.

The relay agent uses `X-WireGuard-Agent-Token` and its relay id for:

- `GET /api/internal/wireguard/relays/{relayId}/desired`
- `POST /api/internal/wireguard/relays/{relayId}/heartbeat`

The desired response contains only enabled peer public keys and their `/32`
allowed IPs, an opaque revision, and the expected ingress interface name. The
heartbeat contains the ingress public key, public endpoint observed from local
configuration, applied revision, and `wg show ... dump` peer counters. Invalid,
missing, or rotated tokens receive `401`.

## Agent Convergence and Metrics

The systemd timer runs once per minute. The agent:

1. fetches desired state;
2. validates the relay id, interface, revision, public keys, and allowed IPs;
3. builds a root-only temporary `wg syncconf` input from the live interface
   section plus the desired peer set;
4. applies the complete dedicated-interface peer set atomically;
5. reads `wg show wg-users dump`;
6. posts heartbeat, applied revision, and counters;
7. removes the temporary file through a shell trap.

For each peer, the backend adds the positive delta from the previous raw
counter. If a raw counter decreased because the interface restarted, the new
raw value is treated as the delta. Unknown public keys in a heartbeat are
ignored and never create database peers.

The UI distinguishes desired state from observed state. A relay heartbeat older
than two agent intervals is stale; a peer is shown as recently active only when
its latest handshake is recent.

## Administrator UI

A new `WireGuard` feature is registered in the feature catalog with
`requiresAdmin: true`. The page contains:

- relay setup/status, public endpoint, client CIDR, public key, last heartbeat,
  and token rotation;
- a peer table with name, address, enabled state, latest handshake, upload,
  download, total traffic, and metrics age;
- create, enable/disable, and delete actions;
- a credential modal available from every peer row with masked/revealable
  config text, `.conf` download, copy action, and a locally rendered QR code.

The QR code is rendered inside the browser from the returned config. The config
is not sent to a third-party QR service or persisted in browser storage. The
modal clears its in-memory credential state when closed.

## Host Installation

Versioned scripts under `ops/wireguard/` provide:

- a `veesp` preparation flow that verifies the exact `amnezia-awg` container,
  backs up its configuration, creates one dedicated relay peer without printing
  keys, adds the `10.89.0.0/24` return route and persistent forwarding/NAT rules,
  and writes a root-only client configuration artifact;
- a relay install flow that installs standard WireGuard ingress and official
  AmneziaWG egress tooling, consumes the root-only client artifact, configures
  `wg-users`, `awg-exit`, fail-closed source-policy routing, forwarding, and the
  systemd control-plane agent;
- validation commands that check interface state, routes, the agent timer, and
  public internet egress without printing private keys or tokens.

Scripts reject missing arguments, non-root execution, unsupported operating
systems, unsafe interface names, public client CIDRs, and overwriting existing
WireGuard configuration unless an explicit `--replace` option is supplied.
Backups are timestamped before an approved replacement.

Opening host firewall ports, changing host forwarding, and installing the
scripts are infrastructure mutations. They are prepared in the repository but
are not run by the ordinary application deployment pipeline.

## Failure Behavior

- Peer creation fails with `409` while the relay has not reported a server
  public key.
- CIDR exhaustion returns `409` and creates no peer.
- Duplicate names and public keys are rejected.
- Agent authentication fails closed when a token is missing, malformed, or
  rotated.
- Invalid heartbeat rows do not partially update counters.
- A failed agent sync leaves the previous kernel configuration active and does
  not report the new revision as applied.
- A missing or failed `awg-exit` rejects client-CIDR traffic on utils instead of
  falling back to the host's ordinary default route.
- A failed `veesp` peer or NAT update restores the timestamped configuration
  backup before the existing Amnezia service is considered healthy.
- UI actions display API errors and retain the current table state for retry.

## Verification

Backend tests cover X25519/key encoding, CIDR allocation, encrypted private-key
persistence, encryption-key failure behavior, repeat credential retrieval,
admin authorization, agent-token authorization, desired-state responses,
counter reset accumulation, and lifecycle operations. The complete backend
gate is `./gradlew test` plus `git diff --check`.

Frontend tests cover endpoint construction, repeatable credential rendering,
download content, QR payload, status presentation, and peer actions. The
complete frontend gate is `npm exec eslint -- src`, `npm test`, `npm run build`,
and `git diff --check`, followed by desktop and narrow browser smoke tests.

Shell scripts are checked with `bash -n`; where ShellCheck is available they
are checked with `shellcheck` as well. Installation is not considered live
verified until it is run on the exact `utils` and `veesp` hosts, the original
Amnezia peers remain usable, and a disposable client proves that its public
egress address is `veesp` while ordinary utils traffic retains its original
route.
