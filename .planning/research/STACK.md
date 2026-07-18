# Stack Research

**Domain:** Read-only LangGraph verifier specialist image (Python) — TIDE v1.0.9 "Slack Tide"
**Researched:** 2026-07-18
**Confidence:** HIGH (PyPI JSON API pulled directly for every version/dependency claim; httpx CA-trust behavior confirmed by reading `httpx/_config.py` and `anthropic-sdk-python/_base_client.py` source; Anthropic `effort`/`output_config` confirmed against `platform.claude.com` docs; Context7 used for LangGraph/LangChain usage patterns)

This file covers ONLY the new Python/LangGraph verifier image (Pillar 2 of `vnext-specialist-verify-MILESTONE.md`). The existing Go operator, CLI subagent image, credproxy, and envelope contract are validated capabilities and out of scope here. This supersedes the prior STACK.md content (which covered the now-shipped v1.0.8 "Phoenix Rising" OTel/Phoenix work) for the current v1.0.9 research pass.

## Answers to the Assigned Questions (point-by-point)

### 1. Version pins (verified 2026-07-18 via PyPI JSON API — `curl https://pypi.org/pypi/<pkg>/json`)

| Package | Pin | Released | Verified via |
|---|---|---|---|
| `langgraph` | **1.2.9** | 2026-07-10 | pypi.org/pypi/langgraph/1.2.9/json |
| `langchain-core` | **1.4.9** | 2026-07-08 | pypi.org/pypi/langchain-core/1.4.9/json |
| `langchain-anthropic` | **1.4.8** | 2026-06-26 | pypi.org/pypi/langchain-anthropic/1.4.8/json |
| `langchain` | **1.3.14** | current | pypi.org/pypi/langchain/json |
| `anthropic` (raw SDK, transitive) | **0.117.0** | current | pypi.org/pypi/anthropic/json |
| `pydantic` | **2.13.4** | 2026-05-06 | pypi.org/pypi/pydantic/json |
| `httpx` (transitive) | **0.28.1** | current | pypi.org/pypi/httpx/json |
| `langgraph-checkpoint` (transitive) | **4.1.1** | current | pypi.org/pypi/langgraph-checkpoint/json |
| `langgraph-prebuilt` (transitive — do not install directly) | **1.1.0** | 2026-05-12 | pypi.org/pypi/langgraph-prebuilt/ |

All version constraints cross-checked from actual `requires_dist` metadata (not changelogs): `langgraph==1.2.9` requires `langchain-core>=1.4.7,<2`, `langgraph-checkpoint>=4.1.0,<5`, `langgraph-prebuilt>=1.1.0,<1.2.0`, `pydantic>=2.7.4`. `langchain-anthropic==1.4.8` requires `langchain-core>=1.4.7,<2`, `anthropic>=0.96.0,<1.0.0`, `pydantic>=2.7.4,<3`. `langchain==1.3.14` requires `langchain-core>=1.4.9,<2`, `langgraph>=1.2.5,<1.3.0`. Every pin above satisfies every constraint simultaneously — **no version conflicts in this set.** This resolves the milestone's open pinning question; re-verify at build time per the polyglot doc's discipline (1.x moves ~weekly — LangGraph shipped 6 patch releases in the 40 days before this research).

### 2. Structured output for `gate_decision`

**Use `langchain`'s `create_agent(..., response_format=GateDecision)`, not a bare `with_structured_output` bolted onto a hand-rolled loop.** This is the current (2026) idiomatic LangChain agent API — `langgraph.prebuilt.create_react_agent` still exists and works, but `langchain.agents.create_agent` is the documented front door and is what LangGraph's own docs now lead with. It takes the model + tools + a `response_format` schema and returns `result["structured_response"]` already validated — the tool-use loop (git-read, bash-gate-command) and the terminal verdict are one declarative call, not a manual two-node wire-up.

**Schema shape (mirrors GSD's verifier APPROVED/BLOCKED convention and the milestone's "findings carry severity + confidence tags" requirement):**

