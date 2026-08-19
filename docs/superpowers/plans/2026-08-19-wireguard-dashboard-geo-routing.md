# WireGuard Dashboard and Geo Routing Implementation Plan

> **For agentic workers:** Execute the tasks in order. Preserve unrelated user changes, use tests before implementation, and do not expose relay tokens or private keys.

**Goal:** Deliver a modern live WireGuard admin panel, 30-day peer traffic charts, and fail-closed direct routing for Russian IPv4 destinations.

**Architecture:** The existing backend remains the control plane. Agent heartbeats persist minute traffic deltas and report a non-secret host routing status. The frontend polls current state and requests bounded chart data. A host-side nftables interval set marks Russian destinations before the existing AWG policy rule, with atomic validated daily updates.

**Tech Stack:** Kotlin, Spring Boot, JPA/Flyway, PostgreSQL, React, TypeScript, Vitest, React Testing Library, Recharts, Bash, Python 3, nftables, iproute2, iptables, systemd.

**Spec:** `docs/superpowers/specs/2026-08-19-wireguard-dashboard-geo-routing-design.md`

## Global Constraints

- Keep public REST paths backward-compatible.
- Preserve the single-relay topology and current peer enrollment behavior.
- Interpret relay RX as client upload and relay TX as client download.
- Do not put secrets in logs, state files, fixtures, or documentation.
- A GeoIP download or validation failure must retain the previous set; an empty first install must keep AWG-only routing.
- Use separate commits and deployments for backend and frontend repositories.

## Task 1: Persist and serve metric history

**Files:**

- Create `src/main/resources/db/migration/V25__wireguard_peer_metrics_and_routing.sql`
- Create `src/main/kotlin/dev/myutils/api/domain/WireGuardPeerMetricSample.kt`
- Create `src/main/kotlin/dev/myutils/api/domain/WireGuardPeerMetricSampleRepository.kt`
- Modify `src/main/kotlin/dev/myutils/api/domain/WireGuardRelay.kt`
- Modify `src/main/kotlin/dev/myutils/api/service/WireGuardControlPlaneService.kt`
- Modify `src/main/kotlin/dev/myutils/api/web/WireGuardController.kt`
- Modify `src/main/kotlin/dev/myutils/api/web/dto/WireGuardDtos.kt`
- Modify `src/test/kotlin/dev/myutils/api/web/WireGuardControllerIntegrationTest.kt`

### Steps

1. Add an integration test that posts two heartbeats and asserts chart direction mapping, time-range response, relay routing status, authorization, and `no-store` headers.
2. Run the focused test and record the expected compile or assertion failure.
3. Add the migration, entity, repository, DTOs, controller endpoint, bounded range aggregation, retention cleanup, and heartbeat routing status handling.
4. Run the focused test until green.
5. Run `./gradlew test` against disposable PostgreSQL and Redis services.
6. Run `git diff --check`.

## Task 2: Redesign the WireGuard admin surface

**Files:**

- Modify `src/features/wireguard/types.ts`
- Modify `src/features/wireguard/api.ts`
- Modify `src/features/wireguard/WireGuardPage.tsx`
- Create `src/features/wireguard/WireGuardPeerMetricsDrawer.tsx`
- Modify `src/features/wireguard/wireguard.css`
- Modify `src/features/wireguard/WireGuardPage.test.tsx`

### Steps

1. Add component tests for correct arrow direction and colors, visible-tab polling, the flat operational state, chart drawer opening, and all four ranges.
2. Run the focused Vitest file and record the expected failure.
3. Add metric types and API calls with `cache: 'no-store'`.
4. Replace nested panels with a flat header, status strip, peer list, and collapsed infrastructure section.
5. Add a visibility-aware 15-second refresh loop.
6. Add the responsive Recharts drawer and accessible range controls.
7. Run `npm exec eslint -- src`, `npm test`, `npm run build`, and `git diff --check`.

## Task 3: Add validated RU-direct host routing

**Files:**

- Create `ops/wireguard/render-geo-prefixes.py`
- Create `ops/wireguard/update-geo-routing.sh`
- Create `ops/wireguard/install-geo-routing.sh`
- Create `ops/wireguard/systemd/my-utils-geo-routing.service`
- Create `ops/wireguard/systemd/my-utils-geo-routing-update.service`
- Create `ops/wireguard/systemd/my-utils-geo-routing-update.timer`
- Modify `ops/wireguard/wireguard-agent.sh`
- Modify `ops/wireguard/README.md`
- Create `src/test/kotlin/dev/myutils/api/ops/GeoRoutingScriptsTest.kt`
- Modify `src/test/kotlin/dev/myutils/api/ops/WireGuardAgentScriptTest.kt`

### Steps

1. Add process-level tests that prove CIDR normalization, unsafe-range rejection, count bounds, non-mutating plan mode, and optional heartbeat status serialization.
2. Run the focused tests and record the expected failure.
3. Implement strict Python validation and deterministic nft element rendering.
4. Implement plan/apply installation, idempotent policy rules/filter/NAT rules, atomic updates, last-known-good retention, and systemd units.
5. Teach the agent to read only the non-secret routing status file.
6. Run focused Kotlin tests, `bash -n` for shell files, Python compile/tests, and `shellcheck` when available.

## Task 4: Deploy in dependency order

### Steps

1. Commit and push the backend repository; wait for Woodpecker and read back production health/API behavior.
2. Copy the versioned scripts and units to `utils`, run installer plan mode, inspect exact nftables, ip-rule, filter, and NAT changes, then apply.
3. Verify the live RU prefix count, priority `1088` marked lookup, priority `1089` AWG fallback, state heartbeat, and safe updater failure behavior.
4. Commit and push the frontend repository; wait for Woodpecker and read back the production asset/API state.

## Task 5: Production acceptance

### Steps

1. Verify an existing peer has a current handshake and increasing kernel counters.
2. Verify admin totals update without a full reload and remain stable during refresh.
3. Verify download and upload directions and colors against relay counters.
4. Verify chart drawer ranges return and render production samples.
5. Verify desktop and narrow layouts, loading placeholders, empty/error states, keyboard focus, and reduced-motion behavior.
6. Verify one Russian destination resolves through the main route and one non-Russian destination through `awg-exit` without opening a broad direct fallback.
7. Run final repository status, commit identity, CI, health, and production read-back checks before reporting completion.
