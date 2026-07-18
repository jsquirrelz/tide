# Pitfalls Research

**Domain:** Adding an in-cluster LLM verify tier (plan-check / level-verify / integration-check) on a new read-only Python/LangGraph specialist image to an already-shipped Go/controller-runtime K8s orchestrator (TIDE v1.0.8) with an established envelope/credproxy/PodJob dispatch seam, two existing halt conditions with a known resume-ordering hazard class, a git-as-artifact-store convention, and a shipped OpenInference/OTel trace tree with a runtime-neutral `SelfInstruments` adapter seam.
**Researched:** 2026-07-18
**Confidence:** HIGH on architecture/pitfall shape (grounded directly in TIDE source and prior incident post-mortems); MEDIUM/LOW on Python-ecosystem specifics (httpx/langchain-anthropic internals — flagged per-claim below).

## Critical Pitfalls

### Pitfall 1: Double span emission — the verifier both self-instruments AND gets synthesized

**What goes wrong:**
`openinference-instrumentation-langchain` auto-instruments any LangChain/LangGraph process it's imported into — it patches `langchain_core.callbacks` and emits OpenInference spans in-process to the configured OTLP endpoint the moment a `ChatAnthropic`/`ChatOpenAI` call happens. TIDE's reporter Job *also* has a code path (`internal/reporter/tracesynth.go`, gated by `pkg/dispatch.SelfInstruments(vendor)`) that synthesizes spans from `events.jsonl` for CLI-runtime dispatches. If the verifier image is registered under a vendor string that `SelfInstruments` still maps to `false` (today it returns `false` for every known vendor — `anthropic`, `openai`, `google`, `xai`, `opencode` — and fails closed for anything unrecognized, per `pkg/dispatch/vendor_capabilities.go`), the reporter's `--skip-message-spans` guard never fires, `tracesynth.go` still tries to parse `events.jsonl`, and Phoenix ends up with two independent span trees for the same dispatch: one native (LangChain instrumentation → OTLP directly from the pod) and one synthetic (reporter Job → OTLP from `events.jsonl`). Because both use a `traceparent`-derived parent, they'll often nest under the same root, making the double-count *harder* to notice, not easier — it looks like a busy trace, not a duplicate one.

**Why it happens:**
`SelfInstruments` was deliberately built fail-closed (D-03: "every current and unrecognized vendor returns false... a false 'synthesize' assumption produces (at worst) visible duplicates — always fail toward visibility, never toward silence"). That's the *right* default for an unknown vendor arriving unannounced, but it means the LangGraph verifier image does not get skip-synthesis behavior for free — someone has to add a vendor literal (e.g. `"langgraph-verifier"` or reuse `"anthropic"` with a runtime discriminator) to the `switch` and flip it to `true`, and wire the envelope/Job spec to declare it. Miss that one-line change and the double-emission bug ships silently, because both paths individually look correct in isolation — only a live two-span-tree comparison in Phoenix reveals it (this is exactly the failure class Phase 45's "stub-runtime contract test, zero duplicate spans" was built to catch, but only for a *stub* — it must be re-exercised against the *real* LangGraph image, whose instrumentation is a live third-party library, not TIDE's own stub).

