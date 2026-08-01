# Saturday Health Report Table Design

## Goal

Send the existing Telegram health snapshots on Saturday at 12:00
`temporal.zone-id` time and include a readable table of the latest seven
calendar days below each chart.

## Report layout

Each metric remains a single PNG so Telegram delivers the chart and its daily
values as one artifact. The canvas grows vertically while keeping the existing
1200 px width:

1. title, period, and latest value;
2. the existing 90-day chart;
3. the existing summary statistics;
4. a `Последние 7 дней` table with `День` and metric-specific value columns.

The table always contains seven calendar dates ending on the report date,
newest first. A missing step or weight value renders as `—`; values are never
carried forward or invented. Rows and type are sized for both the native PNG
and Telegram chat-preview scale.

## Schedule and migration

The report runs at Saturday 12:00 in the configured Temporal zone. The
currently running Sunday workflow cannot safely replay code whose timer changed
in place. A versioned workflow ID starts the Saturday implementation. After the
new workflow is confirmed started or already running, bootstrap terminates the
legacy Sunday workflow ID so only one weekly report remains active.

The termination is idempotent: a missing or already-closed legacy execution is
treated as no work. No REST path or health data contract changes.

## Verification

- Renderer tests assert the larger image and seven visible table rows using
  representative steps and sparse weight data.
- Temporal workflow tests prove the report fires at the next Saturday noon.
- Service tests cover the v2 workflow ID and the legacy workflow termination
  boundary where practical.
- The complete backend gate is `./gradlew test` followed by
  `git diff --check`.
- Representative steps and weight PNGs are inspected at native size and at
  Telegram-preview scale.
