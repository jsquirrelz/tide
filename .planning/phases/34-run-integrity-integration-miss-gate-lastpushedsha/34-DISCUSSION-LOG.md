# Phase 34: Run Integrity — Integration-Miss Gate + lastPushedSHA - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-03
**Phase:** 34-Run Integrity — Integration-Miss Gate + lastPushedSHA
**Areas discussed:** Integration topology, Gate mechanics, Miss remediation, Operator surface

---

## Todo Fold (pre-discussion)

| Option | Description | Selected |
|--------|-------------|----------|
| Fold it (Recommended) | Tagged resolves_phase: 34; carries the only concrete repro evidence | ✓ (conditional) |
| Note as reviewed only | Reference without folding content | |

**User's choice:** "Safe to fold in if it doesn't contain any private company or PII data."
**Notes:** Verified before folding — the todo names TIDE task UIDs, a commit SHA, one generic file path, and "local minikube"; no company name, repo URL, or PII; content already committed in this repo. Folded.

---

## Integration topology

| Option | Description | Selected |
|--------|-------------|----------|
| Both layers (Recommended) | Per-wave integration Jobs for every wave incl. final + boundary-push cumulative idempotent re-merge | ✓ |
| Per-wave Jobs only | Every wave gets a Job; boundary push becomes push-only | |
| Boundary-push only | Keep last-wave skip; make boundary-push integration authoritative | |

**User's choice:** Both layers

| Option | Description | Selected |
|--------|-------------|----------|
| Single-flight gate (Recommended) | Controller lists in-flight integration/push Jobs project-wide before dispatch; requeue if active (D3 count-gate pattern) | ✓ |
| Chained Job sequence | Monotonic Job names + flock-wait ordering on the PVC side | |
| You decide | Defer to research/planning | |

**User's choice:** Single-flight gate

| Option | Description | Selected |
|--------|-------------|----------|
| Every trigger carries set (Recommended) | Cumulative Succeeded-branch set computed in shared triggerBoundaryPush for all levels | ✓ |
| Verify gate catches it | Leave triggers as-is; gate + remediation absorb the race | |
| You decide | Defer to planning | |

**User's choice:** Every trigger carries set

| Option | Description | Selected |
|--------|-------------|----------|
| Bounded retry (Recommended) | Mirror boundary-push retry state machine for wave-integration Jobs | ✓ |
| Keep fail-fast | First integration Job failure fails the Plan | |
| You decide | Defer pending envelope failure taxonomy | |

**User's choice:** Bounded retry

---

## Gate mechanics

| Option | Description | Selected |
|--------|-------------|----------|
| Every push (Recommended) | All four levels verify before pushing | ✓ |
| Plan + project only | Gate only pushes following task execution and the final pre-Complete push | |
| You decide | Defer until verifier placement settled | |

**User's choice:** Every push

| Option | Description | Selected |
|--------|-------------|----------|
| Inside tide-push (Recommended) | One Job: integrate → verify → push; nonzero exit + typed envelope reason on miss | ✓ |
| Separate verify Job | Dedicated verify Job before push dispatch | |
| You decide | Defer to research on envelope plumbing | |

**User's choice:** Inside tide-push

| Option | Description | Selected |
|--------|-------------|----------|
| Project-wide Succeeded (Recommended) | Controller lists every Succeeded Task CR owned by the project at dispatch time | ✓ |
| Scoped to the triggering level | Plan push checks its plan; higher levels check their subtree | |
| You decide | Defer, noting arg-size limits | |

**User's choice:** Project-wide Succeeded

**Notes:** Recorded without asking — INTEG-03 pins `merge-base --is-ancestor` as the predicate; empty-diff Succeeded tasks pass naturally; no per-task merge-commit bookkeeping.

---

## Miss remediation

| Option | Description | Selected |
|--------|-------------|----------|
| Retry then stick (Recommended) | Miss fails Job with typed reason; #13b bounded retry re-integrates/re-verifies; sticky condition only after cap | ✓ |
| Stick immediately | First miss parks the push | |
| You decide | Defer pending failure taxonomy | |

**User's choice:** Retry then stick

| Option | Description | Selected |
|--------|-------------|----------|
| Classify, don't retry (Recommended) | Conflicts get a distinct envelope reason, skip retries, park immediately | ✓ |
| Treat uniformly | All failures ride bounded retry to cap | |
| You decide | Defer to conflict-detection feasibility check | |

**User's choice:** Classify, don't retry

| Option | Description | Selected |
|--------|-------------|----------|
| Plan fails (Recommended) | Same-wave conflict = defective plan; Plan Failed with condition naming both branches; recovery via resume after replanning | ✓ |
| Park for manual merge | Operator resolves on the PVC + annotation clear | |
| You decide | Defer to Responsibility A semantics review | |

**User's choice:** Plan fails

**Notes:** Carried forward without asking — per locked #13b decision, Complete still stamps; the push is what parks.

---

## Operator surface

| Option | Description | Selected |
|--------|-------------|----------|
| Project + Plan split (Recommended) | integration-incomplete on Project (beside BoundaryPushed); conflict conditions on the failing Plan | ✓ |
| Project only | Everything on the Project | |
| You decide | Defer to condition-taxonomy alignment | |

**User's choice:** Project + Plan split

| Option | Description | Selected |
|--------|-------------|----------|
| Named + metric (Recommended) | Condition names each missing task+branch (bounded); result-labeled counter beside PushJobsTotal | ✓ |
| Count + envelope pointer | Count only; detail stays on the PVC | |
| You decide | Defer to etcd size discipline check | |

**User's choice:** Named + metric

| Option | Description | Selected |
|--------|-------------|----------|
| tide resume verb (Recommended) | Extend tide resume to reset push attempts via the existing annotation mechanism; auto-clear on later success | ✓ |
| Annotation only | kubectl annotate directly; no CLI change | |
| You decide | Defer to resume plumbing check | |

**User's choice:** tide resume verb

**Notes:** Noted without asking — dashboard display of the new condition/lastPushedSHA belongs to Phase 37 (DASH-03).

---

## Claude's Discretion

- flock lockfile placement/naming inside the Job
- Branch-list handoff format (args vs file vs ConfigMap)
- Retry cap sizing, condition/reason naming, metric labels
- kind-suite repro harness shape (single-wave degenerate case + 2-parallel-task final-wave case)

## Deferred Ideas

- Dashboard display of integration-incomplete / lastPushedSHA → Phase 37 (DASH-03)
- LLM verify-tier subagents → STAGE-01, own milestone
- Manual conflict-resolution protocol on the PVC → rejected for v1; revisit only if Plan-fails-on-conflict proves too blunt
