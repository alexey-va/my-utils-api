# WireGuard Ansible and Alerting Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild the dual-exit WireGuard/AWG topology from encrypted Ansible variables and alert on persisted VPN data-plane health.

**Architecture:** A built-in-only Ansible playbook orchestrates the existing guarded shell installers and carries secrets only in vaulted variables and `no_log` in-memory transfers. A custom Prometheus collector projects the persisted relay model into bounded gauges consumed by provisioned Grafana rules and the existing Discord contact point.

**Tech Stack:** Ansible Core built-ins, Bash, systemd, WireGuard, AmneziaWG, Docker Compose, Go 1.26, Prometheus client_golang, Grafana unified alert provisioning, Python/PyYAML validation.

**Spec:** `docs/superpowers/specs/2026-08-21-wireguard-ansible-alerting-design.md`

## Global Constraints

- Ansible is plan-only unless `vpn_apply=true` is supplied explicitly.
- Replacement of managed runtime state additionally requires `vpn_replace=true`.
- Plaintext private keys, PSKs, agent tokens, and generated client parameters never enter Git or controller-side files.
- The relay's `wg-users` private key is stable and sourced from an encrypted variable so existing client profiles survive rebuilds.
- VPN alerts use the existing notification receiver and do not install a second alerting stack.
- Existing fail-closed routing, RU-direct behavior, private DNS, and safe AWG fallback remain unchanged.

---

### Task 1: Make the host installers composable for a fresh relay

**Files:**
- Modify: `ops/wireguard/install-relay.sh`
- Modify: `ops/wireguard/veesp-exit/install-utils-client.sh`
- Create: `ops/wireguard/veesp-exit/prepare-utils-host.sh`
- Modify: `ops/wireguard/scripts_test.go`

**Interfaces:**
- `install-relay.sh --server-private-key-file FILE` consumes a mode-0600 WireGuard private key and installs it without printing it.
- `install-utils-client.sh --interface NAME` supports both `awg-exit` and later interfaces while leaving policy table `51889` untouched.
- `prepare-utils-host.sh [--apply]` installs the official AmneziaWG utilities and protected working directories.

- [ ] Write focused ops tests that require the new flags, protected key handling, generic client wording, and absence of policy-route selection.
- [ ] Run `go test ./ops/wireguard -run 'TestRelayInstaller|TestUtilsExitClientInstaller|TestUtilsHostPreparation' -count=1` and confirm failure on the missing behavior.
- [ ] Implement the minimal installer changes with plan-only defaults and existing replacement safeguards.
- [ ] Run the focused tests and `bash -n ops/wireguard/*.sh ops/wireguard/veesp-exit/*.sh`.

### Task 2: Add safe Ansible orchestration

**Files:**
- Create: `ops/wireguard/ansible/ansible.cfg`
- Create: `ops/wireguard/ansible/inventory.example.yml`
- Create: `ops/wireguard/ansible/vault.example.yml`
- Create: `ops/wireguard/ansible/site.yml`
- Create: `ops/wireguard/ansible/validate.py`
- Create: `ops/wireguard/ansible/README.md`
- Modify: `ops/wireguard/scripts_test.go`
- Modify: `ops/wireguard/README.md`

**Interfaces:**
- Inventory groups are exactly one `vpn_relay` and exactly two `vpn_exits`.
- Vault variables are `vault_wireguard_agent_token`, `vault_wireguard_server_private_key`, and `vault_awg_client_private_keys` keyed by exit hostname.
- `python3 validate.py --inventory inventory.yml --vault-vars vault.yml` validates structure without printing values.
- `ansible-playbook site.yml` validates and plans; `-e vpn_apply=true` authorizes mutation.

- [ ] Write an ops test that checks the inventory contract, default apply gate, `no_log` on every secret task, absence of shell interpolation of secrets, and required verification tasks.
- [ ] Run the focused test and confirm it fails because the Ansible bundle is absent.
- [ ] Implement the example inventory, vault placeholders, static validator, and multi-play orchestration.
- [ ] Document key generation, Vault encryption, plan, apply, replacement, and disaster-rebuild flows.
- [ ] Run the validator on examples, parse every YAML file, and run Ansible syntax check when the binary is available.

### Task 3: Export persisted WireGuard health to Prometheus

**Files:**
- Create: `internal/observability/wireguard_metrics.go`
- Create: `internal/observability/wireguard_metrics_test.go`
- Modify: `internal/observability/metrics.go`
- Modify: `cmd/my-utils-api/main.go`

**Interfaces:**
- `type WireGuardRelaySource interface { ListRelays(context.Context) ([]wireguard.Relay, error) }`.
- `func (m *Metrics) RegisterWireGuard(source WireGuardRelaySource)` registers one collector.
- Metric names and labels match the design spec exactly.

- [ ] Write collector tests for a ready relay with two exits, optional latency omission, packet-loss values, and source failure.
- [ ] Run `go test ./internal/observability -run WireGuard -count=1` and confirm failure because registration and series do not exist.
- [ ] Implement the collector with a bounded scrape context and collection-success gauge.
- [ ] Register it after the WireGuard service is constructed in `main.go`.
- [ ] Run focused observability tests and existing metrics tests.

### Task 4: Provision VPN Grafana alert rules and notification copy

**Files:**
- Create: `observability/config/grafana/provisioning/alerting/vpn-alert-rules.yaml`
- Modify: `observability/config/grafana/provisioning/alerting/metal-templates.yaml`
- Modify: `observability/config/grafana-metal-discord-template.txt`
- Modify: `observability/scripts/apply-metal-discord-template.py`
- Create: `observability/scripts/validate-vpn-alerts.py`
- Modify: `ops/wireguard/scripts_test.go`
- Modify: `observability/sync-to-server.sh`

**Interfaces:**
- Alert labels include `team: vpn`, `severity`, `relay_id`, and `relay` where supplied by Prometheus.
- The existing `Metal Discord` receiver selects VPN-specific template branches when `team == "vpn"`.
- `validate-vpn-alerts.py` rejects missing rules, unsafe no-data behavior, absent anti-flap windows, wrong receiver, or expressions using undeclared metrics.

- [ ] Write a failing ops test for the expected alert titles, expressions, receiver, anti-flap durations, and VPN template branches.
- [ ] Run the focused test and confirm failure on absent alert provisioning.
- [ ] Add the eight provisioned rules and extend the shared template without changing hardware messages.
- [ ] Add static alert validation and call it from `sync-to-server.sh --check` preflight.
- [ ] Run template and alert validators plus YAML parsing.

### Task 5: Documentation, full verification, release, and production read-back

**Files:**
- Modify: `README.md`
- Modify: `ops/wireguard/README.md`

**Interfaces:**
- The runbook distinguishes repository availability, Ansible plan, applied host state, deployed API metrics, and provisioned production alerts.

- [ ] Update the top-level and WireGuard runbooks with Ansible and alert metric/rule references.
- [ ] Run `go test ./ops/wireguard ./internal/observability -count=1`.
- [ ] Run every shell through `bash -n` and both observability validators.
- [ ] Run the full PostgreSQL-backed `go test ./...`, `go vet ./...`, static build, and `git diff --check` gates.
- [ ] Commit the single backend/ops repository change and push `main`.
- [ ] Confirm local HEAD equals remote `main`, wait for terminal Woodpecker success, and verify deployed API health and VPN metric names.
- [ ] Run the versioned observability sync, then read back all VPN rule titles and their unpaused state from Grafana without forcing a notification.
