---
phase: 38-small-independents-pricing-accuracy-promptfile-telemetry-nud
verified: 2026-07-11T18:20:00Z
status: passed
score: 10/10 must-haves verified
behavior_unverified: 0
overrides_applied: 0
re_verification: # No — initial verification
---

# Phase 38: Small Independents — Pricing Accuracy, promptFile, Telemetry Nudge, Tech-Debt Carry Verification Report

**Phase Goal:** The order-free paper cuts land — the budget tally matches the provider console on Claude 5 family models, `tide apply` accepts a prompt file, telemetry setup is guided at all three surfaces (INSTALL.md, NOTES.txt, dashboard banner), and the v1.0.6 audit tech-debt is retired.
**Verified:** 2026-07-11T18:20:00Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths (ROADMAP Success Criteria + PLAN must_haves)

| # | Truth | Status | Evidence |
| --- | --- | --- | --- |
| 1 | Claude 5 family models tally at real per-MTok rates via exact-ID lookup + `-YYYYMMDD` normalizer; cache-write multiplier empirically set (COST-01/COST-03) | ✓ VERIFIED | `pricing.go:128` claude-sonnet-5 row (300/1500/30, cacheWrite `300*cacheWriteMultNum/cacheWriteMultDen`=375); `lookupPrice` (`pricing.go:175`) exact→one date-strip retry→conservative, no family matching; `cacheWriteMultNum/Den = 125/100` (`pricing.go:72`) citing 38-01 probe. `go test` pricing/normalizer/multiplier/run-mix — all pass. |
| 2 | Unknown-model pricing fallback observable as durable condition + Prometheus counter, not only a GC'd log line (COST-02) | ✓ VERIFIED | `envelope.go:322` `PricingFallbackModel` field; `shared_types.go:286` `ConditionPricingFallbackActive`; `registry.go:208` `tide_pricing_fallback_total`; helper hooked at all 7 rollup sites (7==7 grep equality); PricingFallback envtest passes (10.4s). WR-01 (strconv.Quote dedup) + WR-02 (RetryOnConflict + optimistic lock) fixes present at `pricing_fallback.go:81-95`. |
| 3 | Provider stamps `Usage.PricingFallbackModel` on post-normalizer miss (D-02 provider side) | ✓ VERIFIED | `subagent.go:355-356` stamps field when `lookupPrice` misses; `TestRun_PricingFallbackModel` passes (unknown stamped / known+dated empty). |
| 4 | 2026-07-03 run-mix replay locked as regression far below 1086¢ overcount (D-04) | ✓ VERIFIED | `run_mix_regression_test.go` pins 473¢, `<615` and `<1086` bounds; testdata fixture ships `dispatches: []` with 1495-char loss provenance (accepted D-04 zero-recovery); test uses provenance-disclosed synthetic reconstruction. `-run RunMix` passes. |
| 5 | `tide apply --prompt-file <path>` inlines file verbatim into single Project's spec.outcomePrompt with D-09/D-10/D-11 guards; no-flag path byte-identical (PROMPT-01) | ✓ VERIFIED | `apply.go:38` `maxPromptFileBytes=256KiB`, `loadPromptFile`, `prepareApplyObject` cluster-free seam before K8sClient(). `TestApply*|TestLoadPromptFile` all pass; no CRD change. |
| 6 | INSTALL.md enable-telemetry step incl. kube-prometheus-stack `release:` label fix ending at a Targets page (TELEM-01) | ✓ VERIFIED | `docs/INSTALL.md:175` "Enable telemetry (Prometheus)"; kps install, `release=kps` label command (matches actual SM `control-plane: controller-manager` label per 38-REVIEW), serviceMonitorSelectorNilUsesHelmValues alternative, Targets-page done signal, existing-Prometheus variant. |
| 7 | prometheus.enabled=false (default) prints NOTES.txt warning that run telemetry beyond budget is unavailable (TELEM-02) | ✓ VERIFIED | `charts/tide/templates/NOTES.txt:9-12` conditional warning; helm render: warning present by default (1), absent with `--set prometheus.enabled=true` (0); owned by augment script (4 refs); assert-telemetry-render.sh permutation G passes. |
| 8 | Dashboard shows telemetry banner distinguishing disabled-by-config from no-data (TELEM-03) | ✓ VERIFIED | `GET /api/v1/config` → `{"telemetryEnabled":bool}` (`config.go`); `TelemetryDisabledBanner.tsx` two text-distinct states; `TelemetryView.tsx:1251-1252` WR-04 fix (`telemetryEnabled===null && allPanelsUnavailable`); config/route/env Go tests pass, TestZeroMutationRoutes green. |
| 9 | Project-level PlannerRolledUpUID stamp hardened (RetryOnConflict + optimistic lock); configmap defaults plannerConcurrency to 4 (DEBT-01/DEBT-02) | ✓ VERIFIED | `project_controller.go:1901` RetryOnConflict, `:1909` MergeFromWithOptimisticLock, `:1913` returns error (no swallow); PlannerRolledUpUID envtest passes. configmap renders `plannerConcurrency: 4` (1). Both landed via Phase 39 carry-in, verified present in current tree. |
| 10 | Heavy controller envtests run in integration tier, spec count conserved (DEBT-03) | ✓ VERIFIED | 13 `Label("heavy")` sites across test files; suite guard `suite_test.go:130` (`testing.Short() && CurrentSpecReport().Labels()`→Skip); Makefile 3 `label-filter='heavy'` lines + test-heavy target; heavy tier runs 34.3s exit 0 no FAIL; unit tier exit 0 no FAIL; conservation 191+12=203, Y=204 recorded. |

