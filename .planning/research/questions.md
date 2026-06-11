# Open Research Questions

## Dashboard ↔ Prometheus integration shape (2026-06-10, from /gsd:explore tide-on-tide-dogfood)

The analytics dogfood project makes Prometheus the historical store. Open questions for
its planning phase:

- PromQL proxy through the existing chi `manager.Runnable` server vs the dashboard
  hitting Prometheus directly as a second datasource? (Proxy keeps one origin + auth
  surface; direct keeps the manager out of the query path.)
- What does the dashboard do when `prometheus.enabled=false` (the chart default)?
  Graceful degradation to live-only CRD-`.status` views must be first-class, not an
  error state.
- Label-cardinality budget enforcement: project/phase/wave labels ok, per-task not —
  where is that guarded (metric registration review? lint?)?
- Retention expectations: dogfood runs span days — default Prometheus retention vs
  what the cost-over-time charts need.

## Draft/pending Project state API shape (2026-06-10, from /gsd:explore tide-on-tide-dogfood)

The dashboard Project editor (dogfood project 3) supports create + edit-drafts, which
needs a saved-but-not-running state the reconciler honors. Open questions for its
planning phase:

- API shape: `spec.paused`-style boolean vs annotation vs phase-based gating? Precedent:
  Flux/CAPI use `spec.paused`/`spec.suspend`; annotations are weaker contracts.
- What does "edit" mean at the CRD level — server-side apply with a field manager owned
  by the dashboard, or full spec replace? Conflict behavior if someone kubectl-edits the
  same draft?
- Transition semantics: draft → running is one-way for v1? Can a running Project be
  re-paused, and if so what happens to in-flight waves (relates to spec failure-handling
  profiles)?
- CEL validation: can/should immutability of certain spec fields after leaving draft be
  enforced with `x-kubernetes-validations` (e.g. `oldSelf` transition rules)?
