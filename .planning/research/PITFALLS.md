# Domain Pitfalls — v1.0.8 Phoenix Rising: OpenInference Trace Emission + Self-Hosted Phoenix

**Domain:** Adding OpenTelemetry/OpenInference span emission to a shipped Go controller-runtime operator (TIDE v1.0.7) whose dispatch chain runs across three separate pod types (long-running manager, short-lived per-Task subagent Jobs, short-lived in-namespace reporter Jobs), plus documenting a self-hosted Arize Phoenix install for an 8 GiB dev VM.

**Researched:** 2026-07-15

**Confidence:** HIGH for codebase-grounded pitfalls (direct inspection of `pkg/otelai/`, `internal/otelinit/provider.go`, `internal/subagent/anthropic/subagent.go`, `internal/harness/redact/`, `cmd/tide-reporter/main.go`, `internal/controller/task_controller.go`, `charts/tide/values.yaml`); HIGH for OTel Go SDK defaults and Phoenix Helm-chart mechanics (Context7-verified against `arize-ai/phoenix` docs + `open-telemetry/opentelemetry-go` source); MEDIUM for LangGraph self-instrumentation propagation specifics (WebSearch-verified against the OTel spec's env-carrier page, but the concrete `openinference-instrumentation-langchain` behavior in TIDE's own future adapter is not yet built, so the failure mode is inferred from the general OTel pattern, not observed).

**Binding constraints these pitfalls must not violate:** D-O5 (no payload-bearing helper on `pkg/otelai`'s public surface — `TestNoPayloadHelperOnPublicSurface` already enforces this at the attribute-construction layer, but does NOT stop a call site from passing raw secret-laden text into the existing `Content` field); CRD-`.status`-only persistence (any "have I emitted this span" guard must not balloon `.status`); never bake a sampler into Go code (`OTEL_TRACES_SAMPLER`/`_ARG` stay env-driven per the existing `TestNoWithSamplerInSource` guard); no subchart dependency / no version coupling to Arize's Helm chart (self-host recipe stays documented-install, not vendored).

---

## Critical Pitfalls

### Pitfall 1: `events.jsonl` is deliberately unredacted — the reporter is the missing secret/PII scrub boundary

**What goes wrong:**
The per-Task audit log the milestone plans to parse for `LLMInputMessages`/`LLMOutputMessages` is written by `ParseStream(stdout, eventsFile)` in `internal/subagent/anthropic/subagent.go:311-331`, and the code comment is explicit: *"Phase 4 OpenInference parsing reads this file untouched; we tee every line through ParseStream as it arrives"* (subagent.go:308-310). That file is the **raw** Claude CLI `stream-json` output — it is never passed through `internal/harness/redact.RedactingWriter`, the six-pattern secret denylist (`sk-ant-api03-*`, `sk-*`, `ghp_/ghs_*`, `xox[abp]-*`, `AKIA*`, JWT) that HARN-04 already applies to the *separate* stdout capture path used for git-commit content. Credproxy (`internal/credproxy/doc.go`) only protects TIDE's own `ANTHROPIC_API_KEY` — it says nothing about a secret the *model itself* reads out of the target repo (a committed `.env`, a leaked key in a code comment, a customer credential pasted into a Project outcome prompt) and echoes back into its own output. If the in-namespace reporter naively feeds `events.jsonl` message content into `otelai.LLMInputMessages`/`LLMOutputMessages`, whatever leaked into that stream ships straight into Phoenix's database — bypassing every redaction layer TIDE already built for the git-push path.

**Why it happens:**
The comment that makes `events.jsonl` "untouched" was written for a *different* reason (full-fidelity OpenInference parsing) than the reason it's dangerous (no redaction pass exists on this path at all). It reads as intentional and correct, which makes it easy to build the reporter's span emission directly on top of the raw file without noticing the redaction gap it inherits.

**How to avoid:**
Give the reporter its own scrub boundary — reuse `redact.SecretPatterns` (the compiled regexp list, not the streaming `RedactingWriter`, since the reporter operates on already-buffered message strings, not a live stream) as a required pass over `Message.Content` before calling `LLMInputMessages`/`LLMOutputMessages`. Treat this as the span-emission equivalent of credproxy's environment boundary: the reporter is the one place that has both the raw payload and the outbound network hop to an external-ish system (Phoenix), exactly like credproxy is the one place with both the raw key and the outbound HTTP call.

**Warning signs:** A reporter/parser PR that calls `otelai.LLMInputMessages` with content read straight from `events.jsonl` with no intermediate transform; no new test asserting a known secret pattern is absent from emitted span attributes; `redact` package imported nowhere under `internal/reporter/`.

**Phase to address:** LLM message-array spans (in-namespace emitter) — the phase that reads `events.jsonl` and calls the OpenInference message-array helpers.

---

### Pitfall 2: Full message-array inlining collides with OTLP/gRPC's 4 MB message ceiling — and Phoenix's own docs call this out as a top pitfall

**What goes wrong:**
`pkg/otelai.LLMInputMessages`/`LLMOutputMessages` flatten a message slice into `2*N` string-valued attributes with **no size guard** (`pkg/otelai/attrs.go:94-107` — the loop has no length check). A single Task's prompt can already legitimately include rendered repo context, a diff, or planner instructions; if the reporter inlines the full `events.jsonl` message content rather than deferring to `ArtifactPath`, one large Task can produce a span whose attribute payload alone approaches or exceeds gRPC's default 4 MB max-receive-message size. Arize's own docs name this exact failure mode: *"gRPC has message-size limits, and spans exceeding 4 MB (often due to large attributes like full document text ... can hit these limits"* — and the failure is a batch-level `ResourceExhausted` rejection, not a graceful per-span truncation, so a single oversized span can cause the **whole batch** (potentially several unrelated spans queued in the same `BatchSpanProcessor` flush) to be dropped.

**Why it happens:**
D-O5 already anticipated *some* form of this ("never inline LLM payload as a span attr") and locked the public surface to exactly two message-array helpers plus `ArtifactPath` — but the enforcement test (`TestNoPayloadHelperOnPublicSurface`) only forbids a *new named helper*; it does nothing to stop a call site from passing the full raw content into the existing `Content` field. The milestone's own target-feature text ("LLM message-array spans ... including full LLM input/output message arrays") explicitly asks for the risky shape.

**How to avoid:**
Resolve D-O5's payload-boundary decision with an explicit size threshold, not a binary "always inline" choice: inline via `LLMInputMessages`/`LLMOutputMessages` only under a documented byte ceiling (comfortably below 4 MB with room for the rest of the batch — Phoenix's docs suggest truncating via `TraceConfig` or chunking on the server side, but TIDE's own emitter is the cheaper place to cap it), and fall back to `ArtifactPath` for anything larger. Make the threshold a named constant next to `pkg/otelai`'s existing constants so it is discoverable and testable.

**Warning signs:** No byte-length check anywhere between `events.jsonl` parsing and `LLMInputMessages` construction; integration tests only exercise small fixture prompts; no test that dispatches (or synthesizes) a multi-hundred-KB message and asserts graceful `ArtifactPath` fallback instead of an OTLP export error.

**Phase to address:** LLM message-array spans (in-namespace emitter) — this is the D-O5 payload-boundary decision the milestone already flags as required.

---

### Pitfall 3: Reconcile-loop semantics give span creation no natural idempotency — unlike Job `Create`, duplicate span emission is not self-healing

**What goes wrong:**
`TaskReconciler` already requeues itself constantly while a Task is in flight — `internal/controller/task_controller.go` alone has requeue delays of 5s, 10s, and 30s scattered across more than half a dozen gate branches. Every one of those re-entries re-runs the reconcile function from the top. TIDE's existing dispatch code tolerates this because Job creation is idempotent-by-construction (`BuildReporterJob`'s deterministic `tide-reporter-<parentUID>` name plus an `AlreadyExists`-is-success check, mirrored across `push_helpers.go`). **Span creation has no equivalent.** `tracer.Start(...)` mints a brand-new random span ID (and, if called with no propagated parent context, a brand-new random trace ID) on every call — there is no "AlreadyExists" outcome at the OTel API level. If the "create the dispatch span" logic is wired into the reconcile body without a guard keyed off something more durable than the in-memory reconcile invocation, a single real dispatch can emit dozens of near-duplicate spans into Phoenix, and — worse — if any of those reconcile passes fail to correctly propagate the *same* parent trace context, some of those duplicates become entirely disconnected root traces (trace fragmentation: what should be one dispatch-chain tree per run shows up as several unrelated stubs).

