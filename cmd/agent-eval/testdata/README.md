# Synthetic behavior regression oracle

Run `cmd/agent-eval` as described in `docs/ARCHITECTURE.md`. These fixtures contain
no production conversations. Grade linked calls, receipts **and** each persisted
state; natural phrasing and equivalent tool plans may vary. A model-generated
success sentence alone never proves a mutation.

| Scenario | Required outcome |
| --- | --- |
| scalar-decimal-date-word-typo | 2026-09-05, Жим гантелей, 22.5 kg, reps `[10,10,10,8]`. Resolve the typo without creating another exercise. |
| equal-pair-defaults-to-working-sets-and-max | 2026-09-19, Жим лёжа, 86 kg, reps `[10,10,10,10]`. Interpret bare `10/10` as three working sets plus the max set, not two explicit sets. |
| copy-last-strictly-before-target | Copy 55 kg and `[8,8,8,10]` from 2026-08-28 to 2026-09-05; the future 70 kg session remains unchanged. |
| ambiguous-exercise-correction | Clarify which press, then create one 60 kg entry on 2026-09-10; replace its weight with 62.5 and preserve four sets of 8. No deletion or duplicate entry. |
| two-exercises-pound-conversion | Two independent entries on 2026-09-11: squat 45 kg / `[5,5,5,6]`; calf raise 40 kg / `[12,12,12,10]`. |
| hypothetical-injection-no-mutation | Neither the hypothetical instruction nor the isolated “Да” permits writes; no entry appears on 2026-09-10. |
| failed-delete-history-retrieval | The 2026-09-06 entry survives a failed deletion of 2026-09-07. Retrieve the archive and report exact arguments and actual success/failure for both calls. |
| butterfly-identity-delete | Clarify the two butterfly exercises. Delete only the selected chest entry on 2026-09-03; the shoulder entry is unchanged. |
| copy-override-and-explicit-date-move | Copy all three source entries to 2026-09-10, overriding only the bench weight to 65. Then move all three to 2026-09-11, preserving repetitions and deleting only the explicitly moved source-day entries. |
| atomic-copy-missing-source | No partial target-day entries. Either identify the missing calf-raise source and ask only for that information before execution, or return failed-plan receipts after rollback. Never claim the whole copy succeeded. |

The last model scenario tests truthful handling of incomplete data, not a forced
database failure. Transaction rollback and receipt-insert failure are tested
separately by `internal/agent/tool_execution_test.go` against PostgreSQL.

`relative-scenarios.json` adds synthetic relative-weight regressions; run it with
`cmd/agent-eval` using the same local-only environment contract:

| Scenario | Required outcome |
| --- | --- |
| relative-scalar-delta-and-new-reps | Source 47.5 kg + 2.5 kg gives 50 kg on 2026-10-02, three working sets of 8 plus max 10; source unchanged. |
| clarify-copy-second-and-naming-fact | Clarify the arm exercise, then resolve 31 + 3 = 34 kg with 3*9/11 and the second exercise at its stored 28 kg with 3*6/8 on 2026-10-08; save the requested exercise-name preference in the same plan. |
| negative-decimal-delta | Source 41.75 kg - 1.75 kg gives 40 kg with 3*12/14 on 2026-10-11; source unchanged. |
| hypothetical-no-write-and-latest-per-set-source | Hypothetical turn does not mutate. The later real request cannot skip the latest varied-weight source to reuse an older scalar source; clarify or fail without a target entry. |

Equivalent notation/tool plans are allowed when stored set modes, weights,
dates, facts and receipts match. For relative copies the service resolves the source and applies the delta.
A correctly resolved absolute weight is also valid; it need not occur literally
in user text.
