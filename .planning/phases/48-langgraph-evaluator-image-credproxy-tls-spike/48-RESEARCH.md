# Phase 48: LangGraph Evaluator Image + Credproxy-TLS Spike - Research

**Researched:** 2026-07-18
**Domain:** Python/LangGraph read-only specialist image + live TLS trust-chain spike, behind TIDE's existing Go `pkg/dispatch.Subagent` + file-envelope seam
**Confidence:** HIGH — every package-API claim in this document was verified by downloading the EXACT pinned wheel from PyPI and reading its source directly (not training data, not changelog prose), cross-checked against Context7 official docs, and cross-checked against direct reads of the relevant TIDE Go source. Two of CONTEXT.md's/STACK.md's inherited assumptions are corrected below with primary-source evidence — flagged prominently, not buried.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**Image scope & shape**
- **D-01 (Scope depth):** Bare seam-conformance shell. Happy path = decode `in.json` → git-**read** the worktree + run **one** read-only bash gate command → make **one** `ChatAnthropic` call (the TLS proof) → emit a trivial `EnvelopeOut` (exitCode 0, one-line `result`, no findings, no `Git`/`ChildCRDs`). No `gate_decision` schema — Phase 49 formalizes the output.
- **D-02 (LangGraph proof):** The image must genuinely exercise the LangGraph runtime, leaning `create_react_agent` (langgraph-prebuilt) with two hand-authored `@tool` functions: git-read and a `subprocess` bash gate-command tool. No `deepagents`/file-edit tooling; no checkpointer. Exact graph internals = researcher/planner discretion. **[See Pitfall C below — this research recommends `langchain.agents.create_agent` instead; `create_react_agent` is formally deprecated in the pinned `langgraph==1.2.9`. This stays within "exact graph internals = discretion."]**
- **D-03 (Envelope transport):** Files on the per-Project PVC (`/workspace/envelopes/<task-uid>/in.json` → `out.json` + ≤4KB TerminationStub to `/dev/termination-log`), NOT stdin/stdout. The Python image re-implements the JSON envelope shape independently and MUST validate `apiVersion == "dispatch.tideproject.k8s/v1alpha1"` + `kind == "TaskEnvelopeIn"` with strict equality first.
- **D-04 (Vendor sentinel):** `provider.vendor = "anthropic"` for this phase. New `"langgraph"` sentinel deferred to Phase 51.
- **D-05 (Naming):** `cmd/tide-langgraph-verifier/` + image `ghcr.io/jsquirrelz/tide-langgraph-verifier`. Distinct from `internal/eval/`/`cmd/tide-eval/`.

**Credproxy TLS trust spike**
- **D-06 (Spike harness & upstream):** Standalone container spike (mirrors `cmd/tide-spike` precedent). Build the image, stand up the real `internal/credproxy` binary with a freshly-minted self-signed CA on `127.0.0.1:8443`, set `ANTHROPIC_BASE_URL=https://127.0.0.1:8443` + `SSL_CERT_FILE=/etc/tide/proxy/ca.crt`, make **one** real `ChatAnthropic(...).invoke()` with `max_tokens=1` against the real Anthropic API (key at `~/.tide/anthropic.key`). Binary pass/fail.
- **D-07 (Client-construction posture):** Separate the spike from the shipped image. Spike uses **plain** `ChatAnthropic` trusting `SSL_CERT_FILE` alone. Shipped image uses a "defensive client factory" — **[See Pitfall A below: the literal mechanism CONTEXT.md names (`ChatAnthropic(..., client=...)` / `anthropic_client=`) does not exist in the pinned `langchain-anthropic==1.4.8`. This is a genuine correction the plan must account for — see the corrected options.]**

**Read-only enforcement**
- **D-08 (Read-only jobspec variant lands in Phase 48):** `ReadOnly: true` on the `/workspace` worktree mount + separate ephemeral `emptyDir` scratch volume + omit git-write/push credentials + no manager-side child-CRD consumption path. Unit-tested and asserted, NOT yet dispatched by any reconciler (Phase 51 wires dispatch).
- **D-09 (RO proof — both layers):** (a) Static unit assertions on the verifier jobspec; (b) lightweight behavioral container test — run the image against a fixture worktree on a read-only bind mount, have its git/bash tool attempt `git commit`/`git push`, assert failure at the filesystem/credential layer.

**Dependency pinning + CI gate**
- **D-10:** Patch-exact pins (`langgraph==1.2.9`, `langchain-anthropic==1.4.8`, `anthropic==0.117.0`, `langchain-core==1.4.9`, `httpx==0.28.1`, base `python:3.13-slim-bookworm`) in a hash-locked `requirements.txt` via `pip install --require-hashes`, plus `make verify-langgraph-pins` grep gate rejecting range/unpinned specifiers.

### Claude's Discretion
- LangGraph graph internals (`create_react_agent` vs. a minimal `StateGraph` vs. `create_agent` — see Pitfall C), the exact trivial gate command the shell runs, multi-stage Dockerfile layout, and whether the standalone spike is driven by `pytest` or a plain `python` entrypoint.

### Deferred Ideas (OUT OF SCOPE)
- New `"langgraph"` vendor sentinel + `SelfInstruments` registration + `EVALUATOR`-kind span → Phase 51 (OBS-03).
- `VerifyContext` envelope field, `gate_decision` verdict schema, findings persistence → Phase 49.
- `role="verifier"` orchestrator-side Go prompt templates + coverage-not-conservatism → Phase 51 (EVAL-04).
- `TaskReconciler` dispatch of the verifier + concurrency-gate accounting + `LoopPolicy.BudgetCents` → Phase 51.
- Kind-cluster full-fidelity TLS/dispatch gate (optional promotion) → Phase 51.
- `cache_control`/`AnthropicPromptCachingMiddleware` + `Provider.Params` passthrough → future (CACHE-F1).
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| EVAL-01 | Read-only Python/LangGraph evaluator image behind the unchanged `pkg/dispatch.Subagent` + envelope seam (git-read + bash gate-command tool only; no file-edit/commit/push; no checkpointer), read-only enforced structurally, proven by an adversarial commit/push fixture test | Architecture Patterns (jobspec read-only variant, exact file:line seams); Pitfall D (git worktree add vs. ReadOnly mount contradiction — resolves how "git-read" must actually work); Don't Hand-Roll (envelope re-implementation); Validation Architecture (adversarial test design) |
| EVAL-02 | Credproxy TLS trust proven live pass/fail spike (`SSL_CERT_FILE` alone through real `ChatAnthropic`) before evaluation logic depends on it, `http_client=`/`anthropic_client=` fallback planned as contingency, patch-exact pins with CI gate | Standard Stack (re-verified pins, zero drift since 2026-07-18 committed research); Pitfall A (the fallback mechanism CONTEXT.md names does not exist — corrected options given); Code Examples (hash-locked requirements.txt, `make verify-langgraph-pins` gate); Package Legitimacy Audit |
</phase_requirements>

## Summary

This phase's prior research (STACK.md/PITFALLS.md, committed `f85ee3d`) is directionally sound and the version pins are **still exactly current** — re-verified live against PyPI today: zero packages moved. The genuinely new value of this research pass comes from going one level deeper than STACK.md did: downloading the **exact pinned wheels** (`langchain-anthropic==1.4.8`, `anthropic==0.117.0`, `langgraph==1.2.9`, `langgraph-prebuilt==1.1.0`, `langchain==1.3.14`) and reading their actual source, rather than relying on docs pages or changelog prose. That deeper read surfaces four concrete corrections the planner needs before writing tasks:

1. **The D-07 "defensive client factory" fallback mechanism does not exist in the pinned version.** `ChatAnthropic` in `langchain-anthropic==1.4.8` has no `client=`/`anthropic_client=`/`http_client=` constructor parameter — its Anthropic SDK client is built entirely inside two private `@cached_property`s with no injection hook. Worse: passing an unrecognized kwarg like `client=...` does **not** raise a constructor error — Pydantic's `build_extra` validator silently swallows it into `model_kwargs` and forwards it as a **call-time** API parameter, producing a confusing failure deep inside `.invoke()`, not a clean "unknown argument" error at construction. The real fallback (if the plain env-var spike fails) is subclassing `ChatAnthropic` to override its private `_client`/`_client_params` cached properties — exactly the "fragile monkey-patching" the upstream issue (`langchain#35843`) itself names as today's only workaround.
2. **`langgraph.prebuilt.create_react_agent` is formally deprecated** in the pinned `langgraph==1.2.9` (decorated `@deprecated(..., category=LangGraphDeprecatedSinceV10)`, pointing callers at `langchain.agents.create_agent`). CONTEXT.md's D-02 text ("leaning create_react_agent") predates this confirmation; `create_agent` is the non-deprecated, actively-maintained API for this exact pin and is recommended instead — still within D-02's "exact graph internals = discretion."
3. **`create_agent`'s compiled graph bakes in `recursion_limit=9999`** (not the "default 25" STACK.md cited from a docs page, and not even LangGraph's own module-level default of `10007`). If the plan wants the LangGraph loop's iteration count bounded for HARN-02 parity, it must **explicitly** pass `config={"recursion_limit": N}` at invoke time to override the framework default — relying on "the default" gives a ~10,000-step budget, not a small cap.
4. **`git worktree add` is not actually a read-only operation** and directly contradicts D-08's `ReadOnly: true` mount. It writes administrative files into `repoPath/worktrees/<name>/` (inside `repo.git`, which lives under the same `/workspace` mount) — this will hard-fail (`Read-only file system`) under D-08's ReadOnly mount. TIDE's existing `pkg/git/worktree.go` shows the executor's Task worktree is **already materialized** at `/workspace/worktrees/{task-uid}/` by the time any verifier would run — so the verifier's "git-read" tool must read that **already-existing** directory (`git show`, `git diff`, `git log`, `cat`) and must never call `git worktree add` itself.

Separately, the pin/dependency-lock mechanics (question 4) are now concretely proven end-to-end in this session: `uv pip compile requirements.in --generate-hashes` (available in this environment) produces a pip-compatible hash-locked `requirements.txt`, and — verified directly — the default output includes hashes for **all** platform wheels (confirmed the linux/manylinux `cp313` wheel hash for `pydantic-core` is present even though the compile ran on macOS), so generating the lockfile on a dev laptop and installing it inside the `python:3.13-slim-bookworm` container with `pip install --require-hashes` will work without extra flags.

**Primary recommendation:** Use `langchain.agents.create_agent(model, tools=[git_read, run_gate_command], system_prompt=..., ...)` (not `create_react_agent`) with an explicit `config={"recursion_limit": N}` at invoke time; build the shipped image's "defensive" TLS posture as an explicit pre-flight `httpx.Client(verify=ssl.create_default_context(cafile=SSL_CERT_FILE))` probe against credproxy before constructing `ChatAnthropic` (not a client-injection hook, which doesn't exist at this pin); implement "git-read" as read-only commands against the Task's **already-materialized** worktree directory, never `git worktree add`; and extend `internal/dispatch/podjob/jobspec.go`'s existing `BuildOptions`/`BuildJobSpec` with a single `ReadOnly bool` field rather than forking a parallel function.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Envelope decode/validate (`in.json` → `out.json`) | Subagent Pod (Python image) | — | Same in-pod contract every subagent image implements; orchestrator never parses inside the pod |
| LangGraph agent loop (tool-calling, LLM call) | Subagent Pod (Python image) | — | Runs entirely inside the K8s Job's main container; no orchestrator involvement mid-loop |
| Credential/TLS trust (credproxy sidecar) | K8s native sidecar (Go, `internal/credproxy`) | Subagent Pod (trusts via env var) | Existing infra, reused as-is; the Python image is a pure *consumer* of `SSL_CERT_FILE`/`ANTHROPIC_BASE_URL`, no TLS logic of its own beyond the optional pre-flight probe |
| Read-only enforcement (mount, credentials, child-CRD path) | K8s Job spec (Go, `internal/dispatch/podjob/jobspec.go`) | — | Structural, not prompt-based, per D-08/Pitfall 4 — must be enforced at the tier that controls mounts/secrets, which is the controller, not the pod |
| Dependency pin integrity (`requirements.txt` hash-lock) | CI (`make verify-langgraph-pins` + `pip install --require-hashes`) | Dockerfile build stage | Two layers: the grep gate catches a human editing `requirements.in` with a range; the hash-check catches a supply-chain substitution at install time |
| Image build/publish | CI (Dockerfile + build-images matrix) | Makefile (`docker-buildx-snapshot`, local dev) | Mirrors the existing `images/claude-subagent/Dockerfile` + matrix pattern exactly |
| Dispatch/reconciliation of the verifier | *(none this phase)* | Phase 51 (`TaskReconciler`) | Explicitly out of scope — D-08 says "not yet dispatched" |

## Standard Stack

### Core (re-verified live against PyPI JSON API today — zero drift since the 2026-07-18 committed STACK.md)

| Library | Version | Verified against | Notes |
|---------|---------|-------------------|-------|
| `langgraph` | 1.2.9 | `pypi.org/pypi/langgraph/json` — still latest | `requires_dist`: `langchain-core<2,>=1.4.7`, `langgraph-checkpoint<5,>=4.1.0`, `langgraph-prebuilt<1.2,>=1.1.0`, `pydantic>=2.7.4` — all satisfied by the pin set below |
| `langchain` | 1.3.14 | still latest | `requires_dist`: `langchain-core<2,>=1.4.9`, `langgraph<1.3,>=1.2.5` — the tightest constraint in the set (langchain-core must be *exactly* its own floor) |
| `langchain-anthropic` | 1.4.8 | still latest (87 total releases; 1.4.8 is newest) | `requires_dist`: `anthropic<1.0,>=0.96.0`, `langchain-core<2,>=1.4.7`, `pydantic<3,>=2.7.4` |
| `langchain-core` | 1.4.9 | still latest | required exactly at floor by `langchain==1.3.14` |
| `anthropic` (transitive) | 0.117.0 | still latest | `requires_dist`: `httpx<1,>=0.25.0`, `pydantic<3,>=1.9.0` |
| `pydantic` | 2.13.4 | still latest | `pydantic-core==2.46.4` exact pin under it |
| `httpx` (transitive) | 0.28.1 | still latest | — |
| `langgraph-checkpoint` (transitive) | 4.1.1 | still latest | not activated (no checkpointer passed) |
| `langgraph-prebuilt` (transitive) | 1.1.0 | still latest | **do not `pip install` directly** — comes in automatically; also now confirmed the module its `create_react_agent` lives in issues a `DeprecationWarning` on every call (Pitfall C) |

**[VERIFIED: PyPI JSON API + direct wheel-source read, 2026-07-18]** — every constraint above was cross-checked from live `requires_dist` metadata today, not from the committed STACK.md's prior pull. No re-pin is needed; the "1.x moves weekly" risk flagged in PITFALLS.md Pitfall 3 has not materialized in this window.

