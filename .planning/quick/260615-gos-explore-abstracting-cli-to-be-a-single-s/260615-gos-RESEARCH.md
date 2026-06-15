# Quick Task 260615-gos: Abstract CLI into one strategy + add a Python/LangGraph SDK strategy — Research

**Researched:** 2026-06-15
**Domain:** TIDE subagent runtime contract; LangGraph (Python) agent runtime; polyglot container-image dispatch
**Confidence:** HIGH on the TIDE contract surface (read from source); MEDIUM-HIGH on LangGraph fit (Context7 + PyPI verified, headless-container specifics ASSUMED)

This is decision-grounding research for a **semi-scoped backlog milestone**, not a phase/task breakdown. It enumerates the parity surface, the contract any image must satisfy, the LangGraph mapping, the hard pitfalls, and where the milestone entry goes.

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
1. **Strategy seam = reuse `pkg/dispatch.Subagent` + the documented container-image contract.** Each strategy is a separate container image behind the same envelope-in/envelope-out contract. The "CLI" becomes one named strategy/image; the SDK runtime is a second image. **No new in-process LLM-call abstraction inside a shared Go agent loop** — the seam stays at the image boundary.
2. **Second strategy = a Python container image** implementing the documented envelope contract, free to use **LangGraph** natively for its agent loop. TIDE stays Go; the second strategy is polyglot but isolated behind the language-neutral image contract. **Per-task image (Job-per-task), NOT a long-lived LangGraph HTTP service.**
3. **Scope = full agent-loop parity** with the CLI image: tool use, file operations, MCP. Any subagent role must be runnable on either strategy.
4. **Commitment level = lock architecture, defer task breakdown.** The backlog artifact locks seam/topology/scope and frames problem + success criteria; it does NOT draft phases/tasks/requirements.

### Claude's Discretion
- Exact milestone numbering / backlog placement (after v1.0.2 "Ebb Tide" and the next "OpenAI backend + run #2" milestone).
- Success-criteria phrasing; which open questions to enumerate.
- Whether to include an "Alternatives considered" section (recommended — done below).

### Deferred Ideas (OUT OF SCOPE)
- Task/phase/requirement breakdown for the milestone (deferred to a future plan cycle).
- The OpenAI-backend + dogfood-run-#2 milestone's own scope (this milestone enables it but must not absorb it).
- v1.0.2 "Ebb Tide" cost-optimization scope (related; not absorbed).
</user_constraints>

---

## Summary

TIDE's subagent seam is already a clean, language-neutral image contract. The Go interface `pkg/dispatch.Subagent.Run(ctx, EnvelopeIn) (EnvelopeOut, error)` is only the *in-process reference* shape; the **real** cross-image contract is operational, not Go-typed: a container that (1) reads `in.json` from a per-task PVC path, (2) talks to the credproxy sidecar on `127.0.0.1:8443` using the injected `ANTHROPIC_API_KEY=<signed token>`, (3) writes `out.json` + a `<4 KB` `TerminationStub` to the termination-message path, and (4) honors the exit-code/Reason semantics and the wall-clock SIGTERM cap. The current Go `claude-subagent` binary (`cmd/claude-subagent/main.go`) is *itself* a thin shim around the harness that happens to be written in Go and shells out to the `claude` CLI — a Python image only has to reproduce the **shim's externally observable behavior**, not its Go internals.

The hard part of "full parity" is not the loop — LangGraph 1.x (`init_chat_model` + `create_react_agent` / the functional API) gives a mature, multi-provider tool-calling agent loop with first-class token-usage metadata and `with_structured_output` for the child-CRD JSON handoff. The hard part is the **CLI-bundled capabilities** (`--bare` hermeticity, the Write/Edit/Bash tools, MCP client, hooks, skills) the Python image must explicitly re-create or explicitly declare N/A, plus the **provider-firewall gap**: the firewall is a Go-import analyzer, so a Python image sidesteps it entirely and needs a *different* enforcement mechanism for the same invariant.

