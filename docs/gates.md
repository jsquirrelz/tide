# TIDE Gates

Phase 4 stub — Phase 5 expands with a decision-table reference, per-level
escalation examples, and reconciler-side gate-policy patterns. Treat this
as the operator vocabulary for v0.1.x.

## What a gate is

A **gate** is a human-or-policy checkpoint that an up-stack reconciler
(Milestone, Phase, Plan) consults before transitioning a child level into
a Succeeded state. Gates are declared in `Project.Spec.gates`:

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
| `auto`    | Reconciler advances on its own once all child levels succeed (default).    |
| `approve` | Reconciler sets `Status.Phase=AwaitingApproval`; operator runs `tide approve <project>` to unblock. |
| `pause`   | Reconciler halts indefinitely after the level completes; operator runs `tide resume <project>` to advance. |

`Plan.Spec.gates` overrides the Project-level default for a specific plan,
e.g. for plans that need extra scrutiny.

## Operator vocabulary

| Verb                                      | Effect                                                                                          |
| ----------------------------------------- | ----------------------------------------------------------------------------------------------- |
| `tide approve <project> [--wave plan/N]`  | Writes the canonical `approve` annotation; reconciler clears `AwaitingApproval` and advances.   |
| `tide reject <project> [--reason "..."]`  | Writes the reject annotation; reconciler halts the entire Project on its next reconcile.        |
| `tide resume <project>`                   | Clears a prior `reject` annotation (the only way out of a rejected Project short of `cancel`).  |
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
2. Reconciler completes the Milestone's children. Before marking the
   Milestone `Succeeded`, the gate hook checks `gates.policy(project, "milestone")`,
   reads `approve`, patches `Milestone.Status.Phase=AwaitingApproval`,
   bubbles up a `WaveOrLevelPaused` condition on the Project.
3. Operator runs `tide approve hello-tide` (or, equivalently, clicks
   the dashboard's copy-to-clipboard "Approve" button and pastes into a
   shell).
4. `cmd/tide approve` discovers the first child level whose
   `Status.Phase=AwaitingApproval` (iteration order: Milestone → Phase →
   Plan → Task — dependency-ordered), patches the `approve` annotation
   onto it.
5. Reconciler's next loop reads the annotation via `gates.ConsumeApprove`,
   clears the annotation in the same patch, advances the level to
   `Succeeded`. The Project condition flips off.

## Reject flow

`tide reject` is **distinct from cancel** — reject halts the Project at
the next reconciler tick but preserves all state. Operator can iterate by
fixing the underlying issue, running `tide resume`, and re-applying. By
contrast `tide cancel --force` destroys the Project + every child via
cascading delete.

## What's coming

Phase 5 expands this doc with:

- Per-level gate-policy precedence rules (Project vs Plan)
- Decision tables for the four-state transition
  (`Running` → `AwaitingApproval` → `Approved` → `Succeeded`)
- Reconciler-side patterns for adding new gate types (a how-to guide
  for operators writing custom gates)
- Auditing the gate decisions via `tide_gate_evaluations_total` metric
