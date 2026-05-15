---
phase: 03-up-stack-reconcilers-git-integration-real-subagent-resumptio
plan: 01
subsystem: pkg/dispatch
tags: [envelope-schema, provider-pluggable, child-crd-spec, foundation]
requires: []
provides:
  - "pkg/dispatch.ProviderSpec — Vendor/Model/Params triple every dispatch carries (D-C3)"
  - "pkg/dispatch.ChildCRDSpec — typed child-CRD declaration emitted by planner subagents (D-A1)"
  - "pkg/dispatch.GitOutput — git-side EnvelopeOut sub-struct (HeadSHA for --force-with-lease)"
  - "EnvelopeIn.Provider — value type on every envelope"
  - "EnvelopeOut.ChildCRDs []ChildCRDSpec — authoritative source for server-side child-CRD materialization"
  - "EnvelopeOut.Git *GitOutput — push-Job + executor-harness output channel"
  - "Usage.CacheReadTokens + Usage.CacheCreationTokens — Anthropic stream-json cache_*_input_tokens passthrough (D-C5)"
affects:
  - "Stream A (up-stack reconcilers) — reads Provider/Role/Level, materializes ChildCRDs"
  - "Stream B (push Job) — writes Git.HeadSHA on push success"
  - "Stream C (Claude subagent) — Provider.Vendor sentinel check + cache-token usage capture"
  - "Stream D (chaos-resume) — envelope shape is the contract Plans 03-09/03-10 rely on"
tech_added:
  - "k8s.io/apimachinery/pkg/runtime (transitive — for runtime.RawExtension in ChildCRDSpec.Spec)"
patterns:
  - "Additive schema bump under v1alpha1 (no apiVersion rev; ValidateAPIVersionKind body unchanged)"
  - "Optional struct fields use pointer + omitempty (Git *GitOutput follows Dev *Dev convention)"
  - "Optional slice fields use omitempty (ChildCRDs []ChildCRDSpec follows DependsOn []string convention)"
  - "Value-type required fields (ProviderSpec on EnvelopeIn) follow Caps Caps convention"
  - "runtime.RawExtension as the typed-but-deferred-decode escape hatch (avoids pkg/dispatch → api/v1alpha1 dependency arrow inversion)"
key_files:
  created:
    - "pkg/dispatch/provider.go — ProviderSpec type + D-C3 godoc"
    - "pkg/dispatch/childcrd.go — ChildCRDSpec type + D-A1 godoc + T-308 consumer-side allowlist note"
    - "pkg/dispatch/provider_test.go — 3 round-trip + omitempty + required-field tests"
    - "pkg/dispatch/childcrd_test.go — 3 round-trip + empty-kind + nested-JSON tests"
    - ".planning/phases/03-up-stack-reconcilers-git-integration-real-subagent-resumptio/deferred-items.md — out-of-scope discoveries (envtest binaries)"
  modified:
    - "pkg/dispatch/envelope.go — added Provider field on EnvelopeIn; ChildCRDs + Git fields on EnvelopeOut; CacheReadTokens + CacheCreationTokens on Usage; GitOutput type"
    - "pkg/dispatch/envelope_test.go — extended assertRoundTrip helpers + fixtures + 7 new Phase 3 tests"
    - "pkg/dispatch/doc.go — narrowed SUB-01 import firewall to allow k8s.io/apimachinery (Rule 3 deviation)"
    - "Makefile — narrowed verify-dispatch-imports allowlist for apimachinery transitive closure (Rule 3 deviation)"
decisions:
  - "Kept GitOutput type in pkg/dispatch/envelope.go (one-of-many planner-acceptable locations) — small struct tightly coupled to EnvelopeOut.Git, splitting to git.go would scatter the EnvelopeOut contract across two files for one struct."
  - "Narrowed SUB-01 import firewall to permit k8s.io/apimachinery/pkg/runtime (and its required transitive closure) inside pkg/dispatch — the plan explicitly commits to runtime.RawExtension as ChildCRDSpec.Spec type, which is incompatible with the original 'no k8s.io/* at all' wording. sigs.k8s.io/controller-runtime/* and github.com/anthropics/* remain forbidden so the load-bearing properties (no controller-runtime in out-of-tree subagent images, no LLM SDK lock-in) are preserved."
  - "Did NOT add validation methods on ProviderSpec — D-C3 explicitly puts the Vendor-mismatch fail-fast check inside internal/subagent/{vendor}/, NOT here."
