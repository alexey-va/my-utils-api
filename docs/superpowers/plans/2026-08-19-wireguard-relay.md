# WireGuard Relay Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build an administrator-only WireGuard relay control plane, host agent, exit-server installer, peer traffic view, and repeatable `.conf`/QR credential delivery.

**Architecture:** The Spring API stores relay and peer desired state, encrypts client private keys, and authenticates a narrow host-agent API. A root systemd agent on the utils host converges a dedicated `wg-users` interface and reports WireGuard counters; a separate `wg-exit` interface policy-routes client traffic to an external NAT exit. The React admin page manages relays and peers while rendering credentials locally as downloads and QR codes.

**Tech Stack:** Kotlin 2.1, Spring Boot 3.4, PostgreSQL/Flyway, Bouncy Castle X25519, AES-256-GCM, React 19, TypeScript, Ant Design, `qrcode.react`, Bash, systemd, WireGuard, iptables-nft.

**Spec:** `docs/superpowers/specs/2026-08-19-wireguard-relay-design.md`

## Global Constraints

- API containers remain unprivileged and never receive WireGuard server private keys or SSH credentials.
- Client private keys are persisted only as AES-256-GCM ciphertext and nonce using `WIREGUARD_CREDENTIALS_ENCRYPTION_KEY`.
- Relay enrollment tokens are persisted only as SHA-256 digests.
- Administrator routes require server-enforced `ROLE_ADMIN`; agent routes require `ROLE_WIREGUARD_AGENT` from the dedicated token filter.
- The first version forwards IPv4 only and never advertises `::/0` to clients.
- Host installation scripts target Debian or Ubuntu with systemd and must not overwrite existing WireGuard configuration without `--replace`.
- Backend, frontend, and host scripts are verified independently before either repository is pushed.

---

### Task 1: Persist relays and encrypted client peers

**Files:**
- Create: `src/main/resources/db/migration/V24__wireguard_control_plane.sql`
- Create: `src/main/kotlin/dev/myutils/api/domain/WireGuardRelay.kt`
- Create: `src/main/kotlin/dev/myutils/api/domain/WireGuardPeer.kt`
- Create: `src/main/kotlin/dev/myutils/api/domain/WireGuardRelayRepository.kt`
- Create: `src/main/kotlin/dev/myutils/api/domain/WireGuardPeerRepository.kt`
- Create: `src/test/kotlin/dev/myutils/api/wireguard/Ipv4CidrTest.kt`
- Create: `src/main/kotlin/dev/myutils/api/wireguard/Ipv4Cidr.kt`

**Interfaces:**
- Produces: `Ipv4Cidr.parse(value: String): Ipv4Cidr`, `hostAddress(offset: Int): String`, `contains(address: String): Boolean`, and JPA repositories used by the control-plane service.
- Produces: relay fields for endpoint, CIDR, DNS, interface, token digest, public key, heartbeat, revision, and timestamps.
- Produces: peer fields for public key, encrypted private key, nonce, address, enabled state, observed counters, accumulated counters, handshake, and timestamps.

- [ ] **Step 1: Write CIDR allocation tests**

```kotlin
@Test
fun `allocates relay and client hosts from a private cidr`() {
    val cidr = Ipv4Cidr.parse("10.89.0.0/24")
    assertEquals("10.89.0.1", cidr.hostAddress(1))
    assertEquals("10.89.0.2", cidr.hostAddress(2))
}

@Test
fun `rejects public and oversized client networks`() {
    assertFails { Ipv4Cidr.parse("8.8.8.0/24") }
    assertFails { Ipv4Cidr.parse("10.0.0.0/8") }
}
```

- [ ] **Step 2: Run the focused test and observe failure**

Run: `./gradlew test --tests '*Ipv4CidrTest'`

Expected: compilation failure because `Ipv4Cidr` does not exist.

- [ ] **Step 3: Add schema, entities, repositories, and CIDR implementation**

Use private IPv4 prefixes `/16` through `/29`, normalize the network address,
reserve host offset `1` for the relay, and allocate peers from offset `2` to the
last non-broadcast host. Add unique constraints for `(relay_id, name)`,
`(relay_id, public_key)`, and `(relay_id, assigned_ip)`.

- [ ] **Step 4: Run the focused test**

Run: `./gradlew test --tests '*Ipv4CidrTest'`

Expected: PASS.

- [ ] **Step 5: Commit the persistence slice**

```bash
git add src/main/resources/db/migration/V24__wireguard_control_plane.sql src/main/kotlin/dev/myutils/api/domain/WireGuardRelay.kt src/main/kotlin/dev/myutils/api/domain/WireGuardPeer.kt src/main/kotlin/dev/myutils/api/domain/WireGuardRelayRepository.kt src/main/kotlin/dev/myutils/api/domain/WireGuardPeerRepository.kt src/main/kotlin/dev/myutils/api/wireguard/Ipv4Cidr.kt src/test/kotlin/dev/myutils/api/wireguard/Ipv4CidrTest.kt
git commit -m "feat: persist WireGuard relays and peers"
```