```python
from typing import Literal
from pydantic import BaseModel, Field

class Finding(BaseModel):
    description: str
    severity: Literal["blocker", "major", "minor"]
    confidence: Literal["high", "medium", "low"]
    location: str | None = Field(default=None, description="file:line or gate-command reference")

class GateDecision(BaseModel):
    verdict: Literal["APPROVED", "REJECTED", "BLOCKED"]
    summary: str
    findings: list[Finding] = Field(default_factory=list)
```

`verdict` is 3-valued, not boolean, to match the milestone's two distinct failure paths: `REJECTED` (plan-check → bounded re-plan loop) vs `BLOCKED` (level-verify/integration-check → immediate `ConditionVerifyHalt`) are different reconciler actions, so the schema must let the model distinguish them, not just emit a bool.

**`with_structured_output(GateDecision)` directly on `ChatAnthropic`** is the right call ONLY if the verifier needs zero tool use for a given stage (unlikely — level-verify must run the gate command, so it always needs at least a bash tool). Reserve it for a possible future no-tool stage; default to `create_agent(response_format=...)` for all three current stages.

**Typed graph state vs structured output — these are orthogonal, not competing.** LangGraph's `StateGraph`/`create_agent` internal state (messages, accumulated tool results) is how data flows *between* the agent's own loop steps (Pillar 4's "plan → act → self-check → retry"). `response_format`/`with_structured_output` governs only the *terminal* API response shape. Use both: typed state for the in-pod conditional loop, `response_format=GateDecision` for the one artifact that leaves the pod.

**Parse-failure/retry behavior (verified against `docs.langchain.com/oss/python/langchain/structured-output`):** `create_agent`'s default `response_format` strategy auto-selects `ProviderStrategy` for models with native structured-output support (Anthropic qualifies) — the provider itself enforces the schema, which is materially more reliable than the CLI image's free-text-then-`sanitizeJSONStringControls` parse path (the exact failure class Phase 10 fought). Where a `ToolStrategy` path is used instead (e.g. a future non-Anthropic model), `handle_errors` defaults to `True`: on a schema-validation failure or a double-output, the agent feeds the validation error back to the model and retries in-loop automatically — no image-level retry code needed for the common case. Recommend one *outer* bound: if `create_agent.invoke()` still raises after LangChain's internal retries, catch it, emit `sys.exit(1)` + `TerminationStub{Reason: "gate-decision-parse-failure"}` — this is a task-level failure, not a dispatch-level crash, exactly mirroring `readChildCRDs`' task-level-failure framing in `subagent.go`.

### 3. TLS/CA trust — polyglot doc assumption A1: **RESOLVED, CONFIRMED YES, with one correction**

Verified two ways, not one:

1. **httpx source** (`httpx/_config.py::create_ssl_context`, read directly from `github.com/encode/httpx`, master branch, 2026-07-18):
   ```python
   if verify is True:
       if trust_env and os.environ.get("SSL_CERT_FILE"):
           ctx = ssl.create_default_context(cafile=os.environ["SSL_CERT_FILE"])
       elif trust_env and os.environ.get("SSL_CERT_DIR"):
           ctx = ssl.create_default_context(capath=os.environ["SSL_CERT_DIR"])
       else:
           ctx = ssl.create_default_context(cafile=certifi.where())
   ```
   Only `SSL_CERT_FILE` and `SSL_CERT_DIR` are read — gated on `trust_env` (default `True`) and `verify is True` (also the default).

2. **Anthropic Python SDK** (`anthropic/_base_client.py::_DefaultHttpxClient`, which `langchain-anthropic`'s `ChatAnthropic` constructs under the hood): sets defaults for `timeout`/`limits`/`follow_redirects` only. It does **not** set `verify` or `trust_env` — both fall through to httpx's own defaults (`verify=True`, `trust_env=True`). So the credproxy CA cert becomes trusted with **zero code in the image** — set one env var, nothing else.

**Correction to the milestone doc's phrasing** — the doc lists `SSL_CERT_FILE`/`REQUESTS_CA_BUNDLE` as the two candidates. Only **`SSL_CERT_FILE`** (or `SSL_CERT_DIR` for a directory of certs) is real for this stack. `REQUESTS_CA_BUNDLE` is a `requests`-library convention; httpx does not read it (confirmed by the source excerpt above — no such key anywhere in `_config.py`). Since neither `anthropic`, `langchain-anthropic`, nor `langgraph` pulls in `requests`, `REQUESTS_CA_BUNDLE` is dead in this image and should be dropped from the contract-conformance table, not carried forward as an "either/or."

**Action for the image:** set `SSL_CERT_FILE=/etc/tide/proxy/ca.crt` (same mount path the CLI image already uses for `NODE_EXTRA_CA_CERTS`) — this is the direct Python analog, one env var, no `verify=` kwarg, no custom `ssl.SSLContext` needed.

**Build-time verification recipe** (run once when the image is built, per this repo's "Verify Before Claiming" discipline — don't just cite the source read):
```bash
# Inside the built verifier image, against the real credproxy sidecar:
docker run --rm -e SSL_CERT_FILE=/etc/tide/proxy/ca.crt -e ANTHROPIC_BASE_URL=https://127.0.0.1:8443 \
  -e ANTHROPIC_API_KEY=$SIGNED_TOKEN -v <ca-mount>:/etc/tide/proxy/ca.crt:ro tide-verifier:dev \
  python -c "from anthropic import Anthropic; Anthropic().messages.create(model='claude-haiku-4-6', max_tokens=8, messages=[{'role':'user','content':'hi'}])"
# PASS = no SSLCertVerificationError. This is the load-bearing runtime check;
# the source-read above explains WHY it should pass.
```

### 4. `questions.md` §3 passthrough surface — answered point-by-point

- **`cache_control` passthrough:** YES, confirmed, at content-block level, without dropping below the LangChain abstraction. `ChatAnthropic` forwards any key it doesn't explicitly model straight to `Anthropic.messages.create(...)` — `cache_control` on a content block round-trips untouched. There is also a purpose-built `AnthropicPromptCachingMiddleware` (in `langchain_anthropic.middleware.prompt_caching`) that auto-tags the last stable system-message block and the last tool definition with a cache breakpoint, so the shared prefix (system prompt + gate-command/tool schema) gets a `cache_control` marker without manual per-block bookkeeping. This is CACHE-F1's fix shape, now confirmed rather than assumed: an in-image SDK path can place breakpoints on the stable prefix with **no per-request random nonce** ahead of it — the exact defect that killed cross-pod caching on the CLI path (CACHE-01 finding) does not exist here.
- **Sampling/thinking params exposed:** `ChatAnthropic` exposes `temperature`, `top_p`, `top_k` as first-class constructor kwargs (confirmed against `reference.langchain.com/python/langchain-anthropic/chat_models/ChatAnthropic`) — these are exactly the four keys in the currently-dead `Provider.Params` allow-list (`subagent.go:68`), now reachable. Anthropic's own API enforces "set temperature OR top_p, not both."
- **Thinking budget:** `thinking={"type": "enabled", "budget_tokens": N}` (fixed budget) or `thinking={"type": "adaptive"}` (model picks its own budget; requires the newer effort-aware models). Both pass straight through to the Messages API's `thinking` field.
- **Opus-4.8 effort-equivalent: CONFIRMED, and it's better than the CLI's version.** `effort` is a genuine top-level `ChatAnthropic` constructor kwarg (`effort="low"|"medium"|"high"|"xhigh"|"max"`), confirmed against both `reference.langchain.com` and Anthropic's own `platform.claude.com/docs/en/build-with-claude/effort`. It maps to the raw Messages API's `output_config.effort` — the same parameter CLAUDE.md's subagent-tuning note identifies as the CLI's `--effort` flag, but reached here as a typed, documented SDK field rather than a shelled-out CLI arg. Supported on Claude Opus 4.8, Opus 4.7, Opus 4.6, Sonnet 5, Sonnet 4.6, Fable 5, Mythos 5 — i.e. every model tier this milestone would plausibly dispatch a verifier on. Default is `"high"`.
- **Does `init_chat_model` degrade any of these?** NO. `init_chat_model("anthropic:claude-...", effort="high", thinking={...}, temperature=..., top_k=...)` forwards every kwarg it doesn't recognize straight into the resolved provider class's `__init__` (confirmed against `docs.langchain.com/oss/python/langchain/models` and the `init_chat_model` reference — "additional model-specific keyword args are passed to the underlying chat model's `__init__` method"). There is no ChatAnthropic-only param that becomes unreachable through the single-string path. The one thing to get right: `anthropic_api_key`/`anthropic_api_url` (or `base_url`) must also be passed as kwargs to `init_chat_model` to route through the credproxy — same requirement whether constructed directly or via `init_chat_model`.

### 5. Base image + tooling

- **Base image:** `python:3.13-slim-bookworm`. `langgraph==1.2.9`'s PyPI classifiers list support for 3.10–3.13 only (no 3.14 classifier yet as of this pin) — pin to the highest version the pinned LangGraph release actually declares support for, not the newest Python available (3.14.6 is current upstream but untested against this dependency set per its own metadata). Re-evaluate the Python floor at each re-pin per the polyglot doc's discipline.
- **Git:** install the `git` package via `apt-get`, same as the CLI image. **Read-only means read-only git operations only** — `git show`, `git diff`, `git log`, `git worktree add` (to materialize the run branch read-only), never `git commit`/`git push`/`git config user.*`. Shell out via `subprocess.run(["git", ...], check=True, capture_output=True)` — do **not** add GitPython; it's a heavier dependency for operations `subprocess` + the `git` binary already handles, and it mirrors the existing Go pattern (`cmd/claude-subagent/main.go` also shells to the `git` binary directly rather than using `go-git` for the executor's worktree ops — the polyglot doc calls this "pure-shell-out; trivially portable").
- **Bash:** the gate-command execution tool is a thin `@tool`-wrapped `subprocess.run(gate_command, shell=True, cwd=worktree_root, timeout=...)`. No `deepagents`, no third-party bash-tool package — this is a few lines, and the milestone explicitly wants a small surface, not parity tooling.
- **Envelope in/out + TerminationStub:** plain `json.load`/`json.dump` against `$TIDE_ENVELOPE_PATH` (or the `/workspace/envelopes/$TIDE_TASK_UID/{in,out}.json` default) and `$TIDE_TERMINATION_MESSAGE_PATH`. Nothing library-specific — this is the language-neutral row of the polyglot doc's contract-conformance table, already verified language-neutral there; no new library decision needed. Validate `apiVersion`/`kind` the same way `ValidateAPIVersionKind` does in Go — a ~15-line function, not a dependency.
- **SIGTERM/wall-clock cap parity (HARN-02 equivalent):** `signal.signal(signal.SIGTERM, handler)` + reuse LangGraph's own `recursion_limit` config (default 25, raises `GraphRecursionError` — confirmed against `docs.langchain.com/oss/python/langgraph/errors/GRAPH_RECURSION_LIMIT`) as the iteration-cap parity mechanism for the in-pod conditional loop, on top of the K8s Job's `activeDeadlineSeconds` backstop that already exists regardless of language.
- **Persistence/checkpointer: NOT NEEDED.** This resolves polyglot-doc assumption A4. `create_agent`/`create_react_agent`'s `checkpointer` param defaults to `None` and is optional — a stateless single-shot `.invoke()` per Job needs no checkpointer, no Postgres, no Redis. This is a genuine simplification versus LangGraph's more commonly-documented multi-turn/HITL use cases.

## Recommended Stack

### Core Technologies

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|-----------------|
| Python | 3.13 (slim-bookworm) | Runtime | Highest version LangGraph 1.2.9's own classifiers declare support for; matches the "current stable, not bleeding-edge" pinning discipline already used for Go/controller-runtime |
| `langgraph` | 1.2.9 | Agent orchestration / conditional loop runtime | The locked successor-runtime bet (per `notes/langgraph-successor-runtime-strategy.md`); native `create_agent`/`create_react_agent` gives the plan→act→self-check→retry loop (Pillar 4) for free instead of hand-building an agent loop in Go |
| `langchain` | 1.3.14 | `create_agent` high-level API with `response_format=` | Current (2026) idiomatic entry point for a tool-using agent that ends in a structured verdict — one call replaces "ReAct loop, then bolt on `with_structured_output`" |
| `langchain-anthropic` | 1.4.8 | Anthropic model binding (`ChatAnthropic`) | Exposes `effort`, `thinking`, `temperature`/`top_p`/`top_k`, and raw `cache_control` passthrough — the exact levers the dead `Provider.Params` allow-list and CACHE-F1 have been waiting for |
| `langchain-core` | 1.4.9 | Shared abstractions (messages, tools, structured-output plumbing) | Transitive requirement of both `langgraph` and `langchain-anthropic`; pin explicitly so both resolve against the same copy |
| `anthropic` (raw SDK) | 0.117.0 | HTTP transport `langchain-anthropic` wraps | Pin explicitly — its `httpx`-based default client is where the A1 CA-trust behavior actually lives; pinning it makes that behavior reproducible, not incidental |
| `pydantic` | 2.13.4 | `GateDecision`/`Finding` schema definitions | v2 is what `langgraph`/`langchain-anthropic` both require; also the natural fit for `with_structured_output`/`response_format` schemas |
| `httpx` (transitive) | 0.28.1 | HTTP client under `anthropic` | Pin explicitly for the same reason as `anthropic` — its `SSL_CERT_FILE` handling is the entire A1 answer |

### Supporting Libraries

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `langgraph-checkpoint` | 4.1.1 (transitive) | Checkpointer backend interface | Not activated for v1.0.9 (no checkpointer passed → stateless single-shot); present only because `langgraph` depends on it |
| `langgraph-prebuilt` | 1.1.0 (transitive) | `create_react_agent` implementation | Do not `pip install` directly — PyPI's own page says "meant to be bundled with `langgraph`"; comes in automatically |
| stdlib `subprocess` | n/a | Shell out to `git` (read-only) and the declared gate command | Both the git-read tool and the bash-gate-command tool; no wrapper library needed |
| stdlib `json`, `signal`, `sys` | n/a | Envelope in/out, TerminationStub, SIGTERM handling, exit codes | The entire contract-conformance surface outside the LLM call itself |

### Development Tools

| Tool | Purpose | Notes |
|------|---------|-------|
| `pytest` | Unit tests for the graph nodes (tool functions, schema validation, envelope read/write) | Mirrors the Go side's Ginkgo discipline — test the tools and the envelope plumbing in isolation from a live model call |
| `ruff` | Lint + format | Single fast tool covering both roles; keeps parity with `golangci-lint` being the one Go-side gate |
| `mypy` (optional) | Static typing on the Pydantic schemas and tool signatures | Not load-bearing; nice-to-have given the schemas are the one place correctness really matters |

## Installation

```bash
# Core — pin every direct AND version-sensitive transitive dependency for
# reproducibility (mirrors the Go side's go.mod pinning discipline).
pip install \
  langgraph==1.2.9 \
  langchain==1.3.14 \
  langchain-anthropic==1.4.8 \
  langchain-core==1.4.9 \
  anthropic==0.117.0 \
  pydantic==2.13.4 \
  httpx==0.28.1

# Dev dependencies
pip install -D pytest ruff mypy
```

```dockerfile
# Minimal shape — see §5 above for rationale on each line.
FROM python:3.13-slim-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends git \
    && git config --system --add safe.directory '*' \
    && rm -rf /var/lib/apt/lists/*
USER 1000
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY verifier/ /app/verifier/
ENV SSL_CERT_FILE=/etc/tide/proxy/ca.crt
ENTRYPOINT ["python", "-m", "verifier"]
```

## Alternatives Considered

| Recommended | Alternative | When to Use Alternative |
|-------------|-------------|-------------------------|
| Plain `@tool`-wrapped `subprocess` for bash + git | `deepagents` (built-in file/bash/planning tools) | Only once an *authoring* role (planner/executor) migrates to this runtime — `deepagents` ships file-edit tools the read-only verifier is explicitly forbidden from having (Pillar 2: "no file-edit tools, no worktree-commit machinery") |
| `subprocess` + `git` binary | `GitPython` | If the image later needs structured git object inspection beyond `show`/`diff`/`log`/`worktree` text output — not needed for v1.0.9's read-only checks |
| `langchain.agents.create_agent(response_format=...)` | `langgraph.prebuilt.create_react_agent` + manual `.with_structured_output()` node | If a future stage needs custom graph topology `create_agent` can't express (e.g. parallel sub-verifications fanning into one judge) — drop to raw `StateGraph` then |
| No checkpointer (stateless single-shot) | `langgraph-checkpoint-postgres` / `-sqlite` | Only if a future stage needs multi-turn HITL within one dispatch (not this milestone; also CRD-`.status`-only persistence constraint argues against ever needing this) |
| Anthropic-only (`langchain-anthropic`) | `init_chat_model` + `langchain-openai` for multi-provider | Deferred to the ladder's "endgame decision" per `notes/langgraph-successor-runtime-strategy.md` — OpenAI arrives via `init_chat_model`'s provider-string dispatch when that rung is reached, not this one |

## What NOT to Use

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| `REQUESTS_CA_BUNDLE` / `CURL_CA_BUNDLE` env vars | httpx does not read either — confirmed by reading `httpx/_config.py`; carrying them forward as "maybe this one" wastes a build-time debugging cycle | `SSL_CERT_FILE` (or `SSL_CERT_DIR` for a cert directory) — the only two httpx actually honors |
| `deepagents` | Ships file-write/edit/planning tools; the verifier is contractually read-only (Pillar 2) | Hand-authored `@tool` functions scoped to exactly: run-gate-command, git-read |
| `GitPython` | Heavier dependency for what `subprocess` + the already-required `git` binary does in a few lines; also diverges from the existing Go-side "shell out to git" pattern the polyglot doc endorses | `subprocess.run(["git", ...])` |
| `langgraph-cli` / `langgraph-api` / a long-lived LangGraph server | This is the hosted "LangGraph Platform" topology — explicitly rejected in `v1.x-polyglot-subagent-MILESTONE.md`'s "Alternatives Considered" (breaks per-task Job dispatch, adds a stateful long-running component TIDE's re-derivable-state design argues against) | Per-task Job invoking the graph once via `.invoke()`, same as the CLI image's per-task process model |
| Any explicit `checkpointer=` (Postgres/SQLite/Redis) | No multi-turn/HITL need in this milestone; also collides with the repo-wide "CRD `.status` only, no external DB" constraint if it ever leaked into how state is tracked | Leave `checkpointer` unset (defaults to `None`) |
| Porting the five Go `text/template` prompt templates to Python | Milestone's open question #3 is leaning orchestrator-side already — a Python template copy is exactly the "two sources of truth" drift risk the polyglot doc's Q2 already flagged for a different (authoring) context; doubly avoid it here since this image authors nothing | Render the verifier prompt orchestrator-side (Go template, sixth compiled-in template) and deliver it via the envelope's `Prompt`/`PromptPath`, identical to how the CLI image already receives rendered prompts |
| `requests` library anywhere in the image | Not used by `anthropic`/`langchain-anthropic`/`langgraph` at all — adding it only reintroduces the `REQUESTS_CA_BUNDLE` red herring | Nothing — httpx (transitive via `anthropic`) is the only HTTP client this image needs |
| `langchain-mcp-adapters` | MCP is unused today even on the CLI path (`--bare` suppresses it) — no consumer for MCP tools exists in this milestone | Omit entirely; revisit only if a future stage has an actual MCP tool to attach |

