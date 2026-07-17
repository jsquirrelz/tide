---
phase: 47-self-hosted-phoenix-install-end-to-end-proof
plan: 02
subsystem: infra
tags: [helm, opentelemetry, otlp, phoenix, secrets, chart]

# Dependency graph
requires:
  - phase: 46-observability-enrichment-dashboard-deep-link
    provides: otel.exporter.endpoint / otel.tracesSampler chart wiring, phoenix.baseURL dashboard deep link
provides:
  - "otel.exporter.headersSecretRef {name, key} values key (empty name -> zero env rendered anywhere)"
  - "Guarded OTEL_EXPORTER_OTLP_HEADERS env via valueFrom.secretKeyRef on manager AND dashboard containers"
  - "NOTES.txt tracing-dark nudge (D-10) mirroring the existing prometheus.enabled warning"
  - "Offline render gates: assert-otlp-headers-env.py + Permutation I in assert-telemetry-render.sh"
affects: [47-03-plan (INSTALL.md auth-ON recipe), 47-01-plan (reporter Job OTLPHeaders threading), phase-47-live-proof]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Secret-ref-only chart values (never a literal secret in a rendered manifest) — headersSecretRef mirrors the signingKey.secretName precedent"
    - "Augment-script marker-keyed idempotent injection (phase47-otlp-headers-env-injected) anchored on an existing marker line, following the phase9/phase28/phase36 idiom"
    - "Render-gate non-vacuity proven via a sabotage probe (scratch chart copy with the guard condition flipped) rather than trusting a single-direction assertion"

key-files:
  created:
    - hack/helm/assert-otlp-headers-env.py
  modified:
    - hack/helm/tide-values.yaml
    - charts/tide/values.yaml
    - hack/helm/augment-tide-chart.sh
    - charts/tide/templates/deployment.yaml
    - charts/tide/templates/dashboard-deployment.yaml
    - charts/tide/templates/NOTES.txt
    - hack/helm/assert-telemetry-render.sh
    - Makefile

key-decisions:
  - "D-08 honored exactly: OTEL_EXPORTER_OTLP_HEADERS is Secret-sourced via valueFrom.secretKeyRef only — assert-otlp-headers-env.py fails any render carrying a literal `value:` on that env entry"
  - "D-10 both-ways gate extends Permutation G's existing tpl+ConfigMap NOTES probe (Phase 38 mechanism) rather than adding a raw-byte or live-cluster/--dry-run assertion, per the orchestrator-binding decision superseding RESEARCH.md Pitfall A"
  - "New render-gate permutation letter is I (next unused after A-H), placed physically between G and H so it reuses G's still-in-scope NOTES_PROBE_DIR/trap without re-copying the probe chart"

requirements-completed: [PHX-02]

# Metrics
duration: 24min
completed: 2026-07-17
---

# Phase 47 Plan 02: Chart wiring for self-hosted Phoenix OTLP-headers auth + tracing-dark NOTES nudge Summary

**Chart-side half of Pitfall B closed: a Secret-ref-only `otel.exporter.headersSecretRef` values key renders `OTEL_EXPORTER_OTLP_HEADERS` via `valueFrom.secretKeyRef` on both the manager and dashboard Deployments (never a literal), a NOTES.txt warning nudges operators toward `docs/INSTALL.md`'s "Enable tracing" step when `otel.exporter.endpoint` is empty, and two new offline render gates (`assert-otlp-headers-env.py` + a new NOTES-probe permutation) prove both wirings both-ways with zero cluster dependency.**

## Performance

- **Duration:** 24 min
- **Started:** 2026-07-17T13:04:00Z
- **Completed:** 2026-07-17T13:28:00Z
- **Tasks:** 3/3 completed
- **Files modified:** 8 (1 created, 7 modified)