### Task 2: Generate and encrypt recoverable WireGuard credentials

**Files:**
- Modify: `build.gradle.kts`
- Modify: `src/main/kotlin/dev/myutils/api/infra/config/MyUtilsProperties.kt`
- Modify: `src/main/resources/application.yml`
- Modify: `.env.example`
- Create: `src/main/kotlin/dev/myutils/api/wireguard/WireGuardKeyPairGenerator.kt`
- Create: `src/main/kotlin/dev/myutils/api/wireguard/WireGuardCredentialsCipher.kt`
- Create: `src/main/kotlin/dev/myutils/api/wireguard/WireGuardClientConfig.kt`
- Create: `src/test/kotlin/dev/myutils/api/wireguard/WireGuardCredentialsTest.kt`

**Interfaces:**
- Produces: `WireGuardKeyPair(privateKey: String, publicKey: String)` from X25519.
- Produces: `EncryptedSecret(ciphertext: String, nonce: String)` and `encrypt/decrypt` methods using a 32-byte base64 key.
- Produces: `WireGuardClientConfig.render(privateKey, address, dns, serverPublicKey, endpoint): String` with IPv4 `AllowedIPs = 0.0.0.0/0` and `PersistentKeepalive = 25`.

- [ ] **Step 1: Write key round-trip and config tests**

```kotlin
@Test
fun `encrypts and decrypts a generated wireguard private key`() {
    val pair = WireGuardKeyPairGenerator().generate()
    val cipher = WireGuardCredentialsCipher(testKey)
    val encrypted = cipher.encrypt(pair.privateKey)
    assertNotEquals(pair.privateKey, encrypted.ciphertext)
    assertEquals(pair.privateKey, cipher.decrypt(encrypted))
}

@Test
fun `client config is ipv4 and contains no ipv6 default route`() {
    val config = WireGuardClientConfig.render("private", "10.89.0.2", "1.1.1.1", "server", "vpn.example:51820")
    assertTrue(config.contains("AllowedIPs = 0.0.0.0/0"))
    assertFalse(config.contains("::/0"))
}
```

- [ ] **Step 2: Run the focused test and observe failure**

Run: `./gradlew test --tests '*WireGuardCredentialsTest'`

Expected: compilation failure for the missing credential classes.

- [ ] **Step 3: Implement X25519 generation, AES-GCM, config rendering, and settings**

Add `org.bouncycastle:bcprov-jdk18on`, decode the environment key with strict
base64 validation, use a fresh 12-byte nonce for each encryption, and bind the
secret from `WIREGUARD_CREDENTIALS_ENCRYPTION_KEY`. Do not log plaintext,
ciphertext, nonce, enrollment tokens, or rendered configs.

- [ ] **Step 4: Run the focused test**

Run: `./gradlew test --tests '*WireGuardCredentialsTest'`

Expected: PASS.

- [ ] **Step 5: Commit the credential slice**

```bash
git add build.gradle.kts .env.example src/main/resources/application.yml src/main/kotlin/dev/myutils/api/infra/config/MyUtilsProperties.kt src/main/kotlin/dev/myutils/api/wireguard src/test/kotlin/dev/myutils/api/wireguard/WireGuardCredentialsTest.kt
git commit -m "feat: encrypt recoverable WireGuard credentials"
```

### Task 3: Add admin lifecycle and authenticated agent convergence APIs

**Files:**
- Create: `src/main/kotlin/dev/myutils/api/wireguard/WireGuardControlPlaneService.kt`
- Create: `src/main/kotlin/dev/myutils/api/infra/security/WireGuardAgentAuthFilter.kt`
- Modify: `src/main/kotlin/dev/myutils/api/infra/security/SecurityConfig.kt`
- Create: `src/main/kotlin/dev/myutils/api/web/dto/WireGuardDtos.kt`
- Create: `src/main/kotlin/dev/myutils/api/web/AdminWireGuardController.kt`
- Create: `src/main/kotlin/dev/myutils/api/web/InternalWireGuardController.kt`
- Create: `src/test/kotlin/dev/myutils/api/web/AdminWireGuardControllerIntegrationTest.kt`
- Create: `src/test/kotlin/dev/myutils/api/web/InternalWireGuardControllerIntegrationTest.kt`

**Interfaces:**
- Produces: administrator relay and peer CRUD under `/api/admin/wireguard/relays`.
- Produces: `GET .../peers/{peerId}/credentials` with `Cache-Control: no-store`.
- Produces: agent desired-state GET and heartbeat POST under `/api/internal/wireguard/relays/{relayId}`.
- Consumes: persistence, CIDR allocation, credential generation/encryption, and client config rendering from Tasks 1 and 2.