**Why it happens:**
Controller-runtime is level-triggered by design — that's a feature for reconciling state, not a request/response model where "start span, do work, end span" happens exactly once per invocation the way it does in a typical HTTP handler. Code ported mentally from that request/response model (the shape nearly all OTel Go examples use) breaks silently here.

**How to avoid:**
Gate span creation the same way Job creation is already gated: only create the dispatch span on the specific state-transition edge (e.g., the reconcile pass that *first* creates the dispatch Job), and only close/end it on the specific edge where the Job reaches a terminal state — using existing status-condition transitions as the guard, not "did I already do this" checks against ephemeral in-memory state. Persist the minimum needed to resume the span across reconciles (the propagated `SpanContext`, serialized as a W3C `traceparent` string) somewhere already durable — the envelope or a status field already being added for the trace-context contract — rather than inventing new bookkeeping.

**Warning signs:** Span-emission code called unconditionally near the top of `Reconcile()`; no test that calls `Reconcile()` twice for the same object and asserts exactly one span was started; Phoenix showing the same `tide.dispatch.<level>` span name many times per single real dispatch, most with near-zero duration.

**Phase to address:** Dispatch-chain span emission (manager) — the phase that hooks span creation into the reconcilers.

---

### Pitfall 4: Short-lived Jobs drop spans on exit — the manager's flush discipline does not automatically extend to `tide-reporter`

