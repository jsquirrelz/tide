---
phase: 47-self-hosted-phoenix-install-end-to-end-proof
verified: 2026-07-17T20:10:41Z
status: human_needed
score: 3/3 roadmap truths verified; 4/4 prior code gaps closed
overrides_applied: 0
re_verification:
  previous_status: gaps_found
  previous_score: 3/3 must-haves verified (4 gaps + 2 human items routed)
  gaps_closed:
    - "CR-02 — reporter Job no longer forwards decoded OTLP bearer as a plaintext EnvVar.Value (now valueFrom.secretKeyRef); WR-03 doc claim corrected"
    - "CR-01 — reporter-spawn gate no longer re-opens after planner-Job TTL-GC (alreadySpawned predicate at all 4 planner sites; Task site immune; planner-Job-TTL-GC re-entry envtests added)"
    - "Gap #3 — boundary-push --force-with-lease now re-reads the real remote tip and refreshes only already-integrated history, else fails closed (exit 11)"
    - "Gap #4 — medium sample README adds an inline RWO override step (3b) for the tide-projects PVC on kind"
  gaps_remaining: []
  regressions: []
human_verification:
  - test: >
      Review the four committed evidence PNGs and 47-EVIDENCE.md against the PROOF-01
      milestone acceptance bar. The 47-05 Task 3 checkpoint:human-verify (gate=blocking)
      was auto-resolved via its named-gaps branch — no human has actually reviewed the
      evidence. PROOF-01 IS the milestone acceptance bar; its own plan mandates human
      sign-off. The gap-closure plans (47-06..47-10) did NOT re-run a paid live proof
      and were not scoped to; this item is unchanged and carried forward.
    expected: >
      Human confirms: (a) the trace-tree PNG shows all five AGENT levels
      (project→milestone→phase→plan→task) with LLM spans under Task; (b) the query PNG
      shows a working DSL filter over the enrichment; (c) the deep-link PNG lands on the
      right trace. Then explicitly accepts PROOF-01 as met OR converts a shortfall to a
      named gap.
    why_human: "Milestone acceptance-bar sign-off; visual/UX judgment against the bar; the plan's own blocking human gate never fired."
  - test: >
      Judge whether the LLM-span capture (47-evidence-llm-span-redacted.png) satisfies
      PROOF-01's "including redacted message arrays at the Task level" clause. The
      screenshot shows a real multi-role message array with NO visible redaction/elision
      markers. 47-EVIDENCE §4 discloses why: the redaction pass runs unconditionally but
      this run's content had zero secret-pattern matches and the largest message
      attribute was 21,573 B (< 32 KiB elision cap), so pass-through is the boundary's
      correct output; redaction was proven held by a 0-hit key-material search across all
      392 spans.
    expected: >
      Human decides whether pass-through content (with redaction proven via 0-hit search)
      satisfies "redacted message arrays," or requests a supplementary capture from a run
      containing secret-bearing or over-cap content that visibly exercises the redaction/
      elision markers.
    why_human: "Interpretation of the milestone bar's 'redacted' clause; cannot be resolved programmatically."
---

# Phase 47: Self-Hosted Phoenix Install + End-to-End Proof — Verification Report (Re-Verification)

**Phase Goal:** An operator can stand up a self-hosted Phoenix from documented, non-default-safe overrides, point TIDE's existing `otel.exporter.endpoint` chart value at it, and see a real run's complete five-level trace tree — including redacted message arrays — rendered and queryable. This is the milestone's acceptance bar.
**Verified:** 2026-07-17T20:10:41Z
**Status:** human_needed
**Re-verification:** Yes — after gap closure (plans 47-06 → 47-10 + post-code-review re-fix `37f4a46`/`271df2a`)

## Re-Verification Summary

All **four** code gaps named in the prior `gaps_found` verification are **CLOSED**, traced line-by-line in the live codebase (not accepted from SUMMARY). Build + vet are clean on every touched package; the fast unit tiers for gaps #1 and #3 pass, and the CR-01 idempotency envtest suite (the highest-risk gap — it was fixed once incompletely and re-fixed post-review) passes **5/5 specs including both new planner-Job-TTL-GC re-entry pins**. The three roadmap Success Criteria remain verified on their letter, unchanged.

The **two human_verification items are carried forward unchanged** (not re-failed): they are milestone-acceptance-bar human-judgment items the gap-closure plans were not scoped to touch (no paid live proof was re-run). Per the status decision tree, a non-empty human-verification section forces `human_needed` — the code work is done; the PROOF-01 milestone sign-off remains outstanding.

## Goal Achievement

### Observable Truths (Roadmap Success Criteria)

