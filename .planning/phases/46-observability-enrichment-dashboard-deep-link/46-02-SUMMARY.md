---
phase: 46-observability-enrichment-dashboard-deep-link
plan: 02
subsystem: infra
tags: [helm, opentelemetry, otel, phoenix, chart-values, render-gates]

# Dependency graph
requires:
  - phase: 44-llm-message-array-spans-d-o5-redaction-size-boundary
    provides: reporter-emitted LLM message-array spans and their traceparent-driven sampled flag
provides:
  - "otel.tracesSamplerArg chart default flipped 0.1 -> 1.0 (OBS-01) with every stale surface updated"
  - "phoenix.baseURL chart value + conditional PHOENIX_BASE_URL dashboard env (OBS-04 config root)"
  - "hack/helm/assert-phoenix-env.py render gate + assert-telemetry-render.sh Permutation H, both wired into make helm-assert"
  - "docs/observability.md opt-down section (honest sampled-flag semantics) and phoenix.baseURL operator doc"
affects: [46-03, 46-05, 47-phoenix-install]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "PROM_ENDPOINT conditional-env pattern reused byte-for-byte for PHOENIX_BASE_URL (empty string is the sentinel, no always-rendered boolean twin)"
    - "assert-*-env.py sibling scripts share the PyYAML multi-doc-walk + found_dashboard_container guard shape"

key-files:
  created:
    - hack/helm/assert-phoenix-env.py
  modified:
    - charts/tide/values.yaml
    - hack/helm/tide-values.yaml
    - charts/tide/templates/dashboard-deployment.yaml
    - internal/otelinit/doc.go
    - internal/otelinit/provider.go
    - hack/helm/assert-telemetry-render.sh
    - Makefile
    - docs/observability.md

key-decisions:
  - "hack/helm/tide-values.yaml is the true canonical source (copied over charts/tide/values.yaml by augment-tide-chart.sh at `make helm-controller` time) — edited both files identically, not just the plan-listed charts/tide/values.yaml, to avoid a silent revert on the next chart regen. The pre-commit 'chart reproducibility (make helm + diff)' hook confirmed this was required."
  - "phoenix.baseURL placed immediately after the otel: block (not immediately adjacent to prometheus:) since Phoenix consumes otel-emitted traces — still matches the umbrella-block shape the plan asked for."
  - "docs/observability.md opt-down section cites the D-02 sampled-flag fix as Plan 46-04's work (not this plan's), matching the plan's own '(Plan 46-04's D-02 fix)' phrasing rather than implying it shipped here."

patterns-established:
  - "Any future charts/tide/values.yaml edit must also touch hack/helm/tide-values.yaml (or the reverse) — the pre-commit chart-reproducibility hook enforces this but it's worth flagging explicitly since the plan's files_modified list only named the chart-side copy."

requirements-completed: [OBS-01, OBS-04]

# Metrics
duration: ~20min
completed: 2026-07-17
---

# Phase 46 Plan 02: Trace-Sampler Default Flip + Phoenix Deep-Link Config Root Summary

**Chart's OTEL trace-sampler default flipped 0.1 -> 1.0 (full sampling), phoenix.baseURL chart value + conditional PHOENIX_BASE_URL dashboard env added, both CI-pinned via a new assert-phoenix-env.py render gate and an extended assert-telemetry-render.sh permutation, and docs/observability.md rewritten with an honest opt-down section plus a phoenix.baseURL operator guide.**

## Performance

- **Duration:** ~20 min
- **Completed:** 2026-07-17
- **Tasks:** 3/3 completed
- **Files modified:** 8 modified, 1 created

## Accomplishments
- OBS-01 delivered in full: `tracesSamplerArg` default is `1.0`, every doc/comment surface that described the old 0.1/10% default is updated (values.yaml x2, otelinit doc.go + provider.go, docs/observability.md), and the opt-down path is documented with the honest sampled-flag caveat instead of implying coherent per-span ratio sampling.
- OBS-04's chart/env config root landed: `phoenix.baseURL` (default `""`) renders `PHOENIX_BASE_URL` on the dashboard Deployment only when set, byte-for-byte mirroring the existing `PROM_ENDPOINT` conditional pattern — no dead buttons in the SPA when unconfigured.
- Both new/extended render gates (`assert-phoenix-env.py`, `assert-telemetry-render.sh` Permutation H) are wired into `make helm-telemetry-assert` → `make helm-assert`, the exact target CI already runs at `ci.yaml:223`, so this rides CI with zero workflow-file edits. Both gates were proven to bite via live mutation checks (temporarily broke each invariant, observed the gate fail with a non-zero exit, then reverted).

## Task Commits

Each task was committed atomically:

1. **Task 1: Sampler default flip + phoenix.baseURL value + dashboard env** - `c77b283` (feat)
2. **Task 2: Helm render gates — sampler 1.0 assertion + assert-phoenix-env.py wired into make helm-assert** - `62fae82` (test)
3. **Task 3: docs/observability.md — sampler table, honest opt-down section, phoenix.baseURL doc** - `ec96a64` (docs)

_No plan-metadata commit — worktree mode; orchestrator commits SUMMARY.md/state after merge._

