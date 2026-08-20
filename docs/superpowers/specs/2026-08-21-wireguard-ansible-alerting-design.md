# WireGuard Ansible and Alerting Design

## Goal

Make the existing dual-exit VPN reproducible on fresh Ubuntu hosts and page the
operator when the relay, routing, DNS-derived readiness, agent heartbeat, or AWG
exits are unhealthy. The work must reuse the existing guarded host installers,
preserve fail-closed routing, and never place plaintext VPN keys in Git or on the
Ansible controller filesystem.

## Scope

This change covers:

- a built-in-only Ansible orchestration for one `vpn_relay` host and exactly two
  `vpn_exits` hosts;
- an encrypted-variable workflow for the relay agent token, the stable
  `wg-users` server private key, and one AWG client private key per exit;
- plan-only execution by default and an explicit `vpn_apply=true` gate for host
  mutation;
- Prometheus metrics projected from the persisted WireGuard relay state;
- provisioned Grafana rules delivered to the existing `Metal Discord` contact
  point with VPN-specific notification copy and anti-flap windows;
- documentation, static validation, focused tests, and production read-back of
  the metrics and alert-rule provisioning.

An external client canary, IPv6 routing, a second ingress relay, and automatic
destructive failover drills remain separate work. These alerts deliberately
observe the existing relay control plane and data-plane probes; they do not
claim to cover a user's last-mile network.

## Ansible architecture

`ops/wireguard/ansible/site.yml` is a sequence of guarded plays:

1. Validate inventory shape and required public and vaulted variables locally.
2. Stage the versioned WireGuard operations bundle on the relay and prepare
   official AmneziaWG tooling.
3. Materialize stable AWG client private keys only on the relay, derive public
   keys there, and keep both files mode `0600`.
4. Bootstrap each exit, generate its AWG server config from the matching relay
   public key, and install the isolated AWG plus tinyproxy stack.
5. Transfer only the generated `client.params` value through Ansible's in-memory
   registered variables with `no_log: true`; no controller-side temporary file
   is used.
6. Install both relay-side AWG clients without selecting either route, install
   HA failover, install the stable `wg-users` ingress, then install RU routing
   and private client DNS.
7. Verify both exits, the selected fail-closed route, the standard WireGuard
   ingress, the relay agent, geo-routing status, and DNS service.

Every mutating play is skipped unless `vpn_apply | bool` is true. The default
execution validates inventory and prints the intended topology only. Existing
managed runtime state is never replaced unless `vpn_replace | bool` is also
true. Unmanaged state continues to be rejected by the underlying installers.

The relay's standard WireGuard private key is supplied from Ansible Vault to
`install-relay.sh` through a root-only temporary file. This keeps existing
client profiles valid after a relay rebuild. AWG server keys may rotate when an
individual exit is rebuilt because the playbook installs the resulting client
parameters on the relay before routing is selected.

## Inventory and secrets

The public inventory contains host addresses, interface names, overlay numbers,
expected public egress addresses, the relay id, and endpoint ports. The vaulted
file contains only:

- `vault_wireguard_agent_token`;
- `vault_wireguard_server_private_key`;
- `vault_awg_client_private_keys`, keyed by exit inventory hostname.

Tasks that handle these values use `no_log: true`. Example files contain
placeholders only. The README documents generating keys with `umask 077` and
encrypting the secrets immediately with `ansible-vault`; plaintext key files are
not an accepted inventory format.

## Prometheus projection

The API metrics registry owns a custom collector backed by a narrow
`ListRelays(context.Context)` interface. Each scrape reads the persisted relay
view with a short timeout and exports bounded-cardinality gauges:

- `myutils_wireguard_collection_success`;
- `myutils_wireguard_relay_ready{relay_id,relay}`;
- `myutils_wireguard_routing_healthy{relay_id,relay}`;
- `myutils_wireguard_agent_last_seen_timestamp_seconds{relay_id,relay}`;
- `myutils_wireguard_exit_healthy{relay_id,relay,exit}`;
- `myutils_wireguard_exit_selected{relay_id,relay,exit}`;
- `myutils_wireguard_exit_latency_seconds{relay_id,relay,exit}`;
- `myutils_wireguard_route_packet_loss_percent{relay_id,relay,path}`;
- `myutils_wireguard_route_rtt_seconds{relay_id,relay,path}`.

Missing optional latency or quality values omit only that sample. A database
read failure sets collection success to zero and omits stale relay samples; it
must not make the entire `/actuator/prometheus` handler panic.

## Alert rules

Grafana evaluates the following rules every 30 seconds:

- metrics collection failure for two minutes;
- relay not `READY` for one minute;
- stale agent heartbeat for one minute after exceeding 45 seconds;
- routing unhealthy for one minute;
- no healthy AWG exit for 30 seconds;
- primary exit unhealthy for two minutes;
- secondary selected for two minutes;
- packet loss above 20 percent for five minutes.

All rules use the existing Discord receiver, group by alert and relay, and have
repeat intervals of at least four hours. VPN alerts render VPN-specific titles,
details, recovery text, and a direct link to the VPN administration page. The
existing hardware alert wording remains unchanged for non-VPN rules.

## Verification and release

- Go tests prove the collector's label/value contract, missing optional values,
  and collection failure behavior.
- Ops tests prove stable server-key injection, generic primary/secondary AWG
  client installation, the Ansible apply gate, `no_log` coverage, inventory
  validation, and the expected alert expressions.
- YAML is parsed as data; `ansible-playbook --syntax-check` is run when Ansible
  is available, otherwise the missing tool is reported explicitly.
- The full Go test, vet, static build, shell syntax, Python validation, and
  `git diff --check` gates pass before push.
- After Woodpecker succeeds, observability is synced through the existing
  versioned script. Production read-back checks the new Prometheus series and
  Grafana provisioning API without firing a synthetic alert.