| # | Truth | Status | Evidence |
| - | ----- | ------ | -------- |
| 1 | INSTALL.md/observability.md walk an operator through self-hosted Phoenix covering BOTH storage paths + the `auth.enableAuth=true` default (PHX-01) | ✓ VERIFIED | Unchanged from initial verification; `docs/INSTALL.md:217` §"Enable tracing (Phoenix)" + `docs/observability.md` §"Self-hosted Phoenix". Now also carries the `tide-otlp-headers` per-namespace mirror step (INSTALL.md:251,264) from the CR-02 fix |
| 2 | `otel.exporter.endpoint` documented end-to-end in bare `host:port`; NOTES.txt nudges when tracing is dark (PHX-02) | ✓ VERIFIED | Unchanged; Go+chart wiring + docs + Permutation I render gate. The OTLP-headers forward now threads a Secret NAME (not a decoded value) end-to-end |
| 3 | A live five-level trace tree — including redacted message arrays at Task — visible + queryable in self-hosted Phoenix; evidence captured (PROOF-01) | ✓ VERIFIED (letter) | Unchanged; trace `e9124906f6ee4aeba650a6fdd93b86fd`, 4 PNGs, DSL query + deep-link resolve, redaction 0-hit. Human acceptance-bar sign-off still outstanding (human_verification #1/#2) |

**Score:** 3/3 roadmap truths verified (letter); 4/4 prior gaps closed

### Prior Gap Closure

| # | Prior Gap | Prior Status | Now | Evidence (traced in live code) |
| - | --------- | ------------ | --- | ------------------------------- |
| 1 | CR-02 — reporter Job forwards decoded OTLP bearer as plaintext `EnvVar.Value` | partial (blocker) | ✓ CLOSED | `reporter_jobspec.go:324-339` emits `OTEL_EXPORTER_OTLP_HEADERS` via `ValueFrom.SecretKeyRef` (Name=`tide-otlp-headers`, Optional=true), no literal `Value`. No decoded-value field remains anywhere (`grep OTLPHeaders\b` minus `OTLPHeadersSecret` = 0 hits). `cmd/manager/main.go:293-295` reads env for presence only, threads the Secret NAME into `plannerDeps` (:450) + `TaskReconcilerDeps` (:562). All 5 spawn sites pass name-only `OTLPHeadersSecret`. WR-03: `docs/observability.md:170-190` states reading the token requires `get` on Secrets — the false RBAC-equivalence claim is gone. Unit test asserts empty literal + secretKeyRef shape — PASS |
| 2 | CR-01 — reporter-spawn gate re-opens after TTL-GC (OBS-02 regression) | failed (blocker) | ✓ CLOSED | `alreadySpawned := marker != "" && (completedJob == nil \|\| marker == spawnKey)` present at all 4 planner sites (milestone:663, phase:619, plan:663, project:1916). Task site immune: early-returns on `completedJob == nil` (task_controller.go:1085). WR-01 rollup decoupled from `isFirstCompletion` (milestone:726, gated solely on `*RolledUpUID`). Envtest `reporter_spawn_idempotency_test.go` adds BOTH planner-Job-TTL-GC (`completedJob == nil`) re-entry specs — milestone shared-helper (:529) + project inline arm (:662) — not only reporter-Job-GC. **Ran live: 5/5 specs pass** |
| 3 | Gap #3 — boundary-push retry re-asserts stale lease | failed | ✓ CLOSED | `pkg/git.RemoteBranchTip` reads the real remote tip; `cmd/tide-push/main.go:984 deriveEffectiveLease` refreshes the lease ONLY when the remote tip `IsAncestor` of local HEAD (already-integrated), else writes `lease-rejected` + returns `exitLeaseFailed` (both the absent-locally :1008 and present-but-not-ancestor :1030 paths fail closed). Caller returns `rejectExit` without pushing (:677). Cannot force over external divergence. Regression tests (refresh + external-reject) PASS |
| 4 | Gap #4 — medium PVC RWX deadlocks on kind | failed | ✓ CLOSED | `examples/projects/medium/README.md:124-150` step 3b: inline `delete` + `apply` heredoc recreating `tide-projects` PVC as `accessModes: [ReadWriteOnce]`, scoped "kind / RWO-only ONLY". `per-namespace-resources.yaml:40-58` comments now route to step 3b instead of hand-edits |

### Key Link Verification

| From | To | Via | Status | Details |
| ---- | -- | --- | ------ | ------- |
| `cmd/manager/main.go` | Deps structs | `OTLPHeadersSecret: otlpHeadersSecret` (NAME only) | ✓ WIRED | :450, :562 — decoded value never leaves the manager process |
| 5× reconcilers | `reporter_jobspec.go` | `OTLPHeadersSecret: r.Deps.OTLPHeadersSecret` | ✓ WIRED | milestone:675, phase:631, plan:685, project:1943, task:1127 |
| manager env → reporter Job | reporter TracerProvider | `valueFrom.secretKeyRef` (Optional) | ✓ WIRED-SAFE | prior ⚠️ WIRED-BUT-UNSAFE now resolved — Job spec exposes only the Secret name |
| `deriveEffectiveLease` | `pkggit.Push` | ancestry-guarded effective lease | ✓ WIRED | :676-680 — rejection short-circuits before Push |
| 4 planner sites + Task | `*ReporterSpawnedUID` markers | durable gate + RetryOnConflict stamp | ✓ WIRED | markers on all 5 v1alpha3 status types; regenerated CRDs |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| Build touched packages | `go build ./cmd/manager/... ./internal/controller/... ./pkg/git/... ./cmd/tide-push/...` | exit 0 | ✓ PASS |
| Vet touched packages | `go vet ...` (same set) | exit 0 | ✓ PASS |
| Gap #1 secretKeyRef shape | `go test ./internal/controller -run TestBuildReporterJob_OTLPHeaders` | ok | ✓ PASS |
| Gap #3 push-lease regression | `go test ./cmd/tide-push -run TestRunPushMode` (incl. refresh + external-reject) | ok | ✓ PASS |
| Gap #3 RemoteBranchTip | `go test ./pkg/git -run RemoteBranchTip` | ok | ✓ PASS |
| Gap #2 CR-01 idempotency (incl. planner-Job-TTL-GC re-entry) | envtest `-ginkgo.focus=ReporterSpawnIdempotency` | Ran 5 of 238; 5 Passed, 0 Failed | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| ----------- | ----------- | ----------- | ------ | -------- |
| PHX-01 | 47-03/06 | Self-hosted Phoenix recipe, both storage paths, auth default | ✓ SATISFIED | INSTALL.md + observability.md verified. Traceability table row still "Pending" — bookkeeping lag (INFO) |
| PHX-02 | 47-01/02/03/06 | `otel.exporter.endpoint` end-to-end, bare host:port, NOTES nudge | ✓ SATISFIED | Go+chart wiring + docs + render gate. Table row "Pending" — bookkeeping lag (INFO) |
| PROOF-01 | 47-04/05/07/08/09/10 | Live five-level tree w/ redacted arrays, visible+queryable, evidence | ✓ SATISFIED (letter) | Live trace + 4 PNGs + EVIDENCE.md; human acceptance-bar sign-off outstanding (human_verification) |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |
| (prior CR-02 blocker) | — | decoded bearer as plaintext EnvVar.Value | ✓ RESOLVED | now secretKeyRef; no literal path remains |
| (prior CR-01 blocker) | — | name-only spawn gate re-openable after TTL-GC | ✓ RESOLVED | durable `alreadySpawned` marker; envtest-pinned |
| (prior WR-03 warning) | — | RBAC-equivalence doc claim | ✓ RESOLVED | observability.md corrected |
| Phase-47-modified code files | — | debt markers (TBD/FIXME/XXX) | ✓ NONE | scan across all 11 touched code files returns 0 |
| `.planning/REQUIREMENTS.md` | 85-86 | PHX-01/PHX-02 still "Pending" while implemented | ℹ️ Info | bookkeeping lag; does not affect goal |

### Human Verification Required

Both items are carried forward from the initial verification **unchanged** — the gap-closure plans were explicitly not scoped to re-run a paid live proof, so neither could be resolved by code work. They are the sole reason status is `human_needed` rather than `passed`.

1. **Review evidence vs the PROOF-01 milestone acceptance bar.** The 47-05 blocking `checkpoint:human-verify` was auto-resolved via its named-gaps branch — no human reviewed the evidence. Confirm the four PNGs + 47-EVIDENCE.md meet the bar, then explicitly accept or convert shortfalls to gaps.
2. **Judge the "redacted message arrays" clause.** `47-evidence-llm-span-redacted.png` shows a real message array with no visible redaction markers; §4 explains this is correct pass-through (0 secret matches; largest message 21,573 B < 32 KiB cap; redaction proven by a 0-hit key search across all 392 spans). Decide whether pass-through satisfies the clause or a redaction-exercising supplementary capture is wanted.

### Gaps Summary

No code gaps remain. All four prior gaps (CR-02, CR-01, boundary-push stale lease, medium-fixture RWX) are closed at the root and verified against the live codebase, with build/vet clean and the relevant test tiers — including the previously-incomplete CR-01 window — passing when actually run. The only outstanding work is the PROOF-01 milestone-acceptance-bar human sign-off and the "redacted" clause judgment, both human-only and correctly carried forward.

---

_Verified: 2026-07-17T20:10:41Z_
_Verifier: Claude (gsd-verifier)_
_Re-verification: gap-closure pass over plans 47-06 → 47-10 + code-review re-fix 37f4a46/271df2a_