metrics:
  duration: "~9 min"
  tasks_completed: 2
  files_created: 5
  files_modified: 4
  tests_added: 13
  completed: "2026-05-15"
---

# Phase 03 Plan 01: pkg/dispatch envelope schema bump — Summary

Phase 3 foundation. Adds `ProviderSpec`, `ChildCRDSpec`, and `GitOutput` types to `pkg/dispatch`, then extends `EnvelopeIn` with `Provider` (value type) and `EnvelopeOut` with `ChildCRDs`/`Git`/cache-token `Usage` fields — the contract Streams A/B/C/D of Phase 3 all consume. Additive only under `tideproject.k8s/v1alpha1`; `ValidateAPIVersionKind` body unchanged; Phase 2 envelope round-trip + apiVersion-rejection tests still PASS. Two TDD cycles, four commits.

## What Shipped

**ProviderSpec** (`pkg/dispatch/provider.go`):

```go
type ProviderSpec struct {
    Vendor string            `json:"vendor"`
    Model  string            `json:"model"`
    Params map[string]string `json:"params,omitempty"`
}
```

Value type carried on every `EnvelopeIn`. Vendor/Model are orthogonal axes — `Project.Spec.subagent.levels.{level}.{vendor,model,params}` → Project-level defaults → Helm-chart defaults resolution stamps the resolved triple here. `Params` is intentionally `map[string]string` so future per-vendor tuning knobs land without a schema bump.

**ChildCRDSpec** (`pkg/dispatch/childcrd.go`):

```go
type ChildCRDSpec struct {
    Kind string                `json:"kind"`
    Name string                `json:"name"`
    Spec runtime.RawExtension  `json:"spec"`
}
```

Typed child-CRD declaration a planner subagent emits in `EnvelopeOut.ChildCRDs`. The orchestrator (plan 03-08) decodes `Spec.Raw` into the appropriate typed Spec at server-side create time. `runtime.RawExtension` is the K8s-idiomatic escape hatch that keeps `pkg/dispatch` free of any `api/v1alpha1` import (preserves the dependency arrow direction: controllers depend on `pkg/dispatch`, not vice versa). Godoc explicitly notes consumer-side `Kind` allowlist is the T-308 mitigation gate, not the JSON tag.

**EnvelopeIn additions**:

- `Provider ProviderSpec \`json:"provider"\`` — value type, every dispatch declares a provider.
- `Role` + `Level` already present at lines 36-41 (Phase 2 D-A4 baseline) — no change.

**EnvelopeOut additions**:

- `ChildCRDs []ChildCRDSpec \`json:"childCRDs,omitempty"\`` — authoritative source for orchestrator child-CRD materialization (D-A1). Omitted at executor level.
- `Git *GitOutput \`json:"git,omitempty"\`` — pointer + omitempty so dispatches that don't touch git don't serialize `"git": null`. `GitOutput { HeadSHA string \`json:"headSHA"\` }` declared in same file (small struct, tightly coupled to consumer).

**Usage additions** (D-C5 — Anthropic stream-json passthrough):

- `CacheReadTokens int64 \`json:"cacheReadTokens"\`` — `usage.cache_read_input_tokens`.
- `CacheCreationTokens int64 \`json:"cacheCreationTokens"\`` — `usage.cache_creation_input_tokens`.

Cache-read vs cache-creation are billed at different rates; surfacing them separately keeps `Project.Status.budget` (Phase 2 D-D2) from understating real spend on cache-warmup turns.

**Unchanged (verbatim per plan body):**

- `APIVersionV1Alpha1 = "tideproject.k8s/v1alpha1"` — additive bump rides under v1alpha1.
- `ValidateAPIVersionKind` body — Phase 2 D-A3 schema rejection contract preserved.
- `Subagent.Run(ctx, EnvelopeIn) (EnvelopeOut, error)` — Phase 2 D-A1 method signature stable.