**Primary recommendation:** Frame the backlog milestone around three locked pillars — (a) **promote the operational image contract to a documented, language-neutral spec** (the real deliverable that makes a second image possible), (b) **build the Python/LangGraph image to that spec** using `langgraph` + `langchain` `init_chat_model` + `langchain-mcp-adapters` (and evaluate `deepagents` for built-in file/bash tools), and (c) **re-establish the provider-firewall invariant for the non-Go path** (image allowlist + credproxy route allowlist, since the import analyzer cannot reach Python). Defer the task breakdown.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Agent loop (LLM↔tool iteration) | Subagent image (in-pod) | — | Owned entirely by the strategy image; orchestrator never sees iterations except via `Usage.Iterations` |
| Credential injection | credproxy sidecar (in-pod) | Controller (mints token) | Real key never enters the subagent container; language-agnostic by construction |
| Cap enforcement (wall-clock/iter/token) | Subagent image + K8s Job | Controller (sets ctx deadline) | Wall-clock via SIGTERM/`activeDeadlineSeconds`; iter/token via the in-pod loop |
| Token/cost accounting | Subagent image | Controller (rolls up) | Image emits `Usage`; controller maps to `Project.Status.budget` |
| Child-CRD authoring | Subagent image (planner role) | Controller (materializes server-side) | Image writes JSON files; controller validates + creates CRs |
| Git worktree / commit | Subagent image (executor role) | tide-push Job | Executor commits worktree; push Job advances run branch |
| Provider-boundary enforcement | **build-time (Go) + deploy-time (image/route allowlist)** | — | **The Python path breaks the Go-only firewall — see Pitfall 3** |
| OpenInference span emission | events.jsonl artifact (in-pod) → consumer | `pkg/otelai` (Go helpers) | Spans are derived from the audit artifact; Python must emit an equivalent artifact |

---

## Parity Inventory — what the `claude` CLI bundles vs. LangGraph reproducibility

Each row cites the TIDE code that *consumes* the capability, so "parity" is measurable against real call sites rather than a feature wish-list.

