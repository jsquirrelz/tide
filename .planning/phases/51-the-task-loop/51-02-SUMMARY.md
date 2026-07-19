---
phase: 51-the-task-loop
plan: 02
subsystem: verification-loop
tags: [langgraph, verifier, gate-command, deterministic-dominance, vendor-sentinel, pkg-dispatch, pydantic, subprocess]

# Dependency graph
requires:
  - phase: 51-the-task-loop (plan 01)
    provides: VerificationSpec CRD schema (spec.verification.gateCommand/commands), TaskStatus.LoopStatus
  - phase: 48-langgraph-image-credproxy-tls-spike
    provides: cmd/tide-langgraph-verifier image skeleton (__main__.py, tools.py, agent.py), TIDE_GATE_COMMAND-reading run_gate_command tool
  - phase: 49-loop-contract-verdict-envelope-persistence-schema
    provides: pkg/dispatch/verdict.go GateDecision/Finding/ClassifyVerdict, EnvelopeOut.Verdict *GateDecision field, Python verdict.py/envelope.py mirrors
provides:
  - "SelfInstruments(\"langgraph\") = true (pkg/dispatch/vendor_capabilities.go) — the first vendor to self-instrument; every other known/unknown vendor stays false"
  - "cmd/tide-langgraph-verifier image now refuses any Provider.Vendor != \"langgraph\" at startup (SUPPORTED_VENDOR flip)"
  - "Deterministic out-of-band multi-command gate capture: __main__.py._run_commands_out_of_band executes EACH env.verify.commands entry pinned to the worktree, exit codes captured independently of the LLM's own tool narration"
  - "__main__.py._assemble_verdict: a red gate command structurally forces the assembled verdict to REPAIRABLE/BLOCKED regardless of the LLM's own APPROVED text, with a blocker/gate-command Finding carrying exit_code=N; empty/absent commands also fails closed (never APPROVED)"
  - "TIDE_GATE_COMMAND is set from the canonical env.verify.gateCommand before the agent runs, so run_gate_command's advisory narration keeps working"
  - "envelope.write_envelope_out gains a verdict param, writing EnvelopeOut.Verdict's Python mirror"
affects: [51-03, 51-04, 51-05, 51-06, 51-07, 51-08]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Deterministic dominance: an out-of-band subprocess exit code (never LLM-self-reported) is the sole authority forcing a verdict down; a probabilistic judge's APPROVED can never override it"
    - "verify-presence branching: __main__.py only runs the gate-capture/verdict-assembly path when env.verify is not None, preserving the pre-existing D-01 trivial-shell behavior for non-verify dispatches"
    - "Cross-module reuse of a leading-underscore helper within the same Python package (tools._worktree_dir imported into __main__.py) — matches the existing envelope.py-imports-verdict._repo_root precedent"

key-files:
  created: []
  modified:
    - pkg/dispatch/provider.go
    - pkg/dispatch/vendor_capabilities.go
    - pkg/dispatch/vendor_capabilities_test.go
    - cmd/tide-langgraph-verifier/verifier/__main__.py
    - cmd/tide-langgraph-verifier/verifier/envelope.py
    - cmd/tide-langgraph-verifier/verifier/tests/test_verdict.py
    - cmd/tide-langgraph-verifier/verifier/tests/test_agent.py

key-decisions:
  - "The out-of-band gate-capture/verdict-assembly path is gated on env.verify being present (not None) — a non-verify dispatch (env.verify absent) preserves the exact pre-existing D-01 trivial-shell behavior (no verdict key at all), distinct from the fail-closed-BLOCKED case where verify IS present but commands is empty"
  - "A red gate-command finding forces REPAIRABLE (not BLOCKED) unless the LLM itself already said BLOCKED — dominance always pulls the verdict DOWN toward escalation, never silently up, but doesn't gratuitously escalate past what the LLM's own judgment already implied"
  - "_run_commands_out_of_band/tools._worktree_dir reuse (not duplication) — factored per the plan's explicit instruction, matching the existing cross-module private-helper-import precedent (envelope.py imports verdict._repo_root)"

