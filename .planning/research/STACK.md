# Stack Research

**Domain:** v1.0.10 "King Tide" — LangGraph authoring migration (WRITE-capable specialist image), multi-provider `init_chat_model` endgame, eval-gating machinery for rung promotion, dependency impact of the three dynamic-workflow patterns
**Researched:** 2026-07-21
**Confidence:** HIGH for every version pin (re-verified live against PyPI JSON on 2026-07-21, same day as research); MEDIUM for structured-output reliability on non-Anthropic providers (LangChain's own docs, not independently load-tested against this repo's schemas yet); MEDIUM for the eval-gating machinery shape (an architectural recommendation reusing proven pieces, not yet built); LOW/flagged explicitly for Gemini's CA-trust path (inferred from `google-genai`'s dependency graph, not build-verified)

This file covers ONLY the three new capabilities the milestone adds: (A) a WRITE-capable LangGraph **authoring** image, (B) multi-provider dispatch via `init_chat_model`, (C) eval-gating machinery for rung promotion, plus (D) the dependency impact of the three dynamic-workflow patterns. The read-only LangGraph **verifier** image (`cmd/tide-langgraph-verifier/`), its pins, its TLS/CA-trust proof, and its `create_agent(response_format=...)` pattern are shipped, validated v1.0.9 capability — reused and cited here, not re-researched. Re-verified live: `langgraph`, `langchain`, `langchain-anthropic` pins from the 2026-07-18 verifier research show **zero drift** three days later; `langchain-core` alone released a new minor (1.4.9 → 1.5.0) **hours** before this research ran — treated as a "do not adopt yet" flag below, not a bump.

## Answers to the Assigned Questions

### 1. WRITE-capable authoring image

**Base pins: reuse the verifier image's exact pin set, do not fork a second dependency tree.** `langgraph==1.2.9`, `langchain==1.3.14`, `langchain-anthropic==1.4.8`, `langchain-core==1.4.9`, `anthropic==0.117.0`, `pydantic==2.13.4`, `httpx==0.28.1` — all re-verified live 2026-07-21 as still the newest compatible set (see Version Compatibility below). Two images sharing one pin discipline means one `make verify-langgraph-pins`-style gate and one hash-lock recipe (`uv pip compile requirements.in --generate-hashes`) cover both, and a re-pin decision is made once, not twice. **Do not bump `langchain-core` to the fresh 1.5.0** (released 2026-07-21T03:37 UTC, hours old at research time) — it satisfies both `langgraph` (`<2,>=1.4.7`) and `langchain` (`<2.0.0,>=1.4.9`) constraints so it's *compatible*, but adopting an hours-old minor release into a second production image before it's even been re-verified in the first violates this repo's own "re-verify pins at every rung" discipline in the wrong direction (rushing in, not re-checking). Re-pin both images together at the next scheduled cadence.

**Structured child-CRD JSON emission — the actual reason this rung exists.** The CLI path's `readChildCRDs` (`subagent.go`) free-text-then-`sanitizeJSONStringControls` parse is the Phase-10 cascade class this migration is supposed to retire. Use `langchain.agents.create_agent(model, tools=[write_file, edit_file, read_file, bash, git_read], response_format=ChildCRDBatch)` — the same idiom the verifier already proved (`ProviderStrategy` auto-selected for Anthropic, provider enforces the schema server-side). One structural difference from the verifier's single `GateDecision`: an authoring role emits **zero-to-N** children per dispatch, so the schema is a batch, not a singleton:

```python
from typing import Literal
from pydantic import BaseModel

class ChildCRDSpec(BaseModel):
    kind: Literal["Milestone", "Phase", "Plan", "Task"]
    metadata: dict  # name, labels — mirrors api/v1alpha3 ObjectMeta subset
    spec: dict      # kind-specific; validated Go-side against the real CRD schema after write

class ChildCRDBatch(BaseModel):
    children: list[ChildCRDSpec]
```

Keep `spec` as a loosely-typed `dict` rather than a full per-Kind Pydantic mirror of every CRD's Go type — the Go side already re-validates via the real `runtime.RawExtension`/CEL path when the child CRD is applied (defense in depth exists downstream), and a fully-typed Pydantic mirror of four CRD kinds would be exactly the "two sources of truth" drift risk this repo's `RunEvidence`/`verdict.go` golden-fixture pattern was built to avoid for smaller, truly-shared structs. The image still writes the same `children/*.json` files at the same `SourcePath` convention `readChildCRDs` expects — the file-layout contract doesn't change, only how each file gets produced (schema-validated write, not sanitize-after-the-fact).