| CLI-bundled capability | TIDE consumer (cite) | Reproducible in Python/LangGraph? | Notes |
|---|---|---|---|
| **Agent loop** (LLM↔tool iteration until done) | `anthropic/subagent.go` builds `claude -p … stream-json`; loop is *inside* the CLI | **YES — native.** `create_react_agent` (prebuilt) or the functional-API `@entrypoint` loop `[CITED: docs.langchain.com/oss/python/langgraph/workflows-agents]` | This is exactly what LangGraph exists to provide; lowest-risk parity item |
| **Tool use — file ops (Write/Edit/Read)** | `--permission-mode acceptEdits` + `--add-dir eventsDir` (`subagent.go` step 4b); planner writes `children/*.json`, executor edits the worktree | **YES, but must be authored.** LangGraph tools are user-defined functions. `deepagents` [ASSUMED] ships a built-in virtual/real filesystem + planning tool set that mirrors Claude Code's file tools | The CLI gives these *for free*; in Python they are explicit `@tool` functions or `deepagents` built-ins. The write-scoping (`--add-dir`) becomes a tool-level path guard |
| **Tool use — bash** | Implicit in CLI (executor runs commands); TIDE constrains via pod hermeticity + worktree | **YES, authored.** A `bash`/shell tool is a few lines; `deepagents` includes one [ASSUMED] | Sandboxing is the pod, same as today |
| **MCP client** | CLI auto-discovers `.mcp.json` — **suppressed by `--bare`** (`subagent.go` step 4b). TIDE today runs with **no MCP servers** | **YES.** `langchain-mcp-adapters` v0.3.0 [VERIFIED: PyPI] loads MCP tools into a LangGraph agent | Parity here means "can attach MCP tools," not "must" — TIDE's `--bare` path currently uses none. **Largely N/A for v1 parity** but the Python path can support it more directly than the CLI does |
| **Hooks** | **Not used** — `--bare` skips hook discovery (`subagent.go` package doc; Dockerfile note) | **N/A.** TIDE deliberately runs hermetic; no hooks fire today | Out of scope. Document as "CLI-specific, intentionally disabled, no parity required" |
| **Skills** | **Not used** — `--bare` skips skill discovery (same cites) | **N/A.** Same as hooks | Out of scope; LangGraph has no "skills" concept and TIDE doesn't use them |
| **`--bare` hermeticity** | Required flag (`subagent.go` step 4b; Dockerfile) — skips host config dir, `.mcp.json`, hooks, skills, CLAUDE-doc auto-memory | **N/A as a flag; YES as a property.** A Python image is hermetic *by construction* (it only loads what it imports). The Python equivalent of `--bare` is "don't read `~/.config`, don't auto-discover anything" — the default | The *intent* (hermetic per-pod runtime) is the contract; the *mechanism* is CLI-specific |
| **Streaming token/cost accounting** | `stream_parser.go` parses `stream-json` `result` event → `Usage{Input,Output,CacheRead,CacheCreation}`; `subagent.go` then computes `EstimatedCostCents` via `pricing.go` | **YES.** LangGraph `message.output.usage_metadata` exposes input/output/cache token counts `[CITED: docs.langchain.com/oss/python/langgraph/event-streaming]`. Anthropic + OpenAI both populate `usage_metadata` | **Cost computation stays orchestrator-side or in-image?** Today it's in-image (`anthropic/pricing.go`). The Python image must produce the same `Usage` shape; it may compute cents itself or emit raw tokens and let the controller price. **Open question — see below** |
| **Child-CRD JSON emission** | `readChildCRDs` (`subagent.go`) scans `children/*.json`, sanitizes control chars, rejects double-objects/traversal/bad-Kind, stamps `SourcePath` | **YES.** LangGraph `with_structured_output(PydanticModel)` `[CITED: docs.langchain.com/oss/python/langgraph/workflows-agents]` produces validated JSON directly — *stronger* than the CLI's free-text-Write-then-sanitize approach. The Python image writes the same `children/*.json` files | **Potential parity win:** structured output sidesteps the entire `sanitizeJSONStringControls` / double-object / trailing-prose failure class the CLI path fights (Phase 10 cascade). Must still write the same file layout + `SourcePath` convention the controller reads |
| **Prompt template render** | `common/prompt_templates.go` — Go `text/template`, `go:embed templates/*.tmpl`, keyed `<level>_<role>.tmpl` | **YES, re-authored.** Python image needs its own copy/port of the five templates (or they are factored into a language-neutral form) | **Parity risk: template drift.** Two copies of five prompts → two sources of truth. Milestone must decide: port templates to Python, or extract to a shared neutral asset both images embed. See Pitfall 4 |
| **Worktree setup/commit (executor)** | `cmd/claude-subagent/main.go` calls `harness.EnsureWorktree` / `CommitWorktree` (Go, shells to `git worktree add`) | **YES, re-authored.** Python image shells to the same `git` binary (already in the base image) | Pure-shell-out; trivially portable. Same `git config --system safe.directory '*'` requirement |

**Parity verdict:** Of the eight CLI-bundled capabilities, **three are intentionally N/A** (hooks, skills, `--bare`-as-flag — all consequences of running hermetic), **two are native LangGraph strengths** (agent loop, structured output), and **three require deliberate authoring** (file/bash tools, token-usage mapping, template port). MCP is supported-but-unused. No capability is a genuine blocker; the residual risk is *authoring discipline*, not missing primitives.

---

## Contract Conformance — what any image must satisfy to be a drop-in Subagent

The Go `Subagent` interface is the in-process shape; the **cross-image contract** is the union of these operational behaviors, all observed from the current Go shim (`cmd/claude-subagent/main.go`), harness (`internal/harness/`), and credproxy (`internal/credproxy/`).