**What goes wrong:**
`internal/otelinit/provider.go` builds the SDK `TracerProvider` with `sdktrace.WithBatcher(exp)` — the OTel Go SDK's default `BatchSpanProcessor` batches for up to 5 seconds (`DefaultScheduleDelay`) before exporting. `cmd/manager/main.go` gets this right today: it captures `otelShutdown` from `otelinit.NewTracerProvider` and explicitly `defer`s a bounded-context `Shutdown` call tied to `ctrl.SetupSignalHandler()` (main.go:260-284), so the batch processor flushes before the long-running process exits. **`cmd/tide-reporter/main.go` has none of this scaffolding.** It is a genuinely one-shot binary — parse flags, do the work, `os.Exit` with 0/1/2 — with no signal-handled shutdown path today. If a future change simply calls `otelinit.NewTracerProvider()` inside `tide-reporter`'s `main()` to start emitting the reporter-synthesized LLM spans, and does not *also* copy the manager's deferred-`Shutdown`-before-exit pattern, every reporter run will construct spans that never leave the process — Phoenix will show dispatch spans (from the long-running manager) with no LLM children at all, and it will look like the emitter never ran, not like a flush bug.

**Why it happens:**
The manager's correct pattern is buried in a `main.go` most reporter-focused work won't touch; `otelinit.NewTracerProvider` returns a `ShutdownFunc` that is trivially easy to `_ =` away or forget entirely on a binary whose only existing exit paths are three bare `os.Exit(N)` calls.

**How to avoid:**
Any binary that calls `otelinit.NewTracerProvider` MUST call the returned `ShutdownFunc` with a bounded context on every exit path (success, generic failure, and invariant-violation) before the process returns — not just the success path. Consider adding a `TestReporterCallsTracerShutdown` source-grep test mirroring the existing `TestNoWithSamplerInSource` pattern so this is enforced the same way D-O5 and Pitfall 24 already are.

**Warning signs:** `tide-reporter`'s exit paths call `os.Exit` directly instead of returning through a single `defer`-friendly `run()` (worth checking — some exit paths may already bypass `defer` entirely, which would defeat even a correctly-placed `defer shutdown()`); Phoenix showing dispatch spans with zero children; no explicit `Shutdown` call anywhere under `cmd/tide-reporter/`.

**Phase to address:** LLM message-array spans (in-namespace emitter) — this is exactly the phase that turns `tide-reporter` from a K8s-API-only binary into an OTel-emitting one.

---

### Pitfall 5: Retroactive span synthesis across three pods' clocks can render visually "broken" traces in Phoenix

**What goes wrong:**
The dispatch chain spans three separate pods with three separate clocks: the manager (creates the dispatch span, e.g. at Job-create time), the subagent Job pod (writes `events.jsonl` with its own wall-clock timestamps per stream event), and the reporter Job pod (runs later, in a different pod, and is the one that will call `span.Start`/`span.End` with explicit historical timestamps reconstructed from `events.jsonl` to synthesize the LLM child spans). Explicit timestamps (`trace.WithTimestamp(...)` on start, an explicit end time on `span.End`) do not interact with the sampler (sampling is a trace-ID hash, not a time-based decision) — but they are exactly as trustworthy as the clock that produced them. Any meaningful clock skew between the subagent pod's node and the reporter pod's node (or between either and the manager's node, if the dispatch span's own start/end times are also stamped from a different clock) can put a synthesized LLM child span's timestamps outside its parent dispatch span's `[start, end]` window. Phoenix (like every trace UI) renders that as a visibly broken waterfall — child bars extending past the parent, or negative-looking durations — which reads as "the instrumentation is buggy" even though every individual span's *content* is correct.

**Why it happens:**
Normal OTel instrumentation creates and ends spans in real time on one machine, so this class of bug doesn't exist by construction. TIDE's design is different on purpose (the reporter can't emit spans as the LLM call happens — it isn't even running yet) — that's a legitimate three-pod retroactive-synthesis pattern, but it inherits every synchronized-clocks assumption baked into how trace-viewer UIs render span nesting.

**How to avoid:**
Prefer monotonic, self-consistent timestamps over multi-clock ones where possible: derive the dispatch span's end time from the *same* source (`events.jsonl`'s own last-event timestamp, or the Job's `completionTime` as read by the same reconciler that created the span) rather than "whenever the reconciler happened to observe completion." If K8s NTP/chrony skew across nodes is a real risk in the target clusters (kind's single-node dev clusters won't show this; a multi-node production cluster might), document it as a known limitation of the synthesis approach rather than silently trusting it. At minimum, clamp synthesized child-span timestamps to fall within the parent's observed window before calling `span.End`, so a clock anomaly degrades to "slightly imprecise" rather than "visually inverted."

