---
phase: 48-langgraph-evaluator-image-credproxy-tls-spike
plan: 01
subsystem: infra
tags: [python, uv, pytest, langgraph, langchain, pip-compile, ci-gate, dependency-pinning]

# Dependency graph
requires: []
provides:
  - "cmd/tide-langgraph-verifier/ Python tree (7 patch-exact runtime pins + hash-locked lockfile)"
  - "pytest infrastructure (pyproject.toml + conftest.py fixtures) — the repo's first Python test framework"
  - "fixture_worktree + envelope_in_dict shared pytest fixtures"
  - "make test-langgraph-verifier + make verify-langgraph-pins Makefile targets"
  - "verify-langgraph-pins CI gate wired into ci.yaml"
affects: [48-02, 48-03, 48-04, 49, 51]

# Tech tracking
tech-stack:
  added: ["uv (pip compile --generate-hashes)", "pytest==9.1.1", "langgraph==1.2.9", "langchain==1.3.14", "langchain-anthropic==1.4.8", "langchain-core==1.4.9", "anthropic==0.117.0", "pydantic==2.13.4", "httpx==0.28.1"]
  patterns:
    - "Patch-exact requirements.in + hash-locked requirements.txt (D-10), mirroring the Go import-firewall grep-gate idiom"
    - "PINS_GLOB-overridable Makefile gate variable to support a negative self-test without touching real pin files"
    - ".dockerignore deny-by-default ** with a per-tree re-include line"

key-files:
  created:
    - cmd/tide-langgraph-verifier/requirements.in
    - cmd/tide-langgraph-verifier/requirements.txt
    - cmd/tide-langgraph-verifier/requirements-dev.in
    - cmd/tide-langgraph-verifier/requirements-dev.txt
    - cmd/tide-langgraph-verifier/pyproject.toml
    - cmd/tide-langgraph-verifier/verifier/__init__.py
    - cmd/tide-langgraph-verifier/verifier/tests/__init__.py
    - cmd/tide-langgraph-verifier/verifier/tests/conftest.py
    - cmd/tide-langgraph-verifier/verifier/tests/test_sanity.py
  modified:
    - .dockerignore
    - .gitignore
    - Makefile
    - .github/workflows/ci.yaml

key-decisions:
  - "pytest==9.1.1 slopchecked [OK] before being added as the sole dev pin"
  - "verify-langgraph-pins loops per-file (not a single multi-file grep) to avoid grep's 'filename:' prefix breaking the comment/blank-line filter"
  - "Pitfall E comment in requirements.in deliberately avoids spelling out the literal banned package-name substrings, to avoid self-matching its own acceptance-criteria grep"

patterns-established:
  - "Shared pytest fixtures (fixture_worktree, envelope_in_dict) live in verifier/tests/conftest.py for all Wave 2/3 plans to import"

requirements-completed: [EVAL-01, EVAL-02]

# Metrics
duration: ~6min
completed: 2026-07-18
---

# Phase 48 Plan 01: LangGraph Verifier Scaffolding Summary

**Stood up `cmd/tide-langgraph-verifier/` with 7 patch-exact runtime pins + hash-locked lockfiles, the repo's first pytest infrastructure with shared git-worktree/envelope fixtures, and a CI-gated `make verify-langgraph-pins` pin-discipline check.**

## Performance