| Contract element | Current (CLI/Go) mechanism | Python equivalent | CLI/Node-specific? |
|---|---|---|---|
| **Envelope-in read** | Reads `in.json` from `$TIDE_ENVELOPE_PATH` or `/workspace/envelopes/$TIDE_TASK_UID/in.json`; validates `apiVersion`/`kind` via `ValidateAPIVersionKind` | `json.load` + assert `apiVersion == "tideproject.k8s/v1alpha1"`, `kind == "TaskEnvelopeIn"` | No — path + JSON, language-neutral |
| **Executor prompt read** | `PromptPath` (workspace-relative) → read `.spec.prompt`, traversal-defended (`readPromptArtifact`) | Same read + same traversal guards (abs-path reject, `..` reject, must stay under workspace root) | No |
| **Envelope-out write** | `out.json` next to `in.json` (`writeEnvelopeOut`); full `EnvelopeOut` incl. `ChildCRDs`, verbose `Result` | Write identical JSON | No |
| **TerminationStub** | `<4 KB` JSON `{exitCode, reason, usage, headSHA, childCount}` to `$TIDE_TERMINATION_MESSAGE_PATH` (default `/dev/termination-log`) | Write identical small JSON; **must stay <4 KB** (`TestNewTerminationStub_StaysSmall` invariant) | No — but the size invariant must be re-tested in Python |
| **Exit code / Reason semantics** | Process exit 0 = success; non-zero = failure with structured `Reason` (`cap-hit`, `output-paths-violation`, `forced-failure`, …). Dispatch-level vs task-level distinction | Process exit code is the channel; Python sets `sys.exit(code)` + writes `Reason` | No — process exit is universal |
| **Credproxy + signed token** | `ANTHROPIC_BASE_URL = in.ProxyEndpoint`; `ANTHROPIC_API_KEY = in.SignedToken` (HMAC, **never raw key**). Proxy validates token, rewrites `Authorization`/`x-api-key` to real key, forwards to upstream | Python SDK reads the same env. For **Anthropic SDK**: set `base_url` + `api_key=signedToken`. For **OpenAI SDK**: `OPENAI_BASE_URL` + `OPENAI_API_KEY=signedToken` (proxy must learn the OpenAI header form — see below) | **Partially.** The env-var *names* are Anthropic-flavored. The proxy currently rewrites `Authorization: Bearer` + `x-api-key` (`credproxy/server.go`) and allowlists `/v1/messages*` — **OpenAI uses the same `Authorization: Bearer` form but different paths (`/v1/chat/completions`, `/v1/responses`)**, so the credproxy route allowlist needs extension for the OpenAI provider (overlaps the OpenAI-backend milestone — do not absorb) |
| **TLS trust of the proxy** | `NODE_EXTRA_CA_CERTS=/etc/tide/proxy/ca.crt` — Node-specific env making the `claude` CLI trust the sidecar's self-signed CA | **Python equivalent:** `SSL_CERT_FILE=/etc/tide/proxy/ca.crt` and/or `REQUESTS_CA_BUNDLE` (for `requests`) / httpx `verify=` arg. The Anthropic & OpenAI Python SDKs use `httpx`, which honors `SSL_CERT_FILE` | **YES — `NODE_EXTRA_CA_CERTS` is Node-only.** This is the single clearest CLI/Node-specific contract element. Python path uses `SSL_CERT_FILE`/`REQUESTS_CA_BUNDLE`. **Verify httpx honors `SSL_CERT_FILE` in the pinned version** [ASSUMED — confirm during build] |
| **Wall-clock cap / SIGTERM (HARN-02)** | Harness wraps `context.WithTimeout(WallClockSeconds)`; SIGTERM on overrun; `cmd/claude-subagent` installs `signal.NotifyContext(SIGTERM, SIGINT)` | Python: `signal.signal(SIGTERM, …)` + a wall-clock watchdog (e.g. `asyncio` timeout or `signal.alarm`). Must translate overrun → `Reason="wall-clock"`, exit non-zero | No — POSIX signals are universal; the K8s Job `activeDeadlineSeconds` is the backstop regardless of language |
| **Iteration / token caps** | `harness.CheckCaps` compares `Usage` against `Caps` after the run | Python re-implements the same comparison (or LangGraph `recursion_limit` for iterations) and emits the same `cap-hit` reasons | No |
| **Output-path validation (HARN-05)** | `harness.Validate` rejects writes outside `DeclaredOutputPaths` (post-run filesystem scan vs `StartedAt`) | Python re-implements the post-run scan, or constrains its file-tool to the declared set up front | No |
| **Provider sentinel fail-fast** | `anthropic/subagent.go` rejects envelope if `Provider.Vendor != "anthropic"` | Python image rejects if `Provider.Vendor` ∉ its compiled-in set (e.g. `{anthropic, openai}`) | No — but see Pitfall 3 re: enforcement |
| **Params allow-list** | `paramsAllowList` rejects unknown `Provider.Params` keys | Python re-implements the same allow-list per vendor | No |
| **Run as UID 1000, non-root** | Dockerfile `USER 1000` (D-G3) | Same | No |
| **`git` binary present + `safe.directory '*'`** | Base image installs git, sets system safe.directory | Same in the Python base image | No |

