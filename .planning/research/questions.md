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

## langchain-anthropic passthrough surface for the successor runtime (2026-07-06, from /gsd:explore specialist-agents-langgraph)

The evidence-gated successor-runtime strategy (notes/langgraph-successor-runtime-strategy.md)
consolidates CACHE-F1 and the dead `Provider.Params` allowlist onto the LangGraph runtime.
Verify at specialist-image build time:

- Does `langchain-anthropic` pass `cache_control` breakpoints through cleanly (content-block
  level), so the image can place them on the shared stable prefix? This is CACHE-F1's fix
  shape; the ADK eval marked it "possible via direct SDK use inside the image" — confirm it
  works *without* dropping below the LangChain abstraction.
- Which sampling/thinking params does it expose vs. the CLI's `--model`/`--effort`-only
  surface — temperature, thinking budget, top_p, top_k (the `Provider.Params` allowlist),
  and an effort-equivalent for Opus 4.8+?
- Does `init_chat_model` degrade any of these (provider-specific kwargs may be
  ChatAnthropic-only, forcing per-provider construction instead of the single-string path)?
- Record which `langgraph`/`langchain-anthropic` patch versions were verified (the 1.x line
  moves fast; polyglot doc's pinning rule applies).