## Accomplishments
- `hack/helm/tide-values.yaml` gained `otel.exporter.headersSecretRef {name, key}` (default both empty/`OTEL_EXPORTER_OTLP_HEADERS`), propagated to `charts/tide/values.yaml` byte-identically via the existing `cp` step in the augment pipeline — zero hand edits to the tracked chart file.
- Both the manager Deployment (via a new marker-keyed `augment-tide-chart.sh` injection section, `phase47-otlp-headers-env-injected`) and the dashboard Deployment (hand-edited — confirmed not augment-script-owned) render a `{{- if .Values.otel.exporter.headersSecretRef.name }}`-guarded `OTEL_EXPORTER_OTLP_HEADERS` env sourced exclusively via `valueFrom.secretKeyRef`; default render carries zero occurrences, `--set otel.exporter.headersSecretRef.name=...` renders exactly 2 (one per container), neither carries a literal `value:`.
- `NOTES.txt` (both the `augment-tide-chart.sh` step-4b heredoc source and the tracked template) now prints a "tracing is dark" warning, gated `{{- if not .Values.otel.exporter.endpoint }}`, placed directly after the existing prometheus warning block — locked wording from the plan's interfaces section landed verbatim in both sites.
- Two new offline render gates wired into `make helm-telemetry-assert`: `hack/helm/assert-otlp-headers-env.py` (checks both `manager` and `dashboard` containers for the secretKeyRef shape or absence) and a new Permutation I in `assert-telemetry-render.sh` (extends Permutation G's `tpl`+ConfigMap NOTES probe — no raw-byte or live-cluster fallback, per the orchestrator-binding decision).
- `bash hack/helm/augment-tide-chart.sh` is regeneration-safe: run three times across the plan (once per task) with zero drift on every re-run.

## Task Commits

Each task was committed atomically:

1. **Task 1: headersSecretRef values key + guarded env on manager (via augment script) and dashboard (direct)** - `e9ea149` (feat)
2. **Task 2: NOTES.txt tracing-dark nudge — augment heredoc AND tracked template** - `983c98b` (feat)
3. **Task 3: Offline render gates — NOTES.txt permutation (extends Permutation G) + assert-otlp-headers-env.py + Makefile wiring** - `05ae046` (feat)

**Plan metadata:** (this commit, immediately after SUMMARY.md is written)

_Note: no TDD tasks in this plan — all three are chart/script/gate edits with inline `helm template` verification, not Go unit tests._

## Files Created/Modified
- `hack/helm/tide-values.yaml` - source-of-truth values; added `otel.exporter.headersSecretRef {name, key}` with a Phase 47 PHX-02/D-08 comment block
- `charts/tide/values.yaml` - tracked mirror, propagated via the augment script's `cp` step (byte-identical, verified via `diff`)
- `hack/helm/augment-tide-chart.sh` - two new sections: the `phase47-otlp-headers-env-injected` marker-keyed manager-container injection (anchored after `# phase4-env-injected`), and the NOTES.txt heredoc's new tracing-dark warning block
- `charts/tide/templates/deployment.yaml` - manager container's guarded `OTEL_EXPORTER_OTLP_HEADERS` env block (produced by the augment script, not hand-edited)
- `charts/tide/templates/dashboard-deployment.yaml` - dashboard container's matching guarded env block (hand-edited — confirmed not augment-script-owned)
- `charts/tide/templates/NOTES.txt` - tracing-dark warning appended after the prometheus warning (produced by the augment script, not hand-edited)
- `hack/helm/assert-telemetry-render.sh` - new Permutation I (NOTES.txt tracing-dark conditional, both-ways)
- `hack/helm/assert-otlp-headers-env.py` - new offline gate for the manager+dashboard secretKeyRef shape
- `Makefile` - `helm-telemetry-assert` target recipe extended with two new `assert-otlp-headers-env.py` invocations

## Decisions Made
- Placed the new NOTES-probe permutation as letter I (next unused after A-H) directly between the existing G and H blocks in `assert-telemetry-render.sh`, rather than appending after H. This keeps it physically adjacent to Permutation G's `NOTES_PROBE_DIR`/`trap` setup it reuses (both remain in scope for the rest of the script since the `trap` fires on script `EXIT`, not block exit), while still using a letter that wasn't already claimed by Phase 46's Permutation H.
- Comment on the new `otel.exporter.headersSecretRef` values block in `tide-values.yaml` documents that the manager forwards the header pair onto reporter Jobs — this is Plan 47-01's Go-threading concern (already landing `OTLPHeaders` into `BuildReporterJob`), not re-implemented here; the comment is forward-referencing context, not new code in this plan.

## Deviations from Plan

None - plan executed exactly as written. `.planning/phases/47-self-hosted-phoenix-install-end-to-end-proof/47-PATTERNS.md` was referenced by the plan's `<context>` block but does not exist on disk in this worktree; all context needed (exact YAML blocks, anchor points, gate-extension mechanism) was fully specified in the plan's own `<interfaces>` and task `<action>`/`<read_first>` sections, so execution proceeded without needing that file. Noting the missing artifact here for traceability, not as a deviation requiring a fix.

## Self-Check: PASSED

All 9 files created/modified in this plan verified present on disk (`hack/helm/tide-values.yaml`, `charts/tide/values.yaml`, `hack/helm/augment-tide-chart.sh`, `charts/tide/templates/deployment.yaml`, `charts/tide/templates/dashboard-deployment.yaml`, `charts/tide/templates/NOTES.txt`, `hack/helm/assert-telemetry-render.sh`, `hack/helm/assert-otlp-headers-env.py`, `Makefile`). All 3 task commit hashes (`e9ea149`, `983c98b`, `05ae046`) confirmed present via `git log --oneline --all`.