- [ ] **Step 1: Write integration tests for authorization and lifecycle**

Cover unauthenticated and regular-user rejection, admin relay creation with a
one-time token, peer creation blocked before the relay public key is known,
authenticated heartbeat, peer creation, repeat credential equality,
enable/disable desired state, and deletion.

- [ ] **Step 2: Write counter accumulation tests**

```kotlin
@Test
fun `heartbeat accumulates deltas and survives a kernel counter reset`() {
    heartbeat(rx = 100, tx = 200)
    heartbeat(rx = 150, tx = 260)
    heartbeat(rx = 10, tx = 20)
    assertPeerTotals(rx = 160, tx = 280)
}
```

- [ ] **Step 3: Run the controller tests and observe failure**

Run: `./gradlew test --tests '*WireGuardControllerIntegrationTest'`

Expected: compilation failure for missing controllers and service.

- [ ] **Step 4: Implement the service, DTOs, controllers, and token filter**

Use `SecureRandom` for 32-byte enrollment tokens, SHA-256 for stored digests,
`MessageDigest.isEqual` for comparisons, a custom
`ROLE_WIREGUARD_AGENT` principal, transactions for allocation and heartbeat,
and an opaque revision derived from enabled peer ids/public keys/addresses.

- [ ] **Step 5: Run focused and full backend tests**

Run: `./gradlew test --tests '*WireGuardControllerIntegrationTest'`

Expected: PASS.

Run: `./gradlew test`

Expected: PASS.

- [ ] **Step 6: Commit the API slice**

```bash
git add src/main/kotlin/dev/myutils/api/wireguard/WireGuardControlPlaneService.kt src/main/kotlin/dev/myutils/api/infra/security/WireGuardAgentAuthFilter.kt src/main/kotlin/dev/myutils/api/infra/security/SecurityConfig.kt src/main/kotlin/dev/myutils/api/web/dto/WireGuardDtos.kt src/main/kotlin/dev/myutils/api/web/AdminWireGuardController.kt src/main/kotlin/dev/myutils/api/web/InternalWireGuardController.kt src/test/kotlin/dev/myutils/api/web/*WireGuardControllerIntegrationTest.kt
git commit -m "feat: add WireGuard control plane APIs"
```

### Task 4: Package the relay agent and external exit installation

**Files:**
- Create: `ops/wireguard/README.md`
- Create: `ops/wireguard/wireguard-agent.sh`
- Create: `ops/wireguard/install-relay.sh`
- Create: `ops/wireguard/prepare-exit.sh`
- Create: `ops/wireguard/activate-exit.sh`
- Create: `ops/wireguard/systemd/my-utils-wireguard-agent.service`
- Create: `ops/wireguard/systemd/my-utils-wireguard-agent.timer`
- Create: `ops/wireguard/examples/relay-agent.env.example`

**Interfaces:**
- Consumes: desired-state and heartbeat endpoints from Task 3.
- Produces: `wg-users`, `wg-exit`, source-policy routing for the client CIDR, exit NAT, and a one-minute agent timer.

- [ ] **Step 1: Implement strict argument and platform validation**

Scripts require root, Debian/Ubuntu, systemd, validated interface names and
ports, private client CIDRs, and explicit inputs. Existing config causes a
failure unless `--replace` is present; replacement makes timestamped backups.

- [ ] **Step 2: Implement agent sync and heartbeat**

Use `mktemp`, `umask 077`, and `trap` cleanup. Validate API JSON through `jq`,
preserve the live interface section, replace only the complete peer set on the
dedicated ingress interface through `wg syncconf`, and post numeric counters.

- [ ] **Step 3: Implement two-phase exit and relay setup**

`prepare-exit.sh` installs WireGuard and generates the exit keypair without
printing the private key. `install-relay.sh` accepts the exit public key and
endpoint, creates ingress/egress keys and policy routing, and prints the relay
egress public key. `activate-exit.sh` accepts that public key, adds return routes
for the client CIDR, enables forwarding and NAT, and starts `wg-exit`.

- [ ] **Step 4: Validate scripts**

Run: `bash -n ops/wireguard/*.sh`

Expected: no output and exit code 0.

Run when available: `shellcheck ops/wireguard/*.sh`

Expected: no warnings or errors.

- [ ] **Step 5: Commit the operations slice**

```bash
git add ops/wireguard
git commit -m "feat: package WireGuard relay and exit installers"
```

### Task 5: Add the administrator WireGuard page and repeatable QR credentials

