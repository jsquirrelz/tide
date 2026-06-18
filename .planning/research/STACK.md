# Stack Research — TIDE v1.0.3 Planning Resumption & Cost Resilience

**Domain:** Envelope-resumption and budget-bypass fixes for a mature Go/Kubernetes operator
**Researched:** 2026-06-18
**Confidence:** HIGH — all findings derived from the live codebase, go.mod, Go stdlib, and the salvaged dogfood PVC envelopes. No new external libraries proposed.

This file covers ONLY the stack questions specific to v1.0.3. The full prior stack (controller-runtime, Ginkgo, Prometheus, OTel, goldie, etc.) is validated and unchanged from prior milestones.

---

## Context — The Specific Problem

Dogfood run #2 budget-halted mid-planning (~$90, zero execution). The full plan tree survived on the PVC:

```
examples/projects/dogfood/salvage-20260618/pvc-envelopes/envelopes/<old-uid>/
  in.json        (carries dispatch.parentName + level — the stable identity)
  out.json       (the authored EnvelopeOut — the artifact we want to reuse)
  children/      (authored child CRD specs)
  MILESTONE.md   (authored artifact)
  events.jsonl   (replay log)
```

59 envelopes total: 3 milestone, 15 phase, 39 plan, 1 project.

The problem is that `envelopes/` is keyed by the K8s UID of the dispatched CRD object (`in.json: taskUID`), and a fresh `kubectl apply` on the same YAML assigns **new** UIDs to every CRD. The existing `FilesystemEnvelopeReader.ReadOut(ctx, projectUID, taskUID)` cannot find old envelopes because the new run's task UIDs differ from the salvage UIDs.

This research answers:
1. What scheme allows stable-key envelope lookup across UID churn?
2. Does the planner-skip idempotency pattern require any new library?
3. What hashing or identity primitives (if any) are needed?

---

## Recommended Stack

### Core Technologies — No Changes

| Technology | Version | Purpose | v1.0.3 Role |
|------------|---------|---------|-------------|
| Go stdlib: `path/filepath` | Go 1.26 | File path construction | Already used in `FilesystemEnvelopeReader`; extended for stable-key path |
| Go stdlib: `encoding/json` | Go 1.26 | JSON marshal/unmarshal | Already used throughout; validates adopted envelopes |
| Go stdlib: `crypto/sha256` | Go 1.26 | Digest for integrity check | Already imported in `internal/credproxy/token.go`; optional write-side checksum |
| controller-runtime v0.24.1 | locked | Reconcile loop, patch | Existing `AlreadyExists`-is-success pattern extends naturally to planner-skip |

### Supporting Libraries — No New Additions

| Library | Status | Assessment |
|---------|--------|------------|
| `github.com/cespare/xxhash/v2 v2.3.0` | Already indirect dep | Available at zero cost if a non-crypto hash is preferred. Not needed — name-based identity requires no hashing at all. |
| Any CAS library (`ipfs/go-cid`, `opencontainers/go-digest`) | NOT in go.mod | Do not add. The PVC directory structure is already the content store; a full block-level CAS library solves a problem TIDE does not have. |
| Any external resume-state library | NOT in go.mod | Do not add. Violates spec constraint: resumption state = indegree map + completed-task set, rederivable from artifacts. |

---

## Pattern 1: Name-Based Stable-Key Envelope Directory (PRIMARY RECOMMENDATION)

### What

Add a parallel stable-key shadow path alongside the existing UID-keyed path:

```
# Existing (live-run correlation, unchanged):
/workspaces/{projectUID}/workspace/envelopes/{crdUID}/out.json

# New (stable-key, resume lookup):
/workspaces/{projectUID}/workspace/envelopes-by-name/{level}/{crdName}/out.json
```

The orchestrator **writes to both paths** when a planner Job completes. On a fresh run, the reconciler **checks the stable-key path before dispatching** a new planner Job. If a valid, validated `out.json` exists there, it adopts the envelope and skips the planner.

### Why name-based, not content-addressed (prompt hash)

