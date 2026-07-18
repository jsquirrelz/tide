---
phase: 48-langgraph-evaluator-image-credproxy-tls-spike
reviewed: 2026-07-18T19:31:27Z
depth: deep
files_reviewed: 13
files_reviewed_list:
  - cmd/tide-langgraph-verifier/verifier/tools.py
  - cmd/tide-langgraph-verifier/verifier/envelope.py
  - cmd/tide-langgraph-verifier/verifier/agent.py
  - cmd/tide-langgraph-verifier/verifier/__main__.py
  - cmd/tide-langgraph-verifier/spike/tls_spike.py
  - cmd/tide-langgraph-verifier/Dockerfile
  - hack/minttoken/main.go
  - hack/scripts/spike-langgraph-tls.sh
  - hack/scripts/test-verifier-readonly.sh
  - internal/dispatch/podjob/jobspec.go
  - internal/dispatch/podjob/jobspec_readonly_test.go
  - Makefile
  - .github/workflows/ci.yaml
findings:
  critical: 2
  warning: 2
  info: 7
  total: 11
status: issues_found
---

# Phase 48: Code Review Report

**Reviewed:** 2026-07-18T19:31:27Z
**Depth:** deep
**Files Reviewed:** 13
**Status:** issues_found

## Summary

Phase 48 ships a pure-Python LangGraph verifier runtime (envelope re-implementation, a two-tool agent), a read-only jobspec variant, and a retained credproxy-TLS spike harness. The `ReadOnly` jobspec branch is correct and well-tested (all three flips — ReadOnly mount, scratch emptyDir, ReadOnlyRootFilesystem — fire together; git-write creds are structurally absent and regression-pinned). The envelope validator fails closed on the common malformed cases (missing file, bad JSON, non-object root, skewed apiVersion/kind). Secret hygiene in the spike/mint helpers is deliberate and mostly sound (token presence reported, never the value; the spike never prints `str(exc)`).

The two headline focus questions both resolve **negative**:

1. **`run_gate_command`'s "orchestrator-authored, never model output" invariant is documented but NOT structurally enforced.** It is registered as an LLM-callable tool, so by construction the model supplies its `command` argument — and it runs with `shell=True`. This is model-driven arbitrary command execution, reachable from adversarial repo content via prompt injection.
2. **`git_read`'s allowlist validates only the subcommand name, not its options.** `git diff --no-index <path> <path>` (empirically confirmed below) reads arbitrary container files including `/proc/self/environ` (the signed token), fully escaping the "confined to the Task worktree" guarantee the docstring claims.

Both are contained by the sandbox (read-only rootfs, read-only workspace, non-root, dropped caps, no push creds) and nothing dispatches this variant yet (Phase 51 wires it), which bounds live blast radius — but the code as submitted exposes a credential-exfiltration and arbitrary-execution surface with a security justification that is factually incorrect for the tool-call path.

Empirical confirmations run during review:
- `git diff --no-index /tmp/fakeenv /dev/null` dumps `SECRET_TOKEN=abc123` from an out-of-repo file (mechanism for CR-02).
- `test_run_agent_one_tool_call_then_final_answer` (test_agent.py:51) itself drives the model emitting `run_gate_command(command="true")` — direct evidence that the model controls the command string (CR-01).
- `internal/dispatch/podjob` builds clean under Go 1.26; `new(opts.ReadOnly)` is the Go 1.26 value-form `new` builtin — not a defect.

## Critical Issues

### CR-01: `run_gate_command` is model-driven arbitrary shell execution; the "orchestrator-authored only" invariant is unenforced

**Severity:** blocker | **Confidence:** high (technical gap); medium (live impact — sandboxed + not yet dispatched)
**File:** `cmd/tide-langgraph-verifier/verifier/tools.py:100-119`, `cmd/tide-langgraph-verifier/verifier/agent.py:27,33`

**Issue:** `run_gate_command` is placed in `TOOLS` (agent.py:27) and bound into the agent via `create_agent(model, tools=TOOLS, ...)` (agent.py:33). In a ReAct agent, tool arguments are emitted *by the model* — so the `command: str` parameter is model-generated text at runtime, by construction. The docstring and the `# noqa: S602 — command is orchestrator-authored, never model output` suppression assert the opposite:

```python
SECURITY INVARIANT (T-48-05 / ASVS V5): `command` must originate ONLY
from the orchestrator-authored envelope — never from repo content or
model-generated text. The entrypoint (__main__.py) never routes model
output into this parameter.
```

