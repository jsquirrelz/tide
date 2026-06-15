---
phase: 20-sharedcontext-injection-cache-verification-spike
plan: "01"
subsystem: dispatch-envelope
tags: [cache, shared-context, envelope, crd-schema, omitempty]
dependency_graph:
  requires: []
  provides: [SharedContext-contract, CACHE-02-foundation]
  affects:
    - pkg/dispatch/envelope.go
    - pkg/dispatch/childcrd.go
    - api/v1alpha1/task_types.go
    - api/v1alpha1/phase_types.go
    - api/v1alpha1/plan_types.go
    - api/v1alpha1/milestone_types.go
    - config/crd/bases/tideproject.k8s_tasks.yaml
    - config/crd/bases/tideproject.k8s_phases.yaml
    - config/crd/bases/tideproject.k8s_plans.yaml
    - config/crd/bases/tideproject.k8s_milestones.yaml
tech_stack:
  added: []
  patterns:
    - additive omitempty string field (planner-only complement to executor-only PromptPath/Branch)
    - CRD spec carry-field stamp at materialization (mirrors SourcePath → PromptPath precedent)
key_files:
  created: []
  modified:
    - pkg/dispatch/envelope.go
    - pkg/dispatch/childcrd.go
    - pkg/dispatch/envelope_test.go
    - api/v1alpha1/task_types.go
    - api/v1alpha1/phase_types.go
    - api/v1alpha1/plan_types.go
    - api/v1alpha1/milestone_types.go
    - config/crd/bases/tideproject.k8s_tasks.yaml
    - config/crd/bases/tideproject.k8s_phases.yaml
    - config/crd/bases/tideproject.k8s_plans.yaml
    - config/crd/bases/tideproject.k8s_milestones.yaml
decisions:
  - "D-05: SharedContext carry path via EnvelopeOut → ChildCRDSpec → child CRD Spec → EnvelopeIn confirmed as the implementation shape"
  - "D-06: Pure stable-prefix text field, no provider markers, no CEL validation"
  - "D-07: Uniform on all four CRD spec levels including Milestone (vestigial at top level avoids a level-conditional branch)"
  - "TerminationStub left untouched per RESEARCH Pitfall 4 (< 4 KB invariant)"
  - "zz_generated.deepcopy.go unchanged because string is a value type requiring no explicit deepcopy"
metrics:
  duration: "~15 minutes"
  completed: "2026-06-15T23:21:34Z"
---

# Phase 20 Plan 01: SharedContext Contract Fields Summary

**One-liner:** Additive `SharedContext string` (omitempty) wired across the CACHE-02 carry path — EnvelopeIn, EnvelopeOut, ChildCRDSpec, and all four CRD spec types — with four round-trip/omitempty tests proving empty serializes as absent.

## What Was Built

The interface-first foundation for the CACHE-02 byte-identical-carry data channel:

1. **`EnvelopeIn.SharedContext`** (pkg/dispatch/envelope.go, after `Dev` field) — planner-only, never populated by executor dispatches (CACHE-02 lock). Planner templates will reference `{{.SharedContext}}` in the D-07 reserved slot (Plan 02).

2. **`EnvelopeOut.SharedContext`** (pkg/dispatch/envelope.go, after `ChildCount`) — parent planner emits one curated wave-scoped blob; controller stamps it byte-identically onto all siblings (Plan 03 wiring).

3. **`ChildCRDSpec.SharedContext`** (pkg/dispatch/childcrd.go, after `SourcePath`) — orchestrator-set carry field, NOT LLM-authored; materializer copies it into each typed child Spec mirroring the SourcePath → PromptPath precedent (Plan 03 wiring).

4. **TaskSpec / PhaseSpec / PlanSpec / MilestoneSpec SharedContext** — uniform `+optional` omitempty string on all four CRD spec types. MilestoneSpec field is vestigial (no parent planner above Project) but kept uniform to avoid a level-conditional branch (D-07). All four CRD YAMLs under `config/crd/bases/` regenerated.

## Tests

Four new tests in `pkg/dispatch/envelope_test.go`:

- `TestEnvelopeIn_SharedContext_OmittedWhenEmpty` — empty `""` does not appear in JSON (omitempty proof)
- `TestEnvelopeIn_SharedContext_RoundTrip` — non-empty value round-trips byte-identically
- `TestEnvelopeOut_SharedContext_RoundTrip` — EnvelopeOut with ChildCRDs + SharedContext round-trips
- `TestChildCRDSpec_SharedContext_RoundTrip` — ChildCRDSpec SharedContext round-trips

TDD RED → GREEN flow followed: tests written first (build failure confirmed), then fields added.

## Commits

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | SharedContext on EnvelopeIn, EnvelopeOut, ChildCRDSpec + tests | 4a103e4 | envelope.go, childcrd.go, envelope_test.go |
| 2 | SharedContext on all four CRD spec types + regenerate manifests | 63e741a | task_types.go, phase_types.go, plan_types.go, milestone_types.go, 4 CRD YAMLs |

## Verification

- `go test ./pkg/dispatch/... ./api/...` — green (all existing + 4 new tests pass)
- `go build ./...` — green
- `grep -c 'SharedContext string ...' pkg/dispatch/envelope.go` — 2 (EnvelopeIn + EnvelopeOut)
- `grep -c 'sharedContext' config/crd/bases/*.yaml` — 4 matches (task/phase/plan/milestone)
- TerminationStub has 0 SharedContext references (< 4 KB invariant preserved)
- No provider markers added (`grep -nE "anthropic|openai|cache_control" pkg/dispatch/envelope.go` — pre-existing doc comments only, zero new matches)
- No MinLength or CEL validation on any SharedContext field

## Deviations from Plan

None — plan executed exactly as written.

## Known Stubs

None — this plan adds struct fields only. No data population occurs in this plan; that wiring is deferred to Plan 03 (controller stamp) and Plan 02 (template rendering).

## Threat Flags

None — no new network endpoints, auth paths, file access patterns, or schema changes at trust boundaries beyond those explicitly modeled in the plan's threat register (T-20-01-01, T-20-01-02).

## Self-Check: PASSED

- [x] `pkg/dispatch/envelope.go` — exists, contains 2x `SharedContext string \`json:"sharedContext,omitempty"\``
- [x] `pkg/dispatch/childcrd.go` — exists, contains SharedContext field
- [x] `pkg/dispatch/envelope_test.go` — exists, 4 new SharedContext tests
- [x] `api/v1alpha1/task_types.go` — SharedContext field added
- [x] `api/v1alpha1/phase_types.go` — SharedContext field added
- [x] `api/v1alpha1/plan_types.go` — SharedContext field added
- [x] `api/v1alpha1/milestone_types.go` — SharedContext field added
- [x] `config/crd/bases/tideproject.k8s_{tasks,phases,plans,milestones}.yaml` — sharedContext in CRD YAML
- [x] Commit 4a103e4 exists (Task 1)
- [x] Commit 63e741a exists (Task 2)