The salvaged `in.json` files carry `dispatch.parentName` (the CRD name, e.g., `milestone-01-codex-subagent`) and `level` (e.g., `milestone`). CRD names are human-authored, stable across re-applies, unique within a namespace and level. This is the available stable identity.

Hashing the outcome prompt would be wrong for two reasons:

1. **Edit-after-halt risk.** If the operator edits the outcome prompt between runs (common after a budget halt to adjust scope), a hash match would silently adopt a stale envelope authored from a different prompt. Name-based identity makes the adoption intent explicit.
2. **Hash derivation coupling.** Correct prompt hashing would require re-running the same prompt-derivation logic the controller uses at dispatch time. That couples the import tool to internal dispatch code and is fragile if the derivation changes.

SHA-256 of `in.json` would work as a tie-breaker or integrity check *within* the stable-key directory, but not as the primary identity.

### Stable-key path constructor (stdlib-only)

```go
// pkg/dispatch/envelope_paths.go (new file)

// StableKeyEnvelopePath returns the resume-lookup path for a pre-authored
// planner envelope keyed by level + CRD name rather than UID.
// Format: {workspaceRoot}/{projectUID}/workspace/envelopes-by-name/{level}/{crdName}/out.json
func StableKeyEnvelopePath(workspaceRoot, projectUID, level, crdName string) string {
    return filepath.Join(workspaceRoot, projectUID, "workspace",
        "envelopes-by-name", level, crdName, "out.json")
}
```

No new imports. `filepath` is already used in `FilesystemEnvelopeReader`.

### `ReadByName` method on FilesystemEnvelopeReader

Extend the existing `EnvelopeReader` interface with a second method (or add a separate `StableKeyReader` interface to avoid breaking existing implementations):

```go
// ReadByName reads the stable-key envelope for a CRD identified by level + name.
// Returns (EnvelopeOut, true) if a valid envelope exists; (zero, false) if not found
// or if validation fails. Never returns an error for missing files — missing = no
// prior run, not a hard failure.
ReadByName(ctx context.Context, projectUID, level, crdName string) (pkgdispatch.EnvelopeOut, bool)
```

The method reads `StableKeyEnvelopePath(...)`, unmarshals, then validates with the existing `ValidateAPIVersionKind` + `ExitCode == 0` + `len(ChildCRDs) > 0` checks. On any validation failure it returns `(zero, false)` — falling through to normal dispatch.

### Validation before adopting

All four conditions must hold:

1. `pkg/dispatch.ValidateAPIVersionKind` returns nil (existing function).
2. `out.ExitCode == 0` — non-zero means the previous run's planner failed; re-author.
3. `len(out.ChildCRDs) > 0` for non-leaf levels (milestone, phase, plan). Zero children at a level that must produce children indicates a corrupt or incomplete envelope.
4. All `out.ChildCRDs[i].Name` values are non-empty (basic format guard; no K8s API call needed here).

If any check fails, fall through to normal planner dispatch. The stable-key file is left in place; it will be overwritten if the new run completes successfully.

### Write path (on planner Job completion)

The existing `handleJobCompletion` in each planner reconciler writes nothing to the PVC directly — it reads from the PVC via `FilesystemEnvelopeReader.ReadOut`. The stable-key write should happen at the same moment:

```go
// After reading and validating out.json from the UID-keyed path:
if writeErr := r.EnvReader.WriteStableKey(ctx, project.UID, level, crdName, out); writeErr != nil {
    logger.Error(writeErr, "stable-key envelope write failed (non-fatal)")
    // Non-fatal: the stable-key path is a resume convenience, not required
    // for the current run's correctness.
}
```

The `WriteStableKey` implementation is `os.MkdirAll` + `os.WriteFile` — both already used in the podjob package.

### Integration point in the reconcile loop

The stable-key check belongs in the same early-return position as the existing child-existence idempotency guard (line 304 of `milestone_controller.go`), placed **after** the terminal/AwaitingApproval/Running short-circuits and **before** pool acquisition and Job creation:

```go
// Step 2b: Stable-key envelope adoption guard — skip planner dispatch when a
// pre-authored valid envelope exists for this CRD name.
if r.EnvReader != nil {
    if out, ok := r.EnvReader.ReadByName(ctx, projectUID, "milestone", ms.Name); ok {
        return r.adoptEnvelopeAndMaterialize(ctx, ms, out)
    }
}
// Fall through: normal planner dispatch.
```

`adoptEnvelopeAndMaterialize` is a new method that:
1. Stamps the `tideproject.k8s/envelope-adopted-from` annotation (see Pattern 2 below).
2. Materializes child CRDs from `out.ChildCRDs` exactly as `handleJobCompletion` does (the existing materialization path is already factored separately and can be called directly).
3. Does NOT dispatch a planner Job.

---

## Pattern 2: CRD Annotation as the Adoption Signal

When a stable-key envelope is adopted (planner skipped), stamp an annotation on the CRD:

```
tideproject.k8s/envelope-adopted-from: envelopes-by-name/milestone/milestone-01-codex-subagent
```

**Why this matters:**

1. Makes the adoption visible in `kubectl describe` without a dashboard lookup.
2. The existing child-existence idempotency guard (line 304, `milestone_controller.go`) checks for children via `spec.milestoneRef` list scan. After adoption, children are materialized synchronously — the guard would find them and return on the next reconcile without re-dispatching. The annotation is belt-and-suspenders: it provides a cheap early return before the List scan.
3. Prevents a re-entrant reconcile from dispatching a planner for a CRD whose children were materialized from an adopted envelope, not from a completed Job.

**Implementation:** standard `r.Patch(ctx, obj, client.MergeFrom(base))` on `metadata.annotations`. This is the exact pattern used for `AnnotationBillingResumedAt` and `AnnotationFailureResumedAt` in `api/v1alpha2/shared_types.go`. No new library.

**Annotation constant:**

```go
// api/v1alpha2/shared_types.go (add alongside existing annotation constants)
AnnotationEnvelopeAdoptedFrom = "tideproject.k8s/envelope-adopted-from"
```

---

## Pattern 3: `tide import-envelopes` CLI Subcommand

The operator import use case (copying salvaged envelopes to stable-key paths before re-applying the Project CR) needs a concrete workflow. Recommend a `tide import-envelopes` subcommand:

```
tide import-envelopes \
  --salvage-dir examples/projects/dogfood/salvage-20260618/pvc-envelopes \
  --pvc-mount /workspaces \
  --project-uid <new-project-uid>
```

**Algorithm (stdlib-only):**

```
for each <uid>/ in salvage-dir/envelopes/:
    read in.json → extract level, dispatch.parentName (= crdName)
    read out.json → validate (ExitCode==0, ChildCRDs non-empty)
    if valid:
        target = StableKeyEnvelopePath("/workspaces", new-project-uid, level, crdName)
        os.MkdirAll(dir(target), 0755)
        os.WriteFile(target, out_json_bytes, 0644)
        also copy children/*.json alongside out.json
```

The salvage `in.json` files already carry `dispatch.parentName` in the `dispatch` struct field (confirmed from `examples/projects/dogfood/salvage-20260618/pvc-envelopes/envelopes/045b44c8.../in.json`). The field is populated by `DispatchMeta.ParentName` in `pkg/dispatch/envelope.go`. No new parsing infrastructure needed.

---

## Budget-Bypass Resume Correctness: No New Stack

The `project_controller.go:1257` bypass-clears-to-Pending bug requires changing `PhasePending` → `PhaseRunning` in `handleBudgetGate`. No new library. The fix is a one-line constant swap in the existing status-patch block.

The cap-raise ergonomics improvement (raising absolute cap should not leave rolling cap re-halting) requires a guard in `handleBudgetGate` that checks both caps simultaneously before clearing `BudgetExceeded`. Again, pure logic in existing code, no new dependency.

---