**Base image digest** (re-resolved live today, do not treat as durable — re-verify at actual build time per D-10's own instruction):
```
python:3.13-slim-bookworm@sha256:9d7f287598e1a5a978c015ee176d8216435aaf335ed69ac3c38dd1bbb10e8d64
```
**[VERIFIED: docker pull, 2026-07-18]**

### Installation (hash-locked, generated and proven in this session)

```bash
# requirements.in (hand-maintained, patch-exact; make verify-langgraph-pins greps THIS file)
langgraph==1.2.9
langchain==1.3.14
langchain-anthropic==1.4.8
langchain-core==1.4.9
anthropic==0.117.0
pydantic==2.13.4
httpx==0.28.1

# Compile the hash-locked lockfile (uv is available in this environment; pip-tools'
# `pip-compile --generate-hashes` is the equivalent if uv is unavailable in CI).
uv pip compile requirements.in --generate-hashes --output-file requirements.txt

# Dockerfile install step (fails hard if any transitive dep lacks a hash — this
# is pip's own enforcement, not a custom check):
pip install --require-hashes -r requirements.txt
```

**[VERIFIED: ran this exact sequence in this session]** — `uv pip compile` produced a 1163-line `requirements.txt` covering the full transitive closure (33 packages including `pydantic-core`, `orjson`, `ormsgpack`, `xxhash`, `langsmith`, etc.), every top-level requirement line pinned with `==` and one-or-more `--hash=sha256:...` continuation lines. **Confirmed cross-platform:** even though the compile ran on macOS with no `--python-platform` flag, the output includes the `manylinux_2_17_x86_64` `cp313` wheel hash for `pydantic-core==2.46.4` (cross-checked against PyPI's own file listing) — so building this lockfile on a dev laptop and running `pip install --require-hashes` inside the linux container will not fail on a missing platform hash. For determinism it's still good practice to pass `--python-platform linux --python-version 3.13` explicitly (pins the resolution scope instead of relying on uv's default multi-platform behavior), but it is not strictly required based on this test.

## Package Legitimacy Audit

Ran the full gate protocol in this session.

```
slopcheck checking 7 package(s) on pypi before install...
[OK] langchain-anthropic (pypi)  -- "Name looks like LLM bait but package is established."
[OK] httpx (pypi)
[OK] langgraph (pypi)
[OK] anthropic (pypi)
[OK] langchain-core (pypi)  -- same naming-pattern note as langchain-anthropic
[OK] langchain (pypi)
[OK] pydantic (pypi)
scanned 7 packages: 7 OK
```

| Package | Registry | Source Repo | slopcheck | Disposition |
|---------|----------|--------------|-----------|-------------|
| langgraph | PyPI | github.com/langchain-ai/langgraph | [OK] | Approved |
| langchain | PyPI | github.com/langchain-ai/langchain | [OK] | Approved |
| langchain-anthropic | PyPI | github.com/langchain-ai/langchain (monorepo) | [OK] (flagged the name pattern, confirmed established) | Approved |
| langchain-core | PyPI | github.com/langchain-ai/langchain (monorepo) | [OK] (same note) | Approved |
| anthropic | PyPI | github.com/anthropics/anthropic-sdk-python | [OK] | Approved |
| pydantic | PyPI | github.com/pydantic/pydantic | [OK] | Approved |
| httpx | PyPI | github.com/encode/httpx | [OK] | Approved |

**Packages removed due to slopcheck [SLOP] verdict:** none.
**Packages flagged as suspicious [SUS]:** none.

**Package name provenance note:** These 7 package names were not newly discovered this session — they were carried forward from the already-committed STACK.md (itself sourced via PyPI JSON API + Context7). This session independently re-verified existence, current-ness, and legitimacy via a fresh `slopcheck` run plus direct wheel download, so the `[VERIFIED: PyPI registry + direct source read]` tag is warranted for the 7 direct pins specifically (not merely "registry existence," which the provenance rule correctly treats as insufficient alone — the verification here is the actual downloaded source code, the strongest available evidence short of Context7's curated docs). Transitive dependencies pulled in by `uv pip compile` (e.g. `langsmith`, `orjson`, `ormsgpack`, `xxhash`) were not individually slopchecked — they are standard, long-established packages in the LangChain ecosystem; if the plan wants belt-and-suspenders coverage, run `slopcheck` against the full compiled `requirements.txt` package list at Dockerfile-build time as an additional CI step.

## Architecture Patterns

### System Architecture Diagram

```
                    ┌─────────────────────────────────────────┐
                    │  K8s Job (JobKindExecutor-shaped, D-08)  │
                    │                                           │
  in.json (PVC) ───►│  1. Python entrypoint reads in.json       │
                    │     validates apiVersion=="dispatch...    │
                    │     /v1alpha1" && kind=="TaskEnvelopeIn"  │
                    │     (strict equality, first step)         │
                    │            │                               │
                    │            ▼                               │
                    │  2. Build ChatAnthropic model              │
                    │     (env: ANTHROPIC_API_KEY=signed token,  │
                    │      ANTHROPIC_BASE_URL=https://127.0.0.1  │
                    │      :8443, SSL_CERT_FILE=ca.crt)          │
                    │            │                               │
                    │            ▼                               │
                    │  3. create_agent(model, tools=[             │
                    │       git_read,          # read-only,      │
                    │                           # NEVER           │
                    │                           # `worktree add`  │
                    │       run_gate_command,  # subprocess       │
                    │     ])                                      │
                    │            │                               │
                    │            ▼                               │
                    │  4. agent.invoke({"messages": [...]},       │
                    │       config={"recursion_limit": N})        │
                    │       ── tool-call loop ──►  (a) git show   │
                    │                              (b) bash gate  │
                    │                              (c) one real   │
                    │                                  LLM call  │
                    │                                  (via cred-│
                    │                                  proxy TLS)│
                    │            │                               │
                    │            ▼                               │
                    │  5. Build trivial EnvelopeOut                │
                    │     (exitCode 0, Result=last AIMessage       │
                    │      content, no findings/ChildCRDs)         │
                    └─────────────┬─────────────────────────────┘
                                  │
                     out.json ────┼──► /workspace/envelopes/<uid>/out.json (PVC)
                     TerminationStub ─► /dev/termination-log (≤4KB)
                                  │
  Volumes:                       │
   /workspace  (ReadOnly:true, subPath={project}/workspace)  ◄── D-08
   /scratch    (emptyDir, read-write, for incidental writes) ◄── D-08
   /etc/tide/proxy (ReadOnly:true, cert-shared emptyDir, credproxy sidecar output)

  Credproxy sidecar (native K8s 1.33 sidecar, RestartPolicy:Always) — UNCHANGED,
  reused as-is. Mints self-signed CA at pod start; listens 127.0.0.1:8443;
  validates HMAC signed token; injects real ANTHROPIC_API_KEY; enforces the
  hardcoded (POST /v1/messages) allowlist. Same sidecar the CLI subagent uses.
```

### Recommended Project Structure

```
cmd/tide-langgraph-verifier/
├── Dockerfile                 # single-stage: python:3.13-slim-bookworm@sha256:...
├── requirements.in            # hand-maintained, patch-exact (grep target for the pin gate)
├── requirements.txt           # generated: uv pip compile --generate-hashes
└── verifier/
    ├── __init__.py
    ├── __main__.py             # entrypoint: python -m verifier
    ├── envelope.py             # ReadEnvelopeIn/WriteEnvelopeOut/ValidateAPIVersionKind
                                # port (mirrors internal/harness/envelope_io.go's contract,
                                # NOT its Go code — pkg/dispatch import-firewalled to Go)
    ├── tools.py                # @tool git_read, @tool run_gate_command
    ├── agent.py                # create_agent(...) wiring
    ├── tls_client.py           # the defensive pre-flight probe (Pitfall A)
    └── tests/
        ├── test_envelope.py    # strict apiVersion/kind rejection
        ├── test_tools.py       # git_read / run_gate_command in isolation
        └── test_readonly.py    # adversarial commit/push behavioral test (D-09b)

cmd/tide-spike/                # existing precedent — D-06's TLS spike follows this
                                # shape but as a Python script, not a Go binary; house
                                # it at cmd/tide-langgraph-verifier/spike/ or similar,
                                # planner's call, since it drives THIS image
```

### Pattern 1: `create_agent` (not `create_react_agent`) as the graph factory

**What:** `langchain.agents.create_agent(model, tools, *, system_prompt=..., checkpointer=None, ...)` — the non-deprecated agent factory in the pinned `langchain==1.3.14`.
**When to use:** Every stage of this phase's graph (D-02's "genuinely exercise the LangGraph runtime" requirement).
**Exact verified signature** (read directly from the pinned wheel, `langchain/agents/factory.py:808`):
```python
def create_agent(
    model: str | BaseChatModel,
    tools: Sequence[BaseTool | Callable[..., Any] | dict[str, Any]] | None = None,
    *,
    system_prompt: str | SystemMessage | None = None,
    middleware: Sequence[AgentMiddleware] = (),
    response_format: ResponseFormat | type | dict | None = None,   # unused this phase (D-01: no verdict schema)
    state_schema: type[AgentState] | None = None,
    context_schema: type | None = None,
    checkpointer: Checkpointer | None = None,                       # leave None (D-02: no checkpointer)
    store: BaseStore | None = None,
    interrupt_before: list[str] | None = None,
    interrupt_after: list[str] | None = None,
    debug: bool = False,
    name: str | None = None,
    cache: BaseCache | None = None,
    transformers: Sequence[TransformerFactory] | None = None,
) -> CompiledStateGraph: ...
```
**Minimal working shape (Phase 48's D-01 trivial scope):**
```python
# Source: langchain-1.3.14 factory.py + langchain_core-1.4.9 tools/convert.py,
# both read directly from the pinned wheels in this session.
import subprocess
from langchain.agents import create_agent
from langchain_core.tools import tool
from langchain_anthropic import ChatAnthropic

@tool
def git_read(args: str) -> str:
    """Run a read-only git command (show, diff, log, ls-tree, cat-file) against
    the already-materialized Task worktree and return its output. Never
    'worktree add', 'commit', 'push', or 'config user.*' — those mutate .git
    metadata and will fail under the ReadOnly mount anyway (Pitfall D)."""
    result = subprocess.run(
        ["git"] + args.split(),
        cwd=WORKTREE_DIR, capture_output=True, text=True, timeout=30, check=False,
    )
    return result.stdout or result.stderr

@tool
def run_gate_command(command: str) -> str:
    """Execute the declared read-only gate command and return combined output.
    `command` comes only from the orchestrator-authored envelope, never from
    repo content or model output (ASVS V5 — see Security Domain)."""
    result = subprocess.run(
        command, shell=True, cwd=WORKTREE_DIR,
        capture_output=True, text=True, timeout=60, check=False,
    )
    return f"exit_code={result.returncode}\nstdout:\n{result.stdout}\nstderr:\n{result.stderr}"

model = ChatAnthropic(model=envelope_in.provider.model)  # reads ANTHROPIC_API_KEY/
                                                          # ANTHROPIC_BASE_URL from env
                                                          # (already set by jobspec.go)
agent = create_agent(model, tools=[git_read, run_gate_command],
                      system_prompt=envelope_in.prompt)

result = agent.invoke(
    {"messages": [{"role": "user", "content": envelope_in.prompt}]},
    config={"recursion_limit": 10},   # EXPLICIT — see Pitfall B, do not rely on the
                                       # framework default (9999, not 25)
)
final_text = result["messages"][-1].content
```
**Invoke input/output shape** (`langchain/agents/middleware/types.py`, read directly): input is `InputAgentState.messages: list[AnyMessage | dict]` (plain role/content dicts accepted); output is `OutputAgentState.messages: list[AnyMessage]` (full history via the `add_messages` reducer) — the harness takes `result["messages"][-1].content` as the one-line `EnvelopeOut.Result`.

### Pattern 2: Read-only jobspec variant — minimal-diff extension of `BuildOptions`

**What:** Extend the existing `internal/dispatch/podjob/jobspec.go` `BuildOptions` struct (currently `Kind`/`Task`/`ParentObj`/... — no read-only concept yet) with one new field, reusing the same `BuildJobSpec` function rather than forking a parallel `BuildVerifierJobSpec`.
**Why this shape:** The file already uses exactly this "single function + boolean-gated branch" idiom for `credproxyEnabled` (line 322) — reusing it for read-only-ness keeps one code path instead of two diverging ones, and every existing jobspec test (`jobspec_test.go`, 30+ table-style `TestBuildJobSpec_*` functions) stays a template to copy for the new assertions.
**Exact seams (verified by direct read, `internal/dispatch/podjob/jobspec.go`):**
```go
// BuildOptions (struct definition ~line 82) — add:
type BuildOptions struct {
    // ... existing fields ...

    // ReadOnly marks this dispatch as a read-only verifier variant (D-08).
    // When true: the /workspace mount is ReadOnly, a separate scratch emptyDir
    // is added, and ReadOnlyRootFilesystem flips true on the subagent container.
    // Git-write/push credentials are NEVER added to this Job regardless of this
    // flag — they are already isolated to the separate tide-push Job
    // (push_helpers.go), so no additional omission logic is needed here; a
    // regression test should assert this stays true.
    ReadOnly bool
}

// Step 7 (subagent container, ~line 394) — the /workspace mount gains ReadOnly:
subagentMounts := []corev1.VolumeMount{
    {
        Name:      VolumeProjectWorkspace,
        MountPath: "/workspace",
        SubPath:   subPath,
        ReadOnly:  opts.ReadOnly,   // NEW
    },
}
if opts.ReadOnly {
    subagentMounts = append(subagentMounts, corev1.VolumeMount{
        Name:      "verifier-scratch",   // NEW emptyDir, added to `volumes` in step 8
        MountPath: "/scratch",
    })
}

// SecurityContext (~line 445-452) — flip ReadOnlyRootFilesystem for the ReadOnly case:
ReadOnlyRootFilesystem: new(!opts.ReadOnly), // was hardcoded false; true when opts.ReadOnly
```
**Confirmed for free (verified by direct read of `push_helpers.go:131/179-180/324/455`):** git push credentials (`GIT_PAT` via `project.Spec.Git.CredsSecretRef`) are injected **only** into the separate `tide-push` Job spec built inline in `push_helpers.go` — `BuildJobSpec` in `jobspec.go` never wires them into the subagent/verifier container today. D-08's "omit git-write/push credentials" requirement is therefore **already satisfied by construction**; the task is to write a regression test proving it (`grep` the built `EnvFrom`/`Env` for the credentials secret name and assert absence), not to add new omission logic.
**No manager-side child-CRD consumption path:** also free this phase — nothing dispatches the verifier yet (D-08), so there is no reconciler code to NOT-write.

### Anti-Patterns to Avoid
- **Calling `git worktree add` from inside the verifier's git-read tool** — see Pitfall D. It is not a read-only operation and will fail under D-08's mount.
- **Passing `client=`/`anthropic_client=` to `ChatAnthropic(...)`** expecting a clean rejection if wrong — it silently succeeds at construction and fails confusingly later (Pitfall B).
- **Relying on `create_agent`'s default recursion limit** as an iteration-cap parity mechanism — it's 9999, not a small number (Pitfall C-2).
- **A parallel `BuildVerifierJobSpec` function** duplicating `BuildJobSpec`'s ~350 lines — prefer the single-function + `ReadOnly bool` extension (Pattern 2) unless Phase 51 discovers real divergence.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Envelope JSON shape | A from-scratch Python dataclass guessing at field names | Re-implement `EnvelopeIn`/`EnvelopeOut`/`TerminationStub` field-for-field from `pkg/dispatch/envelope.go` (read directly, lines 45/170/394) — the Go struct tags are the wire contract | The Python image cannot import `pkg/dispatch` (import-firewalled), but the JSON shape is still frozen contract; a hand-guessed shape drifts silently |
| Agent tool-calling loop | A manual while-loop calling `.bind_tools()` + parsing tool_calls | `langchain.agents.create_agent(model, tools=[...])` | Gets the loop, message accumulation, and stop-condition handling for free — hand-rolling reproduces what the library already does correctly |
| TLS trust verification | A custom SSL handshake test script | The credproxy's existing `MintSelfSignedCert`/`ListenAndServeTLS` (`internal/credproxy/cert.go`, `server.go`) — reuse as-is, zero changes needed | Already built, tested infra from Phase 5; the spike's job is to prove the *Python client* trusts it, not to reinvent the proxy |
| Hash-lock generation | Manually computing `pip hash` per package | `uv pip compile requirements.in --generate-hashes` (or `pip-compile --generate-hashes` from pip-tools) | Automatically resolves the full transitive closure and emits every wheel's hash across platforms — verified in this session to include the linux wheel even when compiled on macOS |

**Key insight:** Every piece of this phase that looks like it needs new code has an existing TIDE precedent (credproxy, envelope contract, jobspec boolean-gated branches, `tide-spike` harness shape) or an existing LangChain/LangGraph library mechanism (`create_agent`, `@tool`) — the actual new surface area is narrow: the envelope re-implementation, two tool functions, one model construction, and one jobspec field.

## Common Pitfalls

### Pitfall A: The D-07 "defensive client factory" mechanism does not exist in `langchain-anthropic==1.4.8`

**What goes wrong:** CONTEXT.md's D-07 describes the shipped image's fallback as constructing `anthropic.Anthropic(http_client=httpx.Client(verify=...))` and passing it into `ChatAnthropic(..., client=...)` "or the pinned-version equivalent `anthropic_client=`". **Neither kwarg exists.** Verified by downloading `langchain_anthropic-1.4.8-py3-none-any.whl` and reading `chat_models.py` directly: `ChatAnthropic`'s only client-construction path is two private `@cached_property`s (`_client_params` at line 1184, `_client` at line 1205) that unconditionally build `anthropic.Client(**params)` via an internal `_get_default_httpx_client(base_url=..., timeout=..., anthropic_proxy=...)` helper — there is no pydantic field, alias, or constructor parameter named `client`, `anthropic_client`, or `http_client` anywhere in the class (confirmed by grep across the entire file).

**Why it happens:** This is the exact, still-open upstream gap (`langchain-ai/langchain#35843`) PITFALLS.md already flagged from the outside (via the GitHub issue text). This session confirms it from the *inside* — reading the actual shipped code, not just the issue description — and additionally discovers a sharper trap: passing the wrong kwarg doesn't fail loudly.

**How to avoid:**
1. Read `langchain_core/utils/utils.py::_build_model_kwargs` (confirmed by direct read this session): any constructor kwarg pydantic doesn't recognize as a declared field is not rejected — it's popped into `model_kwargs` with a `warnings.warn(...)` (easy to miss in container logs), then forwarded as a **call-time** kwarg to `Anthropic.messages.create(client=<object>, ...)` at the first `.invoke()`. This will not raise "unexpected keyword argument" at construction; it will raise something confusing (a serialization error, or a silently-ignored extra API param) deep inside the agent loop's first LLM call.
2. Given #1 cannot work, the real choices for the shipped image's "defensive" posture are:
   - **(a) Subclass override** — `class DefensiveChatAnthropic(ChatAnthropic):` overriding the private `_client`/`_client_params` cached properties to inject an explicit `ssl.create_default_context(cafile=SSL_CERT_FILE)` wrapped in `httpx.Client(verify=ctx)`. Fragile (private/underscore API, could rename on the next patch) — but it is literally what the upstream issue calls "monkey-patching... today's only workaround."
   - **(b) Pre-flight probe, not a construction hook** — before constructing `ChatAnthropic`, make one raw `httpx.Client(verify=ssl.create_default_context(cafile=os.environ["SSL_CERT_FILE"])).get(f"{base_url}/v1/models")`-style health check (or reuse the spike's own probe). If it raises `SSLCertVerificationError`, fail the Task with an explicit `TerminationStub{Reason: "credproxy-tls-untrusted"}` instead of surfacing an opaque error mid-agent-loop. This is NOT literally "a defensive client factory" as CONTEXT.md's text describes it, but it delivers the same operational goal (fail fast and clearly on a CA-trust misconfiguration) without touching private APIs.
   - **(c) Bypass `ChatAnthropic` entirely for the TLS-sensitive call** and use the raw `anthropic.Anthropic(http_client=...)` SDK client directly — confirmed this constructor **does** accept `http_client: httpx.Client | None = None` (read directly, `anthropic/_client.py:164`). This loses LangGraph/LangChain's `BaseChatModel` integration unless wrapped in a custom adapter — a heavier lift than the phase's "small surface" goal justifies.
3. **Recommendation for the plan:** ship option (b) — the pre-flight probe — as the primary defensive mechanism (simple, no private-API coupling, gives an actionable failure), and record option (a) as the documented fallback-of-last-resort if the plain env-var path from the spike (D-06/D-07) fails outright and a genuine client-level override becomes unavoidable. Do not attempt option (c) unless Phase 51 needs full transport control.

**Warning signs:** A `UserWarning` in container logs reading `"client is not default parameter... was transferred to model_kwargs"` — this means the code attempted the non-existent kwarg and it's silently misrouted, not working.

**Confidence:** HIGH — `[VERIFIED: direct wheel source read, langchain_anthropic-1.4.8, langchain_core-1.4.9, this session]`.

### Pitfall B: `create_react_agent` is deprecated; `create_agent` is the real current API for this pin

**What goes wrong:** STACK.md's own text already leaned toward `create_agent` for structured-output reasons, but CONTEXT.md's D-02 still says "leaning `create_react_agent`". Direct read of the pinned `langgraph-prebuilt==1.1.0` wheel (`chat_agent_executor.py:274-277`) shows `create_react_agent` is wrapped in `@deprecated("create_react_agent has been moved to langchain.agents. Please update your import to from langchain.agents import create_agent.", category=LangGraphDeprecatedSinceV10)` — every call emits a `DeprecationWarning` at runtime.

**Why it happens:** LangChain/LangGraph consolidated the prebuilt-agent surface into the `langchain` package as of the 1.x line; `langgraph.prebuilt.create_react_agent` is kept only for backward compatibility.

**How to avoid:** Use `from langchain.agents import create_agent` (Pattern 1 above) — it is the actively-maintained, non-deprecated equivalent at this exact pin, has an equivalent (in fact richer) signature, and avoids a `DeprecationWarning` noise line in every container's logs. This stays inside D-02's explicitly-granted discretion ("exact graph internals... researcher/planner to decide").

**Confidence:** HIGH — `[VERIFIED: direct wheel source read, langgraph_prebuilt-1.1.0, this session]`.

### Pitfall C: `create_agent`'s baked-in `recursion_limit` is 9999, not the "25" STACK.md cites

**What goes wrong:** STACK.md §5 recommends "reuse LangGraph's own `recursion_limit` config (default 25...) as the iteration-cap parity mechanism," citing `docs.langchain.com/oss/python/langgraph/errors/GRAPH_RECURSION_LIMIT`. Direct read of the pinned `langchain==1.3.14` wheel (`langchain/agents/factory.py:1778-1780`) shows `create_agent`'s `graph.compile(...)` call is preceded by `config: RunnableConfig = {"recursion_limit": 9_999}` — a hardcoded override citing `github.com/langchain-ai/langgraph/issues/7313`. Separately, `langgraph`'s own module-level default (`langgraph/_internal/_config.py:32`, read directly) is `DEFAULT_RECURSION_LIMIT = int(getenv("LANGGRAPH_DEFAULT_RECURSION_LIMIT", "10007"))` — neither the docs-cited 25 nor a small number.

**Why it happens:** The docs page STACK.md cited likely describes `create_react_agent`'s older default or a different graph-construction path; `create_agent` (Pattern 1's recommendation) explicitly re-sets it to 9999 for its own reasons (linked upstream issue about interrupt/checkpoint step counting), independent of the module default.

**How to avoid:** If HARN-02 iteration-cap parity matters for this phase (the K8s Job's `activeDeadlineSeconds` is always the ultimate backstop regardless), pass `config={"recursion_limit": N}` explicitly at `.invoke()` time with a small N (e.g. 10-20, sized to "one gate command + one git-read + one LLM call" for D-01's trivial scope) — do not rely on any framework default at any layer, since three different layers of this exact stack disagree by three orders of magnitude (25 vs. 9999 vs. 10007).

**Confidence:** HIGH — `[VERIFIED: direct wheel source read, langchain-1.3.14 + langgraph-1.2.9, this session]`. This directly corrects a MEDIUM-confidence citation in the committed STACK.md.

### Pitfall D: `git worktree add` is not read-only and will fail under the D-08 mount

**What goes wrong:** STACK.md §5 lists example read-only git operations as "`git show`, `git diff`, `git log`, `git worktree add` (to materialize a run branch read-only)". `git worktree add` is **not** a read-only filesystem operation: it writes administrative metadata (`HEAD`, `index`, `gitdir`, `commondir`, `ORIG_HEAD`) into `<repoPath>/worktrees/<name>/` inside the bare repo's own directory — confirmed directly against TIDE's own `pkg/git/worktree.go:59-79`, which shows the executor's `AddWorktree` running exactly `git -C repoPath worktree add -b <branch> <worktreeDir> <runBranch>` where `repoPath` is `/workspace/repo.git` — the SAME `/workspace` mount D-08 makes `ReadOnly: true` for the verifier. Calling this from inside the verifier's git-read tool will fail with a permission/read-only-filesystem error.

**Why it happens:** The confusion is understandable — `worktree add` *feels* like a read operation (you're just "checking out a view"), but git's implementation always writes admin state alongside the new working directory, regardless of what you do inside that working directory afterward.

**How to avoid:** The verifier never needs to materialize a NEW worktree — by the time any verifier runs (even in Phase 48's fixture-driven adversarial test), the Task whose output it's checking has **already** run `AddWorktree`, which placed a working tree at `/workspace/worktrees/{task-uid}/` (confirmed directory layout, `pkg/git/worktree.go:50-58`). The verifier's `git_read` tool should `cd`/`-C` into that **already-existing** directory and run only non-mutating commands: `git show <ref>:<path>`, `git diff`, `git log`, `git ls-tree`, `git cat-file -p` — never `worktree add`, `commit`, `push`, or `config user.*`. For Phase 48's own adversarial fixture test (D-09b), the test harness materializes a fixture worktree **before** mounting it read-only for the verifier container — the verifier itself performs zero worktree-creation.

**Confidence:** HIGH — `[VERIFIED: direct read, pkg/git/worktree.go, this session; general git internals knowledge]`. This is a load-bearing correction: it resolves a genuine internal contradiction between two of this phase's own inherited research artifacts (STACK.md's git-ops list vs. D-08's ReadOnly mount) that would otherwise surface as a confusing runtime failure mid-implementation.

### Pitfall E (carried forward from PITFALLS.md, sharpened): `SelfInstruments` double-emission risk applies even to the trivial Phase 48 shell

Even though Phase 48 doesn't register a `"langgraph"` vendor sentinel (D-04 keeps `"anthropic"`), if `openinference-instrumentation-langchain` (or any LangChain OTel auto-instrumentation) is installed as a transitive dependency and imported, it will patch `langchain_core.callbacks` and may emit spans in-process the moment `ChatAnthropic.invoke()` runs — regardless of whether Phase 48 wires TRACEPARENT propagation. **For Phase 48's scope specifically:** do not add `openinference-instrumentation-langchain` (or any OTel exporter) to `requirements.in` at all — it is not in the locked pin set (D-10 lists exactly `langgraph`/`langchain`/`langchain-anthropic`/`langchain-core`/`anthropic`/`httpx`, no OTel packages), and adding it prematurely would trigger the double-span-emission risk PITFALLS.md Pitfall 1 describes, one phase before the vendor-registration work (Phase 51) that's supposed to land alongside it. Confirm at code-review time that no transitive dependency silently pulls in OTel auto-instrumentation.

**Confidence:** HIGH for the "don't add it yet" recommendation (D-10's pin list is authoritative and excludes it); MEDIUM for whether any of the 7 pinned packages transitively imports OTel instrumentation (not exhaustively checked this session — `pip show` the installed tree at build time to confirm zero `opentelemetry-instrumentation-langchain`/`openinference-*` packages are present).

## Code Examples

### Hash-locked requirements.txt + `make verify-langgraph-pins` gate

```makefile
# Mirrors the style of verify-dispatch-imports (Makefile:522) and
# verify-no-aggregates (Makefile:563) — grep-based, fails loud, single purpose.
##@ LangGraph Verifier Pin Gate (EVAL-02 / Pitfall 3)

.PHONY: verify-langgraph-pins
verify-langgraph-pins: ## Assert cmd/tide-langgraph-verifier/requirements.in pins every direct dependency patch-exact (EVAL-02).
	@echo "verifying cmd/tide-langgraph-verifier/requirements.in has no range/unpinned specifiers..."
	@FILE=cmd/tide-langgraph-verifier/requirements.in; \
	if [ ! -f "$$FILE" ]; then echo "no $$FILE found — gate misconfigured"; exit 1; fi; \
	VIOLATIONS=$$(grep -vE '^\s*(#|$$)' $$FILE | grep -vE '^[A-Za-z0-9_.-]+==[0-9]' || true); \
	if [ -n "$$VIOLATIONS" ]; then \
		echo "EVAL-02 violation: range/unpinned specifiers detected in $$FILE:"; \
		echo "$$VIOLATIONS"; \
		exit 1; \
	fi
	@echo "OK: all direct pins are patch-exact"
```
Note the split: this gate greps the **hand-maintained** `requirements.in` (catches a human typing `langgraph>=1.2`), while `pip install --require-hashes -r requirements.txt` at Docker build time is a **second**, independent enforcement layer that catches a hash mismatch/substitution in the **machine-generated** lockfile — together they cover both failure classes (a careless edit vs. a supply-chain substitution).

### Dockerfile (single-stage — no Go build stage needed, unlike `images/claude-subagent/Dockerfile`)

```dockerfile
# syntax=docker/dockerfile:1
# Build the tide-langgraph-verifier runtime image.
# Invoked from REPO ROOT:
#   docker build -t ghcr.io/jsquirrelz/tide-langgraph-verifier:test \
#                -f cmd/tide-langgraph-verifier/Dockerfile .
#
# Digest re-verified live 2026-07-18 (docker pull); re-check at actual build time
# per D-10's "1.x moves ~weekly" discipline — this is the BASE image, not the
# pinned Python packages, but it drifts too (rebuilt periodically upstream).
FROM python:3.13-slim-bookworm@sha256:9d7f287598e1a5a978c015ee176d8216435aaf335ed69ac3c38dd1bbb10e8d64

RUN apt-get update \
    && apt-get install -y --no-install-recommends git \
    && rm -rf /var/lib/apt/lists/* \
    && git config --system --add safe.directory '*'

WORKDIR /app
COPY cmd/tide-langgraph-verifier/requirements.txt .
RUN pip install --no-cache-dir --require-hashes -r requirements.txt

COPY cmd/tide-langgraph-verifier/verifier/ /app/verifier/

USER 1000

ENV SSL_CERT_FILE=/etc/tide/proxy/ca.crt
ENTRYPOINT ["python", "-m", "verifier"]
```

### `.dockerignore` re-includes needed

The repo's `.dockerignore` (read directly) is deny-by-default (`**`) with targeted re-includes for Go/TOML/tmpl/dashboard-dist patterns — none of which match Python source or `requirements.txt`. Add:
```
!cmd/tide-langgraph-verifier/**
```
Without this, the Docker build context silently excludes the new image's entire source tree.

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|---------------|--------|
| `langgraph.prebuilt.create_react_agent` | `langchain.agents.create_agent` | Formal deprecation present in `langgraph==1.2.9` (current pin) | Use `create_agent`; `create_react_agent` still runs but emits a `DeprecationWarning` on every call |
| Assuming `recursion_limit` default ≈ 25 | `create_agent` bakes in 9999; bare `langgraph` module default is 10007 | Confirmed at the exact pinned versions this session | Must explicitly pass `config={"recursion_limit": N}` for any real iteration cap |
| `ChatAnthropic(..., client=...)` as a documented override hook | No such hook exists at this pin; `langchain#35843` still open | Unresolved as of 1.4.8 (latest release) | Plan the pre-flight-probe fallback (Pitfall A), not a construction-time override |

**Deprecated/outdated:** `create_react_agent` (see above); `REQUESTS_CA_BUNDLE`/`CURL_CA_BUNDLE` (already correctly excluded by STACK.md/PITFALLS.md — httpx never reads them, re-confirmed by source read this session, no new finding here).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | The recommended defensive-posture fallback (pre-flight `httpx` probe, option b in Pitfall A) is the best tradeoff vs. subclassing `ChatAnthropic`'s private cached properties | Pitfall A / Summary | If the plan instead wants the subclass-override approach for stronger defense-in-depth, that's a reasonable alternative choice — flag as a plan-time decision, not re-litigate the research |
| A2 | No transitive dependency in the 7-package pin set silently pulls in OTel auto-instrumentation (`openinference-instrumentation-langchain` or similar) | Pitfall E | If one does, Phase 48's trivial shell could double-emit spans a phase before Phase 51's vendor-registration work lands — verify with `pip show`/`pip list` at build time before shipping |
| A3 | `uv pip compile`'s default (no `--python-platform` flag) multi-platform hash output is sufficient for the target linux container without further flags | Standard Stack / Installation | Confirmed for `pydantic-core` specifically this session; if a different package's compiled-C wheel has platform-specific naming quirks, worth spot-checking at Dockerfile-build time that `pip install --require-hashes` actually succeeds inside the real linux container, not just assumed from this one cross-check |

**All four numbered corrections in the Summary (Pitfalls A–D) are `[VERIFIED]` via direct primary-source reads in this session, not `[ASSUMED]`** — they do not need user confirmation as "assumptions," but the planner should treat them as supersede-and-replace corrections to the committed STACK.md/PITFALLS.md/CONTEXT.md text on these four specific points.

## Open Questions (RESOLVED)

1. **Exact shape of the D-06 spike's own harness (pytest vs. plain `python` entrypoint)**
   - What we know: `cmd/tide-spike/main.go` is the Go precedent (flag-driven, env-var-driven, fail-closed, never logs the token). D-06 says "standalone container spike," explicitly Claude's Discretion on driver mechanism.
   - What's unclear: whether the Python spike should live as a `pytest` test (integrates with `make test` conceptually) or a bare `python spike.py` script (closer parity with the Go precedent's flag/env style).
   - RESOLVED: bare `python` script with the same flag/env pattern as `cmd/tide-spike/main.go` (proxy endpoint, signed token via env, never logged, binary exit code) — keeps the TLS proof runnable standalone outside any test framework, matching D-06's "standalone container" framing more literally than a pytest wrapper would. (Followed by Plan 48-05.)

2. **Whether the read-only jobspec variant needs a `JobKindVerifier` const now, or stays a boolean flag on the existing `JobKindExecutor` shape**
   - What we know: `JobKind` currently has exactly two values (`executor`, `planner`), used for wall-clock floor + label derivation. Phase 51 will eventually want a `"verifier"` role label for real dispatch/observability.
   - What's unclear: whether introducing `JobKindVerifier` now (unused until Phase 51) is worth the forward-compatibility vs. keeping Phase 48's change to the smallest possible diff (a `ReadOnly bool` field only, label role stays `"executor"` for now).
   - RESOLVED: smallest diff for Phase 48 (boolean field only, per Pattern 2) — since D-08 explicitly says "not yet dispatched by any reconciler," introducing a whole new `JobKind` value with no consumer yet risks looking "done" in a way that invites premature wiring. Phase 51 can introduce `JobKindVerifier` when it actually needs the role label for dispatch/labels. (Followed by Plan 48-02.)

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Docker | Building/running the image locally, D-09(b)'s adversarial bind-mount test | ✓ | confirmed via `docker pull`/`docker inspect` this session | — |
| `uv` | Generating the hash-locked `requirements.txt` | ✓ | 0.11.18 (this session's sandbox) | `pip-tools`' `pip-compile --generate-hashes` if `uv` is unavailable in CI |
| `pip` (with hash-checking support) | `pip install --require-hashes` in the Dockerfile | ✓ | any modern pip (feature is long-stable) | — |
| `slopcheck` | Package Legitimacy Audit | ✓ | installed via `pip install slopcheck` this session | mark all packages `[ASSUMED]` if unavailable in the actual execution environment |
| A durable Anthropic API key at `~/.tide/anthropic.key` | D-06's live spike (real API call) | Not verified in this research session (out of scope — file lives outside the repo) | — | The spike is explicitly designed to fail closed and report "key absent" per the `tide-spike` precedent's `requireFlag` pattern; no code fallback needed, just confirm before running the spike live |

**Missing dependencies with no fallback:** none identified.
**Missing dependencies with fallback:** `uv` → `pip-tools` if CI lacks `uv`.

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Go framework | Ginkgo v2.28 + Gomega (existing, `internal/dispatch/podjob/jobspec_test.go` is plain `go test`, not Ginkgo — table-style `func Test...(t *testing.T)`) |
| Python framework | `pytest` (STACK.md's recommendation; **not yet present in this repo** — Wave 0 gap, no `pyproject.toml`/`pytest.ini` exists anywhere in the tree, confirmed by search this session) |
| Config file | none yet — Wave 0 must add `cmd/tide-langgraph-verifier/pyproject.toml` (or `pytest.ini`) |
| Quick run command (Go) | `go test ./internal/dispatch/podjob/... -run TestBuildJobSpec` |
| Quick run command (Python) | `cd cmd/tide-langgraph-verifier && python -m pytest verifier/tests/ -x` (once Wave 0 lands) |
| Full suite command | `make test` (Go unit tier) + the new Python test invocation, both wired into `make test` or a dedicated `make test-langgraph-verifier` target |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| EVAL-01 | Envelope strict apiVersion/kind rejection | unit (Python) | `pytest verifier/tests/test_envelope.py -x` | ❌ Wave 0 |
| EVAL-01 | `git_read`/`run_gate_command` tools work in isolation against a fixture worktree | unit (Python) | `pytest verifier/tests/test_tools.py -x` | ❌ Wave 0 |
| EVAL-01 | Static jobspec assertions (`ReadOnly:true`, no git-write creds in `EnvFrom`, `ReadOnlyRootFilesystem:true`) | unit (Go) | `go test ./internal/dispatch/podjob/... -run TestBuildJobSpec_Verifier` | ❌ Wave 0 (new test functions alongside existing `jobspec_test.go`) |
| EVAL-01 | Adversarial commit/push behavioral test — attempted git commit/push fails at the filesystem layer under a read-only bind mount | integration (Docker) | new `make` target, e.g. `make test-verifier-readonly` shelling to `docker run --read-only -v <fixture>:/workspace:ro <image> ...` and asserting non-zero exit / EROFS in output | ❌ Wave 0 — genuinely new test infrastructure, no existing container-behavioral test precedent in this repo (confirmed by search this session) |
| EVAL-02 | Live TLS pass/fail spike (credproxy + real Anthropic API) | manual-only (deliberately, per D-06 — costs real cents, needs the durable key) | `python cmd/tide-langgraph-verifier/spike/tls_spike.py` (or equivalent), run manually, verdict recorded as a decision artifact (mirrors CACHE-01's precedent) | ❌ Wave 0 (new script) |
| EVAL-02 | `make verify-langgraph-pins` rejects a deliberately-introduced range specifier | unit (Makefile/shell) | `make verify-langgraph-pins` (should exit 0 on the real file; a fixture-based negative test can assert exit 1 on a temp file with `langgraph>=1.2`) | ❌ Wave 0 |
| EVAL-02 | `pip install --require-hashes` fails on a tampered hash | integration (Docker build) | Part of the normal `docker build` — no separate test needed; the enforcement IS the build step | N/A — self-enforcing |

### Sampling Rate
- **Per task commit:** the relevant `pytest`/`go test` slice for the file(s) touched.
- **Per wave merge:** full Python `pytest` suite + `go test ./internal/dispatch/podjob/...` + `make verify-langgraph-pins`.
- **Phase gate:** all of the above green, PLUS the adversarial behavioral test (D-09b) and — separately, manually, once — the live TLS spike (D-06), whose PASS/FAIL verdict is recorded as a decision artifact before Phase 49 proceeds.

### Wave 0 Gaps
- [ ] `cmd/tide-langgraph-verifier/pyproject.toml` (or `pytest.ini`) — no Python test framework config exists anywhere in this repo yet.
- [ ] `cmd/tide-langgraph-verifier/verifier/tests/conftest.py` — shared fixtures (fixture worktree builder, envelope fixtures).
- [ ] New Go test functions in `internal/dispatch/podjob/jobspec_test.go` (or a new `jobspec_readonly_test.go`) for the `ReadOnly` field — no existing test covers it since the field doesn't exist yet.
- [ ] New `make test-verifier-readonly`-style Makefile target — no existing container-behavioral test infrastructure in this repo (confirmed by search this session; the only `docker run` in the Makefile is the unrelated goreleaser snapshot step).
- [ ] `.dockerignore` re-include line for `cmd/tide-langgraph-verifier/**`.

## Security Domain

`security_enforcement` is not set to `false` in `.planning/config.json` (absent → enabled per the operating rule) — this section is required.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-------------------|
| V2 Authentication | No (this pod holds no user-facing auth; the HMAC signed-token scheme is credproxy's existing, unchanged mechanism) | — |
| V3 Session Management | No | — |
| V4 Access Control | Yes — structural read-only enforcement | ReadOnly mount + credential omission + no child-CRD path (D-08/D-09), NOT prompt-based (Pitfall 4 in PITFALLS.md, re-confirmed as the correct model this session) |
| V5 Input Validation | Yes — two surfaces | (1) `ValidateAPIVersionKind` strict-equality check on envelope decode (mirrors Go); (2) the `run_gate_command` tool's `shell=True` subprocess call is a command-injection-shaped surface — see Known Threat Patterns below |
| V6 Cryptography | Partial — TLS trust chain, not crypto primitives | credproxy's existing `MintSelfSignedCert` (ECDSA P-256) is unchanged, out of this phase's scope; the Python image is a pure *consumer* of the trust chain via `SSL_CERT_FILE`, never generates or handles key material itself |
| V12 Files and Resources | Yes | The `git_read` tool must not allow path traversal outside the mounted worktree — mirror HARN-05's output-path-validation discipline (Go side) conceptually, even though this tool only reads |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|----------------------|
| Command injection via `run_gate_command`'s `subprocess.run(command, shell=True, ...)` | Tampering | The gate `command` string must originate **only** from the orchestrator-authored `EnvelopeIn` (itself controller-authored, never from repo content or model output) — never let the LLM construct the command string it then executes; document this as an explicit invariant in the tool's docstring/comment, not just tribal knowledge |
| Prompt injection via untrusted repo content read by `git_read` | Tampering / Elevation of Privilege | Carried forward from PITFALLS.md's "Integration Gotchas" — repo content the verifier reads (comments, strings) could attempt to instruct the model; Phase 48's trivial scope has no `gate_decision` to manipulate yet, but the pattern should be named now so Phase 49's verdict-schema work inherits the awareness, not discovers it fresh |
| Read-only bypass via a symlink or bind-mount escape inside the worktree | Tampering | K8s `ReadOnly: true` on the VolumeMount is enforced at the kernel/mount-namespace level, not the application level — a symlink inside the worktree pointing outside it would still be read-only from the pod's perspective (the underlying PVC subPath is the trust boundary); no additional code needed, but worth a one-line adversarial-test assertion if time permits |
| Silent misconfiguration surfacing as a confusing runtime error (Pitfall A/B) | Tampering (config drift) / Denial of Service | Prefer the fail-fast pre-flight probe (Pitfall A option b) over letting a TLS misconfiguration surface as an opaque failure deep in the agent loop — an explicit, early, structured `TerminationStub.Reason` is itself a security-relevant clarity improvement (operators can distinguish "credproxy misconfigured" from "model refused" from "gate command failed") |

## Sources

### Primary (HIGH confidence — direct primary-source reads this session)
- PyPI JSON API (`pypi.org/pypi/<package>/json`) — live re-verification of all 9 package versions/`requires_dist`, 2026-07-18.
- Downloaded and read wheel source directly: `langchain_anthropic-1.4.8-py3-none-any.whl` (`chat_models.py`, `_client_utils.py`), `anthropic-0.117.0-py3-none-any.whl` (`_base_client.py`, `_client.py`), `langgraph-1.2.9-py3-none-any.whl` (`_internal/_config.py`, `errors.py`), `langgraph_prebuilt-1.1.0-py3-none-any.whl` (`prebuilt/chat_agent_executor.py`), `langchain-1.3.14-py3-none-any.whl` (`agents/factory.py`, `agents/middleware/types.py`), `langchain_core` (`tools/convert.py`, `utils/utils.py`).
- TIDE source, direct read: `pkg/dispatch/{envelope.go,subagent.go,provider.go,vendor_capabilities.go,doc.go}`, `internal/dispatch/podjob/{jobspec.go,jobspec_test.go,caps.go}`, `internal/harness/envelope_io.go`, `internal/credproxy/{server.go,cert.go}`, `pkg/git/worktree.go`, `internal/controller/push_helpers.go`, `cmd/tide-spike/main.go`, `images/claude-subagent/Dockerfile`, `.dockerignore`, `.planning/config.json`.
- `docker pull python:3.13-slim-bookworm` — live digest resolution, this session.
- `slopcheck install ... --ecosystem pypi` — live legitimacy scan, this session, 7/7 OK.
- `uv pip compile requirements.in --generate-hashes` — live, end-to-end run producing a real 1163-line lockfile, this session; cross-checked one hash (`pydantic-core` manylinux cp313) against PyPI's own file listing.
- Context7 `/websites/langchain_oss_python_langchain`, `/langchain-ai/langgraph` — `ChatAnthropic` construction docs, `create_react_agent` example snippets (corroborated, did not contradict, the direct source reads above).

### Secondary (MEDIUM confidence)
- `github.com/langchain-ai/langchain` issue `#35843` (referenced, not re-read this session — carried from PITFALLS.md, now corroborated from the inside by the direct source read of Pitfall A).
- `github.com/langchain-ai/langgraph` issue `#7313` (cited in-source by `factory.py`'s recursion-limit override comment — not independently read this session, but the source comment itself is a primary artifact).

### Tertiary (LOW confidence)
- None — every claim in this document traces to a primary source read in this session or an already-committed, live-re-verified prior research artifact.

## Metadata

**Confidence breakdown:**
- Standard stack (pins): HIGH — re-verified live against PyPI today, zero drift.
- Python API mechanics (ChatAnthropic construction, create_agent, recursion_limit): HIGH — direct wheel-source reads, not docs pages or training data.
- Architecture (jobspec read-only extension, envelope reuse): HIGH — direct reads of the exact TIDE files that need to change.
- git worktree read-only contradiction (Pitfall D): HIGH — direct read of `pkg/git/worktree.go` plus well-established git internals.
- Security domain: MEDIUM-HIGH — the command-injection and prompt-injection patterns are well-understood classes; this phase's trivial scope limits their current blast radius (no verdict to corrupt yet), so treat as "name it now" awareness rather than a blocking finding.

**Research date:** 2026-07-18
**Valid until:** ~7 days for the Python pins specifically (STACK.md's own "1.x moves ~weekly" caveat still applies going forward even though nothing moved in this window) — re-run the PyPI live-check immediately before Phase 48 actually starts executing if more than a few days elapse. The architectural/API-mechanics findings (Pitfalls A-D) are stable against the exact pinned versions and do not expire unless the pins themselves change.