- **Duration:** ~6 min
- **Started:** 2026-07-18T14:13Z (approx, first commit in this session's chain)
- **Completed:** 2026-07-18T14:18:42-04:00
- **Tasks:** 2/2 completed
- **Files modified:** 13 (9 created, 4 modified)

## Accomplishments
- `cmd/tide-langgraph-verifier/requirements.in`/`requirements-dev.in` hold exactly the 8 patch-exact pins (7 runtime + 1 dev) D-10 specifies, with zero OTel/openinference/deepagents packages (Pitfall E)
- `requirements.txt`/`requirements-dev.txt` hash-locked via `uv pip compile --generate-hashes --python-platform linux --python-version 3.13` (1023 + 10 sha256 hashes respectively)
- `pyproject.toml` + `verifier/tests/conftest.py` give the repo its first pytest scaffold, with `fixture_worktree` (real git repo, already-committed, mirroring the already-materialized Task worktree per Pitfall D) and `envelope_in_dict` (factory producing a valid `TaskEnvelopeIn` dict using the exact field names/discriminators from `pkg/dispatch/envelope.go`)
- `make test-langgraph-verifier` stands up `.venv` via `uv venv --python 3.13`, installs both lockfiles with `--require-hashes`, and runs pytest (3 passed)
- `make verify-langgraph-pins` greps both `requirements*.in` files for any non-`==` specifier, is `PINS_GLOB`-overridable for the negative self-test, and is wired into `ci.yaml` adjacent to `verify-dispatch-imports`

## Task Commits

1. **Task 1: Scaffold cmd/tide-langgraph-verifier — pins, lockfiles, pytest config, conftest fixtures** - `22872d5` (feat)
2. **Task 2: make verify-langgraph-pins gate + negative self-test + CI wiring** - `178d3c9` (feat)

_Note: no TDD tasks in this plan (`tdd` not set on either task)._

## Files Created/Modified
- `cmd/tide-langgraph-verifier/requirements.in` - 7 patch-exact runtime pins (D-10)
- `cmd/tide-langgraph-verifier/requirements.txt` - hash-locked transitive closure (1023 hashes)
- `cmd/tide-langgraph-verifier/requirements-dev.in` - `pytest==9.1.1`, slopchecked [OK]
- `cmd/tide-langgraph-verifier/requirements-dev.txt` - hash-locked dev closure (10 hashes)
- `cmd/tide-langgraph-verifier/pyproject.toml` - `[tool.pytest.ini_options]` testpaths only
- `cmd/tide-langgraph-verifier/verifier/__init__.py` - package marker + docstring
- `cmd/tide-langgraph-verifier/verifier/tests/conftest.py` - `fixture_worktree` + `envelope_in_dict` shared fixtures
- `cmd/tide-langgraph-verifier/verifier/tests/test_sanity.py` - 3 sanity tests proving both fixtures build
- `.dockerignore` - `!cmd/tide-langgraph-verifier/**` re-include
- `.gitignore` - `.venv/` + `__pycache__/` entries for the new Python tree
- `Makefile` - `test-langgraph-verifier` + `verify-langgraph-pins` targets
- `.github/workflows/ci.yaml` - `verify-langgraph-pins` step + header comment entry

## Decisions Made
- **pytest version:** resolved live against PyPI at execution time (9.1.1, newer than any version cited in RESEARCH.md, consistent with its "re-verify at execution time" instruction); slopcheck ran `[OK]` before adding it to `requirements-dev.in`.
- **verify-langgraph-pins implementation shape:** looped per-file rather than passing multiple files to one `grep` invocation — multi-file grep prefixes every line with `filename:`, which would have broken the `^\s*(#|$)` comment/blank-line filter (comment lines no longer start with `#` once prefixed). Looping avoids that trap entirely and keeps the offending-line output attributable to its source file.
- **Pitfall E comment phrasing:** the `requirements.in` header comment explaining why OTel/agentic-framework packages are absent deliberately does not spell out the literal banned substrings (uses "OTel auto-instrumentation" and "agentic file-edit-framework" instead) — spelling them out verbatim would have made the file self-match its own Pitfall-E acceptance-criteria grep (`grep -ciE 'openinference|opentelemetry|deepagents'` would count the comment as a hit).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Added `__pycache__/` exclusion to `.gitignore`**
- **Found during:** Task 1, after the first `make test-langgraph-verifier` run
- **Issue:** Running pytest generated `verifier/__pycache__/` and `verifier/tests/__pycache__/` directories that showed up as untracked files. The plan's `.gitignore` scope only covered the `.venv/` directory, not Python bytecode caches — this is the repo's first Python tree, so no prior `__pycache__` ignore pattern existed.
- **Fix:** Added `cmd/tide-langgraph-verifier/**/__pycache__/` to `.gitignore` and removed the already-generated cache directories before staging.
- **Files modified:** `.gitignore`
- **Verification:** `git status --short` shows no untracked `__pycache__` directories after a fresh `make test-langgraph-verifier` run.
- **Committed in:** `22872d5` (Task 1 commit)

---

**Total deviations:** 1 auto-fixed (1 missing critical — generated-artifact gitignore gap)
**Impact on plan:** Necessary for repo hygiene (first Python tree in an otherwise all-Go repo); no scope creep.

## Issues Encountered
None.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- `verifier/tests/conftest.py`'s `fixture_worktree` and `envelope_in_dict` fixtures are available for Wave 2/3 plans (envelope decode/validate tests, git-read tool tests, adversarial read-only tests) with no further setup.
- `make verify-langgraph-pins` is CI-gated; any future range-specifier edit to `requirements*.in` will fail the build before merge.
- Dockerfile/image-build work (plan 48-04) can now `COPY cmd/tide-langgraph-verifier/requirements.txt` and `pip install --require-hashes -r requirements.txt` directly — the lockfile already includes the linux/manylinux cp313 wheel hashes despite being generated on macOS (confirmed via `--python-platform linux --python-version 3.13`).
- No blockers.

---
*Phase: 48-langgraph-evaluator-image-credproxy-tls-spike*
*Completed: 2026-07-18*

## Self-Check: PASSED

All 9 created files verified present on disk; all 3 commit hashes (`22872d5`, `178d3c9`, `69372d8`) verified present in `git log --oneline --all`.