## Files Created/Modified
- `charts/tide/values.yaml` - `tracesSamplerArg` 0.1 → 1.0 + rewritten comment; new `phoenix: baseURL: ""` block
- `hack/helm/tide-values.yaml` - identical edit (canonical source mirrored into the chart above by `augment-tide-chart.sh`)
- `charts/tide/templates/dashboard-deployment.yaml` - conditional `PHOENIX_BASE_URL` env, mirrors the `PROM_ENDPOINT` guard shape
- `internal/otelinit/doc.go`, `internal/otelinit/provider.go` - three doc-comment surfaces updated to describe the 1.0/100% default (comment-only, zero behavior change)
- `hack/helm/assert-phoenix-env.py` - new render gate: `--expect-absent` / `--expect-value <url>` on the dashboard container's `PHOENIX_BASE_URL` env
- `hack/helm/assert-telemetry-render.sh` - new Permutation H asserting `OTEL_TRACES_SAMPLER_ARG` renders `"1.0"` by default
- `Makefile` - `helm-telemetry-assert` target extended with two phoenix-env render+assert steps
- `docs/observability.md` - sampler table row updated, new "Opting down from 100% sampling" and "Dashboard deep link to Phoenix (`phoenix.baseURL`)" sections

## Decisions Made
- **Dual-file edit for the chart values.** `hack/helm/tide-values.yaml` is the hand-maintained canonical source; `make helm-controller` copies it over `charts/tide/values.yaml` verbatim via `augment-tide-chart.sh`. The plan's `files_modified` only named the chart-side copy, but editing only that file would silently revert on the next chart regeneration — a "silent leftover" of exactly the kind D-01 warns against, just one layer removed from the doc surfaces the plan already called out. Edited both files identically; the pre-commit "chart reproducibility (make helm + diff)" hook passed, confirming the two stayed in sync.
- **Phoenix block placement.** Placed the new `phoenix:` block immediately after `otel:` (not immediately adjacent to `prometheus:`) since Phoenix is the OTel trace *consumer* — semantically closer to the otel block. Still matches the plan's "umbrella shape" requirement (single top-level key with a doc comment).
- **D-02 attribution in docs.** The opt-down section's "one coherent path" paragraph attributes the sampled-flag propagation fix to Plan 46-04 (per the plan's own "(Plan 46-04's D-02 fix)" wording), not to this plan — avoids overclaiming a fix that ships in a sibling plan within the same phase.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Edited hack/helm/tide-values.yaml alongside charts/tide/values.yaml**
- **Found during:** Task 1 (Sampler default flip)
- **Issue:** `charts/tide/values.yaml`'s own header comment plus `hack/helm/augment-tide-chart.sh` both establish that `hack/helm/tide-values.yaml` is the canonical hand-maintained source, copied verbatim over `charts/tide/values.yaml` by `make helm-controller`. The plan's `files_modified` list only named the chart-side file. Editing only it would pass this plan's own verification (which greps `charts/tide/values.yaml`) but silently revert to the stale 0.1 default and lose the new `phoenix:` block the next time the chart is regenerated.
- **Fix:** Applied the identical sampler-flip + phoenix-block edit to `hack/helm/tide-values.yaml`.
- **Files modified:** `hack/helm/tide-values.yaml`
- **Verification:** `diff hack/helm/tide-values.yaml charts/tide/values.yaml` confirmed byte-identical after the edit; the repo's pre-commit "chart reproducibility (make helm + diff)" hook passed on the Task 1 commit, which independently re-derives the chart from the hack-dir source and diffs against the committed chart — it would have failed had the two drifted.
- **Committed in:** `c77b283` (Task 1 commit)

---

**Total deviations:** 1 auto-fixed (1 bug-prevention)
**Impact on plan:** Necessary to make the chart-default change durable across regenerations; no scope creep — same logical edit, applied to both copies of one canonical value.

## Issues Encountered

Intermittent `make helm-assert` failures were observed during verification (2 of 8 total runs), each time on a **different, pre-existing** permutation (`PROM_ENDPOINT` presence/absence — code this plan did not touch) rather than on the new sampler/phoenix logic. Root-caused to this plan executing as one of several parallel worktree agents in the same wave, all invoking `helm template`/`helm lint` concurrently against hardcoded, non-worktree-unique `/tmp/tide-helm-render*.yaml` paths in the Makefile — a pre-existing race in shared render-gate infrastructure, not a regression from this plan's changes. Confirmed via 6 total clean re-runs (isolated + repeated) that `make helm-assert` is deterministically green when not raced, and via targeted mutation checks that the new gates correctly bite on the invariants they're meant to protect. Not fixed in this plan (the Makefile's tmp-path scheme is shared CI infrastructure touching `helm-rbac-assert` too — a broader change than this plan's task scope); flagging as a latent parallel-execution risk for a future infra plan or `/gsd:quick`.

## User Setup Required

None - no external service configuration required. (Phase 47 will document the actual self-hosted Phoenix install; this plan only lands the config value.)

## Next Phase Readiness

- OBS-01 fully satisfied; OBS-04's chart/env root is CI-pinned in both render states, ready for Plan 46-03 (dashboard `GET /api/v1/config` → `phoenixBaseURL`) and Plan 46-05 (SPA deep-link affordance) to consume `PHOENIX_BASE_URL`.
- No blockers. One latent risk noted above (shared `/tmp` render-gate paths under parallel worktree execution) — does not affect CI (single-runner, isolated `/tmp`) but worth a future infra cleanup if GSD parallel-wave execution against this repo becomes routine.

---
*Phase: 46-observability-enrichment-dashboard-deep-link*
*Completed: 2026-07-17*

## Self-Check: PASSED

All 9 files created/modified verified present on disk; all 4 task/summary commits (`c77b283`, `62fae82`, `ec96a64`, `3ee9664`) verified present in git log.