**The task executor is a different shape — it emits no child CRDs.** It's leaf-level: file edits + a commit. `response_format=` doesn't apply to its terminal output the way it does for planner roles; its "structured output" win is really about tool-call reliability (`write_file`/`edit_file` as `@tool` functions with typed args), not a terminal Pydantic schema. Migrate the executor last, as the ladder already specifies — this is also where `create_agent`'s native tool-calling loop has to prove it matches Claude Code's battle-tested file-edit loop, the hardest parity bet on the ladder.

**Write tools: hand-authored `@tool` functions, explicitly reject `deepagents` — more strongly than the verifier did.** `deepagents==0.6.12` (checked live) now hard-depends on `langchain-google-genai>=4.2.5` and `wcmatch>=10.1` even for its Anthropic-only use, and its own PyPI summary calls its file layer a "**mock file system**" — an in-memory abstraction that then needs syncing to the real worktree, friction for a component whose entire job is producing real, `git commit`-able changes. For the read-only verifier this was moot (no file writes at all); for a WRITE-capable image exposed to prompt-injected repo content, a bigger third-party tool surface is a bigger audit and injection-blast-radius liability, not a convenience. Stay with a handful of `@tool`-wrapped functions doing direct `pathlib`/`open()` I/O scoped under the worktree root — mirrors the CLI path's `--add-dir`-style scoping and keeps the surface auditable.