## Read-Only Image Scope — What NOT to Add (explicit, per milestone Pillar 2)

- No file-edit tools (`write_file`, `edit_file`, `str_replace`, etc.) of any kind.
- No `git commit` / `git push` / `git config user.name|user.email` — read-only git operations only (`show`, `diff`, `log`, `worktree add` for materializing a read-only checkout).
- No child-CRD authoring (`with_structured_output`/`response_format` here targets `GateDecision`, never a `ChildCRDSpec`-shaped schema; the verifier never enters the planner's `children/*.json` handoff path at all).
- No port of the five Go prompt templates — verifier prompts are rendered orchestrator-side (open question #3 leans this way; nothing here should pre-empt that decision by building a Python template system).
- No `deepagents` and no MCP tooling — both are authoring-parity concerns for a later rung of the ladder, not this beachhead.
- No provider diversity (OpenAI, etc.) — Anthropic-only via `langchain-anthropic`; the credproxy route allowlist extension for OpenAI paths is explicitly a different milestone's problem.
- No long-lived server process, no checkpointer/persistence backend — per-task Job, single `.invoke()`, exit.

## Version Compatibility

| Package A | Compatible With | Notes |
|-----------|-----------------|-------|
| `langgraph==1.2.9` | `langchain-core>=1.4.7,<2` (have 1.4.9), `langgraph-checkpoint>=4.1.0,<5` (have 4.1.1), `langgraph-prebuilt>=1.1.0,<1.2.0` (have 1.1.0), `pydantic>=2.7.4` (have 2.13.4) | All satisfied simultaneously — verified via `requires_dist` in each package's PyPI JSON metadata, not changelog prose |
| `langchain==1.3.14` | `langchain-core>=1.4.9,<2` (have exactly 1.4.9), `langgraph>=1.2.5,<1.3.0` (have 1.2.9) | The tightest constraint in the set — `langchain-core` must be >= exactly what `langchain` demands; pinning `langchain-core` to 1.4.9 (its own latest) satisfies this with zero slack for a downgrade |
| `langchain-anthropic==1.4.8` | `langchain-core>=1.4.7,<2` (have 1.4.9), `anthropic>=0.96.0,<1.0.0` (have 0.117.0), `pydantic>=2.7.4,<3` (have 2.13.4) | Satisfied |
| `anthropic==0.117.0` | `httpx>=0.25.0,<1` (have 0.28.1), `pydantic>=1.9.0,<3` (have 2.13.4) | Satisfied; this is the pin where the A1 CA-trust behavior actually lives (see §3 above) |
| Python 3.13 | Required by all of the above (`>=3.10` floor across the set) | LangGraph 1.2.9's classifiers cap at 3.13 — do not jump to 3.14 until a LangGraph release explicitly classifies it |

## Sources

- PyPI JSON API (`https://pypi.org/pypi/<package>/json` and `/<package>/<version>/json`) — direct `requires_dist`/`requires_python`/`version` reads for `langgraph`, `langchain`, `langchain-anthropic`, `langchain-core`, `anthropic`, `pydantic`, `httpx`, `langgraph-checkpoint`, `langgraph-prebuilt`. Pulled 2026-07-18; this is ground truth, not a summarized page.
- `https://github.com/encode/httpx/blob/master/httpx/_config.py` — `create_ssl_context`, read directly, confirms `SSL_CERT_FILE`/`SSL_CERT_DIR` are the only env-based CA overrides httpx recognizes, gated on `trust_env`/`verify` defaults.
- `https://github.com/anthropics/anthropic-sdk-python/blob/main/src/anthropic/_base_client.py` — `_DefaultHttpxClient`, confirms `trust_env`/`verify` are left at httpx defaults, not overridden by the SDK.
- `https://www.python-httpx.org/environment_variables/` — official httpx docs, corroborates the source read.
- `https://platform.claude.com/docs/en/build-with-claude/effort` — Anthropic's own `effort`/`output_config` documentation (via WebSearch snippet + cross-reference).
- Context7 `/langchain-ai/langgraph` and `/websites/langchain_oss_python_langchain` — `create_react_agent`, `create_agent`, `with_structured_output`, `init_chat_model` usage patterns.
- `https://docs.langchain.com/oss/python/langchain/structured-output`, `/oss/python/integrations/chat/anthropic`, `/oss/python/langchain/agents` — `response_format`/`ToolStrategy`/`ProviderStrategy` retry semantics, `ChatAnthropic` param list, `effort`/`thinking` examples.
- `https://reference.langchain.com/python/langchain-anthropic/chat_models/ChatAnthropic` — full constructor parameter list, pass-through-to-`messages.create` behavior.
- `https://docs.langchain.com/oss/python/langgraph/errors/GRAPH_RECURSION_LIMIT` — `recursion_limit` default (25) and `GraphRecursionError` semantics.
- In-repo: `.planning/milestones/v1.x-polyglot-subagent-MILESTONE.md` (contract-conformance table, Assumptions Log A1/A4), `.planning/notes/langgraph-successor-runtime-strategy.md`, `.planning/research/questions.md` §3, `internal/subagent/anthropic/subagent.go` (the CLI-image contract this image must match).

---
*Stack research for: TIDE v1.0.9 "Slack Tide" — read-only LangGraph verifier specialist image*
*Researched: 2026-07-18*
