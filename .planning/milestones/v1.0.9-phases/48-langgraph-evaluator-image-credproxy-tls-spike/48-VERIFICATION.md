---
phase: 48-langgraph-evaluator-image-credproxy-tls-spike
verified: 2026-07-18T19:28:53Z
status: passed
score: 6/6 must-haves verified
overrides_applied: 0
re_verification:
  previous_status: none
  note: initial verification
---

# Phase 48: LangGraph Evaluator Image + Credproxy-TLS Spike Verification Report

**Phase Goal:** A minimal read-only Python/LangGraph evaluator image runs behind the unchanged `pkg/dispatch.Subagent` + envelope seam, and its credproxy TLS trust path is proven live — de-risking the runtime's trust seam before any evaluation/verdict logic is built on top of it.
**Verified:** 2026-07-18T19:28:53Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
| --- | ------- | ---------- | -------------- |
| 1 | Minimal read-only Python/LangGraph container conforms to the unchanged `pkg/dispatch.Subagent` + envelope seam: `create_agent` (not `create_react_agent`), exactly two tools (`git_read` read-only allowlist + `run_gate_command`), no checkpointer, re-implements envelope JSON independently (no Go import), strict `apiVersion`/`kind` validation | ✓ VERIFIED | `agent.py:13` imports `from langchain.agents import create_agent`; zero `create_react_agent` matches in `verifier/`; `TOOLS = [git_read, run_gate_command]`; `test_build_agent_has_no_checkpointer` runtime-asserts `graph.checkpointer is None`; `envelope.py` strict apiVersion/kind equality is the FIRST check (`read_envelope_in` raises `EnvelopeError` on skew/malformed); 34/34 pytest green |
| 2 | Live pass/fail spike proves `SSL_CERT_FILE` alone trusts credproxy's CA through the real `ChatAnthropic` construction path | ✓ VERIFIED | `48-TLS-SPIKE-VERDICT.md` frontmatter `verdict: PASS` (not PENDING) with verbatim evidence `TLS-SPIKE: PASS`; operator-run live 2026-07-18; durable key precondition `~/.tide/anthropic.key` confirmed present; spike harness `spike/tls_spike.py` is plain `ChatAnthropic` (zero client-injection kwargs), fail-closed, never echoes secrets |
| 3 | Adversarial `git commit`/push against a fixture worktree fails at the mount/credential layer (ReadOnly mount + omitted creds), not prompt refusal | ✓ VERIFIED | **Ran live** `make test-verifier-readonly` → 3/3 PASS: commit → `Read-only file system` (exit 128, mount layer); push → `could not read Username ... terminal prompts disabled` (exit 128, credential layer); writes to `/workspace` + `/` → EROFS both. Plus D-08 `ReadOnly bool` on `jobspec.go:205` + 5 `TestBuildJobSpec_Verifier_*` static assertions green (incl. `_NoGitCredsInAnyContainer` for both ReadOnly true/false) |
| 4 | Every Python dependency patch-exact pinned + a CI gate rejects unpinned/range specifiers | ✓ VERIFIED | 7 runtime pins (`requirements.in`) + 1 dev pin, all `==`; `requirements.txt` hash-locked (1023 `--hash=sha256:`); Dockerfile `pip install --require-hashes`; `make verify-langgraph-pins` wired into `ci.yaml:90`; gate exits 0 on real files, nonzero on `>=` and bare/unpinned fixtures |
| 5 | FIXED contracts preserved — `pkg/dispatch/` + `charts/tide/values.yaml` untouched | ✓ VERIFIED | `git diff df9d646..HEAD -- pkg/dispatch/ charts/tide/values.yaml` = 0 lines |
| 6 | Scope fence held — no `gate_decision` schema, no `VerifyContext` field, no `TaskReconciler` verifier dispatch, no `SelfInstruments` "langgraph" registration | ✓ VERIFIED | No `gate_decision` in phase code files; no `VerifyContext` in `pkg/dispatch/`; no reconciler sets `BuildOptions{ReadOnly:true}` and no `langgraph` in `internal/controller/` (non-test); no `langgraph` in `vendor_capabilities.go` (all deferred to Phases 49/51 as designed) |

