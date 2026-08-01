# Saturday Health Report Table Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move weekly Telegram health reports to Saturday and append a readable seven-calendar-day table to both generated PNGs.

**Architecture:** Extend `WeeklyHealthReportRenderer` with a shared table renderer that receives literal date/value rows derived from the report end date. Start the changed timer logic under a versioned Temporal workflow ID and terminate the legacy Sunday execution only after the Saturday execution is started or already present.

**Tech Stack:** Kotlin, Java2D, JUnit 5, Temporal Java SDK, Gradle.

## Global Constraints

- Keep each metric in one 1200 px-wide PNG.
- Show exactly the latest seven calendar dates, newest first.
- Render missing values as `—`.
- Run Saturday at 12:00 in the configured Temporal zone.
- Preserve all REST and persisted-data contracts.
- Do not push or deploy without separate authorization.

---

### Task 1: Seven-day table

**Files:**
- Modify: `src/test/kotlin/dev/myutils/api/service/WeeklyHealthReportRendererTest.kt`
- Modify: `src/main/kotlin/dev/myutils/api/service/WeeklyHealthReportRenderer.kt`

**Interfaces:**
- Consumes: `renderSteps(points, from, to)` and `renderWeight(points, from, to)`
- Produces: PNGs with a larger height and a bottom table covering `to` through `to.minusDays(6)`

- [ ] **Step 1: Write failing renderer tests**

Add assertions for the enlarged canvas and sample pixels/text-row regions for
seven rows. Use complete daily steps and sparse weight fixtures so the latter
proves missing dates remain visible rather than being dropped.

- [ ] **Step 2: Verify the renderer tests fail**

Run:

```bash
./gradlew test --tests dev.myutils.api.service.WeeklyHealthReportRendererTest
```

Expected: failure because the current image remains 1200×760 and contains no
table region.

- [ ] **Step 3: Implement the shared table renderer**

Add a `DailyValueRow(date: LocalDate, value: String)` representation, derive
seven dates newest first from `to`, draw a titled header and seven readable
rows, and increase `CANVAS_HEIGHT`. Steps use grouped integers and weight uses
one decimal plus `кг`; absent values use `—`.

- [ ] **Step 4: Verify renderer tests pass**

Run the focused renderer test command and confirm all cases pass.

### Task 2: Saturday schedule and safe migration

**Files:**
- Modify: `src/test/kotlin/dev/myutils/api/temporal/TemporalWorkflowTests.kt`
- Modify: `src/main/kotlin/dev/myutils/api/temporal/report/WeeklyHealthReportWorkflowImpl.kt`
- Modify: `src/main/kotlin/dev/myutils/api/temporal/TemporalWorkflowService.kt`
- Modify: `src/main/kotlin/dev/myutils/api/temporal/TemporalReminderBootstrap.kt`

**Interfaces:**
- Produces: `nextSaturdayNoon(zoneId, nowMillis)`
- Produces: workflow ID `weekly-health-report-v2-<chatId>`
- Consumes: legacy workflow ID `weekly-health-report-<chatId>` for one-time termination

- [ ] **Step 1: Write the failing Saturday workflow test**

Change the test fixture to compute the next Saturday noon and assert the
activity fires only after that instant.

- [ ] **Step 2: Verify the schedule test fails**

Run:

```bash
./gradlew test --tests dev.myutils.api.temporal.TemporalWorkflowTests
```

Expected: compilation or assertion failure because only the Sunday helper
exists.

- [ ] **Step 3: Implement the Saturday timer**

Rename the date helpers to Saturday equivalents and use
`DayOfWeek.SATURDAY`. Keep the report time at 12:00 and the configured zone.

- [ ] **Step 4: Add versioned ID migration**

Start `weekly-health-report-v2-<chatId>`. After start succeeds or reports that
it already exists, terminate the legacy `weekly-health-report-<chatId>` with a
clear migration reason. Ignore `WorkflowNotFoundException`.

- [ ] **Step 5: Verify Temporal tests pass**

Run the focused Temporal test command and confirm the Saturday activity date.

### Task 3: Documentation and full verification

**Files:**
- Modify: `AGENTS.md`
- Modify: `README.md`
- Modify: `src/main/kotlin/dev/myutils/api/temporal/TemporalWorkflowService.kt`
- Modify: `src/main/kotlin/dev/myutils/api/temporal/TemporalReminderBootstrap.kt`

- [ ] **Step 1: Replace Sunday-facing documentation and logs**

Document Saturday 12:00 and describe the report package without the old weekday
name.

- [ ] **Step 2: Run the complete repository gate**

```bash
./gradlew test
git diff --check
```

- [ ] **Step 3: Render representative images**

Set `WEEKLY_REPORT_PREVIEW_DIR` for the renderer test and run it to produce
`steps.png` and `weight.png`.

- [ ] **Step 4: Inspect both scales**

Inspect each native 1200 px image and a Telegram-preview-sized rendering.
Confirm seven rows are legible, calendar gaps are retained, and chart
annotations do not overlap data marks.