**How to avoid:**
1. Add a real vendor literal for the LangGraph verifier to `pkg/dispatch.SelfInstruments` (e.g. `"langgraph"`) returning `true`, and thread it through `ResolveProvider(...).Vendor` for the verifier's `Provider.Vendor` field — the same lookup call sites already exist at all five dispatch controllers (`task_controller.go:1117`, `phase_controller.go:622`, `plan_controller.go:675`, `milestone_controller.go:666`, `project_controller.go:1933`); the verifier dispatch site needs the identical call.
2. Do **not** rely on the LangGraph image "opting itself out" — `SkipMessageSpans` is manager-computed and carried on the reporter Job spec as data (`reporter_jobspec.go`'s `SkipMessageSpans` field), specifically so "the reporter itself... trusts only the manager-computed boolean carried on the Job" (never a self-report from the pod, which an adversarial or buggy image could get wrong).
3. Write a conformance test that dispatches the *real* LangGraph verifier image (not a stub) end-to-end against a test OTLP collector and asserts exactly one span tree per dispatch — the Phase 45 stub test proves the seam compiles; it does not prove the real `openinference-instrumentation-langchain` package's behavior matches the `SelfInstruments=true` assumption (e.g., it must not ALSO write an `events.jsonl` the reporter tries to parse if the skip guard has any gap).
4. Because the verifier's OTLP export happens in-process during a K8s Job that exits when done, confirm the LangChain instrumentation's exporter flushes on exit (i.e. the pod doesn't get SIGKILLed before its `BatchSpanProcessor` drains) — this is the same class of concern the reporter itself has, just moved into the subagent pod.

**Answering "does it belong in the trace tree, and under which parent?":**
Yes — as a **new, distinct span kind**, not folded into the AGENT span it's checking. OpenInference's semantic conventions define an `EVALUATOR` span kind precisely for "a call to a function or process performing an evaluation of ... outputs" — distinct from `AGENT` (which TIDE's `pkg/otelai.AgentInvocation` hardcodes today) and from `LLM` (`pkg/otelai.LLMSpanKind`). `pkg/otelai/attrs.go` has no `EVALUATOR` helper yet; add one (`EvaluatorSpanKind()`) mirroring `LLMSpanKind()`'s shape. Parent it as a **sibling** to the level's own AGENT span under the same level node — not nested inside it — because the verify dispatch is read-only, post-hoc, and a genuinely separate K8s Job/dispatch from the authoring dispatch it inspects; nesting it inside the authoring span would misrepresent it as part of that dispatch's work. The manager already has the mechanism to do this correctly: it's the same `TRACEPARENT`-injection contract used for every other dispatch (`opts.TraceParent` in `jobspec.go`), rooted at the level whose artifact is being checked.

**Warning signs:** Phoenix shows two span trees (or two root-adjacent subtrees) for what was one dispatch; span counts roughly double after the verify tier ships versus the PROOF-01 baseline (392 spans); `events.jsonl` exists in the verifier's workspace even though the image never uses TIDE's harness event-capture format.

**Phase to address:** The trace-adapter/vendor-registration work must land in the SAME phase that first wires the verifier's dispatch call sites — do not sequence "ship the verifier" before "register its `SelfInstruments` vendor," or the double-emission window is live from day one of the beachhead. Verification: a live dispatch against a test OTLP collector, asserting span count == 1 tree per verify stage, checked into CI as a regression the way Phase 45's stub test was.

---

### Pitfall 2: httpx (and langchain-anthropic) may not honor SSL_CERT_FILE the way the CLI/Node path does

**What goes wrong:**
The existing CLI subagent trusts credproxy's self-signed CA via `NODE_EXTRA_CA_CERTS` (Node-specific, confirmed working — `internal/dispatch/podjob/jobspec.go:429`). The polyglot milestone doc's assumption A1 is that the Python analog (`SSL_CERT_FILE`/`REQUESTS_CA_BUNDLE`) "just works" for httpx-based SDKs. Verified findings (MEDIUM confidence, official httpx docs): httpx *does* support `SSL_CERT_FILE` — **but only when `trust_env=True`**, which is httpx's own client default, not necessarily the default of every wrapper built on top of it. The Anthropic Python SDK (which `langchain-anthropic`'s `ChatAnthropic` wraps) constructs its **own** internal `httpx.Client`/`httpx.AsyncClient` via a custom transport (`DefaultHttpxClient`/`_DefaultHttpxClient` in `anthropic._base_client`) rather than a bare `httpx.Client()` — and there is at least one confirmed upstream report (`anthropics/anthropic-sdk-python#923`) of proxy-related env vars *not* being honored through that custom transport. Separately, an open `langchain-ai/langchain` feature request (#35843, unresolved as of this research) asks for `ChatAnthropic` to accept a custom `httpx.Client`/`httpx.AsyncClient` for exactly this class of problem (mTLS / custom CA for enterprise proxies) — its own text states today's workaround is "monkey-patching or wrapping the internal client creation, which is fragile." That combination means: (a) it is NOT verified that `SSL_CERT_FILE` alone suffices through `langchain-anthropic`'s specific client-construction path, and (b) if it doesn't, there is no first-class passthrough today — only monkey-patching.

**Why it happens:** The polyglot doc correctly flagged this as assumption A1 and marked it "confirm during build," but the milestone doc still frames it as effectively settled ("Expected: yes... verify at build time"). The two SDK-side signals above (custom transport construction + an open unresolved GitHub feature request asking for exactly this passthrough) are enough evidence that the honest status is "unverified, plausibly broken," not "verify as a formality."

**How to avoid:**
1. Treat this as a build-time SPIKE, not a build-time formality — schedule it as its own small task at the start of the image-build phase, before any verify-stage logic is written on top of it.
2. Concrete test recipe: stand up credproxy with its self-signed CA (already exists — `internal/credproxy/server.go`'s `TLSConfig`), build a minimal Python container with `langchain-anthropic` pinned, set `ANTHROPIC_BASE_URL=https://127.0.0.1:8443`, `SSL_CERT_FILE=/etc/tide/proxy/ca.crt`, and `REQUESTS_CA_BUNDLE=/etc/tide/proxy/ca.crt`, then make one real `ChatAnthropic(...).invoke(...)` call inside the pod against the sidecar. Pass/fail is binary: connection succeeds (verifies the whole chain, not just "httpx supports the env var" in isolation) or it raises an SSL verification error.
3. If it fails: the documented httpx-level fallback is passing `verify=<ssl.create_default_context(cafile=...)>` explicitly to a custom `httpx.Client`, but `ChatAnthropic` doesn't expose that hook yet (per #35843) — so the fallback becomes constructing the underlying `anthropic.Anthropic`/`AsyncAnthropic` client with an explicit `http_client=` and passing THAT into `ChatAnthropic(..., anthropic_client=...)` if that constructor argument exists in the pinned version, or dropping to `anthropic-sdk-python` directly instead of the LangChain wrapper for the HTTP layer. Either fallback is a real code path, not a prompt fix — plan schedule slack for it rather than discovering it mid-build.
4. Whatever the outcome, write it down as a decision record (like CACHE-01's) rather than re-verifying from scratch on the next rung of the successor-runtime ladder — the next migrating role (planner) hits the identical CA-trust question.

**Warning signs:** `SSLCertVerificationError` / `httpx.ConnectError` in the verifier pod's logs when calling the LLM; the credproxy sidecar shows a completed TCP handshake at the network level but the client-side TLS verification still fails (rules out "sidecar not ready" and confirms it's a trust-store problem specifically).

**Phase to address:** Image-build phase, as a gating spike before any verify-stage business logic depends on outbound LLM calls working. Verification: the live curl-equivalent test above, checked into the image's build/test recipe so a future `langchain-anthropic` bump can't silently regress it.

---

### Pitfall 3: LangGraph / langchain-anthropic 1.x churn breaks the pin between milestones

**What goes wrong:** The polyglot doc's own research-validity note says "Research valid until ~2026-07-15 (LangGraph 1.x moves fast; re-pin versions when this milestone is activated)" and separately notes "40+ releases already" on the 1.x line. Floating or stale pins mean: (a) a rebuild months from now silently picks up breaking changes in tool-calling semantics, `with_structured_output` behavior, or `init_chat_model` argument handling; (b) `deepagents` (an *assumed*, unverified dependency per A2) may not even exist at the pinned surface the doc describes by the time this milestone is actually built.

**Why it happens:** Go's stack has an explicit version-coupling discipline (STACK.md: "pin Anthropic SDK to a minor... it rev-bumps weekly with beta surfaces"; kind images pinned by `@sha256`). Python/pip ecosystems don't get that discipline for free — `requirements.txt`/`pyproject.toml` pins are easy to leave loose (`langgraph>=1.0`) under time pressure, and nothing in TIDE's existing CI enforces patch-exactness for a language TIDE has never shipped code in before.

**How to avoid:**
1. Patch-pin exactly (`langgraph==1.2.x`, `langchain-anthropic==X.Y.Z`), mirroring the Go SDK pinning rule verbatim — add this to STACK.md for this milestone the same way the Anthropic Go SDK pin is documented today.
2. Re-verify pins as an explicit build-phase task, not an assumption carried from the exploration doc — the doc itself flags this "research valid until" boundary; the plan phase should treat it as expired the moment `/gsd:new-milestone` actually starts rather than trusting the 2026-07-06 findings unchanged.
3. Add a CI job that fails the build on an unpinned/range dependency in the Python image's lockfile (the Go-side has `make verify-*` analyzer gates for its own invariants — this is the Python equivalent of that discipline, scoped narrowly: reject `>=`/`^`/no-version specifiers in the verifier image's dependency file).
4. Confirm `deepagents` (A2) at its pinned version actually ships the assumed file/bash tool surface before committing code to depend on it — if it doesn't, hand-authored `@tool` functions are the fallback (still tractable per the doc, just more tasks).

**Warning signs:** A routine `docker build` of the verifier image months after this phase ships produces a different `pip freeze` output than the one recorded at build time; `with_structured_output` starts returning a shape that doesn't match the previously-working Pydantic model.

**Phase to address:** Image-build phase — lockfile pin + CI gate land together with the first Dockerfile. Verification: `make verify-<verifier>-pins` (or equivalent) analogous to the Go import-firewall analyzers, checked in CI.

---

### Pitfall 4: Read-only enforcement relies on the prompt instead of a structural boundary

**What goes wrong:** The verifier image ships with git and bash (needed to run the declared gate command and read the worktree) — the same primitives an authoring image uses to commit and push. A prompt that says "never commit, never push, never author CRDs" is not an enforcement mechanism; it's a request to an LLM that can be wrong, jailbroken by adversarial repo content it's asked to evaluate, or simply buggy in an agent-loop edge case (e.g., a LangGraph tool node that calls `git commit` because a badly-scoped `bash` tool has no path/command restriction). If the only thing standing between "read-only verifier" and "verifier that pushes a commit" is instruction-following, this is a single-point-of-failure that a determined or confused execution can breach — and unlike the read-only *dashboard* (whose read-only-ness is enforced by there being no mutation API at all, per the existing Key Decision), the verifier has the actual git/bash primitives live in its pod.

**Why it happens:** The polyglot doc's contract table shows git + bash as necessary, general-purpose tools shared with the authoring image family; there is no TIDE precedent yet for a *partially capable* image (has the tools, must not use all of them) — every existing dispatch class either has full authoring capability (planner/executor) or none (dashboard, which has no tools at all).

**How to avoid — structural, not prompt-only:**
1. **Run the verifier pod's worktree mount read-only at the K8s level wherever possible.** The `VolumeProjectWorkspace` mount that authoring images get read-write should be a `ReadOnly: true` `VolumeMount` for the verifier (mirroring the pattern already used for the credproxy sidecar's cert mount, `jobspec.go:434`, which sets `ReadOnly: true`). Any incidental writes the verifier legitimately needs (test caches, build artifacts while running the gate command) go to a *separate*, ephemeral `emptyDir` scratch volume, never the mounted worktree — this makes "commits to the run branch" structurally impossible, not just discouraged, because the git working tree the verifier can execute a gate command against is not the same writable path an authoring dispatch uses.
2. **Never inject git push/write credentials into the verifier's env at all.** The authoring path's git credentials (whatever secret/token drives `git push`) should simply not appear in the verifier's `EnvFrom`/`Env` — omission, not permission-checking, is the enforcement. If the verifier never holds write credentials, "never pushes" isn't a behavior guarantee, it's a capability fact.
3. **No child-CRD authoring path wired at all** — the manager-side reconciler should not even look for `children/*.json` in a verifier dispatch's output, structurally mirroring how the milestone doc already says "no five-template parity port" — if the consuming code path doesn't exist, an errant write is inert, not just against policy.
4. Reserve prompt instructions ("coverage, not conservatism"; "never commit") for the *judgment* layer (what findings to report, how to phrase them) — not as the sole safeguard against a structural capability the pod shouldn't have in the first place.
5. Write a contract test analogous to Phase 45's stub test: dispatch the verifier against a fixture worktree, have its LangGraph agent loop *attempt* a `git commit`/`git push` (adversarial fixture prompt), and assert it fails at the filesystem/credential layer — not merely that the "good" prompt path doesn't try.

**Warning signs:** The run branch gains a commit authored by the verifier's agent identity; the verifier's `git status` inside a fixture worktree shows staged changes after a run.

**Phase to address:** Same phase as the image contract itself (Pillar 2) — the read-only volume mount + credential omission are part of the Job spec, so they belong wherever `jobspec.go`'s verifier branch is authored, not deferred to a later hardening pass. Verification: the adversarial fixture test above, plus a static check that no git-write secret name appears in the verifier's `EnvFrom`.

---

### Pitfall 5: Cost multiplication compounds with the existing concurrency ceiling, not just the budget cap

**What goes wrong:** A verify stage at every lifecycle seam roughly *doubles* LLM dispatches for the levels it covers (each authoring dispatch gets a companion verify dispatch). This is a budget problem the existing `BudgetConfig`/`ReservationStore`/`BudgetBlocked` machinery can absorb (it already halts on cap-exceeded) — but it's *also* a concurrency problem the budget system does NOT model: run-2b's D3 incident OOM'd a single-node kind cluster at ~60 in-flight planner pods because the in-process semaphore capped `r.Create` calls, not actually-running pods (fixed in Phase 32 via `plannerInFlightCount` + `PlannerPool.Capacity()`, default cap 4). If verifier dispatches are a THIRD, uncounted dispatch class — not counted against `plannerInFlightCount`'s pool, and with no analogous `verifierInFlightCount` gate of its own — the exact same OOM class recurs: wave-parallel fanout now produces authoring pods AND verify pods concurrently, silently doubling the effective pod count the Phase 32 fix was sized against, on the same single-node dev/CI topology that already failed once at this exact failure mode.

**Why it happens:** Phase 32's concurrency gate was built and sized for the dispatch classes that existed at the time (planner-tier). A new dispatch class arriving later doesn't automatically get counted by a gate that was scoped to "planner" — this is a "looks done but isn't" trap: the cap machinery exists and looks like it covers "concurrency," but it covers a specific pool, and a new pool needs its own accounting or must be folded into the existing one explicitly.

**How to avoid:**
1. Verify dispatches MUST be counted against a concurrency gate before this ships — either extend `plannerInFlightCount`'s query to include the verifier Job label (if verify stages share the planner pool's budget) or add a dedicated `verifierInFlightCount`/`VerifierPool.Capacity()` mirroring Phase 32's exact shape. Do not assume the existing planner cap "just also" covers a label it was never queried against.
2. Set a conservative **default posture** that bounds the multiplier rather than assuming the operator will configure it down: per the milestone doc's own open question 6 (off / milestone+project-only / all levels), default to the narrowest useful scope — e.g., level-verify + integration-check at milestone/project boundaries only, off at task tier by default — since task-tier is where wave-parallel fanout is widest and the OOM history is worst. Make "all levels" an explicit opt-in, not the shipped default.
3. Cost-model the multiplier explicitly before launch: document expected $ delta per run (roughly 1x for plan-check + level-verify per authored level, plus integration-check at boundaries) so the operator's budget cap is sized with the new stage in mind, not just legacy authoring costs.
4. Reuse a cheap model tier for verify stages by default (the milestone doc's open question 4 — "cheap-model verify vs. planner-tier") — this is both a cost AND (modestly) a latency lever, and the per-level model-override slot already exists in `SubagentConfig`/`LevelConfig` to express it without controller changes.

**Warning signs:** Pod count in a wave roughly doubles versus pre-verify-tier baselines; a kind/single-node dev cluster starts showing the same `exit 137`/OOM signature Phase 32 fixed, on a cluster sized for pre-verify-tier concurrency.

**Phase to address:** The concurrency-gate extension must land in the SAME phase that wires the verifier dispatch call sites (not a follow-up patch) — this is precisely the shape of the run-2b D3 defect (a dispatch path shipped before its concurrency accounting existed). Verification: a kind-cluster load test dispatching a wave with both authoring and verify pods concurrently, confirming pod count stays under the sized cap, mirroring Phase 32's own regression test shape.

---

### Pitfall 6: Unbounded or ambiguous re-plan loop on repeated plan-check REJECT

**What goes wrong:** plan-check REJECT re-dispatches the planner with findings appended. Without a hard bound and a clear terminal, a plan that a verifier keeps rejecting (e.g., an ambiguous outcome prompt, a verifier with an overly strict rubric, or the same root disagreement recurring) churns indefinitely — burning planner-tier LLM cost (typically the most expensive tier) on every iteration with no forward progress, and with no operator-visible signal other than a project that appears "stuck re-planning" in the dashboard.

**Why it happens:** The milestone doc already flags this as open question 1 — "N default (1 or 2), and whether the loop shares `maxAttemptsPerTask` machinery or gets its own counter" — and the risk is picking the wrong answer to the SECOND half of that question. `maxAttemptsPerTask` (`task_controller.go:729`, `project.Spec.MaxAttemptsPerTask`) exists for a semantically different thing: retrying the SAME task execution after a transient/execution failure. A plan-check reject-loop is re-authoring DIFFERENT content each iteration (a new plan draft responding to findings) — conflating the two counters means a task's execution retry budget and a plan's re-authoring budget compete for the same number, and a config change tuned for one silently changes the other's behavior.

**How to avoid:**
1. Give the re-plan loop its own dedicated counter/field (e.g. `Status.PlanCheck.Attempts` on the Plan CRD, mirroring the shape `Status.BoundaryPush.Attempts` already uses on Project per Phase 34's D-13 pattern cited in `resume.go`) — do not overload `maxAttemptsPerTask`.
2. Default N conservatively (1, per the milestone doc's own framing "1 or 2") — a single re-plan attempt with findings appended, then a **terminal halt**, not a Failed/silently-abandoned state. The terminal state should be the SAME `ConditionVerifyHalt` class used for level-verify/integration-check BLOCKED (see Pitfall 8) — "gave up after N re-plan attempts" is a halt requiring a human decision, not a task failure to retry blindly.
3. Make the halt message actionable: include the LAST rejection's findings (or a pointer to them, given the size×locality rule — see Pitfall 7) so the operator isn't left re-deriving why plan-check kept failing.
4. Log/metric the re-plan counter per project so "stuck re-planning" is dashboard-visible before it silently burns the full attempt budget, not only discoverable after the halt fires.

**Warning signs:** A project's cost accelerates disproportionately to visible progress (planner-tier dispatches without a corresponding new Phase/Plan artifact landing); `Status.PlanCheck.Attempts` (or equivalent) reaches N repeatedly across different levels of the same project, suggesting a systemic rubric mismatch rather than a one-off bad plan.

**Phase to address:** Lands with plan-check itself (Pillar 1's first stage) — the bound and its terminal halt are not a hardening follow-up, they're part of what makes plan-check safe to enable by default. Verification: an envtest exercising N consecutive REJECTs and asserting the halt fires at N+1 with findings attached, not an (N+1)th re-dispatch.

---

### Pitfall 7: `gate_decision` findings persistence violates the etcd size×locality rule

**What goes wrong:** A verifier's findings (especially coverage-not-conservatism prompted findings, per CLAUDE.md's subagent-tuning note — "find everything with severity/confidence tags") can be a substantial blob: potentially many findings, each with a description, severity, confidence, and often a code/diff excerpt. If this lands wholesale in a CRD `.status` field, it repeats the exact class of mistake `envelopes-as-artifacts` was decided against project-wide (size×locality rule: "never blobs in etcd... size×locality rule (PVC for large same-ns / API-created CRs for small cross-ns / termination msg for tiny / object-store-behind-interface only for future large cross-ns)"). It's also a smaller-scale repeat of the `TerminationStub` discipline (`<4 KB`, size-tested by `TestNewTerminationStub_StaysSmall`) — a `gate_decision` findings blob has no equivalent invariant test yet, and CRD size grows unboundedly with the number/verbosity of findings, especially once integration-check runs across a whole project's sibling outputs (the largest-fan-in verify stage).

**Why it happens:** The natural, easiest-to-implement shape ("just add a `Findings []Finding` field to the status struct") is exactly the shape every prior TIDE persistence decision has explicitly rejected (`make verify-no-aggregates` already greps for exactly this class of mistake at the schema level, though its regex is scoped to `Schedule`/`Waves[]`/`IndegreeMap`, not findings-shaped fields — a new field type here would not trip the existing guard).

**How to avoid:**
1. Apply the SAME size×locality split the rest of the system uses: a bounded, small **status condition** (`ConditionVerifyHalt` itself + a short human-readable `Message`, on the order of the existing halt conditions' one-paragraph messages) carries the *verdict* and enough context to act; the **full findings artifact** goes where Phase 37 already put planning artifacts — the git run branch (`.tide/planning/`-style convention), read via gitfetch, NOT a ConfigMap, NOT etcd status. This is a direct precedent reuse, not a new decision: "Git is the planning-artifact store... NOT ConfigMaps; full artifact visibility, no truncation anywhere" (Phase 37, validated).
2. If a small in-CRD summary is wanted for dashboard-at-a-glance (the milestone doc's open question 2 floats this explicitly), cap it hard and test the cap the same way `TestNewTerminationStub_StaysSmall` tests the 4 KB stub — e.g. top-3-findings-by-severity + a count of the rest, never "however many findings the verifier produced."
3. Do not let "coverage, not conservatism" prompting (which deliberately produces MORE findings, not fewer) leak into "therefore store all of them in status" — the prompting guidance governs what the verifier *finds*; it says nothing about where the findings *live*. Keep those two decisions separate explicitly when writing the requirement.

**Warning signs:** A `kubectl get plan -o yaml` on a project with a chatty verifier shows a multi-KB (or larger) `.status.findings` field; `make verify-no-aggregates`-style CI gate doesn't catch it because it was never extended to look for findings-shaped fields.

**Phase to address:** The `gate_decision` persistence shape is a schema decision that must be locked BEFORE the reconciler-side halt logic is written (the halt condition's `Message` field and the artifact-location convention are both schema surface). Verification: a size-bound unit test on the CRD-status finding summary (mirroring the TerminationStub test), plus confirming the full findings artifact round-trips through gitfetch the same way Phase 37's planning artifacts do.

---

### Pitfall 8: ConditionVerifyHalt repeats Phase 25's exact resume-ordering bug class

**What goes wrong:** Phase 25 shipped `ConditionFailureHalt` with a real, caught-late bug: `tide resume --retry-failed` cleared the halt condition BEFORE resetting the Failed task phases that caused it — meaning a straggler reconcile between the clear and the reset (or an unrelated task reconciling after the clear) could re-observe the still-failed state and re-stamp the halt, making the resume a no-op, or (the missing-time-fence half of the same bug) a STALE pre-resume failure signal could re-trigger the halt after a legitimate resume, freezing a project that had already been fixed. Both `failure_halt.go` and `billing_halt.go` now carry an explicit resume time-fence (`AnnotationFailureResumedAt`/`AnnotationBillingResumedAt`, compared against the failing event's own completion/creation timestamp) specifically because the ordering bug was caught by code review, not by the green test suite, on the FIRST halt condition built this way — meaning a naive `ConditionVerifyHalt` implementation that doesn't independently re-derive this exact fix is expected to reproduce the identical bug, not a new one.

**Why it happens:** The halt-condition pattern (stamp on bad event, clear via CLI verb, gate dispatch on the condition) looks simple enough to re-implement from scratch by copying the shape without copying the ordering discipline — "clear the condition, then reset the underlying state" is the natural (wrong) order; "reset the underlying state such that new stamps can't fire, THEN clear, with a time-fence guarding against stragglers" is the (correct, harder-won) order. This is exactly the kind of repeated-across-authors structural evidence CLAUDE.md's systems-thinking section calls out: when the same failure shape appears twice, look at the shared interface/pattern, not the individual instance.

**How to avoid:**
1. Do not write `ConditionVerifyHalt` handling from a blank slate — copy the `failure_halt.go`/`billing_halt.go` pattern file-for-file as the starting point: `checkVerifyHalt` (nil-safe boolean read), `setVerifyHaltIfNeeded` (idempotent stamp, with a resume time-fence comparing the triggering event's timestamp against an `AnnotationVerifyResumedAt`-equivalent, fail-closed on zero/unparseable timestamps toward STAMPING the halt, never toward silently clearing it), and a `tide resume`-side verb that clears the condition AND stamps the resume annotation in the same operation Phase 25 landed for the other two halts.
2. Explicitly decide (and document) what "resetting the underlying state" means for VerifyHalt specifically, since it's not identical to FailureHalt's "reset Failed Task phases" — for a plan-check REJECT-exhaustion halt it might mean resetting the re-plan attempt counter; for a level-verify/integration-check BLOCKED halt there may be NO underlying state to reset at all (the verify stage doesn't retry automatically — Pillar 3 says BLOCKED halts immediately for a human, no bounded retry). If there's genuinely nothing to reset, the time-fence discipline still matters (a stale BLOCKED verdict from before a manual fix shouldn't re-freeze a project the operator already addressed), but the "reset order" question may simplify to "just the time-fence, no companion state reset" — decide this explicitly rather than copying FailureHalt's two-step dance where only one step is needed.
3. Reuse the SAME `tide resume` CLI verb surface (add a mode/flag) rather than inventing a fourth recovery command — operators already have `tide resume` (billing) and `tide resume --retry-failed` (failure); a third bespoke verb multiplies the CLI surface for a pattern that should compose.
4. Write the regression test Phase 25's review added AFTER the fact (WR-03) as a TEST-FIRST item for VerifyHalt: a straggler reconcile between clear and reset must NOT re-stamp the halt; a pre-resume-timestamp verify event must NOT re-freeze a project resumed after it.

**Warning signs:** `tide resume` (verify variant) appears to succeed (exits 0, patches the condition) but the project immediately re-halts on the next reconcile; a `kubectl get project -o yaml` shows `ConditionVerifyHalt=True` with a `LastTransitionTime` OLDER than the operator's own resume action.

**Phase to address:** Same phase as the halt condition's introduction — this is not a hardening follow-up, it's the condition's correctness contract from day one, given it's the THIRD instance of this exact pattern and the bug is now a documented, known failure mode. Verification: the straggler-reconcile + stale-timestamp regression tests, written before the happy-path test, mirroring Phase 25's WR-03 proving test.

---

### Pitfall 9: `with_structured_output` failures must fail CLOSED, not silently pass

**What goes wrong:** LangChain's `with_structured_output` can fail in several ways short of a clean exception: the model returns a verdict that fails Pydantic validation and LangChain retries silently (consuming cost) before eventually raising; some provider/model combinations return an empty or partial structured object under certain conditions (truncation, refusal, malformed function-call arguments); and a caller that doesn't defensively handle "verdict object exists but its `gate_decision` field is empty/None" can end up defaulting to whatever the code's fallthrough branch does. If a coding bug or an unhandled edge case causes the reconciler to interpret "no clear verdict" as "not BLOCKED, so proceed" — i.e., **fails open** — that is strictly worse than shipping no verify tier at all: it gives the operator false confidence ("verify passed") when the true state is "verify didn't run correctly," which is a more dangerous silent-failure mode than the one this milestone exists to close (the 2026-07-03 silent-Complete incident that motivated the whole verify tier was exactly this shape — a pass criterion never actually checked, reported as done).

**Why it happens:** The path of least resistance in the reconciler is usually "if I can't parse a clear rejection, treat it as approved" (the same shortcut the mechanical Phase 34 gate deliberately avoided by making completeness a positive, git-verified check rather than an absence-of-evidence pass). Under load, timeouts, or transient LLM API issues, "malformed verdict" will happen at some nonzero rate in production — treating it as an edge case that "shouldn't happen" rather than a first-class state guarantees it eventually fails open in exactly the manner this milestone is designed to prevent.

**How to avoid:**
1. Model the verdict parse outcome as a tri-state at the envelope/reconciler boundary: `APPROVED` / `BLOCKED` / `UNPARSEABLE-OR-MISSING` — never collapse the third state into the first.
2. `UNPARSEABLE-OR-MISSING` routes to the SAME `ConditionVerifyHalt` halt path as an explicit BLOCKED verdict (fail-closed) — a verifier that couldn't produce a clean answer is treated as "couldn't confirm this is safe," not as "didn't say no." This mirrors the fail-closed defaults already established elsewhere in this codebase (`SelfInstruments`'s D-03 fail-closed default; the billing/failure halts' fail-closed handling of zero/unparseable timestamps — "never fail open toward burning credits").
3. Bound `with_structured_output`'s own retry behavior explicitly (don't let LangChain's internal retry-on-validation-failure loop run unbounded either — it has its own cost multiplier character, compounding Pitfall 5) and surface an exhausted-retries outcome as `UNPARSEABLE-OR-MISSING`, not a thrown exception the reconciler might mishandle as a generic dispatch failure (which today routes to task-retry semantics, not verify-halt semantics — conflating the two reintroduces exactly the ambiguity Pitfall 6 warns against for the re-plan counter).
4. Test this explicitly: a fixture verifier response that is empty JSON, one that's valid JSON but missing the `gate_decision` field, and one that's genuinely malformed — all three must reach the halt path, not a pass.

**Warning signs:** A project advances past a level-verify or integration-check stage with no findings artifact present at all (verifier "ran" but produced no evidence of what it checked); verifier dispatch logs show LangChain retry/validation-error messages immediately before a Job that still exits 0.

**Phase to address:** Lands with the reconciler-side `gate_decision` consumption logic (same phase as Pitfall 7/8's halt wiring) — this is the core safety property of the entire verify tier, not a polish item. Verification: the three malformed-verdict fixture tests above, gating the phase's own DoD.

---

### Pitfall 10: Porting the verifier prompt into Python drifts from the Go authoring templates over time

**What goes wrong:** The milestone doc itself is already leaning toward the right answer here ("Verifier prompt source: rendered orchestrator-side from a sixth Go template... vs. in-image. Leaning orchestrator-side") — the pitfall is if plan-phase or implementation drift reverses that lean under Python-ecosystem gravitational pull (LangGraph examples/tutorials overwhelmingly show prompts authored in-Python, adjacent to the graph definition, because that's the idiomatic LangGraph pattern for single-language projects). If the verifier's prompt ends up hand-authored in Python instead of rendered orchestrator-side, TIDE now has two independent prompt-authoring surfaces (`internal/subagent/common/templates/*.tmpl` in Go, plus a Python-side prompt string/file) with no shared source of truth — exactly the "two copies of five prompts → two sources of truth" drift risk the polyglot doc's own Q2 flagged for the FUTURE authoring-migration rungs, arriving one milestone early via the verifier if this discipline isn't held now.

**Why it happens:** It's simply less friction, in the moment, to write the verifier's prompt as a Python f-string/template co-located with the LangGraph graph definition than to build (or reuse) a mechanism for the Go side to render a template and hand the resulting text across the envelope boundary. The milestone doc's "leaning orchestrator-side" is a stated intent, not yet a locked mechanism — nothing structurally prevents the opposite choice from being made under time pressure during implementation.

**How to avoid:**
1. Lock this as a Key Decision (not just a lean) as one of the first outputs of the roadmap/plan phase: the verifier's prompt is authored as a **sixth Go template** (`internal/subagent/common/templates/verifier_*.tmpl`), rendered orchestrator-side, and passed to the Python image via the envelope (`EnvelopeIn`'s prompt field or equivalent) — exactly like the CLI path's `PromptPath`/`.spec.prompt` mechanism, which is already language-neutral per the polyglot doc's contract table ("Executor prompt read... traversal-defended... No" (not CLI/Node-specific)).
2. This also sidesteps the polyglot doc's own stated risk for this exact mechanism ("avoids the polyglot doc's Q2 drift problem") — treat that framing as a design constraint to hold, not a nice-to-have.
3. If LangGraph idioms want prompt fragments closer to tool/graph definitions (e.g., a system message vs. a task message split), keep that split as PURELY a rendering/assembly detail on the Go side (the template can render distinct sections into distinct envelope fields) rather than letting Python author or modify prompt CONTENT.
4. Guard this with a lightweight review checklist item (not necessarily automatable pre-launch): any PR touching verifier prompt behavior should be reviewed for "did this add prompt text inside the Python image" as a smell.

**Warning signs:** A `grep` for prompt-shaped string literals (multi-line f-strings, `SystemMessage(...)` calls with hardcoded text) inside the Python image's source turns up verifier-specific instructions rather than pure structural/tool-wiring code.

**Phase to address:** Locked at the requirements/roadmap stage (this is a decision to make explicit in ROADMAP.md, not something to leave implicit going into planning) — and enforced during the image-build phase's code review. No automated CI gate is proposed here (a Python string-literal grep would be noisy); this is a design-discipline item, flagged explicitly so it isn't silently reversed.

---

### Pitfall 11: A BLOCKED verify must be a new halt class, never a reinterpretation of task/wave failure

**What goes wrong:** The wave-boundary failure contract is one of the most load-bearing invariants in this codebase (spec §"Failure handling at wave boundaries": failed task → siblings in the same wave continue, dependents in later waves never dispatch, non-dependents dispatch in strict profile; conservative profile halts project-wide via `ConditionFailureHalt`). If a level-verify or integration-check BLOCKED verdict is implemented by reusing the Task-failure code path (e.g., marking the checked level's phase `Failed` the way an execution failure does), it inherits semantics that don't fit: a verify BLOCKED isn't "this task's execution failed," it's "the artifact exists and the execution succeeded, but a post-hoc check says it doesn't meet the bar" — a fundamentally different claim that deserves its own halt class (`ConditionVerifyHalt`, already correctly identified in the milestone doc) rather than overloading `Failed`. Overloading it would also incorrectly trigger sibling-continues/dependents-never-dispatch wave semantics for what is actually a project-wide gate, not a per-task execution outcome — exactly inverted from what's needed (a BLOCKED verify at level-verify should probably halt broadly for human judgment, not merely let unrelated wave siblings continue while the checked level sits `Failed`).

**Why it happens:** Reusing an existing, well-tested code path (`Failed` phase + its existing wave-propagation logic) is the path of least implementation resistance — it "just works" in the sense that the reconciler already knows how to propagate a `Failed` phase. The cost is semantic: the milestone doc is explicit that this must NOT happen ("Wave-boundary failure semantics untouched: a BLOCKED verify is a new halt class, not a reinterpretation of task failure" — stated identically in both the milestone doc and the strategy note, i.e., already locked at the decision level), but a plan/task author under implementation pressure could still reach for the familiar `Failed`-phase machinery because it requires less new code.

**How to avoid:**
1. `ConditionVerifyHalt` is a project-level condition (mirroring `ConditionBillingHalt`/`ConditionFailureHalt`'s shape exactly, per Pitfall 8) — NOT a phase value on the checked level's CRD. The checked level's own `Status.Phase` should reflect what actually happened to it (its children Succeeded), while the PROJECT carries the halt.
2. Dispatch gates at all four execution sites (mirroring `checkFailureHalt`'s pattern) check `checkVerifyHalt` before allowing NEW dispatch — this parks the wave, it does not retroactively fail already-succeeded siblings.
3. Explicitly test that a BLOCKED level-verify does NOT flip the checked level's phase to `Failed`, does NOT cause wave-sibling task failures, and does NOT trigger the conservative-profile task-failure propagation path — a regression test asserting these ARE NOT touched is as important as testing the halt itself fires.
4. plan-check REJECT is architecturally different again (Pitfall 6) — it's a bounded RETRY loop with its own terminal halt, not an immediate project-wide freeze the way level-verify/integration-check BLOCKED is (per Pillar 3's explicit split: "plan-check REJECT → bounded re-plan loop... halt" vs "level-verify / integration-check BLOCKED → halt immediately"). Don't conflate these two BLOCKED-shaped-but-semantically-distinct paths into one code path either.

**Warning signs:** A BLOCKED level-verify causes the checked Phase/Plan's `Status.Phase` to read `Failed`; wave-sibling tasks in the same wave as the checked level stop dispatching when they have no dependency relationship to it.

**Phase to address:** Same phase as the halt condition's wiring (with Pitfalls 8 and 6) — this is a design constraint to enforce in code review and tests from the first line of reconciler code, not a later correction. Verification: the "these fields are NOT touched" regression tests above, run alongside the halt-fires-correctly tests.

---

## Technical Debt Patterns

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|-----------------|-----------------|
| Reusing `maxAttemptsPerTask` for the re-plan-reject counter | No new CRD field/reconciler code | Conflates task-execution retry budget with plan-re-authoring budget; a tuning change for one silently changes the other | Never — give the re-plan loop its own counter (Pitfall 6) |
| Storing verifier findings directly in CRD `.status` | Fast to read from the dashboard, no artifact-fetch code needed | Violates the size×locality rule; CRD grows unboundedly with finding count/verbosity; repeats a mistake this codebase has explicitly rejected multiple times | Never for the full findings; acceptable ONLY for a hard-capped top-N summary |
| Reusing `Failed` phase semantics for a BLOCKED verify | Reuses existing, tested wave-propagation code | Inverts wave-boundary failure semantics for what is actually a project-wide gate, not a task outcome (Pitfall 11) | Never |
| Authoring the verifier prompt as a Python string co-located with the LangGraph graph | Faster in the moment, idiomatic LangGraph style | Creates a second, drifting source of truth for prompt content (Pitfall 10) | Never past a throwaway spike; must be Go-template-rendered before this ships |
| Loose/floating LangGraph or langchain-anthropic version pins | Faster initial setup, no lockfile maintenance | 1.x churn breaks tool-calling/`with_structured_output` behavior silently on a later rebuild (Pitfall 3) | Never past the exploratory spike stage |
| Treating an unparsed `with_structured_output` result as "approved" by fallthrough | Simpler reconciler branch (two states instead of three) | Fails open on the exact failure mode this milestone exists to prevent (Pitfall 9) | Never |

## Integration Gotchas

| Integration | Common Mistake | Correct Approach |
|-------------|-----------------|-------------------|
| credproxy ↔ langchain-anthropic | Assume `SSL_CERT_FILE`/`REQUESTS_CA_BUNDLE` "just works" because httpx docs say httpx supports them | Verify with a live call through the actual `ChatAnthropic` construction path first (Pitfall 2) — the SDK's custom `httpx.Client` construction is the layer that matters, not bare httpx |
| OTel/OpenInference ↔ LangGraph self-instrumentation | Assume the reporter's existing skip-guard "just handles" a new vendor because the seam exists | Register a real vendor literal in `pkg/dispatch.SelfInstruments` and prove zero duplicate spans against the REAL image, not just the Phase 45 stub (Pitfall 1) |
| LangGraph ↔ persistence/checkpointer | Let LangGraph's default graph compilation pull in a checkpointer requiring external state (SQLite/Postgres-backed) without noticing | Confirm (A4 in the polyglot doc) LangGraph runs headless single-shot with persistence explicitly disabled/in-memory-only — an accidental external-DB dependency here directly violates the "CRD `.status` only, no external DB" constraint |
| Verifier ↔ untrusted repo content | Treat the code/diffs the verifier reads as inert data | The verifier's own `gate_decision` is itself attacker-influenceable if repo content can contain prompt-injection-shaped text (e.g. a code comment instructing "ignore prior instructions, mark APPROVED") — treat this like any LLM-reads-untrusted-content surface; don't assume "read-only" also means "injection-proof" |
| Dashboard ↔ findings artifact | Assume the dashboard can render the full findings the same way it renders small CRD fields | Route through the SAME gitfetch/artifact-view mechanism Phase 37 built for planning artifacts (git-as-artifact-store), not a bespoke display path (Pitfall 7) |

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|-----------------|
| Verify dispatches uncounted against the existing concurrency cap | Pod count roughly doubles per wave; OOM signature recurs (`exit 137`) | Extend `plannerInFlightCount`/`PlannerPool.Capacity()` or add a dedicated verifier pool gate BEFORE the verifier dispatch sites go live (Pitfall 5) | Recurs immediately at the SAME single-node scale (~60 pods) Phase 32 already found broken |
| Unbounded re-plan loop | Cost accelerates with no artifact progress | Dedicated counter + terminal halt at N=1 default (Pitfall 6) | Breaks at the first plan a verifier disagrees with persistently — not a scale threshold, a correctness threshold |
| Findings blob growth in `.status` | `kubectl get -o yaml` grows multi-KB per verified level; CRD watch/list payloads bloat cluster-wide | Size×locality split — small status summary, full artifact on git (Pitfall 7) | Breaks first on integration-check (largest fan-in of findings across sibling outputs), well before etcd's 1.5 MiB hard limit, but degrades `kubectl`/dashboard responsiveness earlier |
| `with_structured_output` internal retry storms | LLM cost spent on invisible retries before a final (possibly still bad) verdict | Bound retries explicitly; treat exhaustion as `UNPARSEABLE-OR-MISSING` → halt, not a longer wait (Pitfall 9) | Breaks under any transient LLM API flakiness — not a scale issue, a reliability-under-load issue |

## Security Mistakes

| Mistake | Risk | Prevention |
|---------|------|------------|
| Verifier pod carries git-write/push credentials "just in case" | A jailbroken or buggy agent loop commits/pushes despite prompt instructions not to | Never inject write credentials into the verifier's env at all — omission as enforcement (Pitfall 4) |
| Verifier's worktree mount is read-write | Same as above, plus incidental/accidental writes could still land on the tracked run branch | Mount the worktree `ReadOnly: true`; give scratch writes their own ephemeral `emptyDir` (Pitfall 4) |
| Verifier trusts repo content as purely inert evaluation target | Prompt injection via repo comments/strings can attempt to manipulate the `gate_decision` verdict | Treat verifier input the same as any LLM-reads-untrusted-content surface; consider structural signals (e.g., the mechanical Phase 34 git-verified completeness check) as a corroborating, non-LLM check alongside the semantic verdict, not a replacement for defensive prompting |
| Findings artifact on the git run branch includes secrets accidentally quoted from logs/diffs | A leaked secret propagates into a committed/pushed artifact location | Route findings through the SAME redact-before-persist discipline the trace pipeline already established ("redact-before-truncate at a single chokepoint... raw at the source" — Phase 44 pattern) rather than assuming verifier output is inherently safe to persist verbatim |

## UX Pitfalls

| Pitfall | User Impact | Better Approach |
|---------|-------------|-------------------|
| Verify halt message is generic ("verify blocked") with no findings pointer | Operator must hunt for why, defeating the purpose of an "actionable" gate | Include a direct pointer (artifact path/gitfetch reference) to the full findings in the halt condition's `Message`, mirroring how other halts state the exact recovery verb |
| Silent fail-open on unparsed verdict looks identical to a real APPROVED | Operator trusts a `Complete` stamp that was never actually checked — literally reproducing the 2026-07-03 incident this milestone exists to fix | Fail closed to `ConditionVerifyHalt` on any unparseable/missing verdict (Pitfall 9) — never let "verify tier enabled" and "verify tier actually ran successfully" look the same in the UI |
| Dashboard shows no distinction between a task-execution `Failed` and a verify-tier `BLOCKED` | Operator can't tell "code doesn't work" from "code works but doesn't meet the bar" — very different remediation paths | Surface `ConditionVerifyHalt` as its own visually distinct state, not folded into the existing Failed-level UI treatment (ties to Pitfall 11) |
| Re-plan loop churns silently in the background | Operator doesn't notice cost accumulating until the budget cap trips | Surface the re-plan attempt counter live (dashboard or log line) the moment attempt 1 of N starts, not only at exhaustion |

## "Looks Done But Isn't" Checklist

- [ ] **`SelfInstruments` returns `true` for the verifier's vendor:** Often left at the fail-closed `false` default inherited from the existing switch — verify with a live dispatch against a real OTLP collector showing exactly one span tree, not just that the code compiles.
- [ ] **Verifier dispatches counted against a concurrency gate:** Often assumed "covered" by the existing planner-pool cap without ever being queried by it — verify `plannerInFlightCount`'s label selector (or its verifier-specific equivalent) actually matches verifier Jobs, with a live wave-load test.
- [ ] **`ConditionVerifyHalt`'s resume path has a time-fence:** Often shipped with a naive "just clear the condition" resume verb that looks correct in the happy-path test — verify with the straggler-reconcile + stale-timestamp regression tests Phase 25 needed after the fact.
- [ ] **Findings artifact respects size×locality:** Often looks fine in a demo with 2-3 findings — verify against a fixture with a large, chatty coverage-not-conservatism-prompted finding set (dozens of findings with code excerpts), checked against a CRD size assertion.
- [ ] **Read-only enforcement is structural, not just prompted:** Often looks enforced because the happy-path prompt never tries to commit — verify with an adversarial fixture that DOES try, asserting failure at the credential/mount layer.
- [ ] **`with_structured_output` failure modes are handled, not just the happy path:** Often only tested against a clean APPROVED/BLOCKED JSON response — verify against empty, partial, and malformed fixture responses, asserting all three reach the halt path.

## Recovery Strategies

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|-----------------|
| Double span emission shipped silently | LOW | Flip `SelfInstruments` for the vendor, redeploy the manager/reporter image — no data migration; stale duplicated traces in Phoenix are cosmetic and can be filtered by time range post-fix |
| Verify dispatches OOM a cluster | MEDIUM | Same remediation shape as Phase 32's D3 fix — add the missing concurrency gate; no CRD schema change needed, a controller-only patch |
| `ConditionVerifyHalt` resume-ordering bug ships | MEDIUM | Same remediation shape as Phase 25's CR-02 fix — add the time-fence + resume-annotation stamping; requires a patch release, not a data migration, since conditions are derived/rederivable |
| Findings blob bloats CRD status | HIGH | Requires a schema migration (move the field to an artifact reference) — costlier than the others because existing in-flight projects would carry the old shape; strongly prefer getting this right in the FIRST phase rather than recovering later |
| Fail-open verdict handling ships | HIGH | Worse than the others — a "verified" `Complete` stamp built on a silent fail-open cannot be distinguished after the fact from a genuinely passed verify without re-running verification; treat this as a ship-blocker to catch pre-release, not a patchable-later gap |

## Pitfall-to-Phase Mapping

| Pitfall | Prevention Phase | Verification |
|---------|-------------------|----------------|
| 1. Double span emission | Trace-adapter/vendor-registration work, landed together with the verifier's first dispatch call site | Live dispatch against a test OTLP collector; exactly one span tree per verify stage, checked into CI |
| 2. Credproxy TLS / httpx trust | Image-build phase, as a gating spike | Live `ChatAnthropic` call through the credproxy sidecar's self-signed CA, pass/fail binary |
| 3. LangGraph/langchain-anthropic version churn | Image-build phase (lockfile + CI pin gate) | CI job rejecting unpinned/range dependency specifiers in the verifier image's lockfile |
| 4. Read-only enforcement | Image-contract/Job-spec phase (Pillar 2) | Adversarial fixture test: verifier attempts `git commit`/push, asserted to fail at mount/credential layer |
| 5. Cost × concurrency multiplication | Same phase as verifier dispatch call sites (not a follow-up) | Kind-cluster wave-load test with authoring + verify pods concurrent, pod count under sized cap |
| 6. Unbounded re-plan loop | plan-check stage's first implementation (Pillar 1) | envtest: N consecutive REJECTs → halt fires at N+1, not another re-dispatch |
| 7. `gate_decision` persistence size | Schema-lock phase, before reconciler halt logic is written | Size-bound unit test on CRD summary field; findings artifact round-trips via gitfetch |
| 8. VerifyHalt resume-ordering | Halt condition's introduction phase (not a hardening follow-up) | Straggler-reconcile + stale-timestamp regression tests, written before the happy-path test |
| 9. Fail-open structured-output failures | Reconciler-side `gate_decision` consumption phase | Fixture tests: empty/partial/malformed verdict responses all reach the halt path |
| 10. Prompt drift (Python port) | Locked at requirements/roadmap stage; enforced at image-build code review | Design-discipline item — no proposed CI gate; explicit ROADMAP.md decision record |
| 11. Wave-boundary semantics weakening | Halt condition's introduction phase (with 6 and 8) | Regression tests proving checked level's phase, wave siblings, and conservative-profile propagation are NOT touched by a VerifyHalt |

## Sources

- TIDE source (HIGH confidence, direct read): `pkg/dispatch/vendor_capabilities.go`, `internal/reporter/tracesynth.go`, `internal/controller/{failure_halt,billing_halt,budget_blocked,dispatch_helpers,reporter_jobspec,plan_controller}.go`, `internal/dispatch/podjob/jobspec.go`, `internal/credproxy/server.go`, `pkg/otelai/attrs.go`, `cmd/tide/resume.go`, `api/v1alpha3/project_types.go`
- TIDE planning artifacts (HIGH confidence — locked project decisions): `.planning/PROJECT.md` (Key Decisions table, CACHE-01 record), `.planning/milestones/vnext-specialist-verify-MILESTONE.md`, `.planning/notes/langgraph-successor-runtime-strategy.md`, `.planning/seeds/verify-level-subagent.md`, `.planning/milestones/v1.x-polyglot-subagent-MILESTONE.md`, `.planning/research/questions.md`
- [HTTPX Environment Variables docs](https://www.python-httpx.org/environment_variables/) — MEDIUM confidence, confirms `SSL_CERT_FILE`/`SSL_CERT_DIR` support gated on `trust_env` (default True at the bare-httpx-client level)
- [anthropics/anthropic-sdk-python issue #923](https://github.com/anthropics/anthropic-sdk-python/issues/923) — MEDIUM confidence, confirms the SDK's custom httpx transport has a documented history of not honoring some env-var-driven behavior
- [langchain-ai/langchain issue #35843](https://github.com/langchain-ai/langchain/issues/35843) — MEDIUM confidence, open/unresolved feature request confirming `ChatAnthropic` does not currently accept a custom `httpx.Client`/`httpx.AsyncClient`, with monkey-patching cited as today's only workaround
- [OpenInference Semantic Conventions](https://arize-ai.github.io/openinference/spec/semantic_conventions.html) — HIGH confidence (official spec), confirms `EVALUATOR` as a first-class OpenInference span kind distinct from `AGENT`/`LLM`
- Prior TIDE incidents cited by name per CLAUDE.md/PROJECT.md: Phase 25 resume-ordering bug (CR-02/WR-03), run-2b D3 concurrency OOM (Phase 32), the 2026-07-03 silent-`Complete` first external run, Phase 44 redact-before-truncate/D-O5 payload-boundary pattern, Phase 37 git-as-artifact-store decision

---
*Pitfalls research for: TIDE v1.0.9 "Slack Tide" — in-cluster verify tier + LangGraph beachhead*
*Researched: 2026-07-18*