patterns-established:
  - "Verifier entrypoint verdict assembly: LLM text -> verdict.classify_verdict() baseline, then out-of-band command results structurally override toward non-APPROVED — reusable shape for Plan 06/07's controller-side re-check"

requirements-completed: [TASK-04, OBS-03]

# Metrics
duration: 13min
completed: 2026-07-19
---

# Phase 51 Plan 02: LangGraph Vendor Sentinel + Verifier Deterministic Gate Dominance Summary

**Registered `SelfInstruments("langgraph")=true` in Go, flipped the verifier image's vendor refusal to `"langgraph"`, and made the verifier entrypoint execute every resolved pass-criterion command out-of-band so a single red exit code structurally dominates any LLM-reported APPROVED — proven by a live pytest driving the full `entrypoint.main()` flow with a fake judge and a real failing shell command.**

## Performance

- **Duration:** ~13 min (08:20 -> 08:33 local)
- **Tasks:** 2/2 completed
- **Files modified:** 7 (2 Go source, 1 Go test, 2 Python source, 2 Python test)

## Accomplishments
- `pkg/dispatch.SelfInstruments("langgraph")` returns `true` (all other known/unknown vendors stay `false`, fail-closed); `ProviderSpec.Vendor`'s canonical-set doc comment gains `"langgraph"`
- `cmd/tide-langgraph-verifier`'s `__main__.py` `SUPPORTED_VENDOR` flips from `"anthropic"` to `"langgraph"` — the image now refuses every other vendor at startup, including its own prior sentinel
- New `_run_commands_out_of_band` executes EACH `env.verify["commands"]` entry as a real subprocess pinned to the worktree, capturing each exit code independently of anything the LLM's `run_gate_command` tool reports back in its own text
- New `_assemble_verdict` combines the LLM's own fail-closed-parsed verdict with the deterministic command results: ANY non-zero exit forces the verdict to `REPAIRABLE` (or `BLOCKED` if the LLM already said `BLOCKED`) and emits a `blocker`/`gate-command` `Finding` carrying `exit_code=N` — proven live against a fake LLM that unconditionally returns `APPROVED` alongside one genuinely failing (`false`) shell command among several passing ones
- `TIDE_GATE_COMMAND` is set from the canonical `env.verify["gateCommand"]` before the agent runs (proven to actually overwrite a stale pre-existing value, not merely coexist with an absent one)
- Absent/empty `commands` stays fail-closed — never `APPROVED` — mirroring `tools.py`'s existing fail-closed-if-unset discipline
- `envelope.write_envelope_out` gains a `verdict` param, mirroring `EnvelopeOut.Verdict *GateDecision`'s pointer+`omitempty` Go semantics
- Full `cmd/tide-langgraph-verifier` pytest suite: 74/74 green (69 pre-existing + 5 new), via `make test-langgraph-verifier`

## Task Commits

Each task was committed atomically (TDD RED/GREEN split per `tdd="true"`):

1. **Task 1: Register the "langgraph" vendor sentinel (Go)**
   - RED: `4d484cd1` (test) — `TestSelfInstruments_LangGraphTrue` added, confirmed failing against the unmodified default-false switch
   - GREEN: `5e320a1c` (feat) — `case "langgraph": return true` added; `ProviderSpec.Vendor` doc comment updated
