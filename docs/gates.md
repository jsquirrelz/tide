# TIDE Gates

Operator vocabulary and reconciler contract for every human-or-policy
checkpoint in the TIDE hierarchy. Phase 12 updates the approve semantics
(D-01, D-04) and the reject/resume contract (D-05, D-06).

## What a gate is

A **gate** is a human-or-policy checkpoint that an up-stack reconciler
(Milestone, Phase, Plan) consults before dispatching children. Gates are
declared in `Project.Spec.gates`:

```yaml
apiVersion: tideproject.k8s/v1alpha1
kind: Project
metadata:
  name: hello-tide
spec:
  gates:
    milestone: approve  # halt every milestone until `tide approve` annotates it
    phase:     auto     # advance phases without manual approval
    plan:      auto
```

Gate values:

| Value     | Semantics                                                                  |
| --------- | -------------------------------------------------------------------------- |
| `auto`    | Reconciler dispatches children immediately after the level's artifact is authored (default). |
| `approve` | Reconciler parks at `Status.Phase=AwaitingApproval` — children are materialized but held; operator runs `tide approve <project>` to unblock dispatch. |
| `pause`   | Reconciler halts after the artifact is authored; operator runs `tide resume <project>` to release. |

`Plan.Spec.gates` overrides the Project-level default for a specific plan,
e.g. for plans that need extra scrutiny.

## Operator vocabulary

| Verb                                      | Effect                                                                                          |
| ----------------------------------------- | ----------------------------------------------------------------------------------------------- |
| `tide approve <project> [--wave plan/N]`  | Writes the canonical `approve` annotation; reconciler lifts `AwaitingApproval`, returns the level to `Running` with an `ApprovedByUser` condition, and dispatches children. |
| `tide reject <project> [--reason "..."]`  | Parks the Project — sets a `Rejected` condition and halts further dispatch; children pause where they are. Does not write `Status.Phase=Failed` (`Failed` is reserved for real failures). |
| `tide resume <project>`                   | Lifts a reject park (clears the annotation). With `--retry-failed`, also resets `Status.Phase` on genuinely `Failed` levels so reconcilers re-dispatch and record `ResumedByUser`. The flag is deliberate friction — legitimately dead work is not resurrected by accident. |
| `tide cancel <project> --force`           | Cascading delete (`PropagationPolicyForeground`) of Project + all owned children.               |

The dashboard surfaces all four as **copy-to-clipboard** buttons (DASH-05:
read-only end-to-end). No mutation endpoints on the dashboard backend.

## Annotation handshake

The verbs above write canonical annotation keys; the reconcilers (Phase 4
plans 04-04 / 04-05) read + consume them via `gates.Consume*` helpers
under `internal/gates/`. The handshake is **one-shot**: the reconciler
patches the annotation off after consuming it so a future operator
re-annotation (e.g. after a `pause`-then-resume cycle) triggers a fresh
gate evaluation.

| Annotation key                                              | Writer            | Reader (consumer)                |
| ----------------------------------------------------------- | ----------------- | -------------------------------- |
| `tideproject.k8s/approve`                                   | `tide approve`    | up-stack reconciler              |
| `tideproject.k8s/approve-wave/<plan>/<wave>`                | `tide approve --wave plan/N` | up-stack reconciler   |
| `tideproject.k8s/reject`                                    | `tide reject`     | every reconciler (halt check)    |

Phase 5 expands this table with the full set of operator-driven
annotations (retry, bypass-push-lease, force-push, etc.) once those verbs
ship beyond Phase 3.

## End-to-end approve flow

1. Operator applies a Project with `spec.gates.milestone=approve`.
2. Reconciler authors the Milestone's artifact (the planner Job completes).
   Before dispatching child Phases, the gate hook checks
   `gates.policy(project, "milestone")`, reads `approve`, and patches
   `Milestone.Status.Phase=AwaitingApproval` with a
   `WaveOrLevelPaused=True/AwaitingApproval` condition. Child Phase CRDs
   are materialized by the reporter (operator sees the planned children in
   the dashboard and via `kubectl`) but child reconcilers hold all planner
   Job dispatch while the parent is parked — zero spend during review.
3. Operator runs `tide approve hello-tide` (or clicks the dashboard's
   copy-to-clipboard "Approve" button and pastes into a shell).
4. `cmd/tide approve` discovers the first child level whose
   `Status.Phase=AwaitingApproval` (iteration order: Milestone → Phase →
   Plan → Task — dependency-ordered), patches the `approve` annotation
   onto it.
5. Reconciler's next loop reads the annotation via `gates.ConsumeApprove`
   (one-shot), removes the annotation from the object's metadata, and
   patches `Status.Phase=Running` along with a permanent
   `WaveOrLevelPaused=False/ApprovedByUser` condition. Succession to
   `Succeeded` happens only after all children complete via the
   ChildCount-gated boundary check — approval never jumps a level past its
   incomplete children. The Project condition clears once the Milestone
   reaches `Succeeded` through that path.

## Reject flow

`tide reject` is **distinct from cancel** — reject **parks** the Project at
the next reconciler tick but preserves all state. Reconcilers set a
`Rejected` condition (`Reason=RejectedByUser`) and halt further Job
dispatch; in-flight Jobs drain to completion. No `Status.Phase=Failed`
is written — `Failed` is reserved for real planner or executor failures.

Recovery: operator fixes the underlying issue and runs `tide resume`, which
clears the reject annotation. All previously-running levels return to
dispatching children as if the reject never occurred (conditions cleared
on next reconcile).

### Recovering Failed levels

Genuinely `Failed` levels — where a planner or executor Job returned
non-zero — are a separate case from a rejected Project. Use:

```
tide resume <project> --retry-failed
```

This clears `Status.Phase` and conditions on every `Failed` level, causing
reconcilers to re-dispatch and record a `ResumedByUser` condition. The
`--retry-failed` flag is deliberate friction: it will not resurrect levels
that were merely paused by a reject.

**Legacy CRs** (run-1 CRs pre-dating the `tideproject.k8s/project` label
introduced in Phase 15 CUTS-01) lack the label that `tide resume` uses to
enumerate children. Reset them directly:

```
kubectl patch <kind> <name> --subresource=status --type=merge \
  -p '{"status":{"phase":"","conditions":[]}}'
```

The reconciler re-dispatches on the next watch event and sets
`ResumedByUser`.

`tide approve` against a `Failed` level prints the planner failure reason
and directs to `tide resume --retry-failed` — approval is not a
spend-retry path (D-07).

## What's coming

Phase 5 expands this doc with:

- Per-level gate-policy precedence rules (Project vs Plan)
- Decision table for the full transition:
  `Running` → `AwaitingApproval` → `Running` (+`ApprovedByUser` condition) → `Succeeded`
  (approval returns the level to Running; Succeeded fires only via ChildCount-gated succession)
- Reconciler-side patterns for adding new gate types (a how-to guide
  for operators writing custom gates)
- Auditing gate decisions via `tide_gate_evaluations_total` metric