The entrypoint claim is technically true but irrelevant: the injection path is the *tool call*, not `__main__`. The model can emit `run_gate_command(command="printenv ANTHROPIC_API_KEY")` — which returns the signed token (jobspec.go:410-411 places it in `ANTHROPIC_API_KEY`/`ANTHROPIC_AUTH_TOKEN`) into the tool result → model → `out.json`/termination log. With `shell=True`, any shell metacharacter chain runs. Critically, this is reachable from *repo* content too: the untrusted code under review flows into the model context via `git_read`, and can prompt-inject the model to call `run_gate_command` with attacker-chosen commands — directly contradicting "never from repo content." Even the intended happy path ("Run the declared read-only gate command", conftest.py:79) has the model, not the orchestrator, typing the command into the tool call; the boundary rests entirely on the model faithfully relaying + the sandbox, which is not a structural guarantee.

**Fix:** Do not expose a free-form model-callable shell tool. Bind the gate command from the envelope at construction time so the model can only *trigger* it, never author it:

```python
def make_gate_runner(gate_command: str):
    @tool
    def run_gate_command() -> str:
        """Run the orchestrator-declared gate command (no arguments)."""
        result = subprocess.run(
            shlex.split(gate_command), shell=False, cwd=_worktree_dir(),
            capture_output=True, text=True,
            timeout=GATE_COMMAND_TIMEOUT_SECONDS, check=False,
        )
        return f"exit_code={result.returncode}\nstdout:\n{result.stdout}\nstderr:\n{result.stderr}"
    return run_gate_command
```

Read `gate_command` from a new envelope field in `__main__`, pass it into `build_agent`. Drop `shell=True`. This makes the documented invariant real. If a shell is genuinely required for the declared command, keep `shell=True` but still bind the fixed string — the model must not supply it.

---

### CR-02: `git_read` allowlist gates only the subcommand, not its options — `git diff --no-index` reads arbitrary container files (incl. the signed token)

**Severity:** high | **Confidence:** high (`--no-index` file read, confirmed); medium (`--output` write / textconv exec)
**File:** `cmd/tide-langgraph-verifier/verifier/tools.py:22-97`

**Issue:** `git_read` validates `tokens[0]` against `ALLOWED_GIT_SUBCOMMANDS` and rejects `-C`/`--git-dir`/`--work-tree`, but performs no validation of the remaining options. Several allowlisted subcommands accept options that escape the worktree confinement the docstring promises ("reads the worktree ... NEVER ... a different repository/worktree"):

- **Arbitrary file read (confirmed):** `git_read("diff --no-index /proc/self/environ /dev/null")` → `git diff --no-index` compares two paths as plain files *regardless of any repository*. Reviewer confirmed `git diff --no-index /tmp/fakeenv /dev/null` dumps the file's contents (`SECRET_TOKEN=abc123`) into the diff output returned to the model. `/proc/self/environ` leaks the signed token from `ANTHROPIC_API_KEY`. This defeats the entire "read-only, worktree-confined, defense-in-depth" claim (tools.py:59-66) at the tool layer.
- **Arbitrary file write:** `git log`/`git diff`/`git show` honor the diff option `--output=<path>` — e.g. `git_read("log -p --output=/scratch/x")` writes to the one writable emptyDir under ReadOnlyRootFilesystem. Low direct impact (the ReadOnly mount blocks `/workspace`), but it is an unexpected write primitive from a tool named `git_read`.
- **External program execution:** `--ext-diff`/textconv drivers can invoke configured external commands; lower risk here because `.git/config` is TIDE-materialized (not attacker-controlled), but `.gitattributes` is committed content, so this warrants explicit denial rather than reliance on config being clean.

`FORBIDDEN_GIT_OPTIONS` (tools.py:30) is simultaneously over-broad (it rejects legitimate `git diff -C` copy-detection) and under-broad (it misses `--no-index`/`--output`/`--ext-diff`). Note this bypass is a subset of CR-01's capability, but `git_read` advertises an independent hardened guarantee, so it must hold on its own. There is no test covering these bypasses (test_tools.py:52-68 only covers `-C`/`--git-dir`/`--work-tree`).

**Fix:** Move from a subcommand allowlist to a token-level deny/allow that also rejects dangerous options, and run every git invocation with confinement flags forced. Concretely: reject any token starting with `--no-index`, `--output`, `-O`, `--ext-diff`, `--exec`, `--upload-pack`; prepend `-c core.fsmonitor=false -c diff.external=` style hardening; and consider forcing `--no-ext-diff` on `show`/`diff`/`log`. Add negative tests for `diff --no-index`, `log --output=`, and `show --ext-diff`.

## Warnings

### WR-01: non-object `provider` field raises an uncaught `AttributeError`, bypassing the fail-closed-with-stub contract

**Severity:** medium | **Confidence:** high
**File:** `cmd/tide-langgraph-verifier/verifier/envelope.py:97-107`

