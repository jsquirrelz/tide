---
phase: 48-langgraph-evaluator-image-credproxy-tls-spike
plan: 03
subsystem: infra
tags: [langgraph, langchain, python, envelope, tools, agent, tdd]

# Dependency graph
requires:
  - phase: 48-01
    provides: cmd/tide-langgraph-verifier Python scaffold + pinned deps + pytest infra + fixture_worktree/envelope_in_dict conftest fixtures
  - phase: 48-02
    provides: read-only verifier jobspec variant (unrelated surface this plan touches — no code dependency, same phase)
provides:
  - "verifier/envelope.py — field-for-field Python re-implementation of EnvelopeIn/EnvelopeOut/TerminationStub with strict apiVersion/kind validation first (D-03)"
  - "verifier/tools.py — git_read + run_gate_command, the only two tools this image ships (D-02), allowlist-guarded + option-injection-guarded"
  - "verifier/agent.py — create_agent wiring (not the deprecated create_react_agent) with explicit recursion_limit=10, no checkpointer"
  - "verifier/__main__.py — the seam-conformant entrypoint: strict-validate → vendor-check → run agent → write trivial EnvelopeOut + TerminationStub, fail-closed throughout"
affects: [phase-48-05-credproxy-tls-spike, phase-51-task-loop-dispatch]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "@tool-decorated function parameter names must avoid the literal name 'args' — langchain_core's schema builder silently mis-derives the pydantic arg-schema for a parameter named exactly 'args' (produces a bogus 'v__args: list' field instead of the declared string), so git_read's parameter is named git_args, not args"
    - "Injectable seam for the entrypoint (build_model/run_agent_fn/in_path/termination_message_path kwargs on main()) lets tests drive the full fail-closed flow offline with a fake chat model and tmp-path envelope dirs, no monkeypatching of internals required"
    - "GenericFakeChatModel needs a bind_tools override (return self) to work with create_agent's tool-calling loop offline — the base class raises NotImplementedError"

key-files:
  created:
    - cmd/tide-langgraph-verifier/verifier/envelope.py
    - cmd/tide-langgraph-verifier/verifier/tools.py
    - cmd/tide-langgraph-verifier/verifier/agent.py
    - cmd/tide-langgraph-verifier/verifier/__main__.py
    - cmd/tide-langgraph-verifier/verifier/tests/test_envelope.py
    - cmd/tide-langgraph-verifier/verifier/tests/test_tools.py
    - cmd/tide-langgraph-verifier/verifier/tests/test_agent.py
  modified: []

key-decisions:
  - "Added EnvelopeMissingError as an EnvelopeError subclass (not in the plan's literal text) so the entrypoint can distinguish a missing in.json (reason 'envelope-missing') from a malformed/skewed one (reason 'envelope-invalid: ...') — both are still hard failures, just with a more specific TerminationStub reason for operators."
  - "git_read's tool parameter is named git_args, not args (RESEARCH Pattern 1's literal example name) — verified live that langchain_core's @tool schema derivation mis-handles a parameter named exactly 'args', producing a broken pydantic schema (a bogus list-typed 'v__args' field) instead of the declared string type. Confirmed via direct interpreter test before writing tools.py; this is a genuine landmine in the research's example code, not a stylistic choice."
  - "agent.py's module docstring paraphrases the deprecated-factory reference instead of naming it literally, to satisfy the acceptance criterion that greps for zero occurrences of the deprecated function's name anywhere in verifier/ (including comments)."
  - "TerminationStub in this phase carries only exitCode+reason (no usage/headSHA/childCount) per the plan's own <interfaces> block — D-01's trivial scope has no usage tally or child CRDs to report yet."

patterns-established:
  - "Fail-closed entrypoint: every failure path (missing envelope, invalid envelope, unsupported vendor, agent exception) funnels through a single _fail() helper that always writes a structured TerminationStub and returns nonzero — no path can exit 0 without a successful agent run."

requirements-completed: [EVAL-01]

# Metrics
duration: 15min
completed: 2026-07-18
---

