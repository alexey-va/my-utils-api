# WireGuard Relay Control Plane Design

## Goal

Add an administrator-only WireGuard control plane to my-utils. A dedicated
relay on the utils host accepts client WireGuard tunnels and forwards their
IPv4 traffic through a second WireGuard tunnel to an external exit server.
Administrators can provision, disable, and inspect client peers from the UI,
including repeatable `.conf` downloads and QR codes.

## Scope

The first version supports:

- one or more independently enrolled relay records, with one relay expected in
  the initial deployment;
- Debian or Ubuntu relay and exit hosts using systemd, `wireguard-tools`,
  `iptables-nft`, `curl`, and `jq`;
- IPv4 client traffic only;
- one dedicated ingress interface (`wg-users`) and one dedicated egress
  interface (`wg-exit`) on the utils host;
- administrator-only relay and peer management;
- repeatable client credential delivery as WireGuard config text, `.conf`
  download, and an in-browser QR code;
- relay heartbeat, latest handshake, current kernel counters, and accumulated
  upload/download totals;
- explicit enabled/disabled peer state and agent convergence status.

The first version does not provide IPv6 forwarding, traffic quotas, per-domain
policy, multi-hop selection, billing, or automated SSH access to either host.

## Network Topology

```text
client
  | WireGuard, client address 10.89.0.2/32+
  v
utils host: wg-users (10.89.0.1/24, UDP 51820)
  | source-policy route for 10.89.0.0/24 only
  v
utils host: wg-exit (10.90.255.1/30)
  | WireGuard to the configured external endpoint
  v
exit host: wg-exit (10.90.255.2/30)
  | IPv4 forwarding + MASQUERADE for 10.89.0.0/24
  v
internet
```

The relay host itself retains its ordinary default route. Only forwarded
packets whose source is the configured client CIDR use the policy-routing table
for `wg-exit`. The external exit peer routes the complete client CIDR back over
the tunnel.

## Trust Boundaries

`my-utils-api` remains unprivileged and never receives host network
capabilities, WireGuard server private keys, or SSH credentials. A small root
agent installed on the utils host owns WireGuard changes. It calls a narrow
machine API with an opaque, high-entropy relay token.

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

- an exit preparation/install flow that installs packages, generates the exit
  key pair, enables forwarding/NAT, and accepts the relay egress public key;
- a relay install flow that installs packages, generates ingress and egress
  server keys, configures `wg-users` and `wg-exit`, policy routing, forwarding,
  and the systemd control-plane agent;
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
- UI actions display API errors and retain the current table state for retry.

## Verification

Backend tests cover X25519/key encoding, CIDR allocation, encrypted private-key
persistence, encryption-key failure behavior, repeat credential retrieval,
admin authorization, agent-token authorization, desired-state responses,
counter reset accumulation, and lifecycle operations. The complete backend
gate is `./gradlew test` plus `git diff --check`.

Frontend tests cover endpoint construction, one-time provisioning rendering,
download content, QR payload, status presentation, and peer actions. The
complete frontend gate is `npm exec eslint -- src`, `npm test`, `npm run build`,
and `git diff --check`, followed by desktop and narrow browser smoke tests.

Shell scripts are checked with `bash -n`; where ShellCheck is available they
are checked with `shellcheck` as well. Installation is not considered live
verified until it is run on the explicitly selected relay and exit hosts and a
client egress-IP smoke test succeeds.