**Issue:** `read_envelope_in` does `provider = raw.get("provider") or {}` then `provider.get("vendor", "")`. If `provider` is present but not a JSON object (a string, number, or non-empty array — e.g. `{"apiVersion":..., "kind":"TaskEnvelopeIn", "provider":"anthropic"}`), `provider` is truthy and `.get` raises `AttributeError: 'str' object has no attribute 'get'`. This is *not* an `EnvelopeError`, so `__main__.main` (which catches only `EnvelopeMissingError`/`EnvelopeError`, __main__.py:88-91) does not handle it. The exception propagates out of `main()`; `sys.exit(main(...))` never runs, Python prints a traceback and exits 1 — **without writing a structured termination stub or `out.json`**. This violates the module's stated contract ("Fail-closed throughout: ANY failure exits nonzero with a structured stub reason", __main__.py:8) and the harness parity claim ("never partial processing", envelope.py:66-67). The controller reading the Job's empty termination message loses the structured failure reason.

**Fix:** Validate `provider` is a dict (or absent) before `.get`, and coerce the field reads defensively:

```python
provider = raw.get("provider")
if provider is None:
    provider = {}
elif not isinstance(provider, dict):
    raise EnvelopeError(
        f"read envelope {path!s}: 'provider' must be a JSON object, got {type(provider).__name__}"
    )
```

Add a test with a scalar/array `provider` asserting `EnvelopeError` (not `AttributeError`).

---

### WR-02: ReadOnlyRootFilesystem with only `/scratch` writable — Python/langchain runtime writes will fail once the variant is dispatched

**Severity:** medium | **Confidence:** medium
**File:** `internal/dispatch/podjob/jobspec.go:487,516-525`; `cmd/tide-langgraph-verifier/Dockerfile:52-60`

**Issue:** The ReadOnly variant sets `ReadOnlyRootFilesystem: true` and provides exactly one writable path (`/scratch` emptyDir). But `TMPDIR`/`HOME` are not redirected there. httpx, anthropic, langchain, and pip-installed libraries routinely write to `/tmp` and `$HOME/.cache` (token caches, `~/.config`, tmpfiles). Under `ReadOnlyRootFilesystem: true` those become `EROFS`, which can crash the verifier at runtime — a failure that the current tests cannot catch because they exercise the jobspec statically (jobspec_readonly_test.go) and the Python offline (fake model, no real httpx transport). This is latent until Phase 51 actually dispatches `ReadOnly:true`, but it is a real robustness gap baked in now.

**Fix:** When wiring the ReadOnly variant, set `TMPDIR=/scratch` (and `HOME=/scratch` or `XDG_CACHE_HOME=/scratch`) in the subagent env, and/or mount an additional `emptyDir` at `/tmp`. At minimum add a comment at jobspec.go:427-434 flagging that `/scratch` is the sole writable path so Phase 51 redirects tmp/cache there. A live smoke of the built image under `--read-only` running one real `python -m verifier` (not just git/sh probes) would surface this.

## Info

### IN-01: `_fail` interpolates `str(exc)` into the persisted reason / out.json

**Severity:** low | **Confidence:** low
**File:** `cmd/tide-langgraph-verifier/verifier/__main__.py:104,91`

**Issue:** `f"agent-error: {exc}"` and `f"envelope-invalid: {exc}"` embed the raw exception string into the termination stub and `out.json` `reason`. If an underlying library exception ever includes credential material or large request/response bodies, it is persisted. This contrasts with the spike's deliberate discipline of never printing `str(exc)` (tls_spike.py:121-161, which prints only exception *class* names). Anthropic SDK errors generally don't echo the key, so this is low, but the asymmetry with the spike's own posture is worth closing.

**Fix:** Log the full `str(exc)` to stderr; write only a bounded, class-name-based reason to the persisted stub (e.g. `agent-error: {type(exc).__name__}`), mirroring `classify_and_report`.

### IN-02: unbounded subprocess output returned into the tool result

**Severity:** low | **Confidence:** medium
**File:** `cmd/tide-langgraph-verifier/verifier/tools.py:97,119`

**Issue:** `git_read` returns `result.stdout or result.stderr` and `run_gate_command` returns the full stdout+stderr with no size cap. A gate command or `git log` with large output produces a multi-MB tool message → inflated token usage, possible context-window overflow, and (for `run_gate_command`) it flows into `out.json`'s `result` after `_one_line` collapse. `write_termination_stub` caps only the *stub*, not the tool result or `out.json`.

**Fix:** Truncate tool output to a sane cap (e.g. 32 KiB) with a `...(truncated N bytes)` marker before returning.

### IN-03: subprocess `TimeoutExpired` is uncaught in the tools; `shell=True` may orphan grandchildren

**Severity:** low | **Confidence:** medium
**File:** `cmd/tide-langgraph-verifier/verifier/tools.py:89-96,110-118`