## How It Was Built

Two TDD cycles, four commits, plan-author-specified ordering:

| Commit | Phase | Description |
|--------|-------|-------------|
| `64a2836` | RED-1 | Failing tests for `ProviderSpec` + `ChildCRDSpec` (6 tests, fail with `undefined`) |
| `8a22890` | GREEN-1 | `provider.go` + `childcrd.go` types + narrowed SUB-01 firewall |
| `4f1826e` | RED-2 | Failing tests for `EnvelopeIn.Provider` + `EnvelopeOut` Phase 3 fields (7 tests) |
| `8d29ee6` | GREEN-2 | `envelope.go` extensions: Provider/ChildCRDs/Git fields + Usage cache tokens + GitOutput type |

13 new tests total. Existing 8 Phase 2 envelope tests preserved unmodified (fixture-builder functions extended to populate the new fields so they exercise the schema bump too).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 — Blocking issue] Narrowed SUB-01 / DAG-05-mirror import firewall**

- **Found during:** Task 1 GREEN — `make verify-dispatch-imports` failed because `runtime.RawExtension` pulled `k8s.io/apimachinery` and its required transitive closure into `pkg/dispatch`'s transitive deps.
- **Root cause:** The original SUB-01 firewall (Phase 2 `pkg/dispatch/doc.go` + Makefile `verify-dispatch-imports`) forbade *all* `k8s.io/*` imports. Plan 03-01 D-A1 / D-C3 explicitly require `runtime.RawExtension` as `ChildCRDSpec.Spec` type — the two are incompatible at the wording level. PATTERNS.md anticipated this: "`runtime.RawExtension` is the K8s-idiomatic typed-but-deferred-decode escape hatch — keeps `pkg/dispatch` from importing `api/v1alpha1` (which would invert the dependency arrow)."
- **Fix:** Narrowed both the doc.go contract and the Makefile target to allowlist `k8s.io/apimachinery/*`, `k8s.io/klog/*`, `k8s.io/kube-openapi/*`, `sigs.k8s.io/json`, `sigs.k8s.io/structured-merge-diff/*` (the required apimachinery transitive closure). `sigs.k8s.io/controller-runtime/*` and `github.com/anthropics/*` remain forbidden — the load-bearing SUB-01 properties (no controller-runtime in out-of-tree subagent images, no LLM SDK lock-in) are preserved.
- **Files modified:** `pkg/dispatch/doc.go`, `Makefile`.
- **Commit:** `8a22890` (bundled with Task 1 GREEN per plan acceptance criteria — the criteria assume the change is permitted).
- **Why Rule 3 not Rule 4:** The plan body explicitly directs me to add `import "k8s.io/apimachinery/pkg/runtime"` and "verify with `go list -m k8s.io/apimachinery` returns non-error after the change." The plan author already made the architectural decision; this commit is the firewall update that lands in lockstep so the CI gate matches the contract.

## Out-of-Scope Findings (logged, not fixed)

Logged to `.planning/phases/03-up-stack-reconcilers-git-integration-real-subagent-resumptio/deferred-items.md`:

- **envtest binaries missing in fresh worktrees** — `go test ./internal/controller/...` and `go test ./test/integration/envtest/...` fail BeforeSuite because `bin/k8s/` does not exist in this worktree. Confirmed pre-existing by stashing my edits and re-running — same failure on base commit `a9027d9`. Out of scope for `pkg/dispatch`-only plan 03-01. Defer to a worktree-bootstrap hardening pass that runs `make envtest` (or wires `setup-envtest` into `TestMain`).
- **`test/integration/kind` failure** — pre-existing, no kind cluster context in this worktree. Out of scope.

## Acceptance Criteria — Plan-Body Verification

All plan acceptance criteria PASS:

### Task 1