# Phase 48 Plan 03: LangGraph Verifier Runtime (envelope + tools + agent + entrypoint) Summary

**Implemented the tide-langgraph-verifier's full runtime: a strict-validating envelope wire-shape port, the two allowlisted git-read/gate-command tools, `create_agent` wiring with an explicit `recursion_limit=10`, and a fail-closed entrypoint that emits a trivial `EnvelopeOut` — all 34 tests green fully offline (no network, no real API key).**

## Performance

- **Duration:** 15 min
- **Started:** 2026-07-18T14:26:00-04:00 (approx, context gathering)
- **Completed:** 2026-07-18T14:41:13-04:00
- **Tasks:** 3/3 completed
- **Files modified:** 7 (7 created, 0 modified)

## Accomplishments
- `envelope.py` strictly validates `apiVersion`/`kind` as the FIRST step (mirroring `ValidateAPIVersionKind`), tolerates unknown fields, emits the D-01 trivial `EnvelopeOut` shape (no `git`/`childCRDs` keys) at `0o644`, and enforces the `TerminationStub`'s ≤4096-byte cap via progressive truncation.
- `tools.py` ships exactly two tools — `git_read` (8-subcommand read-only allowlist + `-C`/`--git-dir`/`--work-tree` option-injection rejection, ASVS V12) and `run_gate_command` (orchestrator-authored-only invariant documented in its docstring, ASVS V5) — both pinned to the already-materialized worktree with 30s/60s timeouts, never spawning `subprocess` for a rejected command.
- `agent.py` + `__main__.py` wire `langchain.agents.create_agent` (not the deprecated `langgraph.prebuilt` factory) with an explicit `config={"recursion_limit": 10}` at invoke — verified live that the framework default is 9999, three orders of magnitude larger — and the entrypoint fails closed on every error path (missing envelope, skewed envelope, unsupported vendor, agent exception), always writing a structured `TerminationStub`.
- Full suite verified green via both direct `pytest` and the project's own `make test-langgraph-verifier` CI target (34/34); `make verify-langgraph-pins` unaffected and still green.

## Task Commits

Each task's RED/GREEN pair was committed atomically:

1. **Task 1: envelope.py wire-shape + strict validation**
   - `171ff5a` test(48-03): add failing test_envelope.py (RED)
   - `bf83148` feat(48-03): implement envelope.py (GREEN)
2. **Task 2: tools.py — git_read + run_gate_command**
   - `5716c50` test(48-03): add failing test_tools.py (RED)
   - `f0a22a5` feat(48-03): implement git_read + run_gate_command tools (GREEN)
3. **Task 3: agent.py + __main__.py — create_agent wiring + entrypoint**
   - `600f760` test(48-03): add failing test_agent.py (RED)
   - `5b3d256` feat(48-03): implement create_agent wiring + entrypoint (GREEN)

**Plan metadata:** (this commit, following SUMMARY.md write)

_Note: all three tasks are `tdd="true"`; each produced a genuine RED commit (confirmed via `ImportError` before the implementation file existed) followed by a GREEN commit._

## Files Created/Modified
- `cmd/tide-langgraph-verifier/verifier/envelope.py` — `EnvelopeIn`/`EnvelopeError`/`EnvelopeMissingError`, `read_envelope_in`, `write_envelope_out`, `write_termination_stub`.
- `cmd/tide-langgraph-verifier/verifier/tools.py` — `git_read`, `run_gate_command`, `_worktree_dir()`, the allowlist/forbidden-option constants and the 30s/60s timeouts.
- `cmd/tide-langgraph-verifier/verifier/agent.py` — `build_agent`, `run_agent`, `RECURSION_LIMIT = 10`, `TOOLS`.
- `cmd/tide-langgraph-verifier/verifier/__main__.py` — `main()` (injectable entrypoint), `_build_default_model`, `_fail`, path-resolution helpers.
- `cmd/tide-langgraph-verifier/verifier/tests/test_envelope.py` — 9 tests.
- `cmd/tide-langgraph-verifier/verifier/tests/test_tools.py` — 14 tests (5 mutating-subcommand + 3 option-injection parametrizations included).
- `cmd/tide-langgraph-verifier/verifier/tests/test_agent.py` — 8 tests (agent wiring + full entrypoint offline via a fake tool-calling chat model).