**Git write + commit protocol — no new library, replicate the Go identity contract exactly.** Shell out via `subprocess.run(["git", ...])`, same as the verifier's read-only git tools (`git show`/`diff`/`log`/`worktree add`), now extended to `git add` / `git commit` / `git push`. The commit identity contract already exists and is language-neutral: `internal/harness/commit.go` reads `pkggit.AgentIdentity()` (env vars `TIDE_AGENT_NAME`/`TIDE_AGENT_EMAIL`, default `TIDE Agent <tide-agent@tideproject.k8s>`) and shells `git -c user.name=... -c user.email=... commit`. The Python image reads the *same two env vars* and shells the *same* two `-c` flags — zero new plumbing, just don't drift from the string the Go side already produces. Push should go through the same `lastPushedSHA` force-with-lease fence Phase 34 built (`git merge-base --is-ancestor` boundary-push gate) rather than a bare `git push` — that fence is orchestrator-invoked today; confirm at plan time whether the Python image ever needs to push directly or whether push stays a controller-side operation post-dispatch (current CLI executor path: `cmd/claude-subagent/main.go` calls `harness.CommitWorktree`, i.e. commit happens in-pod, push happens at the level-boundary handler — replicate that division, don't invent a new one).

**Output-path validation (HARN-05 parity): stdlib only.** Re-implement the same post-run declared-output-paths filesystem scan the Go harness does (`harness.Validate`) — `pathlib.Path.stat().st_mtime` vs `StartedAt`, no library.

**Prompt templates: do not port.** Repeats the verifier's decision (its STACK.md's "What NOT to Use") — Go renders all six templates (five existing + this rung doesn't add a seventh; the authoring roles reuse the four planner templates + the task-executor template already compiled into `common/prompt_templates.go`), delivers via the envelope's `Prompt`/`PromptPath`. This resolves the polyglot doc's open Q2 by precedent: a Python template copy is the exact two-sources-of-truth risk both docs flag, and nothing about "write-capable" changes that calculus.

**Cost/pricing: resolve the polyglot doc's open Q1 — emit raw tokens, price in Go.** `ChatAnthropic`/`ChatOpenAI` responses expose `usage_metadata` (input/output/cache-read/cache-creation token counts) uniformly across providers via LangChain's standard content-block API. The image should populate `EnvelopeOut.Usage` with raw counts and leave `EstimatedCostCents` computation to the Go-side `pricing.go`, which already carries the exact-ID pricing table and the empirically-probed cache-write multiplier (v1.0.7 Phase 35). Duplicating a second pricing table in Python is a drift hazard with no offsetting benefit — there is exactly one place that should know model prices.

**Observability gap — carried-in debt, not a new decision, but genuinely live.** `pkg/dispatch/vendor_capabilities.go`'s `SelfInstruments("langgraph")` already returns `true`, telling the reporter to skip `events.jsonl` synthesis for any `langgraph`-vendor dispatch — but `openinference-instrumentation-langchain` is **not present** in either `cmd/tide-langgraph-verifier/requirements.in` or `.txt` today (confirmed by direct grep), and its own comment defers the work to "Phase 51 scope" — which shipped without adding it. This means **every live `langgraph`-vendor dispatch today produces zero trace spans**, silently, which is the exact failure mode the "fail toward visibility, never toward silence" doc comment says must not happen. This milestone's write-capable image inherits the same gap and should close it for both images: add `openinference-instrumentation-langchain==0.1.67` (pulls `openinference-instrumentation>=0.1.51`, `openinference-semantic-conventions>=0.1.17`, `opentelemetry-api`/`-instrumentation`/`-semantic-conventions` transitively) plus explicit `opentelemetry-sdk==1.44.0` and `opentelemetry-exporter-otlp-proto-grpc==1.44.0` (Go side uses `otlptracegrpc` per `go.mod`, v1.43.0 — match the gRPC exporter for endpoint/protocol symmetry, not the HTTP variant), then call `LangChainInstrumentor().instrument()` once at process start. No custom endpoint code needed: the OTel Python SDK reads `OTEL_EXPORTER_OTLP_ENDPOINT`/`OTEL_EXPORTER_OTLP_HEADERS` from the environment exactly like the Go side (chart already injects both), so this is additive dependencies + one init call, not new wiring.

### 2. Multi-provider via `init_chat_model`

**No new package for the dispatch mechanism itself** — `init_chat_model` lives in `langchain` (`from langchain.chat_models import init_chat_model`), already pinned at `1.3.14`. What's new is the **provider integration packages**, installed only for providers actually wired:

| Provider | Package | Version (live-verified 2026-07-21) | Transport | CA-trust mechanism |
|---|---|---|---|---|
| Anthropic (existing) | `langchain-anthropic` | 1.4.8 | `httpx` (via `anthropic` 0.117.0) | `SSL_CERT_FILE` — proven (Phase 48) |
| OpenAI | `langchain-openai` | 1.3.5 (requires `openai>=2.45.0,<3.0.0`) | `httpx` (via `openai` 2.46.0) | `SSL_CERT_FILE` — same mechanism, httpx-based, same proof applies |
| Google Gemini (Developer API) | `langchain-google-genai` | 4.2.7 | `httpx` **+ `google-auth[requests]` + `requests`** (transitive, per live PyPI metadata) | **Two paths, not one** — `SSL_CERT_FILE` covers the `httpx`-based calls, but `google-auth`'s token/auth flow pulls in `requests`, which does **not** honor `SSL_CERT_FILE` (confirmed in the verifier's own Phase-48 research: httpx-only, `requests` reads `REQUESTS_CA_BUNDLE` instead). **Flag, do not assume**: a Gemini rung needs a live build-time spike to confirm both `SSL_CERT_FILE` and `REQUESTS_CA_BUNDLE` (both pointed at the same credproxy CA) actually satisfy every code path `langchain-google-genai` exercises — this is new complexity the Anthropic/OpenAI pair doesn't have. Treat Gemini as the stretch provider, not the second one after Anthropic. |
| xAI (Grok) | `langchain-xai` | 1.2.2 | httpx-based (not independently verified here) | Not researched — out of scope unless a rung actually targets it |

