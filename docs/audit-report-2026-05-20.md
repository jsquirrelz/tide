# Tide Codebase Audit Report

Date: 2026-05-20

## Scope

This audit reviewed the Tide Kubernetes operator codebase with emphasis on CRD
design, controller behavior, dispatch execution, Helm wiring, CI guardrails,
and test coverage around production-critical paths.

The review intentionally used a fact-checking pass:

1. Draft initial findings from a broad source audit.
2. Define verification questions that could disprove or narrow the draft.
3. Answer each question from independent source evidence.
4. Produce this verified report from the confirmed evidence.

## Executive Summary

Tide has unusually strong operator discipline for a young codebase. The API
types follow Kubernetes conventions, status is condition-oriented, wave
scheduling is derived instead of cached, and the repository has explicit
guardrails for import boundaries, RBAC wildcards, blocking reconcile code, and
secret isolation.

The main risks are not architectural direction; they are implementation drift
between the intended production path and the currently wired runtime path. The
highest-priority issues are production reconciler wiring, subagent image
configuration, skeletal planner Jobs, ambiguous Project resolution, and a
default task deadline mismatch.

## Verified Questions And Answers

### 1. Is `ProjectReconciler` wired in production to run the real lifecycle?

No.

`ProjectReconciler` only executes real project lifecycle behavior when
`Dispatcher != nil`; otherwise it marks the Project Ready with placeholder
text. The manager setup constructs `ProjectReconciler` without a dispatcher.

Evidence:

- `internal/controller/project_controller.go`
- `cmd/manager/main.go`

Recommendation:

Require a dispatcher at startup, or remove the nil dispatcher branch once the
placeholder path is no longer intentional. Add a manager wiring test or
reconciler factory test that fails if production setup omits required
dependencies.

### 2. Does the configured subagent image reach all controller paths?

No.

The Helm chart passes `--subagent-image` from `images.stubSubagent`. It also
sets `CLAUDE_SUBAGENT_IMAGE`, but that value is read into provider defaults
that do not carry an image through provider resolution. Separately, not all
reconcilers are constructed with a `SubagentImage`, and several planner paths
fall back to the stub image when the field is empty.

Evidence:

- `charts/tide/templates/deployment.yaml`
- `charts/tide/values.yaml`
- `cmd/manager/main.go`
- `internal/controller/dispatch_helpers.go`
- `internal/controller/milestone_controller.go`
- `internal/controller/phase_controller.go`
- `internal/controller/plan_controller.go`

Recommendation:

Make image selection explicit and single-sourced. Production should default to
the intended Claude subagent image, with the stub image only selected by an
explicit development or test value. Wire `SubagentImage` consistently for
Milestone, Phase, Plan, and Task reconcilers.

### 3. Do planner Jobs receive the envelopes they build?

No.

The Milestone, Phase, and Plan controllers serialize planner envelopes, then
discard them. The shared planner Job helper is explicitly skeletal and does not
write an envelope, mount the workspace PVC, start credproxy, mint a signed
token, or set the subagent environment needed for the worker contract.

Evidence:

- `internal/controller/milestone_controller.go`
- `internal/controller/phase_controller.go`
- `internal/controller/plan_controller.go`
- `internal/controller/planner_job_helpers.go`
- `cmd/stub-subagent/main.go`
- `cmd/claude-subagent/main.go`

Recommendation:

Replace the skeletal planner Job helper with a production Job builder that
shares the same execution contract as task dispatch: persisted `EnvelopeIn`,
workspace PVC mount, signed token, credproxy sidecar, bounded deadline,
termination output, and tests that assert each required piece is present.

### 4. Are default task execution limits consistent when `spec.caps` is omitted?

No.

`TaskReconciler` applies a 300-second wall-clock floor for token validity when
caps are omitted or zero. `podjob.BuildJobSpec` derives
`activeDeadlineSeconds` directly from caps and leaves omitted caps at zero,
which produces a 60-second Job deadline. That can terminate a default task long
before the token lifetime assumed by the controller.

Evidence:

- `internal/controller/task_controller.go`
- `internal/dispatch/podjob/jobspec.go`
- `api/v1alpha1/task_types.go`

Recommendation:

Use one shared defaulting function for task caps before both token minting and
Job spec generation. Add a nil-caps test proving the Job deadline matches the
default wall-clock floor plus grace.

### 5. Can resources attach to the wrong `Project` in a namespace?

Yes.

