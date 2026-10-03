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
| butterfly-identity-delete-known-alias | The saved user fact resolves “butterfly” to the chest exercise. Delete that entry immediately; preserve the shoulder entry and naming fact. |
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
| negative-decimal-delta | Explicitly completed workout: source 41.75 kg - 1.75 kg gives 40 kg with 3*12/14 on 2026-09-29; source unchanged. Do not use a future “сделай” proposal to test a completed-workout mutation. |
| hypothetical-no-write-and-latest-per-set-source | Hypothetical turn does not mutate. The later real request cannot skip the latest varied-weight source to reuse an older scalar source; clarify or fail without a target entry. |

Equivalent notation/tool plans are allowed when stored set modes, weights,
dates, facts and receipts match. For relative copies the service resolves the source and applies the delta.
A correctly resolved absolute weight is also valid; it need not occur literally
in user text.

`set-semantics-scenarios.json` checks shorthand versus explicitly enumerated
sets through the same live-model sandbox loop:

| Scenario | Required stored outcome |
| --- | --- |
| standard-shorthand-equal-and-unequal | Four reps per exercise: 9,9,9,9 at 63 kg and 8,8,8,11 at 51 kg; three working sets plus max. |
| enumerated-pairs-equal-and-unequal | Exactly 9,9 at 27 kg and 11,8 at 31 kg; two sets each. |
| two-sets-followup-and-return-to-standard | Two sets for both 2026-09-28 entries; the explicit standard scheme on 2026-09-29 has four reps. No permanent fact is created. |
| relative-copy-with-enumerated-unequal-pair | Source stays 38 kg with 3*8/11; target is 40 kg with exactly 11,8. |
| correction-keeps-weight-and-date | One target row at 63 kg on 2026-09-28, corrected to exactly 9,9. |
| ambiguous-read-and-missing-data | All turns leave workouts empty: a hypothetical question is read-only, missing weight/reps requires clarification, and cancellation plus a future plan must not create a completed-workout entry. |

Grade persisted lists, set counts, dates, source preservation, actual receipts,
and linked history. Reply text alone cannot prove set semantics.