**Issue:** On timeout, `subprocess.run(..., timeout=...)` raises `TimeoutExpired` rather than returning a result. `git_read`/`run_gate_command` don't catch it, so a hung command raises through the LangGraph tool call rather than reporting "gate timed out" back to the model. It fails closed (caught as `agent-error` in `__main__`), which is acceptable — but a timed-out gate is arguably a *gate failure the model should see*, not an agent crash. Separately, `subprocess.run` with `shell=True` + `timeout` kills only the shell, not its child process group — grandchildren can be orphaned (backstopped by the Job's `activeDeadlineSeconds`).

**Fix:** Wrap both `subprocess.run` calls in `try/except subprocess.TimeoutExpired` and return a structured `exit_code=124\n...timed out` message. Consider `start_new_session=True` + process-group kill for `run_gate_command` if orphaning matters.

### IN-04: `verify-langgraph-pins` regex is not end-anchored — wildcard pins like `pkg==1.*` pass the "patch-exact" gate

**Severity:** low | **Confidence:** medium
**File:** `Makefile` (`verify-langgraph-pins` target, ~line 892-910)

**Issue:** The gate flags any line not matching `^[A-Za-z0-9_.-]+==[0-9]`. This correctly rejects `>=`/`~=`/`<`, but because it is not anchored at the end, `pkg==1.*` and `pkg==1.2.*` (PEP 440 prefix-match ranges) match `==[0-9]` and pass — despite being *ranges*, contradicting the gate's "patch-exact" claim. pip-compile output wouldn't emit these, but a hand-edit to `requirements.in` would slip through.

**Fix:** Tighten to a full patch-exact pattern, e.g. reject any line containing `*` and require `^[A-Za-z0-9_.-]+==[0-9][0-9A-Za-z.\-]*$` (also disallowing trailing environment markers unless intended).

### IN-05: Python `EnvelopeOut` omits `taskUID` (and usage/artifacts/completedAt) the Go contract declares

**Severity:** low | **Confidence:** high
**File:** `cmd/tide-langgraph-verifier/verifier/envelope.py:112-140` vs `pkg/dispatch/envelope.go:170-237`

**Issue:** The Go `EnvelopeOut` declares `taskUID`, `usage`, `artifacts`, `completedAt` (non-`omitempty`) alongside the emitted fields. The trivial Python writer emits only `apiVersion/kind/exitCode/result` (+`reason`). This is safe for `json.Unmarshal` (missing keys → zero values) and `IsEnvelopeComplete` still returns true, so it's a valid subset for D-01. But `taskUID` omission means the controller cannot correlate a result via `out.json.taskUID` — it must rely on Job labels. The docstring justifies dropping `git`/`childCRDs` but is silent on `taskUID`.

**Fix:** Echo `taskUID` (available from `EnvelopeIn.task_uid`) into `write_envelope_out` now, or add an explicit note deferring correlation-field parity to Phase 51's dispatch wiring.

### IN-06: `FORBIDDEN_GIT_OPTIONS` is both over-broad and under-broad

**Severity:** low | **Confidence:** high
**File:** `cmd/tide-langgraph-verifier/verifier/tools.py:30,83-87`

**Issue:** The forbidden-token check rejects `-C` anywhere in the argument list, which also blocks legitimate subcommand-scoped uses (`git diff -C` = copy detection, `git log -C`), while missing the actually-dangerous confinement-escape options (see CR-02). The current list conflates "global repo-redirect options" with "any token equal to `-C`."

**Fix:** Fold into CR-02's redesign — deny the real escape options and stop pattern-matching bare `-C`, which git only treats as a repo redirect in the *global* position (which the `tokens[0]`-must-be-a-subcommand rule already precludes).

### IN-07: secrets passed via process arguments in the spike/mint tooling

**Severity:** low | **Confidence:** high (mechanism); low (impact — local dev only)
**File:** `hack/minttoken/main.go:45`; `hack/scripts/spike-langgraph-tls.sh:73-78`

**Issue:** `hack/minttoken` accepts `-signing-key=<key>` as a CLI flag (documented usage line 28), which is visible via `ps`/`/proc/<pid>/cmdline` to other users on the host. The spike script also does `docker run ... -e ANTHROPIC_API_KEY="${REAL_KEY}"` (line 77), exposing the *real* Anthropic key on the `docker run` argv while the container starts. The signing key here is a throwaway (low impact); the real key transiting argv is the notable one. Both helpers otherwise handle secrets well (never logged; token-presence-only reporting).

**Fix:** Prefer the env-var path for `minttoken` (it already supports `TIDE_SIGNING_KEY`) and drop/deprecate the `-signing-key` flag, or read it from a file. For the spike, pass the real key via `--env-file` or `-e ANTHROPIC_API_KEY` sourced from the process env (`-e ANTHROPIC_API_KEY` with the value already exported) rather than interpolating it onto the `docker run` command line.

---

_Reviewed: 2026-07-18T19:31:27Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