`TaskReconciler` and `PlanReconciler` fall back to listing Projects in the
namespace and returning the first item when explicit labels or parent
references do not resolve. This is unsafe in namespaces containing more than
one Project and can misattribute status, budget, dispatch, or ownership
behavior.

Evidence:

- `internal/controller/task_controller.go`
- `internal/controller/plan_controller.go`

Recommendation:

Remove namespace-first Project fallback. Require a label, owner chain, or
explicit parent reference. If the chain is missing, set a clear condition and
requeue instead of guessing.

### 6. Are CI workflows consolidated and deterministic?

Partially.

The main CI workflow contains strong, project-specific gates. Additional
scaffold-style workflows duplicate portions of that work, run `go mod tidy`
directly, and use different setup behavior such as latest Kind in the e2e
workflow.

Evidence:

- `.github/workflows/ci.yaml`
- `.github/workflows/test.yml`
- `.github/workflows/lint.yml`
- `.github/workflows/test-e2e.yml`

Recommendation:

Consolidate CI around the project-specific workflow, or make the smaller
workflows call the same targets with the same pinned tool versions. If CI runs
`go mod tidy`, follow it with a diff check rather than allowing module drift to
be hidden inside the job.

## Best Practices Observed

### Kubernetes API conventions

The CRDs use status subresources, namespaced resources, `metav1.Condition`,
print columns, and conventional spec/status separation. This makes the API
easier to operate with standard Kubernetes tooling.

### Derived scheduling state

Wave scheduling is computed from task dependencies rather than cached as an
aggregate schedule field. This avoids a common controller anti-pattern where
derived state drifts from source-of-truth resources.

### Static guardrails

The repo includes Makefile and analyzer guardrails for several architectural
constraints:

- No aggregate schedule fields on API types.
- No database driver dependency in `go.mod`.
- No RBAC wildcard permissions.
- No blocking sleep/time-after usage inside reconcile bodies.
- Import firewalls around `pkg/dag`, provider SDKs, and dispatch boundaries.
- Metric cardinality checks to avoid task-level Prometheus labels.

### Secret isolation

The dispatch design keeps provider credentials behind a credproxy and gives
subagents scoped signed tokens instead of raw provider keys. The push path also
uses a separate boundary for repository credentials and secret scanning.

### Webhook validation

Plan validation uses DAG checks for dependency cycles and validates conflicting
file-touch declarations. Wave validation prevents client-created Wave resources
without owner references.

## Anti-Patterns And Risks

### Placeholder paths still wired into production setup

Several code paths look like temporary implementation phases but remain
reachable from the Helm/manager setup. Placeholder paths are acceptable during
incremental development, but they should be isolated behind explicit dev/test
configuration so production deployments cannot silently choose them.

### Configuration drift across Helm, manager, and controllers

Subagent image selection is currently spread across Helm values, command-line
flags, environment variables, provider defaults, and per-reconciler fields.
This makes it hard to reason about which image actually runs.

### Discarded planner envelope data

Building an envelope and then discarding it is a strong signal of an incomplete
integration boundary. It also means tests can pass around envelope construction
without proving the runtime pod can consume the envelope.

### Ambiguous parent lookup

Returning the first Project in a namespace is convenient for early development
but unsafe once the namespace can host multiple Projects. Controllers should
prefer failing closed over guessing ownership.

### Split defaulting logic

The task deadline mismatch comes from defaulting being performed in one layer
for token validity but not in the layer that builds the Kubernetes Job. Shared
defaulting should happen before dependent calculations.

### CI duplication

Multiple workflows performing overlapping checks can create false confidence
when they drift in tool versions, setup behavior, or generated-file handling.

## Recommended Priority Order

1. Fix production wiring for `ProjectReconciler` and subagent image selection.
2. Replace skeletal planner Jobs with the real dispatch contract.
3. Remove first-Project namespace fallback from Plan and Task resolution.
4. Centralize task caps defaulting and add nil-caps deadline coverage.
5. Consolidate or align CI workflows.
6. Add Helm-render or manager-construction tests for production-critical
   configuration.

## Verification Performed

The following static guardrails passed:

```bash
make verify-no-aggregates verify-no-sqlite-dep verify-no-rbac-wildcards verify-rbac-marker-discipline verify-no-blocking
```

The following focused package tests passed:

```bash
go test ./internal/dispatch/podjob ./pkg/dag ./pkg/dispatch ./api/v1alpha1 ./internal/webhook/v1alpha1 ./cmd/manager
```

This was not a full CI run. The full suite includes envtest and Kind paths that
were outside the scope of this documentation-only audit update.