2. **Task 2: Verifier entrypoint — vendor flip, multi-command out-of-band execution, deterministic verdict assembly**
   - RED: `ffbb6d4a` (test) — 5 new tests added to `test_verdict.py` driving the full `entrypoint.main()` flow, confirmed failing (vendor still `"anthropic"`, no verdict assembly existed — one failure even hit a real network 401 from `ChatAnthropic`, proving genuine RED, not a vacuous one)
   - GREEN: `72fa3a76` (feat) — `SUPPORTED_VENDOR` flip, `_run_commands_out_of_band`/`_assemble_verdict` added, `write_envelope_out` extended, plus the two pre-existing `test_agent.py` tests the vendor flip broke fixed in the same commit (Rule 1 — directly caused by this task's change)

## Files Created/Modified
- `pkg/dispatch/vendor_capabilities.go` - `SelfInstruments("langgraph")` case, doc comment
- `pkg/dispatch/provider.go` - `ProviderSpec.Vendor` canonical-set doc comment gains `"langgraph"`
- `pkg/dispatch/vendor_capabilities_test.go` - `TestSelfInstruments_LangGraphTrue`
- `cmd/tide-langgraph-verifier/verifier/__main__.py` - vendor flip, `_run_commands_out_of_band`, `_assemble_verdict`, `main()` wiring (TIDE_GATE_COMMAND injection + verdict assembly, gated on `env.verify is not None`)
- `cmd/tide-langgraph-verifier/verifier/envelope.py` - `write_envelope_out` gains `verdict` param; `EnvelopeIn.verify` docstring updated to state Phase 51 is the consumer
- `cmd/tide-langgraph-verifier/verifier/tests/test_verdict.py` - 5 new tests: gate-command dominance, all-green-stays-approved, empty-commands-fail-closed, TIDE_GATE_COMMAND-overwrite proof, wrong-vendor refusal
- `cmd/tide-langgraph-verifier/verifier/tests/test_agent.py` - 2 pre-existing tests updated to pass `provider={"vendor": "langgraph", ...}` (broken by the vendor flip); renamed `test_main_rejects_non_anthropic_vendor` -> `test_main_rejects_wrong_vendor` to match

## Decisions Made
- The gate-capture/verdict-assembly path only runs when `env.verify is not None` — a non-verify dispatch keeps the exact pre-existing D-01 trivial-shell behavior (no `verdict` key serialized at all), which is distinct from the fail-closed-BLOCKED case where `verify` is present but `commands` is empty.
- A red gate-command finding forces `REPAIRABLE` unless the LLM's own text already said `BLOCKED` (in which case `BLOCKED` is preserved) — dominance always pulls the verdict toward escalation, never silently up to `APPROVED`, without gratuitously over-escalating past the LLM's own judgment.
- `tools._worktree_dir()` and `tools.GATE_COMMAND_TIMEOUT_SECONDS` are reused directly (not duplicated) from `tools.py` into `__main__.py`, matching the plan's explicit "factor, don't duplicate" instruction and the existing cross-module private-helper-import precedent (`envelope.py` already imports `verdict._repo_root`).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Two pre-existing `test_agent.py` tests broken by the `SUPPORTED_VENDOR` flip**
- **Found during:** Task 2, after implementing the GREEN vendor flip and running the full `cmd/tide-langgraph-verifier` suite
- **Issue:** `test_main_happy_path_writes_trivial_envelope_out` and `test_main_agent_failure_is_fail_closed` both call `entrypoint.main()` with `envelope_in_dict()`'s default `provider.vendor="anthropic"` and expected success/agent-error outcomes. Flipping `SUPPORTED_VENDOR` to `"langgraph"` made both hit the new vendor-refusal branch instead of exercising the behavior they were written to test — a direct, mechanical consequence of this task's own change, not a pre-existing unrelated failure.
- **Fix:** Both tests now pass an explicit `provider={"vendor": "langgraph", "model": "claude-sonnet-4-6"}` override so they exercise the intended code path again. Also renamed `test_main_rejects_non_anthropic_vendor` -> `test_main_rejects_wrong_vendor` (it already tested generic vendor-mismatch rejection via `"openai"`, which stays correct — only the now-stale name/docstring needed updating) and added `test_main_happy_path...`'s assertion that `"verdict" not in out` (proving the non-verify trivial path stays untouched by the new verdict-assembly logic).
- **Files modified:** `cmd/tide-langgraph-verifier/verifier/tests/test_agent.py`
- **Verification:** Full suite green (74/74) via `make test-langgraph-verifier` after the fix.
- **Committed in:** `72fa3a76` (Task 2 GREEN commit)

**2. [Rule 1 - Bug] `os.environ["TIDE_GATE_COMMAND"]` mutation is a genuine cross-test leak risk — hardened before committing**
- **Found during:** Task 2, self-review of the new test suite before committing GREEN
- **Issue:** `main()` necessarily performs a real, process-wide `os.environ[...] = gate_command` mutation (required so the agent's own subprocess calls see it) — not a `monkeypatch`-scoped one. My first draft of the new tests used `monkeypatch.delenv("TIDE_GATE_COMMAND", raising=False)` to reset state before calling `main()`; pytest's `delenv(raising=False)` is a no-op (registers no restore action) when the key is already absent, so `main()`'s subsequent raw mutation would silently leak the value past the test's teardown into any test running afterward in the same process.
- **Fix:** Switched to `monkeypatch.setenv("TIDE_GATE_COMMAND", "<placeholder>")` before calling `main()` in every test that exercises a non-empty `gateCommand` — `setenv` always registers a restore action (capturing the pre-test value, present or not) regardless of what `main()`'s internal mutation later overwrites it to. The TIDE_GATE_COMMAND-injection test additionally now seeds a deliberately-stale sentinel value and asserts `main()` overwrites it, strengthening the proof from "is present" to "is actually set from the envelope."
- **Files modified:** `cmd/tide-langgraph-verifier/verifier/tests/test_verdict.py`
- **Verification:** Full suite green (74/74); this repo's pytest collection order is alphabetical-by-file with no randomization plugin installed, so the leak was latent (not currently reproducible under `make test-langgraph-verifier`) but was a real correctness gap that would silently reappear the moment test order changes (parallelization, `pytest-randomly`, or a new file collating after `test_verdict.py`).
- **Committed in:** `ffbb6d4a` (Task 2 RED commit, since the hardening was applied before the RED->GREEN transition)

---

**Total deviations:** 2 auto-fixed (both Rule 1 — bugs directly caused by this task's own vendor-flip and env-mutation changes)
**Impact on plan:** Both fixes were necessary for correctness of the plan's own acceptance criteria (`make test-langgraph-verifier` exits 0, genuinely and robustly). No scope creep — neither fix touches files outside the vendor-flip/gate-dominance surface this task owns.

## Issues Encountered

None beyond the two deviations above. `go test ./pkg/dispatch/... -run SelfInstruments -count=1` is a genuine (non-vacuous) invocation this time — `pkg/dispatch` has no shared Ginkgo entry point, so Go's `-run` flag correctly matches the three `TestSelfInstruments_*` top-level functions (confirmed via `-v` output showing all three RUN/PASS lines), unlike `internal/controller`'s `-run <SpecName>` trap documented in 51-01-SUMMARY.md.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- `SelfInstruments("langgraph")` is live for Plan 51-05/06/07's dispatch-site wiring (`skipMessageSpans := pkgdispatch.SelfInstruments(...)` call site per PATTERNS.md).
- `EnvelopeOut.Verdict`/`GateDecision` now has a real Python producer: the verifier image genuinely writes `out.json`'s `verdict` key with structural gate-command dominance, ready for Plan 06's controller-side envelope consumption and Plan 07's controller-side defence-in-depth re-check (`hasDeterministicFailure` per PATTERNS.md item 4).
- The `_run_commands_out_of_band`/`_assemble_verdict` functions currently read `env.verify["commands"]` as whatever the raw envelope dict carries — Plan 06 (populating `env.verify.commands` as the resolved `[gateCommand] ++ commands` ordered union from the locked `VerificationSpec`) is a pure data-population change on the Go dispatch side; no further Python-side plumbing is needed to consume it.
- No blockers. The verifier image's Dockerfile/requirements were untouched this plan (no new Python dependencies).

---
*Phase: 51-the-task-loop*
*Completed: 2026-07-19*