**Files:**
- Modify: `../my-utils/package.json`
- Modify: `../my-utils/package-lock.json`
- Modify: `../my-utils/src/api/endpoints.ts`
- Modify: `../my-utils/src/config/featureCatalog.tsx`
- Modify: `../my-utils/src/config/features.tsx`
- Create: `../my-utils/src/features/wireguard/types.ts`
- Create: `../my-utils/src/features/wireguard/api.ts`
- Create: `../my-utils/src/features/wireguard/WireGuardCredentialsModal.tsx`
- Create: `../my-utils/src/features/wireguard/WireGuardPage.tsx`
- Create: `../my-utils/src/features/wireguard/wireguard.css`
- Create: `../my-utils/src/features/wireguard/WireGuardCredentialsModal.test.tsx`

**Interfaces:**
- Consumes: administrator relay/peer APIs from Task 3.
- Produces: feature id `wireguard`, path `/wireguard`, relay setup/status, peer table/actions, and credential modal.

- [ ] **Step 1: Add `qrcode.react` and define endpoint/type wrappers**

Run from `../my-utils`: `npm install qrcode.react`

Add exact endpoint builders for relay, peer, credential, token rotation, and
delete operations. API wrappers use the existing authenticated `apiClient`.

- [ ] **Step 2: Write the credential modal test**

```tsx
it("renders repeatable config, QR payload, and download", () => {
  render(<WireGuardCredentialsModal open credentials={fixture} onClose={() => {}} />);
  expect(screen.getByText("alex-phone.conf")).toBeInTheDocument();
  expect(screen.getByTestId("wireguard-qr")).toHaveAttribute("data-config", fixture.clientConfig);
  expect(screen.getByRole("button", { name: /download/i })).toBeEnabled();
});
```

- [ ] **Step 3: Run the focused test and observe failure**

Run from `../my-utils`: `npm test -- WireGuardCredentialsModal.test.tsx`

Expected: failure because the modal does not exist.

- [ ] **Step 4: Implement page, modal, feature registration, and styling**

Use `PageLayout`, `AppPanel`, Ant Design components, and existing Linear design
tokens. Fetch credentials only when requested, render `QRCodeSVG` in-browser,
download via a Blob, clear credential state on close, and show stale relay,
pending convergence, recent handshake, and human-readable byte totals.

- [ ] **Step 5: Run frontend verification**

Run from `../my-utils`: `npm exec eslint -- src`

Expected: PASS.

Run from `../my-utils`: `npm test`

Expected: PASS.

Run from `../my-utils`: `env UPDATE_NOTIFIER_IS_DISABLED=true npm run build`

Expected: PASS.

- [ ] **Step 6: Commit the frontend slice**

```bash
git add package.json package-lock.json src/api/endpoints.ts src/config/featureCatalog.tsx src/config/features.tsx src/features/wireguard
git commit -m "feat: add WireGuard admin console"
```

### Task 6: Document, verify, release, and read back

**Files:**
- Modify: `README.md`
- Modify: `docs/ARCHITECTURE.md`
- Modify: `../my-utils/README.MD`
- Modify: `../README.md`

**Interfaces:**
- Produces: operator setup order, recovery behavior, security boundaries, REST contract, UI route, and explicit statement that host installation is separate from app deployment.

- [ ] **Step 1: Document setup and recovery**

Document relay creation, token handling, encryption-key configuration, exit
preparation, relay installation, exit activation, admin UI behavior, token
rotation, lost encryption-key consequences, and safe validation commands.

- [ ] **Step 2: Run final repository gates**

Backend: `./gradlew test && git diff --check && bash -n ops/wireguard/*.sh`

Frontend: `npm exec eslint -- src && npm test && env UPDATE_NOTIFIER_IS_DISABLED=true npm run build && git diff --check`

Expected: all commands exit 0.

- [ ] **Step 3: Perform browser QA with synthetic data**

Verify desktop and narrow layouts, admin authorization, relay setup, peer
creation, repeat credential reopening, QR rendering, download filename/content,
disable/delete confirmation, and stale agent status. Do not expose real client
configs or tokens in screenshots or reports.

- [ ] **Step 4: Commit documentation separately per repository**

Backend commit: `docs: document WireGuard relay operations`

Frontend commit: `docs: document WireGuard admin console`

- [ ] **Step 5: Push each verified repository and wait for terminal CI**

Push backend `main`, confirm local and remote heads match, wait for Woodpecker
verify and deploy success, then check `/api/health` and unauthorized/admin route
behavior. Push frontend `main`, confirm local and remote heads match, wait for
Woodpecker success, then verify `/wireguard` loads only for the administrator.

- [ ] **Step 6: Report the remaining infrastructure boundary**

Report application deployment separately from host tunnel activation. Do not
claim the relay or external exit works until the prepared installers are run on
explicitly selected hosts and a real client proves the expected external egress
IP.