**Conformance summary:** Exactly **one** contract element is genuinely Node/CLI-specific — `NODE_EXTRA_CA_CERTS`. Its Python equivalent is `SSL_CERT_FILE` / `REQUESTS_CA_BUNDLE` (httpx-based SDKs). The credproxy env-var *names* (`ANTHROPIC_*`) are Anthropic-flavored but functionally generic; the proxy's route allowlist is the real Anthropic coupling and intersects the separate OpenAI-backend milestone. Everything else is filesystem-JSON + POSIX-signal + process-exit, all language-neutral.

---

## LangGraph Fit

**Version (verified):** `langgraph` 1.2.5 latest on PyPI [VERIFIED: PyPI 2026-06-15]; the 1.x line is GA (1.0.0 released, currently 1.2.x). `langchain-mcp-adapters` 0.3.0 [VERIFIED: PyPI]. `deepagents` 0.6.10 [VERIFIED: PyPI] — LangChain's harness with built-in filesystem/planning/sub-agent tooling, the closest off-the-shelf "Claude-Code-like" agent in Python.

| Contract need | LangGraph mapping | Source |
|---|---|---|
| Multi-provider (Anthropic + OpenAI) | `init_chat_model("claude-sonnet-4-6")` / `init_chat_model("gpt-5.4-mini")` — provider selected from a string; runtime provider switch shown | [CITED: docs.langchain.com/oss/python/langgraph/use-graph-api] |
| Agent loop | `create_react_agent` (prebuilt) or functional `@entrypoint` while-tool-calls loop | [CITED: docs.langchain.com/oss/python/langgraph/workflows-agents] |
| Tool calling | `@tool`-decorated functions + `model.bind_tools([...])` | [CITED: docs.langchain.com/oss/python/langgraph/quickstart] |
| Structured output (child CRDs) | `model.with_structured_output(PydanticModel)` → validated object, no free-text parsing | [CITED: docs.langchain.com/oss/python/langgraph/workflows-agents] |
| Streaming + token usage | `stream.messages` → `message.output.usage_metadata` (input/output/cache tokens) | [CITED: docs.langchain.com/oss/python/langgraph/event-streaming] |
| MCP tools | `langchain-mcp-adapters` loads MCP server tools into the agent | [VERIFIED: PyPI] |
| File/bash tools | author `@tool` fns, or adopt `deepagents` built-in filesystem/bash/planning tools | [ASSUMED] deepagents tool set; confirm against pinned version |

**Maturity / gotchas (MEDIUM confidence — flag for build-time verification):**
- LangGraph 1.x is GA but moves fast (40+ releases in the 1.x line already). **Pin to a patch (`langgraph==1.2.x`)** mirroring the existing Anthropic-SDK pinning discipline in `STACK.md`. [ASSUMED]
- `usage_metadata` cache-token sub-fields differ by provider; Anthropic surfaces `cache_read`/`cache_creation`, OpenAI's cached-token reporting differs. The Python image must map each provider's usage shape onto TIDE's `Usage{CacheReadTokens, CacheCreationTokens}` — the same mapping work `stream_parser.go` does for the CLI, per-provider. [ASSUMED]
- Headless/container use is well-trodden (LangGraph runs in Jobs/Lambdas widely) but **no checkpointer/persistence is needed** — TIDE is Job-per-task, single-shot. Disable LangGraph persistence/threads; the envelope is the only state. [ASSUMED]
- `init_chat_model` honors `ANTHROPIC_BASE_URL`/`OPENAI_BASE_URL` env via the underlying provider SDK, which is exactly the credproxy hook. Confirm the base-url env precedence for each provider package at build time. [ASSUMED]

---

## Pitfalls & Open Questions (the milestone author must capture)

### Pitfall 1 — Template drift between two images
Five prompts live as `go:embed` Go templates (`common/prompt_templates.go`). A second image needs the same prompts. Two copies = two sources of truth and silent quality divergence (directly adverse to v1.0.2 "Ebb Tide" which is *tuning these very templates*). **Milestone must pick:** (a) port templates to Python and accept dual maintenance, or (b) extract the five prompts to a language-neutral asset (e.g. a shared `prompts/` dir baked into both images) both runtimes render. Recommend (b) to avoid drift, but note it touches the Ebb-Tide template work — sequence after v1.0.2 lands.