**Warning signs:** No test asserts synthesized child-span timestamps are `>= parent.start` and `<= parent.end`; the end-to-end proof screenshot is captured on a single-node kind cluster only (where this bug class can't surface) with no multi-node validation noted anywhere.

**Phase to address:** End-to-end proof — this is exactly the kind of defect that only surfaces when a live trace tree is actually rendered and inspected, not from unit tests of the attribute helpers.

---

### Pitfall 6: `parentbased_traceidratio` at the chart's default 0.1 is a per-*run* coin flip, not a per-span filter — Phoenix will look empty on 9 of 10 single-run demos

**What goes wrong:**
The chart default (`charts/tide/values.yaml`, `otel.tracesSampler = parentbased_traceidratio`, `otel.tracesSamplerArg = "0.1"`) is documented in `docs/observability.md` and enforced via Pitfall 24 (`internal/otelinit/provider.go` explicitly must not call `WithSampler` in code). Because the milestone's design has the manager create ONE root dispatch span per run (per the runtime-neutrality constraint: "manager creates the dispatch span and injects W3C `traceparent`... so synthesized and native spans parent identically") and every other span in the entire Milestone→Phase→Plan→Task tree descends from it via propagated context, `ParentBased` sampling makes exactly **one** sampling decision for the **entire run** — the ratio sampler evaluates the root trace ID once, and every descendant inherits that single sampled/not-sampled verdict. At `arg=0.1`, roughly 9 out of 10 single-project runs will produce **zero** spans in Phoenix, not "10% of the expected spans." An operator running the milestone's own documented quickstart once, on one project, has a 90% chance of concluding the feature doesn't work.

**Why it happens:**
`parentbased_traceidratio` is usually explained (and usually behaves) as "sample X% of requests" in request/response systems with many independent root traces per process lifetime. TIDE's dispatch-chain design deliberately collapses an entire multi-hour run into one trace, which turns the same sampler into an entirely different statistical object: a single low-probability event gating the whole run's observability, not a smoothing knob over many events.

**How to avoid:**
The install/quickstart documentation (and the milestone's own end-to-end proof run) must explicitly override `tracesSamplerArg=1.0` (or `otel.tracesSampler=always_on`) for first-run verification and any single-project demo, with an explicit callout that `0.1` is a steady-state/production default meant for clusters running many concurrent projects, not a single dogfood run. Consider also documenting per-signal guidance: always-sample runs that hit a `BillingHalt`/`FailureHalt` condition (rare, high-value-to-debug) versus ratio-sample routine successful runs — noted as a candidate for the tail-sampling collector already flagged in `docs/observability.md`'s "What's coming" section, not a v1.0.8 requirement.

**Warning signs:** The end-to-end proof's install command reuses the chart's bare defaults; the observability doc's Phoenix section doesn't mention overriding `tracesSamplerArg`; a bug report reads "Phoenix shows nothing" filed against a real, successful run.

**Phase to address:** Self-hosted Phoenix surface (documented-install posture) for the doc fix; End-to-end proof for catching it live before it ships.

---

### Pitfall 7: Double-instrumentation when a self-instrumenting runtime (LangGraph) doesn't receive the propagated trace context

**What goes wrong:**
The milestone's own runtime-neutrality constraints already name this risk and propose the fix (a self-instrumenting capability flag so the reporter skips synthesis for runtimes that emit natively). The pitfall is in the failure mode if that flag or the underlying context handoff is wrong in either direction: (a) if the LangGraph subagent's `openinference-instrumentation-langchain` auto-instrumentation is not given the manager's propagated trace context, it starts its **own** root trace with a fresh random trace ID — the dispatch span (from the manager) and the LLM spans (natively emitted by LangGraph) end up in two disconnected traces in Phoenix instead of one tree, defeating the entire "dispatch-chain span emission" goal for that runtime; (b) if the capability flag is missing, stale, or defaults wrong, the reporter *also* synthesizes LLM spans from whatever event log LangGraph's runtime leaves behind — producing genuine duplicate spans (double-counted tokens/cost in any Phoenix aggregation that sums `llm.token_count.*` across a project).

**Why it happens:**
The OTel ecosystem's standard subprocess-boundary carrier is the `TRACEPARENT`/`TRACESTATE` environment variables (this is the documented pattern at `opentelemetry.io/docs/specs/otel/context/env-carriers`), but auto-instrumentation libraries commonly assume in-process context propagation (a parent span already active in the same process) or HTTP-header extraction — **not** automatic environment-variable extraction on process start. Reading `TRACEPARENT` from the environment and attaching it as the active context before the LangGraph graph is invoked is application code the future subagent adapter has to write explicitly; it does not happen "for free" just because the env var is set on the Job.

**How to avoid:**
Treat the W3C trace-context contract as the durable seam it's already designed to be (per the milestone's own decision), but write an explicit integration test — even a stub-runtime one — asserting that a synthetic `TRACEPARENT` injected into a Job's env is actually extracted and becomes the active context before any span starts, so this isn't discovered for the first time when the LangGraph beachhead milestone lands. Keep the self-instrumenting capability flag as a single source of truth the reporter reads at parse time (not a runtime-guessed heuristic like "does `events.jsonl` contain OpenInference-shaped events already") so there's no ambiguity window where both paths fire.

**Warning signs:** No test exercises the env-carrier extraction path end-to-end; the capability flag has no default-safe value (i.e., an unset flag should default to "synthesize," never to "assume native," since a false "native" assumption silently produces zero spans rather than duplicates); Phoenix showing two same-timeframe, unconnected traces for what was one dispatch.

**Phase to address:** Flagged now as a design constraint (already captured in PROJECT.md's runtime-neutrality section) but the actual double-emission failure mode won't be exercisable until the LangGraph beachhead milestone. Worth a lightweight contract test in this milestone's End-to-end proof phase so the seam is proven before a second runtime depends on it.

---

### Pitfall 8: Phoenix's three friendliest-looking defaults — ephemeral SQLite, infinite retention, no auth — compound into a real exposure on this system specifically

**What goes wrong:**
Three independently-documented Phoenix defaults each look like a reasonable "just try it" posture but stack badly for TIDE's use case:
1. **Ephemeral by default.** Phoenix's own persistence docs state plainly: *"By default, Phoenix uses a file-based SQLite database... stores traces in a temporary folder... this default setup is ephemeral — data will be lost when the container stops unless backed by a persistent volume."* A quickstart that doesn't set `PHOENIX_WORKING_DIR` (or the Helm chart's `persistence.enabled=true` + PVC) to a real volume loses the entire trace history on the next pod restart/reschedule — trivially likely on a kind dev cluster that gets torn down and recreated per the project's own documented "clean-run" recipes.
2. **Infinite retention by default.** `database.defaultRetentionPolicyDays` (Helm) / `PHOENIX_DEFAULT_RETENTION_POLICY_DAYS` defaults to `0` = never expire. Combined with the milestone's plan to embed *full message arrays* (repo content, diffs, prompts) as span attributes, an unattended multi-day dogfood habit fills disk with no natural ceiling.
3. **No authentication by default.** Enabling auth requires explicitly setting `PHOENIX_ENABLE_AUTH=true` plus `PHOENIX_SECRET` and `PHOENIX_DEFAULT_ADMIN_INITIAL_PASSWORD` (or the Helm chart's `auth.*` values) — until that's done, Phoenix's UI, REST, GraphQL, and gRPC surfaces are all unauthenticated. This is the real analog of the "credproxy exists to keep keys out" question for span *payloads*: credproxy solved the API-key leakage problem, but a LAN-reachable, unauthenticated Phoenix instance holding full prompt/completion history (including whatever repo content per Pitfall 1/2 made it in) is its own, separate secret-exposure surface that nothing else in TIDE's threat model currently covers.

**Why it happens:**
Each default is individually defensible for Phoenix's primary use case (a data scientist's laptop, single-user, throwaway experiments) and none of the three official docs flags the combination as risky — that judgment is specific to TIDE embedding full source-repo content in spans and running on a shared/LAN-reachable dev cluster rather than a laptop.

**How to avoid:**
The self-host documentation (INSTALL.md / observability.md) should not just show `helm install` with bare defaults. It should explicitly set: a PV-backed `persistence.enabled=true` (or Postgres via `postgresql.enabled=true`) with a size appropriate to the target cluster (see Pitfall 9), a non-infinite `database.defaultRetentionPolicyDays` matching the project's own `prometheus.retentionTime` documentation pattern (`docs/observability.md` already has precedent for this kind of "size it for your run cadence" callout), and `PHOENIX_ENABLE_AUTH=true` with credentials sourced from a K8s Secret — mirroring the credential-Secret pattern TIDE already uses everywhere else (git creds, LLM API keys).

**Warning signs:** The install recipe's `helm install` command has no `--set` overrides at all; no PVC visible via `kubectl get pvc` after following the doc; Phoenix's login page never appears (i.e., auth was never turned on) when the Service type is anything other than a loopback-only `ClusterIP` accessed via `kubectl port-forward`.

**Phase to address:** Self-hosted Phoenix surface (documented-install posture) — this is precisely the INSTALL.md/observability.md recipe phase.

---

### Pitfall 9: Phoenix's own Helm defaults (20Gi PVC, optional bundled Postgres pod) collide with the project's already-tight 8 GiB dev-VM budget

**What goes wrong:**
Phoenix's Helm chart defaults both the SQLite-persistence PVC size and the bundled `postgresql.storage.requestedSize` to `20Gi`, and enabling the bundled Postgres subchart adds a whole separate pod (with its own memory footprint) to a cluster. TIDE's own operating notes already document that the dev Docker VM is ~7.65 GiB and that running two single-node clusters (or, by extension, two memory-hungry workloads) concurrently OOM-kills the node (exit 137) — the existing guidance is "one heavy run at a time, delete-recreate-prewarm." Installing Phoenix's default configuration alongside a live `kind` cluster already running credproxy + tide-eval containers (the project's documented `make eval` recipe) risks reproducing exactly that OOM pattern, and a 20Gi PVC request against a constrained VM's disk budget can fail to provision or starve other workloads even before memory becomes the bottleneck.

**Why it happens:**
Phoenix's defaults are sized for its primary deployment target (a dedicated observability namespace on a real cluster), not for the "everything runs on one developer's Docker Desktop VM" constraint that is specific to how this project does its own dev/test cycles.

**How to avoid:**
The self-host recipe should explicitly right-size for the documented dev-VM constraint: a small `persistence.size` (a few Gi, not 20Gi, is plenty for dogfood-scale trace volumes once retention is bounded per Pitfall 8), skip the bundled Postgres subchart for dev/kind installs (SQLite-on-PV is sufficient at this scale; Postgres is the production-durability upgrade path, not the default), and fold "install Phoenix" into the project's existing "one heavy workload at a time" discipline rather than assuming it's cheap to run alongside everything else.

**Warning signs:** The install doc copies Phoenix's example `values.yaml` verbatim; a dogfood run against a fresh `kind-tide-dogfood`-style cluster with Phoenix already installed starts failing with pod evictions or exit 137 that weren't happening before Phoenix was added.

**Phase to address:** Self-hosted Phoenix surface (documented-install posture).

---

## Technical Debt Patterns

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|-----------------|------------------|
| Always inline full message content via `LLMInputMessages`/`LLMOutputMessages`, skip `ArtifactPath` entirely | Simpler reporter code; no separate fetch path needed in Phoenix's UI | OTLP size-limit failures (Pitfall 2) + unbounded PII/secret exposure into Phoenix's default-infinite-retention store (Pitfall 1, 8) | Never for real target repos; borderline acceptable only for the milestone's own small, already-public demo/dogfood repo where content has no confidentiality requirement |
| Copy `otelinit.NewTracerProvider()` into `tide-reporter`'s `main()` without also copying the manager's deferred-shutdown pattern | Fast to wire, "it compiles" | Every reporter run silently drops its spans (Pitfall 4) — looks like a config bug, not a code bug, and is hard to diagnose from Phoenix alone | Never — the shutdown call is required, not optional, for any short-lived binary that constructs a batching exporter |
| Wire span creation directly into `Reconcile()` with no state-transition guard | No new status fields, no new bookkeeping | Cardinality explosion of duplicate/fragmented spans on every real dispatch (Pitfall 3), visible the first time a Task takes more than a few requeue cycles | Never in production paths; acceptable only in a throwaway spike/prototype never wired to the real chart |
| Ship the Phoenix install doc with bare `helm install` defaults | Fewer lines in the doc, faster to write | Ephemeral traces on pod restart, unbounded disk growth, unauthenticated LAN exposure (Pitfall 8), and VM resource contention (Pitfall 9) | Never for the documented install recipe; acceptable only as an explicitly-labeled "quick local smoke test, not for real use" snippet, if one is included at all |

## Integration Gotchas

| Integration | Common Mistake | Correct Approach |
|-------------|-----------------|-------------------|
| Phoenix Helm chart persistence | Setting both `persistence.enabled=true` and `postgresql.enabled=true` (mutually exclusive — chart's own NOTES.txt warns on this), or leaving both false with no `database.url` configured (silently falls back to an ephemeral temp-dir SQLite DB) | Pick exactly one persistence strategy explicitly in the install recipe; never leave the operator to discover the mutual-exclusion rule from a chart warning after the fact |
| Phoenix OTLP endpoint wiring | Pointing TIDE's `otel.exporter.endpoint` at Phoenix's UI/HTTP port (6006, which also serves OTLP/HTTP) while TIDE's exporter code (`otlptracegrpc.New` in `internal/otelinit/provider.go`) is gRPC-only | Point at Phoenix's gRPC OTLP port (4317, `host:port` with no scheme) — the only protocol TIDE's exporter code speaks; there is no OTLP/HTTP exporter in the codebase to fall back to |
| LangGraph self-instrumentation (`openinference-instrumentation-langchain`) | Assuming the auto-instrumentor discovers the manager's propagated trace context automatically from the Job's environment | Explicitly extract `TRACEPARENT`/`TRACESTATE` env vars into an OTel `Context` via the W3C propagator and attach it as the active context before invoking the graph — this is adapter code that has to be written, not a free auto-instrumentation behavior |
| Phoenix authentication | Leaving `PHOENIX_ENABLE_AUTH` unset once the Service is reachable beyond `kubectl port-forward` loopback (e.g., a NodePort/LoadBalancer for LAN demo access) | Set `PHOENIX_ENABLE_AUTH=true` + `PHOENIX_SECRET` + `PHOENIX_DEFAULT_ADMIN_INITIAL_PASSWORD` via a K8s Secret whenever the Service type is anything other than loopback-only access |

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|-----------------|
| Inlining full message arrays as span attributes on every dispatch | OTLP export `ResourceExhausted` errors; whole batches (including unrelated spans) silently dropped | Byte-length threshold before choosing `LLMInputMessages`/`ArtifactPath` (Pitfall 2) | A single large diff/prompt can trip this well before "at scale" — one oversized Task is enough |
| Default `BatchSpanProcessor` queue (2048 spans) under a bursty wave of many Tasks completing near-simultaneously | Spans silently dropped with only an SDK-internal log line, no user-visible error | Explicit `WithMaxQueueSize`/`WithMaxExportBatchSize` sizing for the manager's realistic peak wave fan-out, or accept the drop for low-value spans | Unlikely at today's dispatch volumes but unverified — no test asserts a bound on concurrent in-flight spans per wave |
| Unauthenticated Phoenix + infinite retention on a disk-constrained dev VM | Dev VM disk fills silently across many dogfood runs, degrading unrelated workloads (kind, credproxy, tide-eval) sharing the same VM | Explicit, non-infinite `database.defaultRetentionPolicyDays` set in the install doc, not left at the chart default | First multi-day or multi-run dogfood session left unattended |

## Security Mistakes

| Mistake | Risk | Prevention |
|---------|------|------------|
| Treating a self-hosted, LAN-only Phoenix as inherently safe because "it's internal" | Full prompt/completion history — including whatever repo content or credentials leaked into `events.jsonl` — is readable by anyone who can reach the Service, with zero authentication by default | `PHOENIX_ENABLE_AUTH=true` in the documented install recipe as a default, not an opt-in afterthought |
| Assuming credproxy's key-isolation means span payloads are already "safe" | Credproxy only protects `ANTHROPIC_API_KEY`; it has no visibility into secrets the model reads from repo files and echoes into its own output, which flow straight into `events.jsonl` and then into span attribute values | Apply `redact.SecretPatterns` (or an equivalent scrub) to message content at the reporter's span-emission boundary — the direct analog of credproxy's env-var boundary (Pitfall 1) |
| Hardcoding a future Phoenix auth token as a plain env var if `OTEL_EXPORTER_OTLP_HEADERS` is ever wired for authenticated export | Token visible to anyone with pod-read RBAC via `kubectl get pod -o yaml`, same class of leak credproxy exists to prevent for the Anthropic key | Reference the token via a K8s Secret using the same pattern already established for git creds and LLM API keys — never a literal `value:` in a chart template |

## UX Pitfalls

| Pitfall | User Impact | Better Approach |
|---------|--------------|-------------------|
| Operator follows the quickstart once, on one project, at the chart's default 10% sample rate, and sees an empty Phoenix UI | Concludes the whole milestone's headline feature is broken on the very first try (Pitfall 6) | Quickstart doc explicitly overrides `tracesSamplerArg=1.0` for first-run/single-project verification, with a clear callout distinguishing it from the steady-state production default |
| `ArtifactPath` attribute renders in Phoenix as an opaque PVC path string with no click-through | Operator hits a dead end trying to see the actual payload that was deferred out of the span | Document the `tide artifact-get <namespace>/<project>/<path>` recipe directly next to the Phoenix screenshot in `observability.md`, mirroring the dashboard's existing click-through pattern |
| Dispatch spans and reporter-synthesized LLM spans use similar generic names with no visual distinction between "still waiting on the subagent" and "reporter hasn't run yet" | Operator can't tell from the trace tree alone whether a stalled trace means a slow LLM call or a stuck/failed reporter Job | Distinct span names/kinds for the manager's dispatch span vs. the reporter's LLM child span, consistent with `AgentInvocation`'s existing `agent.role` (planner\|executor) vocabulary |

## "Looks Done But Isn't" Checklist

- [ ] **Span emission wired in the manager:** often missing the reporter-side LLM child spans entirely — verify Phoenix shows `LLMInputMessages`/`LLMOutputMessages` nested *under* the dispatch span, not just a bare `AGENT`-kind span with no children.
- [ ] **Self-hosted Phoenix "works":** often missing PV-backed persistence — verify by restarting the Phoenix pod and confirming prior traces still query afterward, not just that traces appeared once during the demo.
- [ ] **Trace-context propagation "works":** often missing the failure/timeout path — verify a Failed or timed-out Task still produces a properly closed span in Phoenix, not a span that never receives an `End()` call.
- [ ] **Sampler "configured":** often missing that the chart's `0.1` default was never overridden for the milestone's own end-to-end proof run — verify the actual install command used for the proof screenshot explicitly set `tracesSamplerArg`.
- [ ] **D-O5 payload boundary "decided":** often missing an actual size/redaction guard at the call site, not just a doc-level decision — verify a synthetic oversized (>1 MB) or secret-containing message dispatched through the real path neither errors the OTLP exporter nor leaks the secret pattern into an emitted span.

## Recovery Strategies

| Pitfall | Recovery Cost | Recovery Steps |
|---------|-----------------|------------------|
| Duplicate/fragmented traces from ungated reconcile-loop span creation (Pitfall 3) | MEDIUM | Add the state-transition guard, redeploy; already-ingested bad spans stay wrong in Phoenix's history but self-correct going forward — bound their visible lifetime with a sane retention policy (Pitfall 8) rather than manually purging |
| Dropped spans from an unflushed short-lived Job (Pitfall 4) | LOW | Add the missing `defer shutdown(ctx)` across every `tide-reporter` exit path, rebuild the image, redeploy — pure code fix, no data-model change |
| Orphaned LangGraph traces from missing context extraction (Pitfall 7) | MEDIUM | Fix the extraction code in the LangGraph subagent adapter; historical orphaned traces stay disconnected in Phoenix but are harmless noise, not a correctness bug, unless retention cost makes cleanup worthwhile |
| Secret/PII already ingested into Phoenix's database (Pitfall 1) | HIGH | No automatic scrub-after-ingest exists; recovery is manual trace/project deletion via Phoenix's admin API/UI plus rotating whatever credential leaked — treat with the same severity as any other secret-leak incident, not as a trace-hygiene cleanup task |

## Pitfall-to-Phase Mapping

| Pitfall | Prevention Phase | Verification |
|---------|-------------------|----------------|
| 1 — unredacted `events.jsonl` into spans | LLM message-array spans (in-namespace emitter) | Test asserts a known secret pattern injected into a fixture `events.jsonl` never appears in emitted span attribute values |
| 2 — OTLP 4 MB ceiling from full inlining | LLM message-array spans (in-namespace emitter) | Test dispatches/synthesizes an oversized message and asserts `ArtifactPath` fallback, not an OTLP export error |
| 3 — reconcile-loop span duplication/fragmentation | Dispatch-chain span emission (manager) | Test calls `Reconcile()` twice for the same object and asserts exactly one dispatch span was started and correctly parented |
| 4 — short-lived Job drops spans on exit | LLM message-array spans (in-namespace emitter) | `tide-reporter` integration test confirms spans reach a fake OTLP collector before the process exits on every exit path |
| 5 — cross-pod clock skew in synthesized spans | End-to-end proof | Test/assertion that synthesized child-span timestamps fall within `[parent.start, parent.end]` |
| 6 — `parentbased_traceidratio` per-run coin flip | Self-hosted Phoenix surface (docs) | Quickstart doc reviewed to confirm it overrides `tracesSamplerArg` for single-run verification; live proof run screenshot uses the override |
| 7 — double-instrumentation with a self-instrumenting runtime | Design constraint captured now; contract-tested in End-to-end proof | Stub-runtime test confirms `TRACEPARENT` env-carrier extraction actually activates the propagated context before span creation |
| 8 — Phoenix's ephemeral/infinite-retention/no-auth defaults | Self-hosted Phoenix surface (docs) | Install recipe reviewed for explicit `persistence`/`database.defaultRetentionPolicyDays`/`PHOENIX_ENABLE_AUTH` overrides; `kubectl get pvc` and Phoenix login page both verified present after following the doc |
| 9 — Phoenix resource footprint vs. 8 GiB dev VM | Self-hosted Phoenix surface (docs) | Install recipe reviewed for right-sized `persistence.size` and skips the bundled Postgres subchart for dev/kind installs |

## Sources

- Codebase (HIGH confidence, direct inspection): `pkg/otelai/attrs.go`, `pkg/otelai/doc.go`, `internal/otelinit/provider.go`, `internal/subagent/anthropic/subagent.go`, `internal/harness/redact/patterns.go`, `internal/harness/redact/redact.go`, `internal/harness/harness.go`, `internal/credproxy/doc.go`, `cmd/tide-reporter/main.go`, `internal/controller/task_controller.go`, `internal/controller/reporter_jobspec.go`, `charts/tide/values.yaml`, `docs/observability.md`, `.planning/PROJECT.md` (v1.0.8 Current Milestone section)
- [Phoenix — Persistence](https://arize.com/docs/phoenix/deployment/persistence) — SQLite default ephemerality, `PHOENIX_WORKING_DIR`, `PHOENIX_SQL_DATABASE_URL`, `PHOENIX_DEFAULT_RETENTION_POLICY_DAYS` (Context7-verified, HIGH confidence)
- [Phoenix — Authentication](https://arize.com/docs/phoenix/self-hosting/features/authentication) — `PHOENIX_ENABLE_AUTH`, `PHOENIX_SECRET`, `PHOENIX_DEFAULT_ADMIN_INITIAL_PASSWORD`, bearer-token/API-key model (WebSearch + Context7-verified, HIGH confidence)
- Arize Phoenix OpenInference exporter docs, "Common Pitfalls" section (via Context7 `/arize-ai/phoenix`, source `docs/phoenix/tracing/concepts-tracing/otel-openinference/exporter.mdx`) — gRPC 4 MB message-size limit, exporter-timeout-vs-processor-timeout interaction (HIGH confidence, official docs)
- Arize Phoenix Helm chart `helm/README.md` and `helm/templates/NOTES.txt` (via Context7 `/arize-ai/phoenix`) — `persistence.enabled`/`postgresql.enabled` mutual exclusion, default `20Gi` storage sizing, `database.defaultRetentionPolicyDays` (HIGH confidence, official chart source)
- Phoenix ports/transport reference (via Context7 `/arize-ai/phoenix`, source `docs/phoenix/self-hosting/configuration.mdx` and `docs/phoenix/tracing/concepts-tracing/otel-openinference/exporter.mdx`) — OTLP gRPC on 4317, OTLP/HTTP+UI on 6006 (HIGH confidence)
- Community/blog resource-sizing discussion (WebSearch, MEDIUM confidence — no single authoritative "minimum requirements" page found; figures are practitioner-reported, ranging 1–3 GiB RAM for small deployments, 20,000-span in-memory queue at ~50 KiB/span)
- [OpenTelemetry — Environment Variables as Context Propagation Carriers](https://opentelemetry.io/docs/specs/otel/context/env-carriers/) — `TRACEPARENT`/`TRACESTATE` as the standard subprocess-boundary carrier convention (WebSearch-verified against the official OTel spec site, HIGH confidence)
- `open-telemetry/opentelemetry-go` `BatchSpanProcessor` defaults (`DefaultScheduleDelay`=5000ms, `DefaultMaxQueueSize`=2048, `DefaultExportTimeout`=30000ms) and the general "short-lived process drops spans without an explicit flush/shutdown" failure mode (WebSearch, cross-referenced against multiple independent write-ups, MEDIUM-HIGH confidence — exact default values corroborated by the SDK's own godoc, referenced but not directly re-fetched in this pass)
- Existing project memory (`CLAUDE.md` Operating Notes) — 8 GiB dev VM constraint, "one heavy run at a time" OOM-avoidance discipline used to ground Pitfall 9's resource-collision reasoning (HIGH confidence, first-party project record)

---
*Pitfalls research for: Adding OpenInference trace emission + self-hosted Arize Phoenix to TIDE (v1.0.8)*
*Researched: 2026-07-15*