**Score:** 6/6 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | ----------- | ------ | ------- |
| `cmd/tide-langgraph-verifier/verifier/envelope.py` | Envelope JSON re-implementation + strict validation | ✓ VERIFIED | 169 lines; `API_VERSION`/`KIND_IN`/`KIND_OUT` constants; strict-first validation; ≤4KB stub cap; no Go import |
| `cmd/tide-langgraph-verifier/verifier/tools.py` | git_read (read-only allowlist) + run_gate_command | ✓ VERIFIED | 2 `@tool`s only; 8-subcommand read-only allowlist; `-C`/`--git-dir`/`--work-tree` option-injection guard; Pitfall-D `worktree add` prohibition documented; timeouts |
| `cmd/tide-langgraph-verifier/verifier/agent.py` | create_agent wiring, explicit recursion_limit, no checkpointer | ✓ VERIFIED | `create_agent` import; `RECURSION_LIMIT = 10` passed at invoke; no checkpointer wired |
| `cmd/tide-langgraph-verifier/verifier/__main__.py` | Seam-conformant entrypoint, plain ChatAnthropic, fail-closed | ✓ VERIFIED | strict-validate → vendor sentinel `"anthropic"` → plain `ChatAnthropic` (D-07 REVISED) → trivial EnvelopeOut + stub; any exception → nonzero structured stub |
| `internal/dispatch/podjob/jobspec.go` | `ReadOnly bool` field + boolean-gated branch (D-08) | ✓ VERIFIED | field at :205; `/workspace` mount `ReadOnly: opts.ReadOnly`; `verifier-scratch` emptyDir + `/scratch` RW; `ReadOnlyRootFilesystem: new(opts.ReadOnly)`; Phase-51 envelopes-subPath forward-note on the field |
| `internal/dispatch/podjob/jobspec_readonly_test.go` | TestBuildJobSpec_Verifier_* static assertions | ✓ VERIFIED | 5 test funcs incl. credential-absence for both ReadOnly states + default non-regression; all green |
| `cmd/tide-langgraph-verifier/Dockerfile` | digest-pinned base + --require-hashes | ✓ VERIFIED | `python:3.13-slim-bookworm@sha256:...`; `pip install --require-hashes`; runtime-source-only COPY (no tests/dev); USER 1000; ENV SSL_CERT_FILE |
| `hack/scripts/test-verifier-readonly.sh` | D-09b adversarial behavioral test | ✓ VERIFIED | 3 probes via `docker run --read-only` + `:ro` + `--entrypoint` override; ran live, all PASS |
| `cmd/tide-langgraph-verifier/spike/tls_spike.py` | D-06 spike, plain ChatAnthropic, binary exit | ✓ VERIFIED | `max_tokens=1`, plain construction, fail-closed on missing token, structured verdict lines, never logs secrets |
| `hack/minttoken/main.go` | throwaway HMAC token minting | ✓ VERIFIED | wraps `internal/credproxy.Sign`; `go build ./hack/minttoken/` OK |
| `cmd/tide-langgraph-verifier/requirements.in`/`.txt` | patch-exact + hash-locked | ✓ VERIFIED | 7 `==` pins; 1023 wheel hashes; zero OTel/openinference/deepagents |
| `48-TLS-SPIKE-VERDICT.md` | recorded PASS verdict | ✓ VERIFIED | `verdict: PASS` + evidence + implication; Phase 49 unblocked |

### Key Link Verification

| From | To | Via | Status | Details |
| ---- | --- | --- | ------ | ------- |
| `ci.yaml` | `make verify-langgraph-pins` | CI step | ✓ WIRED | `ci.yaml:90 run: make verify-langgraph-pins` |
| `ci.yaml` | `make test-langgraph-verifier` / `test-verifier-readonly` | langgraph-verifier job | ✓ WIRED | `ci.yaml:253`/`:259` |
| `release.yaml` | `cmd/tide-langgraph-verifier/Dockerfile` | build-images matrix | ✓ WIRED | `release.yaml:321-322` `component: tide-langgraph-verifier` |
| `Makefile docker-buildx-snapshot` | verifier image | buildx line | ✓ WIRED | `Makefile:389` buildx `-f cmd/tide-langgraph-verifier/Dockerfile` |
| `__main__.py` | `envelope.read_envelope_in` | strict-validate first | ✓ WIRED | entrypoint calls `read_envelope_in` before agent construction |
| `agent.py` | `langchain.agents.create_agent` | non-deprecated factory | ✓ WIRED | import present, `create_react_agent` absent |
| `.dockerignore` | `cmd/tide-langgraph-verifier/**` | deny-by-default re-include | ✓ WIRED | `.dockerignore:50 !cmd/tide-langgraph-verifier/**` |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| Python suite green offline | `.venv/bin/python -m pytest verifier/tests/ -q` | 34 passed | ✓ PASS |
| Verifier jobspec assertions green | `go test ./internal/dispatch/podjob/ -run TestBuildJobSpec_Verifier -count=1` | 5/5 PASS | ✓ PASS |
| Adversarial read-only proof (live docker) | `make test-verifier-readonly` | 3/3 PASS (EROFS commit, credential-fail push, EROFS writes) | ✓ PASS |
| Pin gate rejects range/unpinned | `make verify-langgraph-pins PINS_GLOB=<'>=' / bare>` | nonzero exit | ✓ PASS |
| Pin gate passes real files | `make verify-langgraph-pins` | exit 0 | ✓ PASS |
| Built image has no OTel pkgs | `docker run --entrypoint pip <img> list \| grep -ciE 'openinference\|opentelemetry'` | 0 | ✓ PASS |
| Built image imports resolve | `docker run --entrypoint python <img> -c "import verifier..."` | import OK, exit 0 | ✓ PASS |
| Built image runs as uid 1000 + SSL_CERT_FILE set | `docker inspect` | User=1000, SSL_CERT_FILE=/etc/tide/proxy/ca.crt | ✓ PASS |
| minttoken compiles | `go build ./hack/minttoken/` | OK | ✓ PASS |