**Structured-output reliability per provider** (per LangChain's own docs, Context7-verified 2026-07-21): OpenAI, Anthropic, and xAI have confirmed native provider-side structured output (`ProviderStrategy`, schema enforced by the provider — "the most reliable method when available"). Gemini appears in one pulled doc section listing native support but was omitted from a second, narrower section pulled the same session — **treat Gemini's native-structured-output claim as MEDIUM confidence, not confirmed**, and verify with a live `response_format=` call before trusting it for a Gemini authoring rung. Where a provider lacks native support, LangChain falls back to `ToolStrategy` (function-calling-based extraction) with `handle_errors=True` — an automatic in-loop retry on schema-validation failure, still materially better than the CLI's free-text parse, but not first-choice. **This has a direct rung-ordering implication**: per the evidence-gate ("a role migrates only when it matches or beats the CLI baseline"), any non-Anthropic provider rung should target a provider with confirmed `ProviderStrategy` support first (OpenAI) — a `ToolStrategy` fallback provider starts the quality bar lower than the airtight-JSON goal this whole migration exists to reach.

**`init_chat_model` kwarg passthrough is confirmed** (Phase 48 research, re-affirmed here): it forwards every kwarg it doesn't itself recognize straight into the resolved provider class's `__init__`. This means the currently-Anthropic-only levers (`effort`, `thinking`, `cache_control` breakpoints, `temperature`/`top_p`/`top_k` from the dead `Provider.Params` allowlist) do **not** automatically become cross-provider — they're accepted syntactically by `init_chat_model` but only meaningful to whichever provider class actually implements that constructor kwarg. **New requirement for the multi-provider rung**: the currently-Anthropic-flavored `paramsAllowList` (`subagent.go:68`) needs to become vendor-aware — reject `effort`/`thinking` for a non-Anthropic `Provider.Vendor` rather than silently passing an unrecognized kwarg (LangChain-side, an unrecognized kwarg to a provider `__init__` typically raises `TypeError` at construction, which is fail-closed and acceptable, but should be a deliberate, tested contract rather than an incidental one).

**Credproxy integration point — this is where "the credproxy pattern" actually needs to grow, and it's Go-side architecture, not a Python dependency.** `internal/credproxy/server.go` today has: a single `UpstreamBaseURL` per `Proxy` (hardcoded conceptually to `https://api.anthropic.com`), a hardcoded baseline `allowedRoutes` (`POST /v1/messages`, `POST /v1/messages/count_tokens`), and an `ExtraAllowedRoutes` per-Project extension point (Phase 04.1) that adds *routes* but not a *different upstream host*. Multi-provider dispatch needs either (a) a provider-keyed upstream table inside one `Proxy` (route by request path prefix → different upstream host + route allowlist), or (b) one credproxy sidecar instance per provider the Project actually uses. Additionally, `isCreditExhaustion`'s billing-halt classifier is a case-insensitive substring match on Anthropic's exact wording ("credit balance") — OpenAI and Gemini return differently-shaped billing errors (OpenAI: `insufficient_quota` error code; Gemini: `RESOURCE_EXHAUSTED` status) and need their own classifiers before `BillingHalt` semantics work cross-provider. Neither of these is a library choice; both are the concrete, load-bearing work items behind "extend the credproxy pattern" — flagging them here so they don't get missed as "just add a route."

**Version pins for the OpenAI rung specifically** (the ladder's stated endgame provider): `langchain-openai==1.3.5`, `openai==2.46.0` (pin both explicitly in `requirements.in`, hash-lock via the same `uv pip compile --generate-hashes` recipe, extend whatever the pin-verification make target becomes to cover this image too — do not let a second image's `requirements.in` silently drift out of the grep-gate's coverage).

### 3. Eval-gating machinery for rung promotion

**The actual gap: `internal/eval` today measures token counts and prompt-render fidelity, not authored-artifact quality.** Its current tests (`TestGoldenRender_*`, `TestByteRatchet_*`, `TestDAGAcyclicity_*`, `TestCostReplay_ParseStream`) prove the Go template renderer is stable and cheap — they say nothing about whether a LangGraph-authored `PLAN.md` or task diff is as good as a CLI-authored one. The milestone's rung-promotion criterion ("matches or beats the CLI baseline on quality at comparable cost") needs a genuinely new capability: **a comparative harness that dispatches the same input to both runtimes and scores the two outputs against each other.**

**Recommendation: extend `internal/eval` in Go, reusing already-pinned pieces — do not adopt a third-party evals framework.**

- **Dispatch symmetry is free**: both runtimes already implement `pkg/dispatch.Subagent` behind the identical `EnvelopeIn`/`EnvelopeOut` contract by construction (that's the whole point of Pillar 1's seam). A comparative harness is "run the same `EnvelopeIn` fixture through both `Subagent` implementations, keep both `EnvelopeOut`s" — no new abstraction, it's the interface the codebase already has.
- **The judge is the verifier, re-pointed, not a new schema.** The read-only LangGraph verifier's `GateDecision`/`Finding` structured-verdict pattern (`pkg/dispatch/verdict.go` ↔ `verifier/verdict.py`, already golden-fixture-matched Go↔Python) is a proven, schema-validated LLM-judge call. Reuse it for a **comparative** prompt ("given these two candidate artifacts for the same spec, which better satisfies it, and why") instead of inventing a second verdict shape — this keeps exactly one verdict schema in the whole system, consistent with the "one contract, parameterized" doctrine already established for `LoopPolicy`.
- **Aggregation is arithmetic, belongs in Go, and has a direct precedent.** Win-rate and cost-delta rollup across N fixture runs per role is exactly the shape of the existing byte-ratchet pattern (`internal/eval/render_test.go`'s `ratchetAssert`) — a checked-in baseline file, CI-gated so a later run can't silently regress below the last-proven quality bar for a promoted rung. Recommend a "quality ratchet" file per authoring role, co-committed with the promotion decision, exactly like a template trim co-commits its byte ratchet today.
- **Explicitly reject LangSmith's hosted `evaluate()`/experiment framework**, even though `langsmith==0.10.9` is already an unavoidable transitive dependency of `langchain-core` in both images. LangSmith's evaluation/experiment-tracking surface defaults to phoning results to `smith.langchain.com` — a hosted SaaS dependency this repo's "no hidden host dependencies" distribution constraint and self-hosted-only observability posture (Phoenix, not a vendor SaaS) argue directly against. If `LANGCHAIN_TRACING_V2`/`LANGSMITH_TRACING` env vars are ever left unset (the current posture, confirmed by their absence from both Dockerfiles), the SDK stays inert — keep it that way; do not opt in as a shortcut to "instant eval dashboards."
- **Do not reach for promptfoo, DeepEval, RAGAS, or OpenAI Evals.** None of them are CRD-native, git-artifact-native, or aware of TIDE's envelope contract — adopting one means building an adapter layer to feed it TIDE fixtures and translate its verdict shape back into a promotion decision, which is strictly more work than extending a harness that already runs against the real API (Phase 18's EVAL-05 proof) and already shares a schema with the judge this milestone needs.

**Net new dependency footprint for this piece: zero.** It's new Go code in `internal/eval` (comparative dispatch runner + ratchet files) plus reuse of `verdict.go`'s schema — no new pip package, no new Go module.

### 4. Dynamic workflow patterns — dependency impact (adversarial verification, generate-and-filter, tournament)

Per the locked invariant in `notes/sounding-dynamic-orchestration-design.md` ("No second graph engine... everything is still a dependency DAG of native K8s Jobs; the intra-node fan-and-reduce is a bounded sub-scheduler, not a graph engine"), **none of the three patterns require a new dependency at the orchestration layer.** Each decomposes into primitives this research already covers:

- **Adversarial verification** (N independent refuters at verify seams) = N parallel dispatches of the *existing, unchanged* read-only verifier image (zero new deps) + an aggregation/judge step reusing the same `GateDecision` schema (majority-vote or judge-of-judges, still schema-compatible, no new verdict shape).
- **Generate-and-filter** (N candidates at planner seams) = N parallel dispatches of the WRITE-capable authoring image from §1 + a judge call, same schema-reuse logic as above.
- **Tournament** (cost-gated bracket) = the same two primitives, sequenced across rounds by the orchestrator's existing wave-derivation (layered Kahn already handles staged fan-out/fan-in); "cost-gated" is a budget-check against the existing `ReservationStore`, not a new accounting mechanism.

**Explicit what-not-to-add for this section**: do not adopt `langgraph-supervisor` (checked live on PyPI, current release `0.0.31`) or LangGraph's own multi-agent `Send`-API/subgraphs-as-workers primitives as the CROSS-POD fan-out mechanism for any of the three patterns. Those operate *within one process's graph* — using them for cross-pod orchestration would require either a stateful long-running LangGraph service (the polyglot doc's rejected "LangGraph-as-a-service" alternative) or would stand up a second, competing scheduler next to the Go layered-Kahn orchestrator, directly against CLAUDE.md's "Don't replace layered Kahn... add a wave-internal sub-scheduler behind Kahn" anti-pattern. `langgraph-supervisor`-style patterns remain legitimate **inside a single image's own graph** (e.g., a verifier or authoring pod fanning out sub-checks before its own terminal verdict) — that's an in-pod implementation detail behind the envelope seam, not a cross-pod dependency decision.

## Recommended Stack

### Core Technologies (new/changed for this milestone)

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|-----------------|
| `langchain-openai` | 1.3.5 | OpenAI provider integration for `init_chat_model` | Ladder's stated endgame provider; native `ProviderStrategy` structured output confirmed; `httpx`-based, same `SSL_CERT_FILE` CA-trust as Anthropic |
| `openai` (raw SDK, transitive) | 2.46.0 | HTTP transport under `langchain-openai` | Pin explicitly — mirrors why `anthropic==0.117.0` is pinned explicitly today (the CA-trust behavior lives here) |
| `openinference-instrumentation-langchain` | 0.1.67 | Native OTel span emission for LangGraph/LangChain calls | Closes a live gap: `SelfInstruments("langgraph")` already returns `true` in shipped Go code with nothing behind it — zero spans today for any langgraph dispatch |
| `opentelemetry-sdk` | 1.44.0 | OTel Python SDK (span processor, exporter wiring) | Required by the instrumentor above; reads the same `OTEL_EXPORTER_OTLP_*` env vars the chart already injects |
| `opentelemetry-exporter-otlp-proto-grpc` | 1.44.0 | OTLP gRPC exporter | Matches the Go side's `otlptracegrpc` (go.mod, v1.43.0) — protocol symmetry against the same collector endpoint |

### Supporting Libraries

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `langgraph` | 1.2.9 | Agent orchestration / conditional loop runtime | Same pin as the verifier image — reuse, do not fork |
| `langchain` | 1.3.14 | `create_agent`, `response_format=`, `init_chat_model` | `init_chat_model` lives here; already the authoring-rung's structured-output entry point |
| `langchain-anthropic` | 1.4.8 | Anthropic model binding | Unchanged from verifier pin |
| `langchain-core` | 1.4.9 | Shared abstractions | **Hold at 1.4.9** — do not adopt the hours-old 1.5.0 release from this same research day; re-pin both images together at the next scheduled cadence |
| `anthropic` | 0.117.0 | Transitive, Anthropic transport | Unchanged from verifier pin |
| `pydantic` | 2.13.4 | `ChildCRDBatch`/schema definitions | Unchanged from verifier pin |
| `httpx` | 0.28.1 | Transport under `anthropic`/`openai` | Unchanged from verifier pin; `SSL_CERT_FILE` CA-trust proof (Phase 48) applies to both providers |
| `langchain-google-genai` | 4.2.7 | Gemini (Developer API) provider integration | **Stretch only** — pulls in `google-auth[requests]`/`requests`, a second CA-trust path (`REQUESTS_CA_BUNDLE`) not yet proven against the credproxy sidecar; spike before committing a rung to it |

### Development Tools

| Tool | Purpose | Notes |
|------|---------|-------|
| `pytest` | Unit tests for tool functions, schema validation, envelope plumbing | `9.1.1` is the version actually pinned in the shipped verifier's `requirements-dev.in` — reuse exactly, don't re-pin independently |
| `uv pip compile --generate-hashes` | Hash-lock recipe | Same recipe both images should use; extend the pin-verification grep gate to cover whichever new `requirements.in` this rung adds |

## Installation

```bash
# Core additions for the write-capable authoring image (append to the
# verifier's proven requirements.in — same patch-exact, no-range discipline):
langgraph==1.2.9
langchain==1.3.14
langchain-anthropic==1.4.8
langchain-core==1.4.9
anthropic==0.117.0
pydantic==2.13.4
httpx==0.28.1

# Multi-provider rung (OpenAI first, per the confirmed-ProviderStrategy ordering):
langchain-openai==1.3.5
openai==2.46.0

# Observability gap closure (both images should get this, not just the new one):
openinference-instrumentation-langchain==0.1.67
opentelemetry-sdk==1.44.0
opentelemetry-exporter-otlp-proto-grpc==1.44.0

# Then hash-lock:
uv pip compile requirements.in --generate-hashes --python-platform linux --python-version 3.13 --output-file requirements.txt
```

## Alternatives Considered

| Recommended | Alternative | When to Use Alternative |
|-------------|-------------|--------------------------|
| Hand-authored `@tool` write/edit/read functions | `deepagents==0.6.12` | Never for this milestone — "mock file system" abstraction fights the real-git-write requirement, and its hard deps (`langchain-google-genai`, `wcmatch`) widen the audit surface of a prompt-injection-exposed write path for no proven benefit over a handful of `@tool` functions |
| Go-side comparative harness reusing `verdict.go`'s schema | A third-party evals framework (promptfoo/DeepEval/RAGAS/OpenAI Evals) | Only if TIDE ever needs eval results consumed by an external, non-TIDE audience — none of these integrate with CRDs, git-artifact storage, or the envelope contract without an adapter layer that's more work than the harness extension itself |
| `openai`/`google-genai` reached only via `langchain-*` integration packages | `init_chat_model` with a bare provider string and no explicit package pin | Never — `init_chat_model` requires the corresponding integration package installed; pin it explicitly like every other direct dependency in this repo's discipline |
| OpenAI as the second provider rung | Gemini as the second provider rung | Only after a live spike confirms `google-genai`'s `requests`-based auth path trusts the credproxy CA the same way its `httpx`-based calls do — currently unverified |
| Cross-pod fan-out via existing K8s Jobs + layered Kahn | `langgraph-supervisor` / LangGraph `Send` API for cross-pod dynamic workflows | Only for *in-pod* sub-fan-out inside a single verifier/authoring image's own graph — never as the mechanism spanning multiple Job dispatches |

## What NOT to Use

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| `deepagents` | "Mock file system" abstraction, hard deps on `langchain-google-genai`+`wcmatch`, wrong shape for a real-git-write component exposed to injected content | Hand-authored `@tool` functions, same as the read-only verifier's already-proven pattern |
| `GitPython`/`pygit2` | Heavier than `subprocess` + the already-required `git` binary; diverges from both the Go side's and the verifier's shell-out pattern | `subprocess.run(["git", ...])`, identity via `TIDE_AGENT_NAME`/`TIDE_AGENT_EMAIL` matching `internal/harness/commit.go` exactly |
| LangSmith's hosted `evaluate()`/experiment tracking | Defaults to phoning results to `smith.langchain.com`; contradicts self-hosted-only observability posture (Phoenix) and the "no hidden host dependencies" distribution constraint | Extend `internal/eval` in Go, reuse `verdict.go`'s judge schema, ratchet-file the baseline |
| A third-party evals framework (promptfoo, DeepEval, RAGAS, OpenAI Evals) | Not CRD-native, not envelope-aware, not git-artifact-native — needs an adapter layer that costs more than extending the existing harness | `internal/eval` extension per §3 |
| `langgraph-supervisor` / LangGraph `Send` API as the cross-pod fan-out mechanism | Operates within one process's graph; using it cross-pod means either a stateful long-lived LangGraph service (rejected topology) or a second scheduler competing with layered Kahn (explicit CLAUDE.md anti-pattern) | K8s Jobs + the orchestrator's existing wave derivation; the pattern is legitimate only *inside* a single pod's own graph |
| Porting the five Go prompt templates to Python for the authoring image | Two sources of truth, exact drift risk both the verifier's STACK.md and the polyglot doc already flagged | Go renders, ships via envelope `Prompt`/`PromptPath` — same as the verifier |
| Duplicating a pricing table in the Python image | `pricing.go` is already the one place with exact-ID pricing + the empirically-probed cache multiplier; a second copy drifts | Emit raw `usage_metadata` token counts, price in Go |
| `langchain-core==1.5.0` (as of this research date) | Released hours before this research ran; not yet re-verified against either shipped image | Hold at `1.4.9`, the pin already proven in the shipped verifier image; re-pin both together at the next scheduled cadence |

## Version Compatibility

| Package A | Compatible With | Notes |
|-----------|------------------|-------|
| `langgraph==1.2.9` | `langchain-core>=1.4.7,<2` (have 1.4.9), `langgraph-checkpoint>=4.1.0,<5` (have 4.1.1), `langgraph-prebuilt>=1.1.0,<1.2.0` (have 1.1.0) | Unchanged from verifier's Phase-48 research; re-verified live 2026-07-21, zero drift |
| `langchain==1.3.14` | `langchain-core>=1.4.9,<2` (have 1.4.9 — the tightest constraint in the set), `langgraph>=1.2.5,<1.3.0` (have 1.2.9) | Re-verified live; this is the constraint that makes "hold `langchain-core` at 1.4.9" trivially safe — it's already the exact floor `langchain` demands |
| `langchain-openai==1.3.5` | `langchain-core>=1.4.9,<2` (have 1.4.9), `openai>=2.45.0,<3.0.0` (have 2.46.0) | Satisfied; live-verified 2026-07-21 |
| `langchain-google-genai==4.2.7` | `langchain-core>=1.4.7,<2` (have 1.4.9), `pydantic>=2.0.0,<3` (have 2.13.4) | Satisfied for the LangChain-side constraint; the CA-trust question (see §2 table) is a *runtime* concern this version table doesn't capture |
| `openinference-instrumentation-langchain==0.1.67` | `openinference-instrumentation>=0.1.51`, `openinference-semantic-conventions>=0.1.17`, `langchain-core>=0.3.9` (`instruments` extra — have 1.4.9, far above floor) | All transitive constraints satisfied; live-verified 2026-07-21 |
| `opentelemetry-sdk==1.44.0` / `opentelemetry-exporter-otlp-proto-grpc==1.44.0` | Same `1.44.0` release train | Pin both from the same train to avoid the OTel Python SDK's own well-known cross-minor API breakage between `-api`/`-sdk`/exporter packages |

## Sources

- PyPI JSON API (`https://pypi.org/pypi/<package>/json`), fetched live via `curl` on 2026-07-21 for: `langgraph`, `langchain`, `langchain-core`, `langchain-anthropic`, `langchain-openai`, `langchain-google-genai`, `anthropic`, `openai`, `pydantic`, `httpx`, `langgraph-checkpoint`, `langgraph-prebuilt`, `langsmith`, `langchain-community`, `deepagents`, `google-genai`, `openinference-instrumentation-langchain`, `opentelemetry-sdk`, `opentelemetry-exporter-otlp-proto-http/-grpc`, `openinference-instrumentation`, `openinference-semantic-conventions`, `langgraph-supervisor`, `langchain-google-vertexai`, `langchain-aws`, `langchain-groq`, `langchain-xai`, `langchain-ollama` — direct `requires_dist`/`upload_time_iso_8601` reads, not summarized changelog prose.
- Context7 `/websites/langchain_oss_python_langchain` — `init_chat_model` provider-string dispatch, `{provider}:{model}` format, kwarg passthrough, structured-output `ProviderStrategy`/`ToolStrategy` selection rules.
- In-repo, read directly: `pkg/dispatch/vendor_capabilities.go` (the live `SelfInstruments("langgraph")==true` gap), `internal/harness/commit.go` (agent-identity contract), `internal/credproxy/server.go` (`allowedRoutes`/`ExtraAllowedRoutes`/`UpstreamBaseURL`/`isCreditExhaustion`), `cmd/tide-langgraph-verifier/Dockerfile` + `requirements.in`/`.txt` (the proven pin-and-hash-lock recipe this milestone should replicate), `go.mod` (`otlptracegrpc` protocol choice), `charts/tide/templates/deployment.yaml` (`OTEL_EXPORTER_OTLP_ENDPOINT`/`_HEADERS` env-var wiring, confirms zero custom Python config needed).
- `.planning/research/STACK.md` (superseded by this file, but its httpx/`SSL_CERT_FILE` source-level proof for CA-trust — `httpx/_config.py`, `anthropic-sdk-python/_base_client.py` — carries forward unchanged for the OpenAI provider since `openai`'s Python SDK is also `httpx`-based).
- `.planning/milestones/v1.x-polyglot-subagent-MILESTONE.md` — parity inventory, contract-conformance table, provider-firewall-gap analysis, and open questions Q1/Q2/Q3/Q4/Q5 (Q1 and Q2 resolved by this research per precedent; Q3 resolved by the already-shipped `SelfInstruments` seam, now shown to need its Python-side dependency actually added; Q4 resolved against `deepagents` more strongly than the verifier's own decision).
- `.planning/notes/langgraph-successor-runtime-strategy.md` — the locked evidence-gated ladder this research's rung ordering follows.
- `.planning/notes/sounding-dynamic-orchestration-design.md` — the "no second graph engine" invariant §4's what-not-to-add section is grounded in.

---
*Stack research for: TIDE v1.0.10 "King Tide" — LangGraph authoring migration, multi-provider endgame, eval-gating machinery, dynamic-workflow dependency impact*
*Researched: 2026-07-21*
