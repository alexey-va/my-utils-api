# WireGuard Dashboard, Metrics History, and RU Direct Routing Design

## Goal

Make the existing single-relay WireGuard installation understandable and operational from the admin UI, preserve traffic history for charts, and route Russian IPv4 destinations directly from `utils` while all other client traffic remains fail-closed through the AWG tunnel to Veesp.

## Confirmed topology

- `utils` terminates ordinary WireGuard clients on `wg-users` (`10.89.0.0/24`).
- Non-Russian traffic is policy-routed through `awg-exit` and Veesp.
- The control-plane backend receives one heartbeat per minute from the privileged host agent.
- The installation has one relay. Relay creation is infrastructure setup, not the primary everyday action.

## Admin experience

The `/wireguard` page is a flat operational surface instead of a stack of nested cards.

- Header: `VPN` and the primary `Добавить устройство` action.
- Status strip: relay health, Russian routing state, the non-Russian egress, and data freshness.
- Peer rows: name, address, online state, last handshake, download and upload totals.
- Download uses a blue downward arrow. Upload uses a green upward arrow.
- Server transmit bytes are client download; server receive bytes are client upload.
- The page refreshes relay and peer state every 15 seconds while the tab is visible.
- Clicking `График` opens a side drawer with `1ч`, `24ч`, `7д`, and `30д` ranges.
- Endpoint, CIDR, relay revision, public key, relay creation, and destructive actions live under the collapsed `Инфраструктура` section.
- Layout uses whitespace and hairline separators. Peer rows and subsections do not add their own filled backgrounds or borders.

## Metric history

Each agent heartbeat contains monotonically accumulated kernel counters. The backend already converts counter resets into accumulated totals. It will additionally persist the positive delta for every peer heartbeat:

- `download_bytes` = relay transmit delta;
- `upload_bytes` = relay receive delta;
- `recorded_at` = heartbeat processing time;
- `latest_handshake_at` = most recent kernel handshake, when available.

Samples are retained for 31 days. The chart API exposes fixed bounded ranges and aggregates samples into stable buckets:

| Range | Window | Bucket |
| --- | --- | --- |
| `HOUR` | 1 hour | 1 minute |
| `DAY` | 24 hours | 15 minutes |
| `WEEK` | 7 days | 1 hour |
| `MONTH` | 30 days | 6 hours |

The backend groups the bounded sample list because this is a small personal control plane with few peers. The API returns zero-free buckets; the frontend draws gaps as zero traffic. Dynamic relay, peer, and metric responses use `Cache-Control: no-store`.

## Geo routing

Only IPv4 is changed in this iteration.

1. A native nftables interval set contains validated Russian destination prefixes.
2. A prerouting mangle chain marks packets arriving from `wg-users` for a destination in that set.
3. An `ip rule` with priority `1088` sends marked packets to the main routing table.
4. The existing source rule at priority `1089` continues to send all unmarked `10.89.0.0/24` traffic to table `51889` and `awg-exit`.
5. Marked packets are accepted from `wg-users` to `eth0`, replies are accepted as established traffic, and marked egress is masqueraded on `eth0`.

This preserves fail-closed behavior: an absent, empty, or failed GeoIP update does not create a direct route and therefore leaves traffic on AWG.

The prefix updater downloads IPdeny's aggregated Russian IPv4 zone daily, validates every network, rejects unsafe ranges and implausible list sizes, renders a complete nftables transaction, and loads it atomically. A failed update leaves the previous live set and status untouched.

The updater writes a non-secret status file containing mode, prefix count, and update time. The host agent includes this status in its heartbeat. The admin status strip therefore describes the installed router rather than a frontend assumption.

Known limitation: country selection is by destination IP. A Russian domain served by a foreign CDN address follows AWG; a foreign domain served from a Russian address follows the direct path.

## API additions

- `GET /api/admin/wireguard/relays/{relayId}/peers/{peerId}/metrics?range=HOUR|DAY|WEEK|MONTH`
- Optional `routingStatus` object in the existing agent heartbeat.
- Relay read DTO adds `routingMode`, `ruPrefixCount`, and `routingUpdatedAt`.

The metric endpoint keeps the existing admin authorization boundary. The agent heartbeat keeps the existing relay-token boundary.

## Rollout

1. Deploy the backward-compatible backend schema and API.
2. Install the updated agent and geo-routing units on `utils` in plan mode, then apply after validation.
3. Deploy the frontend.
4. Verify API history, policy-route resolution, nftables/iptables counters, relay heartbeat state, desktop layout, narrow layout, polling, and chart interaction in production.