## What NOT to Add

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| Any content-addressed storage library | Name-based identity suffices; CAS adds complexity without solving the edit-after-halt problem | `filepath.Join` with stable-key directory |
| Prompt hashing as primary identity | Breaks when operator edits outcome prompt between runs | CRD name (`dispatch.parentName` from `in.json`) |
| UID-rewrite import (copy old-uid → new-uid) | Requires live cluster + two-phase apply-then-copy; race window; not idempotent | Stable-key directory (offline, idempotent) |
| New K8s ConfigMap/Secret per envelope | Adds etcd objects, violates no-extra-persistence constraint, hits 1.5 MiB etcd limit at scale | PVC filesystem + CRD annotation |
| External resume-state database | Violates spec constraint: resumption state = indegree map + completed-task set only | PVC files + rederivable from artifacts |
| Merkle tree or DAG diff library | Envelope adoption is per-object (one `out.json` per CRD name), not a block tree | Single-file JSON read + validate |

---

## go.mod Impact

**Zero new `require` entries.**

All implementation uses:
- `path/filepath` (stdlib, already used in `FilesystemEnvelopeReader`)
- `encoding/json` (stdlib, already used throughout)
- `crypto/sha256` (stdlib, already in `internal/credproxy/token.go`) — optional for the write-side integrity checksum
- Standard `os.ReadFile` / `os.WriteFile` / `os.MkdirAll` (stdlib)
- `github.com/cespare/xxhash/v2` is already an indirect dep and could be used for non-crypto fingerprinting, but is not needed

No production code changes require a new module.

---

## Summary

| Question | Answer |
|----------|--------|
| How to find a pre-authored envelope after UID churn? | Stable-key directory `envelopes-by-name/{level}/{crdName}/out.json` written at planner completion, checked before dispatch |
| What identity scheme? | CRD name (from `in.json: dispatch.parentName`) — not prompt hash, not UID-rewrite |
| What hashing libraries needed? | None for identity. `crypto/sha256` (already present) for optional integrity checksum only |
| Does planner-skip need new controller-runtime patterns? | No. It is a pre-dispatch early-return mirroring the existing child-existence idempotency guard at `milestone_controller.go:304` |
| New go.mod entries? | Zero |
| New API surface? | `ReadByName` on `EnvelopeReader` interface + `WriteStableKey` method + `AnnotationEnvelopeAdoptedFrom` constant + `tide import-envelopes` subcommand |

---

## Sources

- `internal/dispatch/podjob/backend.go` — `FilesystemEnvelopeReader.ReadOut` reads `WorkspaceRoot/{projectUID}/workspace/envelopes/{taskUID}/out.json` (HIGH confidence, live code)
- `internal/dispatch/podjob/names.go:SUB-03` — `AlreadyExists`-is-idempotent-success, the canonical TIDE idempotency model (HIGH confidence, live code)
- `internal/controller/milestone_controller.go:304` — existing child-existence idempotency guard; the planner-skip mirrors this structure exactly (HIGH confidence, live code)
- `pkg/dispatch/envelope.go` — `ValidateAPIVersionKind`, `EnvelopeOut`, `DispatchMeta.ParentName` field (HIGH confidence, live code)
- `examples/projects/dogfood/salvage-20260618/pvc-envelopes/envelopes/045b44c8.../in.json` — confirms `dispatch.parentName` carries the CRD name (`milestone-01-codex-subagent`), `level` carries `milestone` (HIGH confidence, live salvage data)
- `go.mod` — `github.com/cespare/xxhash/v2 v2.3.0` is already an indirect dep; no new `require` entries needed (HIGH confidence, live file)
- `api/v1alpha2/shared_types.go:216,253` — annotation constant pattern for `AnnotationBillingResumedAt` / `AnnotationFailureResumedAt` (HIGH confidence, live code)
- Go stdlib documentation — `path/filepath`, `crypto/sha256`, `os`, `encoding/json` (HIGH confidence, stdlib)

---
*Stack research for: TIDE v1.0.3 — Planning Resumption & Cost Resilience*
*Researched: 2026-06-18*