### Pitfall 2 — Token-usage / cost-mapping per provider
The CLI path maps Anthropic `stream-json` usage → `Usage` and computes cents in-image (`pricing.go`). The Python image faces the same mapping for *each* provider's `usage_metadata` shape, and must decide whether to compute cents in-image (duplicating the pricing table) or emit raw tokens for the controller to price. **Open question:** where does pricing live for the SDK path? Emitting raw tokens + letting the controller price is cleaner (single pricing source) but changes the current in-image contract. Cross-references v1.0.2 Phase 21 (cost observability) — coordinate, don't absorb.

### Pitfall 3 — The provider firewall does not reach Python (the load-bearing gap)
`tools/analyzers/providerfirewall` is a **Go import analyzer** — it asserts no `github.com/anthropics/*` / `github.com/openai/*` import leaks into `pkg/controller`, `pkg/dispatch`, etc. A Python image **has no Go imports to analyze**, so it sidesteps the firewall entirely. The *invariant* the firewall protects (no provider lock-in in the orchestrator core; the real key never leaves the credproxy) is still enforced for the Python path by **other existing mechanisms**: (1) the orchestrator still never imports a provider SDK — the Python image is a separate process behind the image contract, so the Go firewall *stays green by construction*; (2) the credproxy route allowlist + signed-token flow still gates what the Python image can reach. **The milestone must state explicitly:** the firewall's scope is unchanged and still valid (it guards the *Go orchestrator*, which doesn't change); the Python image's provider boundary is enforced at **deploy time** (which image is allowed) and **runtime** (credproxy allowlist), not at Go-build time. No new Go analyzer can cover Python — don't try; document the boundary shift.

### Pitfall 4 — Polyglot build/release overhead
A second image means: a new `images/langgraph-subagent/Dockerfile` (Python base), a new `cmd/`-equivalent entrypoint (Python), a new chart `values.yaml` image entry (alongside `stubSubagent`/`claudeSubagent`), inclusion in `make docker-buildx-snapshot` (currently a "7-image" snapshot → 8), kind-prep load, and the release pipeline (memory: **bump chart appVersion as release STEP ONE**; the polyglot image must join the multi-arch buildx + SHA-pin discipline). Python multi-arch + dependency pinning (`requirements.txt`/`uv.lock` pinned, no floating `pip install`) is the analog of the Dockerfile's "never `npm install` without `@version`" rule. This is real, bounded overhead — call it out so the task breakdown sizes it.

### Pitfall 5 — Structured-output is a parity *upgrade*, not just parity
The CLI child-CRD path fights a documented failure class (Phase 10: unescaped control chars, double-objects, trailing prose → `sanitizeJSONStringControls`, `json.Decoder`, double-object detection). LangGraph `with_structured_output` validates against a Pydantic schema and largely eliminates that class. The milestone should frame this as a *benefit* (more robust child emission) but also note the Python image must still write the same `children/*.json` file layout and `SourcePath` convention the controller reads — the robustness is internal; the on-PVC contract is unchanged.

