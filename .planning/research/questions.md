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