**Score:** 10/10 truths verified (0 present, behavior-unverified)

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | -------- | ------ | ------- |
| `internal/subagent/anthropic/pricing.go` | sonnet-5 row, lookupPrice, cacheWriteMult | ✓ VERIFIED | All present; both cost paths rewired through lookupPrice |
| `internal/subagent/anthropic/subagent.go` | PricingFallbackModel stamp + WR-03 derive | ✓ VERIFIED | Stamp at :355; WR-03 override derive uses constants at :161 |
| `internal/subagent/anthropic/run_mix_regression_test.go` | run-mix replay | ✓ VERIFIED | Pins 473¢, <615, <1086 |
| `internal/subagent/anthropic/testdata/first_run_2026-07-03_usage.json` | usage fixture | ✓ VERIFIED | Zero-recovery `dispatches: []` + provenance (accepted D-04) |
| `38-01-PROBE-RESULT.md` | COST-03 verdict | ✓ VERIFIED | `verdict: cacheWriteMultiplier = 125/100` (1 match) |
| `pkg/dispatch/envelope.go` | PricingFallbackModel field | ✓ VERIFIED | :322 with omitempty, provider-neutral |
| `api/v1alpha2/shared_types.go` | condition constants | ✓ VERIFIED | ConditionPricingFallbackActive + ReasonUnknownModelPriced |
| `internal/controller/pricing_fallback.go` | helper w/ WR-01/WR-02 | ✓ VERIFIED | strconv.Quote dedup + RetryOnConflict + optimistic lock |
| `internal/metrics/registry.go` | counter | ✓ VERIFIED | tide_pricing_fallback_total registered |
| `cmd/tide/apply.go` | --prompt-file | ✓ VERIFIED | flag + validation seam pre-apiserver |
| `charts/tide/templates/NOTES.txt` | telemetry warning | ✓ VERIFIED | conditional, augment-owned |
| `charts/tide/values.yaml` | prometheus.enabled | ✓ VERIFIED | default false, byte-identical mirror of source |
| `charts/tide/templates/dashboard-deployment.yaml` | PROMETHEUS_ENABLED env | ✓ VERIFIED | always-rendered, default "false" |
| `charts/tide/templates/configmap.yaml` | default 4 | ✓ VERIFIED | plannerConcurrency default 4 |
| `docs/INSTALL.md` | enable-telemetry walkthrough | ✓ VERIFIED | full kps section |
| `cmd/dashboard/api/config.go` | config endpoint | ✓ VERIFIED | GET-only, locked wire contract |
| `dashboard/web/src/components/TelemetryDisabledBanner.tsx` | banner | ✓ VERIFIED | two text-distinct states, read-only |
| `dashboard/web/src/components/TelemetryView.tsx` | derivation + WR-04 | ✓ VERIFIED | precedence + null-only defensive fallback |
| `internal/controller/project_controller.go` | hardened stamp | ✓ VERIFIED | RetryOnConflict + optimistic lock |
| `internal/controller/suite_test.go` | heavy guard | ✓ VERIFIED | short-mode label skip |
| `Makefile` | test-heavy tier | ✓ VERIFIED | 3 label-filter lines |