### Pitfall 6 — OpenInference observability from Python
Today spans are derived from the `events.jsonl` audit artifact via Go helpers (`pkg/otelai`), and **no Go OpenInference SDK exists** (the team hand-rolled 5 attribute helpers). For Python there *is* an OpenInference instrumentation ecosystem (Arize's `openinference-instrumentation-langchain`) — potentially *easier* than the Go path. **Open question:** does the Python image emit an equivalent `events.jsonl` artifact for the existing consumer, or emit OTel spans directly via OpenInference auto-instrumentation? Either is viable; the milestone should pick the artifact-parity route (write `events.jsonl` in the same shape) to keep one downstream consumer, OR adopt direct OpenInference spans and document the divergence. Cross-references the telemetry foundation (v1.0.1 Phase 16) — coordinate.

### Pitfall 7 — Relationship to adjacent milestones (do not absorb scope)
- **v1.0.2 "Ebb Tide"** (Phases 18–21, in progress) tunes the five templates and adds cache/cost observability. This milestone's template-portability decision (Pitfall 1) and cost-mapping (Pitfall 2) *depend on Ebb Tide's outcomes* — sequence **after** v1.0.2.
- **"OpenAI backend + dogfood run #2"** (next milestone per memory) needs the credproxy to learn OpenAI's routes/headers and a provider-agnostic dispatch path. A LangGraph image with `init_chat_model` is a *natural vehicle* for OpenAI — but the OpenAI-backend milestone may land OpenAI on the **CLI/Go path** first. The milestone must note the synergy (the LangGraph image is the cleanest multi-provider host) **without** claiming to deliver OpenAI support itself. State the dependency direction explicitly: this milestone delivers the *second runtime*; the OpenAI-backend milestone delivers *a provider* — they compose but are independently shippable.

### Open questions to enumerate in the milestone
1. Pricing/cost: in-image (duplicate table) vs raw-tokens-to-controller? (Pitfall 2)
2. Templates: dual-maintained Python copies vs extracted shared neutral asset? (Pitfall 1)
3. Observability: write `events.jsonl` for parity vs direct OpenInference spans? (Pitfall 6)
4. Off-the-shelf harness: build the loop from `create_react_agent` + custom tools, or adopt `deepagents` for built-in file/bash/planning tools? (tradeoff: control vs. surface area)
5. Does the credproxy need any change for the Python *Anthropic* path, or only for the OpenAI path (deferred to the OpenAI milestone)? (likely only env-var names + `SSL_CERT_FILE`; routes unchanged for Anthropic)

---

## Alternatives Considered (and why rejected)

> Included per the spec's argumentative style (CONTEXT.md discretion). These three forks were rejected in CONTEXT.md; captured here so the milestone can reuse the reasoning.

- **In-process Go LLM seam (shared Go agent loop, providers as Go strategies behind one in-process interface).** Rejected: would re-implement the agent loop, tool dispatch, MCP, and file tools in Go — exactly the HARN-06 work the team *deliberately avoided* by shelling out to the CLI. It also drags every provider SDK into the Go module (the firewall's whole reason to exist) and raises blast radius from "a new image" to "a new core abstraction touched by the controller." Highest-risk, contradicts the existing constraint.
- **Go-native SDK strategy (embed the Anthropic/OpenAI Go SDK in a Go subagent image, no CLI, no Python).** Rejected: gains language uniformity but loses the off-the-shelf agent loop — the team would hand-build tool-calling, file ops, and structured retries in Go (the HARN-06 rationale again). LangGraph in Python is *more mature* at exactly this than any Go agent framework in 2026. Provider firewall would have to *relax* to admit the SDK into a Go binary that's still in-module.
- **LangGraph-as-a-service (long-lived LangGraph HTTP server the controller calls per task).** Rejected: breaks the Job-per-task dispatch model (CONTEXT.md), introduces a stateful long-running component into a system whose entire design is "every level boundary is a saved artifact, re-derive from completed-task set" (CLAUDE.md), adds a network hop + auth surface + scaling/HA concern, and makes the credproxy-per-pod sidecar model awkward. The per-task image keeps the contract identical to today (one Job, one envelope-in, one envelope-out).

---

## Backlog Placement

**The repo has no `.planning/backlog/` directory and no `BACKLOG.md`.** The actual convention (verified) is:

- **`.planning/ROADMAP.md`** — the live milestone roadmap. Top has a `## Milestones` bullet list (`✅`/`🚧` status), then a `## Phases` section with collapsible `<details>` per shipped milestone and an expanded section for the in-progress one, then `## Phase Details` and a `## Progress` table.
- **`.planning/MILESTONES.md`** — the narrative milestone log (one-liner + stats + key accomplishments per shipped milestone, newest-relevant first).
- **`.planning/milestones/`** — per-milestone archived artifacts: `v1.0.x-ROADMAP.md`, `v1.0.x-REQUIREMENTS.md`, `v1.0.x-MILESTONE-AUDIT.md`.

**Recommended placement for this future milestone:**
- Add a roadmap entry as a **future/backlog milestone** in `ROADMAP.md`'s `## Milestones` list, marked clearly as not-yet-started (e.g. a `📋 backlog` or `⏳ planned` marker distinct from `🚧`), sequenced **after** v1.0.2 "Ebb Tide" and **after** the "OpenAI backend + dogfood run #2" milestone (per CONTEXT.md discretion + memory). Suggested slot/label: a post-v1.1 milestone such as **"v1.x — Polyglot Subagent Runtimes (LangGraph strategy)"** — exact number is the milestone author's call.
- Because the deliverable is *semi-scoped* (architecture locked, tasks deferred), the natural home is a **single milestone entry + a short framing doc**, NOT a full `milestones/<ver>-REQUIREMENTS.md` (that's authored later when the milestone is picked up). The framing doc can live at `.planning/milestones/<ver>-MILESTONE.md` or as an expanded roadmap entry — match whatever the milestone author's `/gsd:new-milestone` flow produces. Do **not** invent a `backlog/` dir.

---

## Sources

### Primary (HIGH confidence)
- TIDE source (read directly): `pkg/dispatch/subagent.go`, `pkg/dispatch/envelope.go`, `internal/subagent/anthropic/subagent.go`, `internal/subagent/anthropic/stream_parser.go`, `internal/subagent/common/prompt_templates.go`, `internal/harness/{harness,caps}.go`, `cmd/claude-subagent/main.go`, `cmd/credproxy/main.go`, `internal/credproxy/server.go`, `tools/analyzers/providerfirewall/analyzer.go`, `images/claude-subagent/Dockerfile`, `Makefile` (firewall + image targets), `.planning/{ROADMAP,MILESTONES}.md`, `charts/tide/values.yaml`.
- LangGraph docs via Context7 `/websites/langchain_oss_python_langgraph` — agent loop, tool calling, structured output, runtime provider switch, streaming usage_metadata.
- PyPI version checks: `langgraph` 1.2.5, `langchain-mcp-adapters` 0.3.0, `deepagents` 0.6.10 (2026-06-15).

### Secondary / ASSUMED (verify at build time)
- httpx honoring `SSL_CERT_FILE`/`REQUESTS_CA_BUNDLE` for Anthropic/OpenAI Python SDKs (the `NODE_EXTRA_CA_CERTS` analog).
- `deepagents` built-in file/bash/planning tool surface as a Claude-Code parity shortcut.
- Per-provider `usage_metadata` cache-token field shapes.
- OpenInference Python LangChain instrumentation as the observability route.

## Metadata
**Confidence breakdown:**
- TIDE contract surface: HIGH — read from source, every claim cites a call site.
- LangGraph capability fit: MEDIUM-HIGH — Context7 + PyPI verified; headless/container and cache-token specifics ASSUMED.
- Provider-firewall gap analysis: HIGH — analyzer source read directly.
- Backlog placement: HIGH — convention verified (no backlog dir; ROADMAP/MILESTONES + milestones/ archive).

**Research date:** 2026-06-15
**Valid until:** ~2026-07-15 (LangGraph 1.x moves fast; re-pin versions when the milestone is picked up).

## Assumptions Log
| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | httpx-based Python SDKs honor `SSL_CERT_FILE`/`REQUESTS_CA_BUNDLE` as the `NODE_EXTRA_CA_CERTS` analog | Contract Conformance | Proxy TLS trust fails → image can't reach credproxy; needs a different CA-trust mechanism |
| A2 | `deepagents` ships built-in file/bash tools usable as Claude-Code parity shortcut | Parity Inventory, Q4 | If not, file/bash tools must be hand-authored (more tasks, still tractable) |
| A3 | Per-provider `usage_metadata` exposes cache-token sub-fields mappable to TIDE `Usage` | Pitfall 2 | Cache accounting incomplete for SDK path; cost rollup understated |
| A4 | LangGraph 1.x runs cleanly headless single-shot with persistence disabled | LangGraph Fit | Unexpected checkpointer/thread requirement adds complexity |
| A5 | OpenInference Python LangChain instrumentation exists and is current | Pitfall 6 | Observability parity needs the events.jsonl-artifact route instead |
| A6 | For the Anthropic path, credproxy routes need no change (only env-var names + CA) | Open Q5 | If OpenAI-style routing leaks in, overlaps the OpenAI milestone sooner |