### Probe Execution

| Probe | Command | Result | Status |
| ----- | ------- | ------ | ------ |
| Read-only adversarial (D-09b) | `bash hack/scripts/test-verifier-readonly.sh` (via `make test-verifier-readonly`) | exit 0, ALL PASSED | PASS |

_(The EVAL-02 live TLS spike `make spike-langgraph-tls` is a manual, paid, operator-only run by design — not re-executed by the verifier. Its recorded verdict artifact reads PASS; the durable-key precondition was corroborated present.)_

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| ----------- | ---------- | ----------- | ------ | -------- |
| EVAL-01 | 48-01/02/03/04 | Read-only Python/LangGraph image behind unchanged seam; structural read-only; adversarial fixture test | ✓ SATISFIED | Truths 1 + 3; jobspec ReadOnly variant + static asserts + live behavioral proof; REQUIREMENTS.md marks `[x]` Complete |
| EVAL-02 | 48-01/04/05 | Live TLS spike (SSL_CERT_FILE alone) + patch-exact pins + CI range-rejecting gate | ✓ SATISFIED | Truths 2 + 4; verdict PASS + pin gate + `--require-hashes`; REQUIREMENTS.md marks `[x]` Complete |

No orphaned requirements: REQUIREMENTS.md maps only EVAL-01, EVAL-02 to Phase 48; both are claimed across the plans' `requirements:` frontmatter.

### Data-Flow Trace (Level 4)

Not applicable in the classic sense — Phase 48 ships a dispatch-runtime container, not a data-rendering UI. The one live data path (envelope `in.json` → `ChatAnthropic` call → `out.json`) is split by design: the envelope read/validate/write flow is exercised offline (`test_agent.py` happy-path writes real `out.json` with `exitCode 0`, no `git`/`childCRDs` keys), and the single genuinely-live seam (ChatAnthropic through credproxy TLS) is proven by the operator-run spike (verdict PASS). No hollow/disconnected data source found.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |
| — | — | none | — | No `TBD`/`FIXME`/`XXX` debt markers and no `TODO`/`HACK`/`PLACEHOLDER`/"not implemented" markers in any phase-modified source file |

### Human Verification Required

None open. The sole live/external item — the EVAL-02 credproxy-TLS spike (`make spike-langgraph-tls`) — was a `checkpoint:human-verify` task that the operator **completed** and recorded (`48-TLS-SPIKE-VERDICT.md`, `verdict: PASS`, 2026-07-18). Re-running it would spend real API cents and re-confirm an already-recorded human verification; it is intentionally not re-executed by the verifier.

### Gaps Summary

No gaps. All four ROADMAP success criteria are met by the actual codebase, both guardrails (FIXED-contract preservation and the Phase-49/51 scope fence) hold, and both requirements (EVAL-01, EVAL-02) are satisfied. Verification went beyond SUMMARY claims: the read-only adversarial contract was proven by a **live** `docker run --read-only` execution (EROFS on commit/writes, credential failure on push), the built image was inspected directly (zero OTel packages, imports resolve, uid 1000, SSL_CERT_FILE set), the pin gate was exercised on both real and violating inputs, and the TLS-spike verdict was confirmed `PASS` with its live-run precondition corroborated.

**Scope note (not a gap):** Success criterion 1's phrase "dispatches through" is realized in this phase as *conforms to and is structurally dispatchable through* the unchanged seam — the image re-implements + strict-validates the envelope JSON and the `ReadOnly` jobspec variant is unit-proven, but no reconciler dispatches it yet. This is the explicit designed boundary (CONTEXT D-08; TaskReconciler dispatch is Phase 51), reinforced by the verified scope fence.

---

_Verified: 2026-07-18T19:28:53Z_
_Verifier: Claude (gsd-verifier)_