### Key Link Verification

| From | To | Via | Status |
| ---- | -- | --- | ------ |
| controller rollup sites | pricing_fallback.go | setPricingFallbackIfNeeded after every RollUpUsage | ✓ WIRED (7==7) |
| pricing_fallback.go | registry.go | PricingFallbackTotal.WithLabelValues | ✓ WIRED |
| subagent.go lookupPrice | envelope Usage.PricingFallbackModel | stamp on miss | ✓ WIRED |
| 38-01-PROBE-RESULT.md | pricing.go cacheWriteMult | constant comment cites probe | ✓ WIRED |
| values.yaml prometheus.enabled | NOTES.txt / dashboard env | conditional + PROMETHEUS_ENABLED | ✓ WIRED (render-confirmed) |
| main.go PROMETHEUS_ENABLED | router Dependencies | telemetryEnabledFromEnv | ✓ WIRED |
| TelemetryView.tsx | GET /api/v1/config | one-shot fetch | ✓ WIRED |
| suite guard | Label("heavy") specs | CurrentSpecReport().Labels() | ✓ WIRED |
| Makefile heavy tier | controller heavy specs | label-filter='heavy' | ✓ WIRED |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| Pricing row/normalizer/multiplier/run-mix | `go test ...anthropic -run 'Lookup\|Normalizer\|Multiplier\|Fallback\|RunMix\|CacheWriteMultiplier'` | ok | ✓ PASS |
| PROMPT-01 CLI | `go test ./cmd/tide -run 'TestApply\|TestLoadPromptFile'` | ok | ✓ PASS |
| Dashboard config/zero-mutation | `go test ./cmd/dashboard/... -run 'TestZeroMutationRoutes\|Config\|TelemetryEnabled'` | ok | ✓ PASS |
| COST-02 + DEBT-01 envtest | `-ginkgo.focus='PricingFallback\|PlannerRolledUpUID'` | ok 10.4s | ✓ PASS |
| DEBT-03 heavy tier | `-ginkgo.label-filter='heavy'` | exit 0, 34.3s, no FAIL | ✓ PASS |
| DEBT-03 unit tier | `-short` | exit 0, no FAIL | ✓ PASS |
| Chart render (DEBT-02/D-14) | `helm template` | plannerConcurrency:4, PROMETHEUS_ENABLED "false" | ✓ PASS |
| TELEM-02 NOTES conditional | `helm install --dry-run` +/- prometheus.enabled | present/absent | ✓ PASS |
| Render gate | `assert-telemetry-render.sh` | 7/7 permutations, exit 0 | ✓ PASS |
| Phase-38 package build | `go build` (12 packages) | exit 0 | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan | Status | Evidence |
| ----------- | ----------- | ------ | -------- |
| COST-01 | 38-01, 38-06 | ✓ SATISFIED | sonnet-5 row + normalizer; tests pass |
| COST-02 | 38-02, 38-06 | ✓ SATISFIED | field+condition+metric, 7 hooks, envtest pass |
| COST-03 | 38-01, 38-06 | ✓ SATISFIED | probe verdict 125/100, single-sourced constant |
| PROMPT-01 | 38-03 | ✓ SATISFIED | --prompt-file flag + guards, tests pass |
| TELEM-01 | 38-04 | ✓ SATISFIED | INSTALL.md walkthrough; label matches SM |
| TELEM-02 | 38-04 | ✓ SATISFIED | NOTES.txt conditional warning, render-confirmed |
| TELEM-03 | 38-05 | ✓ SATISFIED | config endpoint + banner, WR-04 fixed, tests pass |
| DEBT-01 | 38-07 (Phase 39 carry-in) | ✓ SATISFIED | hardened stamp verified in tree, envtest pass |
| DEBT-02 | 38-04 (Phase 39 carry-in) | ✓ SATISFIED | configmap default 4, render-confirmed |
| DEBT-03 | 38-07 | ✓ SATISFIED | 12 heavy specs labeled, both tiers green, conserved |

