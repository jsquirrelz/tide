---
phase: 48
slug: langgraph-evaluator-image-credproxy-tls-spike
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-18
---

# Phase 48 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Derived from `48-RESEARCH.md` §"Validation Architecture".

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework (Go)** | `go test` — plain table-style `func Test...(t *testing.T)` (matches existing `internal/dispatch/podjob/jobspec_test.go`, not Ginkgo) |
| **Framework (Python)** | `pytest` — **not yet present in the repo** (Wave 0 installs; no `pyproject.toml`/`pytest.ini` exists anywhere in the tree) |
| **Config file** | Go: existing. Python: none — Wave 0 adds `cmd/tide-langgraph-verifier/pyproject.toml` |
| **Quick run command (Go)** | `go test ./internal/dispatch/podjob/... -run TestBuildJobSpec` |
| **Quick run command (Python)** | `cd cmd/tide-langgraph-verifier && python -m pytest verifier/tests/ -x` |
| **Full suite command** | `make test` (Go unit tier) + the Python `pytest` invocation (both wired into `make test` or a dedicated `make test-langgraph-verifier`) + `make verify-langgraph-pins` |
| **Estimated runtime** | ~30–60 seconds (Go unit + Python unit; excludes the manual live TLS spike and the Docker behavioral test) |

---

## Sampling Rate

- **After every task commit:** Run the relevant `pytest`/`go test` slice for the file(s) touched
- **After every plan wave:** `python -m pytest verifier/tests/` (full Python suite) + `go test ./internal/dispatch/podjob/...` + `make verify-langgraph-pins`
- **Before `/gsd:verify-work`:** Full suite green, PLUS the adversarial behavioral read-only test (D-09b), PLUS — separately, manually, once — the live TLS spike (D-06), whose PASS/FAIL verdict is recorded as a decision artifact before Phase 49 proceeds
- **Max feedback latency:** ~60 seconds (automated tiers); the live TLS spike is manual/out-of-band

---

## Per-Task Verification Map

> Task IDs assigned at planning (step 8). Rows are requirement-level until plans land.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| TBD | TBD | 0 | — | — | pytest framework config present | infra | `python -m pytest --version` in image | ❌ W0 | ⬜ pending |
| TBD | TBD | — | EVAL-01 | T-48 V5 | strict `apiVersion`/`kind` mismatch → reject (never process) | unit (Python) | `pytest verifier/tests/test_envelope.py -x` | ❌ W0 | ⬜ pending |
| TBD | TBD | — | EVAL-01 | T-48 V5/V12 | `git_read`/`run_gate_command` read-only against a fixture worktree; no traversal outside it | unit (Python) | `pytest verifier/tests/test_tools.py -x` | ❌ W0 | ⬜ pending |
| TBD | TBD | — | EVAL-01 | T-48 V4 | jobspec `ReadOnly:true` mount + `ReadOnlyRootFilesystem:true` + no git-write secret in `EnvFrom` | unit (Go) | `go test ./internal/dispatch/podjob/... -run TestBuildJobSpec_Verifier` | ❌ W0 | ⬜ pending |
| TBD | TBD | — | EVAL-01 | T-48 V4 | adversarial `git commit`/`push` fails at the filesystem/credential layer (EROFS), not prompt refusal | integration (Docker) | `make test-verifier-readonly` (`docker run --read-only -v <fixture>:/workspace:ro`) | ❌ W0 | ⬜ pending |
| TBD | TBD | — | EVAL-02 | T-48 V6 | live `ChatAnthropic.invoke()` through credproxy CA succeeds (or fails closed) — binary | manual-only | `python cmd/tide-langgraph-verifier/spike/tls_spike.py` | ❌ W0 | ⬜ pending |
| TBD | TBD | — | EVAL-02 | — | `requirements.in` range/unpinned specifier → gate exit 1 | unit (shell) | `make verify-langgraph-pins` (+ negative fixture asserting exit 1 on `langgraph>=1.2`) | ❌ W0 | ⬜ pending |
| TBD | TBD | — | EVAL-02 | T-48 supply-chain | tampered hash → `pip install --require-hashes` fails | integration (build) | part of `docker build` — self-enforcing | N/A | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `cmd/tide-langgraph-verifier/pyproject.toml` (or `pytest.ini`) — no Python test framework config exists anywhere in the repo yet
- [ ] `cmd/tide-langgraph-verifier/verifier/tests/conftest.py` — shared fixtures (fixture-worktree builder, envelope fixtures)
- [ ] New Go test functions in `internal/dispatch/podjob/jobspec_test.go` (or a new `jobspec_readonly_test.go`) for the `ReadOnly` field — no existing test covers it (the field does not exist yet)
- [ ] New `make test-verifier-readonly`-style Makefile target — no existing container-behavioral test infrastructure in this repo
- [ ] `.dockerignore` re-include line for `cmd/tide-langgraph-verifier/**` (deny-by-default `**` currently excludes the new Python tree)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Live credproxy TLS trust via real `ChatAnthropic.invoke()` | EVAL-02 | Costs real Anthropic API cents; needs the durable key at `~/.tide/anthropic.key` (outside the repo); the outcome is genuinely unknown until run live | Stand up `internal/credproxy` with a freshly-minted self-signed CA on `127.0.0.1:8443`; `docker run` the image with `ANTHROPIC_BASE_URL=https://127.0.0.1:8443` + `SSL_CERT_FILE=/etc/tide/proxy/ca.crt`; run one `max_tokens=1` `ChatAnthropic.invoke()`; record binary PASS/FAIL as a decision artifact before Phase 49 |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
