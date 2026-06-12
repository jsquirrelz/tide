# Phase 15: Paper Cuts - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-12
**Phase:** 15-Paper Cuts
**Areas discussed:** Label fix + approve discovery (CUTS-01), File-touch enforcement seat (CUTS-07), artifact-get execution UX (CUTS-04), Cross-plan wave view (CUTS-06)

---

## Label fix + approve discovery (CUTS-01)

| Option | Description | Selected |
|--------|-------------|----------|
| Universal stamping | Shared helper stamps tideproject.k8s/project on every child CR at every create site (reporter + all reconcilers) | ✓ |
| Reporter-only | Fix only MaterializeChildCRDs — the minimal CUTS-01 ask | |
| You decide | Claude picks based on blast radius | |

**User's choice:** Universal stamping

| Option | Description | Selected |
|--------|-------------|----------|
| Label fix only | approve keeps its label filter; one source of truth | ✓ |
| Add OwnerRef fallback | approve falls back to OwnerRef walks when label filter finds nothing | |
| You decide | | |

**User's choice:** Label fix only

| Option | Description | Selected |
|--------|-------------|----------|
| Reconciler backfill | Reconcilers patch the missing project label onto observed CRs (derive from OwnerRef chain) | ✓ |
| Document manual recipe | kubectl label one-liner in docs | |
| No backfill | Only new CRs get labels | |

**User's choice:** Reconciler backfill

| Option | Description | Selected |
|--------|-------------|----------|
| Project label only | CUTS-01's exact ask; smallest diff | ✓ |
| Full parity set | Reporter stamps the standard controller label set | |
| You decide | | |

**User's choice:** Project label only

---

## File-touch enforcement seat (CUTS-07)

Scout root cause presented before questions: the admission webhook early-returns when zero Tasks are visible (Pitfall B), and the reporter flow always creates Tasks after the Plan — so the existing check never ran in run 1.

| Option | Description | Selected |
|--------|-------------|----------|
| Reconciler dispatch gate | PlanReconciler re-runs mismatch check once Tasks materialize, before wave derivation/dispatch; sets dormant ValidationState=FileTouchMismatch | ✓ |
| Task-create webhook | Validate each Task at admission against existing siblings — racy, partial-failure prone | |
| Reporter-side pre-create validation | Atomic but duplicates logic outside the API server; kubectl applies bypass it | |

**User's choice:** Reconciler dispatch gate

| Option | Description | Selected |
|--------|-------------|----------|
| Park + condition | No dispatch; ValidationState + condition naming both tasks and shared path; park-not-fail | ✓ |
| Fail the Plan | Status.Phase=Failed; conflicts with park-not-fail direction | |
| You decide | | |

**User's choice:** Park + condition

| Option | Description | Selected |
|--------|-------------|----------|
| Enforce + prompt patch | Planner prompt addition: sibling tasks must not share files | ✓ |
| Enforce only | Literal CUTS-07 ask | |
| You decide | | |

**User's choice:** Enforce + prompt patch

| Option | Description | Selected |
|--------|-------------|----------|
| Keep as-is | Webhook stays an early-warning layer, untouched | |
| Fix mode resolution too | Webhook resolves the real project mode instead of nil-project fallback | ✓ |
| Remove the webhook check | Single seat at the reconciler | |

**User's choice:** Fix mode resolution too

---

## artifact-get execution UX (CUTS-04)

| Option | Description | Selected |
|--------|-------------|----------|
| Pod + log stream | Bare inspector Pod, stream container logs, CLI deletes the pod | ✓ |
| Job + TTL | Job wrapper with ttlSecondsAfterFinished cleanup backstop | |
| Termination message | 4KiB cap — disqualifying for real artifacts | |

**User's choice:** Pod + log stream

| Option | Description | Selected |
|--------|-------------|----------|
| Raw bytes to stdout | Status to stderr; pipeable | ✓ |
| stdout + -o file flag | | |
| You decide | | |

**User's choice:** Raw bytes to stdout

| Option | Description | Selected |
|--------|-------------|----------|
| Flag w/ default + always clean | --timeout ~60s, deferred cleanup | |
| No timeout, wait forever | | |
| (Free text) | "Inspector pod shouldn't get deleted until the pod being inspected is complete" | ✓ |

**User's choice:** Free text, clarified over a follow-up exchange.
**Notes:** Initial phrasing assumed the inspector watches another pod; after clarification the locked intent is: never read a half-written artifact and never close before the artifact has been created — wait for artifact READINESS (bounded), clean up only after the content is fully streamed. Completeness-detection mechanism left to research/planner, must be race-free against in-flight writers.

| Option | Description | Selected |
|--------|-------------|----------|
| Error + directory listing | Not-found message plus nearest-directory listing | |
| Plain error only | Non-zero exit, cat's stderr relayed | ✓ |
| You decide | | |

**User's choice:** Plain error only (applies after the readiness wait window is exhausted)

---

## Cross-plan wave view (CUTS-06)

| Option | Description | Selected |
|--------|-------------|----------|
| Right-pane default | Replaces the 'Select a plan' empty state; zero new navigation | ✓ |
| New AppShell tab | Dedicated tab alongside Phase 16's Telemetry tab | |
| Strip above the grid | Always-visible ticker; cramped | |

**User's choice:** Right-pane default

| Option | Description | Selected |
|--------|-------------|----------|
| Rich wave cards | Plan name, wave index, task chips with StatusBadge, running/total | ✓ |
| Compact rows | One line per wave | |
| You decide | | |

**User's choice:** Rich wave cards

| Option | Description | Selected |
|--------|-------------|----------|
| Server aggregate + SSE | Manager API label-selector aggregate, streamed over existing SSE | ✓ |
| Client-side aggregation | Client re-derives waves — drift risk | |
| You decide | | |

**User's choice:** Server aggregate + SSE

| Option | Description | Selected |
|--------|-------------|----------|
| Navigate to plan DAG | Clicking a wave card selects that plan | ✓ |
| Read-only | Pure observation surface | |
| You decide | | |

**User's choice:** Navigate to plan DAG

---

## Claude's Discretion

- CUTS-05 chip fix shape (add `Complete` mapping to StatusBadge per UI-SPEC status vocabulary)
- Shared label-stamping helper placement
- artifact-get timeout flag default + inspector-pod RBAC
- Regression-test vehicle per cut (envtest vs kind Layer B vs Vitest)
- Whether inspect_wave CLI machinery shares anything with the running-waves aggregate

## Deferred Ideas

- Wave-view metaphor naming ("currents") — ui-phase/planner's call; plain prose if nothing fits
- Artifact browsing/listing UX (tide artifact-ls or dashboard browser) — new capability
- OwnerRef fallback discovery in tide approve — rejected for v1.0.1; labels are the single source of truth