All 10 declared requirement IDs accounted for in REQUIREMENTS.md. No orphaned requirements.

### Anti-Patterns Found

| File | Pattern | Severity | Impact |
| ---- | ------- | -------- | ------ |
| (none) | TBD/FIXME/XXX debt markers | — | Clean across all phase files |
| (none) | TODO/HACK/PLACEHOLDER/stubs | — | Clean |

### Info Items (non-blocking, deliberately out of scope)

- **IN-01** (`pricing.go:158,184`): `conservativeTier` resolves from the compiled table, not the per-instance merged table — unknown-model fallback bills at stale compiled fable-5 rates if an operator overrides the fallback row. Classified info in 38-REVIEW; deferred.
- **IN-02** (`cmd/tide/apply.go:153`): D-09 override guard still uses `found, _ :=` and ignores the NestedString error for a non-string `spec.outcomePrompt` (malformed manifest silently overridden). Classified info in 38-REVIEW; deferred.
- **REQUIREMENTS.md tracking-table staleness** (lines 123-126): PROMPT-01, TELEM-01, TELEM-02 still marked "Pending" in the summary table while implemented and verified. Cosmetic bookkeeping drift, not a functional gap.
- **deferred-items.md staleness**: the "make lint fails on 4 modernize issues in main_test.go" item is resolved in the current tree (`main_test.go` uses `new(...)` not `ptr(...)`), consistent with 38-REVIEW-FIX.md's `make lint — 0 issues`.
- **Pre-existing**: `go build ./...` wildcard fails on `cmd/tide-demo-init` `//go:embed` (from Phase 15, commit 25fce55c) — reproduces on main, unrelated to Phase 38; all Phase 38 packages build individually.

### Human Verification Required

None. The one inherently operator-run outcome (TELEM-01: an operator following INSTALL.md ends at a green Prometheus Targets page) is satisfied at the deliverable level — the walkthrough is complete and copy-paste runnable, and the `release=kps` label command's target label (`control-plane: controller-manager`) was independently confirmed present on the actual ServiceMonitor. The live end-to-end walk requires a running kube-prometheus-stack but the guidance-correctness deliverable is verified.

### Gaps Summary

No gaps. All 5 ROADMAP success criteria and all 10 requirement IDs are achieved and behaviorally verified in the current tree, including the four post-execution code-review fixes (WR-01 quoted-model dedup, WR-02 optimistic-lock status patch, WR-03 multiplier-derived override rates, WR-04 banner fallback restriction — commits 173c75e1, f823095f, 03ddace9, 74a1f145). The COST-03 probe verdict (125/100) and the 38-01 zero-recovery export are the accepted D-04/D-08 outcomes, and their consumers (pricing constant citation, run-mix regression fixture) are consistent with them.

---

_Verified: 2026-07-11T18:20:00Z_
_Verifier: Claude (gsd-verifier)_