## Decisions Made
- **`EnvelopeMissingError`** added as a distinct `EnvelopeError` subclass so the entrypoint's `TerminationStub.reason` can say `"envelope-missing"` specifically (vs. a generic `"envelope-invalid: ..."`) — a small, non-architectural addition needed to satisfy Task 3's own behavior list, not scope creep.
- **`git_read`'s parameter renamed from `args` to `git_args`** — see Deviations below; this is a genuine bug in the research's literal example, caught before it shipped.
- **`agent.py`'s docstring avoids naming the deprecated factory literally** — satisfies the acceptance criterion's zero-occurrence grep across all of `verifier/`, including comments, not just the import statement.
- Kept `TerminationStub` to exactly `exitCode`+`reason` per the plan's `<interfaces>` block (no `usage`/`headSHA`/`childCount` populated) — D-01's trivial scope has nothing to report on those fields yet.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `git_read`'s parameter cannot be named `args`**
- **Found during:** Task 2 (tools.py), before writing any test — a scratch interpreter check of `langchain_core.tools.tool`'s schema derivation.
- **Issue:** RESEARCH Pattern 1's example code and the plan's action text both name `git_read`'s single parameter `args`. Live-tested this in the pinned `langchain-core==1.4.9`: a `@tool`-decorated function with a parameter named exactly `args` produces a broken pydantic input schema (a bogus `v__args: list` field) instead of the declared `str` type — calling the tool via `.invoke("some string")` raises a `pydantic_core.ValidationError` ("Input should be a valid list"). This is an internal name collision in langchain_core's schema builder, not something the plan's authors could have caught without running the exact pinned wheel.
- **Fix:** Renamed the parameter to `git_args` in `tools.py`. Verified live: `git_read.invoke("log --oneline -1")` and `.invoke({"git_args": "..."})` both work correctly with the renamed parameter.
- **Files modified:** `cmd/tide-langgraph-verifier/verifier/tools.py`
- **Verification:** All `git_read` tests in `test_tools.py` pass via `.invoke(...)`; `git_read.args` schema inspected live and confirmed to show `{"git_args": {"type": "string"}}`.
- **Committed in:** `f0a22a5` (Task 2 GREEN commit)

---

**Total deviations:** 1 auto-fixed (Rule 1 — bug in inherited example code)
**Impact on plan:** Necessary for correctness — the plan's literal `args` parameter name would have shipped a tool that raises a validation error on every real invocation. No scope creep; the fix is a single identifier rename.

## Issues Encountered
None beyond the deviation above. `GenericFakeChatModel` (langchain_core's offline fake chat model) required a small local `bind_tools` override (`return self`) in the test file to work with `create_agent`'s tool-calling loop — this is standard practice for testing LangChain agents offline, not a plan deviation, and lives entirely in `test_agent.py`.

## User Setup Required

None — no external service configuration required. All 34 tests run fully offline (no network, no real API key), as required by the plan.

## Next Phase Readiness

- The verifier runtime (envelope + tools + agent + entrypoint) is complete and fully unit-tested; ready for plan 48-04 (Dockerfile packaging + adversarial read-only behavioral test) to build the actual container image around it.
- Plan 48-05 (the credproxy-TLS live spike) can now exercise `_build_default_model`'s plain `ChatAnthropic` construction path directly — no defensive-factory code exists to interfere with the measurement.
- No blockers. The `EnvelopeMissingError` addition and the `git_args` rename are both small, self-contained, and don't affect any interface downstream plans depend on (the entrypoint's `main()` signature and `envelope.py`'s public read/write functions are exactly what the plan's `<interfaces>` block specified).

---
*Phase: 48-langgraph-evaluator-image-credproxy-tls-spike*
*Completed: 2026-07-18*