```
grep -nE 'type ProviderSpec struct' pkg/dispatch/provider.go               → 1 hit (line 20)
grep -nE 'type ChildCRDSpec struct' pkg/dispatch/childcrd.go               → 1 hit (line 28)
grep -nE 'runtime\.RawExtension' pkg/dispatch/childcrd.go                  → 2 hits (godoc + Spec field)
grep -nE 'json:"vendor"' pkg/dispatch/provider.go                          → 1 hit (line 24)
grep -nE 'json:"params,omitempty"' pkg/dispatch/provider.go                → 1 hit (line 35)
go vet ./pkg/dispatch/...                                                  → exit 0
go test ./pkg/dispatch/... -run 'TestProviderSpec|TestChildCRDSpec' -count=1 → PASS (6 subtests)
grep -rnE 'api/v1alpha1' pkg/dispatch/*.go                                 → 0 Go imports
```

### Task 2

```
grep -nE 'Provider\s+ProviderSpec\s+`json:"provider"`' pkg/dispatch/envelope.go      → 1 hit (line 84)
grep -nE 'ChildCRDs\s+\[\]ChildCRDSpec\s+`json:"childCRDs,omitempty"`'               → 1 hit (line 140)
grep -nE 'Git\s+\*GitOutput\s+`json:"git,omitempty"`'                                → 1 hit (line 147)
grep -nE 'CacheReadTokens\s+int64'                                                   → 1 hit (line 208)
grep -nE 'CacheCreationTokens\s+int64'                                               → 1 hit (line 216)
grep -nE 'APIVersionV1Alpha1 = "tideproject.k8s/v1alpha1"'                           → 1 hit (line 8 — UNCHANGED)
go test ./pkg/dispatch/... -count=1                                                  → PASS (28 tests total)
go vet ./pkg/dispatch/...                                                            → exit 0
```

### Plan-level

```
go test ./pkg/dispatch/... -count=1   → PASS
go vet ./pkg/dispatch/...             → exit 0
go build ./...                        → exit 0
make verify-dispatch-imports          → OK: pkg/dispatch imports are clean
make verify-import-firewall           → clean (tide-lint multichecker PASS)
```

## Threat Surface Scan

No new threat-relevant surface beyond what `<threat_model>` in 03-01-PLAN.md already declared:

- **T-308** (Tampering / Elevation via `EnvelopeOut.ChildCRDs` RawExtension) — type ships with godoc requiring consumer-side allowlist validation; actual allowlist enforcement is deferred to plan 03-08's `internal/controller/dispatch_helpers.go` per the threat register's `mitigate (deferred to consumer plan 03-08)` disposition.
- **T-307** (Information Disclosure / budget hiding via cache tokens) — mitigated: `CacheReadTokens` + `CacheCreationTokens` now carried on `Usage`; test `TestUsage_CacheTokens` asserts round-trip.

No additional threat flags.

## Known Stubs

None. ChildCRDSpec.Spec being `runtime.RawExtension` is NOT a stub — it is the load-bearing typed-but-deferred-decode escape hatch (PATTERNS.md). The actual decoding happens in plan 03-08; this plan ships the contract only, as the plan body explicitly specifies.

## Self-Check: PASSED

**Created files:**

- `pkg/dispatch/provider.go`                                                                                                                                                  → FOUND
- `pkg/dispatch/childcrd.go`                                                                                                                                                  → FOUND
- `pkg/dispatch/provider_test.go`                                                                                                                                             → FOUND
- `pkg/dispatch/childcrd_test.go`                                                                                                                                             → FOUND
- `.planning/phases/03-up-stack-reconcilers-git-integration-real-subagent-resumptio/deferred-items.md`                                                                        → FOUND

**Commits on this branch:**

- `64a2836` `test(03-01): add failing tests for ProviderSpec + ChildCRDSpec`                          → FOUND
- `8a22890` `feat(03-01): add ProviderSpec + ChildCRDSpec dispatch types`                             → FOUND
- `4f1826e` `test(03-01): add failing tests for EnvelopeIn.Provider + EnvelopeOut Phase 3 fields`     → FOUND
- `8d29ee6` `feat(03-01): extend EnvelopeIn.Provider + EnvelopeOut ChildCRDs/Git/Usage cache tokens`  → FOUND
