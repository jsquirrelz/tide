---
phase: 01-foundation-crds-pkg-dag-controller-scaffold
plan: 03
subsystem: infra
tags: [go-analysis, analysistest, singlechecker, ast, go-analyzer, pool-03, pitfall-6, makefile, github-actions, ci]

# Dependency graph
requires:
  - phase: 01-foundation-crds-pkg-dag-controller-scaffold
    provides: "Go module github.com/jsquirrelz/tide at Go 1.26.0; Makefile from kubebuilder init; golang.org/x/tools v0.45.0 already a direct dep from Plan 02"
provides:
  - "tools/analyzers/crosspool — golang.org/x/tools/go/analysis Pass detecting cross-pool select statements"
  - "cmd/tide-lint — singlechecker entrypoint for invoking crosspool over the module"
  - "Makefile `tide-lint` target invoking the analyzer"
  - ".github/workflows/ci.yaml — TIDE-specific CI gate running verify-dag-imports + tide-lint + go vet + bounded unit tests"
  - "analysistest fixture template for identifier-based AST analyzers (mirrors the dagimports template from Plan 02)"
affects: [01-04, 01-05, 01-06, 01-07, 02-*]

# Tech tracking
tech-stack:
  added: []  # golang.org/x/tools v0.45.0 already direct dep from Plan 02
  patterns:
    - "Custom go/analysis Pass: identifier-based AST walking (matchPoolIdent) over *ast.SelectStmt / *ast.CommClause to flag pre-type-system smells without dependency on internal/pool.Pool"
    - "singlechecker.Main for standalone CLI invocation; pattern designed to swap to multichecker.Main when a second analyzer lands (e.g. SUB-05 provider-firewall analyzer in Phase 2+)"
    - "analysistest fixture isolation via per-fixture go.mod under testdata/src/{valid,violation}/ — directly carries forward the Plan 02 dagimports template"
    - "Makefile + ci.yaml split: kubebuilder-default workflows handle broad golangci-lint/envtest/kind; ci.yaml is the TIDE-specific gate layer (DAG-05 + POOL-03 + TEST-01)"

key-files:
  created:
    - "tools/analyzers/crosspool/analyzer.go"
    - "tools/analyzers/crosspool/analyzer_test.go"
    - "tools/analyzers/crosspool/testdata/src/valid/go.mod"
    - "tools/analyzers/crosspool/testdata/src/valid/main.go"
    - "tools/analyzers/crosspool/testdata/src/violation/go.mod"
    - "tools/analyzers/crosspool/testdata/src/violation/main.go"
    - "cmd/tide-lint/main.go"
    - ".github/workflows/ci.yaml"
  modified:
    - "Makefile (appended ##@ Custom Analyzers section with tide-lint target)"
    - ".gitignore (added /tide-lint and /manager root-binary patterns)"

key-decisions:
  - "Identifier-based detection (not type-based): the analyzer matches *ast.Ident nodes whose name contains 'planner' or 'executor' case-insensitive, NOT *ast.SelectorExpr against a *pool.Pool type. Rationale: Plan 01-04 hasn't landed internal/pool yet, and the gate must fire at scaffold time — before the pools exist as a Go type. The identifier-name convention is the load-bearing contract; PR review backstops the rare dynamic-pick case (see 01-RESEARCH.md §POOL-03 Custom Analyzer 'this is harder to detect statically')."
  - "Kept the existing kubebuilder `lint:` Makefile target intact and added ONLY `tide-lint:` as a new target (Rule 3 - blocking issue avoidance). The plan body suggested `lint: tide-lint` composite, but the kubebuilder scaffold already had `lint: golangci-lint`. Clobbering it would break the existing Lint GitHub workflow (.github/workflows/lint.yml). The frontmatter must_haves only mandate `tide-lint:` (plan line 28); composite is left for a future cleanup that consolidates the lint surface."
  - "Created NEW `.github/workflows/ci.yaml` rather than amending the existing lint.yml / test.yml. Rationale: those are kubebuilder-generated and handle the generic Go + envtest surface; ci.yaml is the TIDE-specific gate (DAG-05 + POOL-03 + TEST-01) that the acceptance criteria explicitly check for by filename. Two workflows = two failure surfaces visible in PR status checks; one consolidation is left for Plan 11 per the inline comment in ci.yaml."
  - "TDD discipline applied to Task 1: shipped the failing test + fixtures first (commit fd8f60d, RED), then the analyzer impl (commit 5e5e4c3, GREEN). Each commit was independently verifiable — the RED commit compiles to `undefined: Analyzer`, the GREEN commit makes TestCrosspool pass in 3.5s. This mirrors the Plan 02 pattern and is the established TIDE TDD shape for analyzer work."

patterns-established:
  - "Custom analyzer fixture template: tools/analyzers/<name>/{analyzer.go, analyzer_test.go, testdata/src/valid/{go.mod,*.go}, testdata/src/violation/{go.mod,*.go}}. Per-fixture go.mod is non-negotiable to prevent analysistest from bubbling up to the project go.mod and pulling in real K8s/controller-runtime imports."
  - "Identifier-based smell detection over type-based: when the gate must fire before the target type exists in the module, match on identifier-name conventions (case-insensitive Contains) and document the dynamic-pick case as an explicit out-of-scope (PR-review backstop)."
  - "singlechecker now, multichecker later: cmd/tide-lint ships with singlechecker.Main(crosspool.Analyzer) and an inline comment naming the swap. When the SUB-05 import-firewall analyzer lands in Phase 2+, the call becomes multichecker.Main(crosspool.Analyzer, providerfirewall.Analyzer, ...) — zero call-site code outside main.go changes."
  - "TIDE-specific CI gate layering: .github/workflows/ci.yaml is the TIDE PR gate (DAG-05 + POOL-03 + TEST-01); kubebuilder-default lint.yml / test.yml / test-e2e.yml remain untouched. Each layer is independently reviewable in the PR status checks."

requirements-completed:
  - POOL-03

# Metrics
duration: 4min
completed: 2026-05-12
---

# Phase 1 Plan 03: POOL-03 Custom Go-Analyzer + cmd/tide-lint + CI Gate Summary

**A `golang.org/x/tools/go/analysis` Pass walking `*ast.SelectStmt` AST nodes to reject any select that waits on both planner and executor pool channels in the same case set, wired through `cmd/tide-lint` (singlechecker.Main) and a `make tide-lint` Makefile target gated by a new `.github/workflows/ci.yaml` PR check — Pitfall 6 cannot bake in.**

## Performance

- **Duration:** 4 min
- **Started:** 2026-05-12T20:18:41Z (first commit fd8f60d)
- **Completed:** 2026-05-12T20:22:49Z (last task commit 7f2c453)
- **Tasks:** 2 of 2 (Task 1 split into RED + GREEN per TDD)
- **Files created:** 8
- **Files modified:** 2 (Makefile, .gitignore)

## Accomplishments

- `tools/analyzers/crosspool/analyzer.go` walks `*ast.SelectStmt` nodes; for each `*ast.CommClause` it inspects the comm-clause subtree for `*ast.Ident` whose name contains "planner" or "executor" (case-insensitive). If a single select touches both, `Reportf` cites POOL-03 / Pitfall 6 at the select keyword's position.
- `analysistest` fixture pair: `testdata/src/valid/main.go` (single-pool select — no diagnostic) and `testdata/src/violation/main.go` (cross-pool select with `// want \`cross-pool wait\`` on the select line). Both pass under `go test ./tools/analyzers/crosspool/...` in ~4s.
- `cmd/tide-lint/main.go` ships `singlechecker.Main(crosspool.Analyzer)` for `go run ./cmd/tide-lint ./...` invocation — the standalone-CLI shape, not the `go vet -vettool=…` shape.
- Makefile `##@ Custom Analyzers (POOL-03 / Pitfall 6)` section appended with `tide-lint:` target that simply invokes `go run ./cmd/tide-lint ./...`; the existing kubebuilder `lint:` (golangci-lint) target is preserved intact.
- `.github/workflows/ci.yaml` runs `make verify-dag-imports` (DAG-05 from Plan 02), `make tide-lint` (POOL-03 from this plan), `go vet`, and `go test ./pkg/dag/... ./tools/analyzers/...` with a TEST-01 <30s budget assertion.
- The crosspool analyzer fires on the violation fixture and stays silent on the valid fixture; `make tide-lint` exits 0 against the current Phase 1 codebase (no pools wired yet — pools land in Plan 01-04).
- Identifier-based detection means the gate is live BEFORE `internal/pool.Pool` exists as a type — Plan 04 cannot introduce a unified select without failing CI.

## Task Commits

Each task committed atomically (Task 1 split per TDD discipline):

1. **Task 1 RED: failing analysistest fixtures** — `fd8f60d` (test)
2. **Task 1 GREEN: crosspool analyzer implementation** — `5e5e4c3` (feat)
3. **Task 2: cmd/tide-lint + Makefile target + ci.yaml gate** — `7f2c453` (feat)

**Plan metadata:** _(committed after SUMMARY/STATE/ROADMAP update)_

## Files Created/Modified

### Created

- `tools/analyzers/crosspool/analyzer.go` — `analysis.Analyzer` registration + `run` Pass that walks SelectStmt nodes and `matchPoolIdent` helper performing case-insensitive substring identifier match over the comm-clause subtree
- `tools/analyzers/crosspool/analyzer_test.go` — `TestCrosspool` calling `analysistest.Run` against valid + violation fixtures
- `tools/analyzers/crosspool/testdata/src/valid/go.mod` — `module testdata/valid; go 1.26` isolating the fixture from the project go.mod
- `tools/analyzers/crosspool/testdata/src/valid/main.go` — synthetic Pool type + main() with a select waiting on plannerPool only (no diagnostic expected)
- `tools/analyzers/crosspool/testdata/src/violation/go.mod` — `module testdata/violation; go 1.26`
- `tools/analyzers/crosspool/testdata/src/violation/main.go` — synthetic Pool type + main() with `select { case plannerPool.sem <- ...: case executorPool.sem <- ...: case <-ctx.Done(): }` and a `// want \`cross-pool wait\`` directive on the select line
- `cmd/tide-lint/main.go` — `singlechecker.Main(crosspool.Analyzer)` with package doc explaining the singlechecker/multichecker/unitchecker trichotomy
- `.github/workflows/ci.yaml` — TIDE-specific PR gate: DAG-05 (verify-dag-imports) + POOL-03 (tide-lint) + go vet + TEST-01 budget assertion

### Modified

- `Makefile` — appended `##@ Custom Analyzers (POOL-03 / Pitfall 6)` section with `.PHONY: tide-lint` + the target body `go run ./cmd/tide-lint ./...`
- `.gitignore` — added `/tide-lint` and `/manager` patterns to catch accidental `go build ./cmd/<name>` artifacts at the repo root (caught one during dev)

## Public API Locked in This Plan

```go
package crosspool

import "golang.org/x/tools/go/analysis"

// Analyzer is the registered crosspool Pass.
var Analyzer = &analysis.Analyzer{
    Name: "crosspool",
    Doc:  "rejects select statements that wait on both planner and executor pools (POOL-03 / Pitfall 6 prevention)",
    Run:  run,
}
```

The `run` and `matchPoolIdent` functions are package-internal — only `Analyzer` is exported. `cmd/tide-lint/main.go` consumes it via `singlechecker.Main(crosspool.Analyzer)`.

## Diagnostic Format

When the analyzer fires, it emits exactly one diagnostic per violating select statement at the position of the `select` keyword:

```
cross-pool wait: select waits on both planner and executor pools (POOL-03 / Pitfall 6 violation)
```

The substring `cross-pool wait` is what the `// want` directive matches against (Go-regex enclosed in backticks per `analysistest` conventions).

## Makefile Target Body

```makefile
##@ Custom Analyzers (POOL-03 / Pitfall 6)

.PHONY: tide-lint
tide-lint: ## Run TIDE custom analyzers (POOL-03 / Pitfall 6 enforcement).
	go run ./cmd/tide-lint ./...
```

## CI Workflow Snippet

```yaml
- name: Verify pkg/dag imports (DAG-05)
  run: make verify-dag-imports

- name: Run custom analyzers (POOL-03 / Pitfall 6)
  run: make tide-lint

- name: go vet
  run: go vet ./...

- name: Unit tests with TEST-01 <30s budget (pkg/dag + analyzers)
  run: |
    set -e
    START=$(date +%s)
    go test -timeout 30s ./pkg/dag/... ./tools/analyzers/...
    END=$(date +%s)
    DUR=$((END - START))
    echo "Test duration: ${DUR}s"
    if [ "$DUR" -gt 30 ]; then
      echo "TEST-01 violation: test suite exceeded 30s budget"
      exit 1
    fi
```

## Decisions Made

- **Identifier-based detection (not type-based).** The analyzer matches `*ast.Ident` whose name contains "planner" / "executor" (case-insensitive substring), NOT `*ast.SelectorExpr` against a `*pool.Pool` type. Rationale: Plan 01-04 hasn't landed `internal/pool` yet; the gate must fire at scaffold time. The identifier-name convention is the contract; PR review backstops the dynamic-pick case (`pickPool(spec).Acquire(ctx)`) that the analyzer cannot see statically, as explicitly documented in 01-RESEARCH.md §POOL-03 Custom Analyzer.
- **Preserved the existing kubebuilder `lint:` target.** The plan body suggested `lint: tide-lint` composite, but the scaffold already had `lint: golangci-lint` wired to `.github/workflows/lint.yml`. Clobbering would break the lint workflow. Only `tide-lint:` was added; the frontmatter must-haves (line 28) only require the `tide-lint:` target. A future consolidation pass can fold `tide-lint` into a meta `lint:` rule without re-doing this plan.
- **Created NEW `.github/workflows/ci.yaml`** rather than amending lint.yml / test.yml. Acceptance criteria check for the file by name (`.github/workflows/ci.yaml`); the kubebuilder workflows are conceptually distinct (generic Go + envtest, vs TIDE-specific gates). Phase 11 consolidates them per the inline comment.
- **TDD discipline applied to Task 1.** Shipped the failing test + fixtures first (`fd8f60d`, RED, compiles to "undefined: Analyzer"), then the analyzer impl (`5e5e4c3`, GREEN, TestCrosspool passes in 3.5s). Mirrors the Plan 02 pattern.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Plan body's `lint: tide-lint` composite would have clobbered the existing kubebuilder `lint:` target**

- **Found during:** Task 2 (Makefile editing)
- **Issue:** The plan body's action step proposed appending `lint: tide-lint` + `tide-lint:` to the Makefile, but the kubebuilder-scaffolded Makefile already has `lint: golangci-lint` wired to `.github/workflows/lint.yml`. Literally applying the plan would have produced a duplicate-target error (or worse, silently overwritten the existing target depending on Make version), breaking the existing Lint workflow.
- **Fix:** Added ONLY the `tide-lint:` target under a new `##@ Custom Analyzers (POOL-03 / Pitfall 6)` section. Did NOT touch the existing `lint:` target.
- **Files modified:** `Makefile`
- **Verification:** `make lint` still invokes golangci-lint (the existing path); `make tide-lint` invokes the new analyzer. Both targets coexist without overlap. `grep -q "^tide-lint:" Makefile` (the actual acceptance criterion) passes.
- **Committed in:** `7f2c453` (Task 2 commit)

**2. [Rule 3 - Blocking] Accidental root-level `tide-lint` binary from `go build ./cmd/tide-lint`**

- **Found during:** Task 2 (verifying cmd/tide-lint builds)
- **Issue:** `go build ./cmd/tide-lint` (without `-o`) drops the binary at the repo root as `tide-lint`. The kubebuilder `.gitignore` covers `bin/*` but not root-level binaries, so the artifact appeared as untracked in `git status`.
- **Fix:** Removed the artifact (`rm -f tide-lint`) and added explicit `/tide-lint` + `/manager` patterns to `.gitignore` to prevent future occurrences. Used absolute-rooted patterns (leading `/`) to only ignore at the repo root, not anywhere named "tide-lint" deeper in the tree (e.g., `cmd/tide-lint/` is correctly NOT matched).
- **Files modified:** `.gitignore`
- **Verification:** `git status --short` shows no untracked binary; `cmd/tide-lint/` directory is still tracked.
- **Committed in:** `7f2c453` (Task 2 commit)

### Skipped Plan Step

**Plan body step: `go get golang.org/x/tools/go/analysis@latest` (and analysistest + singlechecker subpackages)**

- **Why skipped:** Plan 02 already promoted `golang.org/x/tools v0.45.0` to a direct dependency in `go.mod` (per the Plan 02 SUMMARY frontmatter: `tech-stack.added: golang.org/x/tools v0.45.0`). All three Go subpackages (`go/analysis`, `go/analysis/analysistest`, `go/analysis/singlechecker`) live inside that module — running `go get` on individual sub-package paths would have produced `not a known dependency` errors (verified). The analyzer compiles, tests pass, and `cmd/tide-lint` builds cleanly without any go.mod / go.sum change.
- **Impact:** Zero. The plan's intent was "ensure analysis deps are available" — they were already available. No go.mod / go.sum churn was needed.

---

**Total deviations:** 2 auto-fixed (2 Rule 3 - Blocking) + 1 documented skip
**Impact on plan:** Both auto-fixes are mechanical scaffolding adjustments — neither changes the analyzer logic, the CI gate, or the acceptance surface. The skipped `go get` step was already-done work from the prior plan. No scope creep.

## Issues Encountered

- **None.** The crosspool analyzer is structurally identical to the dagimports analyzer from Plan 02 (same `analysistest` pattern, same testdata fixture shape, same `// want` directive form). The detection logic is a straightforward `ast.Inspect` of `*ast.SelectStmt` → `*ast.CommClause` → identifier name check, exactly the reference implementation shape from 01-RESEARCH.md §POOL-03 Custom Analyzer.

## User Setup Required

None — this plan introduces no external service dependencies, no API keys, and no runtime infrastructure. The analyzer runs inside `go test` and `go run`; the GitHub Actions workflow uses the standard `actions/setup-go@v5` action with Go 1.26.

## Next Phase Readiness

**Ready for Plan 01-04 (parallelism semaphores + dispatcher seam):**
- The crosspool analyzer is live in CI BEFORE `internal/pool` exists. Plan 04 can introduce `plannerPool` and `executorPool` variables; any select waiting on both fails `make tide-lint` immediately.
- The identifier-name convention (`plannerPool`, `executorPool` — case-insensitive substring) is locked in. Plan 04 must use these names (or any names containing the substrings "planner" and "executor") for the analyzer to fire correctly. Documented in `tools/analyzers/crosspool/analyzer.go`'s package doc.

**Ready for Phase 2+ multi-analyzer expansion:**
- `cmd/tide-lint/main.go`'s `singlechecker.Main(crosspool.Analyzer)` line is the single edit point when a second analyzer lands (e.g., the SUB-05 import-firewall analyzer for the provider-firewall boundary). Flip to `multichecker.Main(crosspool.Analyzer, providerfirewall.Analyzer, ...)`; no other call-site changes.

**Ready for Phase 11 (helmify + RBAC verification):**
- `.github/workflows/ci.yaml` is the existing TIDE-specific gate layer. Phase 11 adds `make verify-no-aggregates`, `make verify-no-rbac-wildcards`, and helmify validation steps to the same workflow — the file's structure is already shaped for incremental appends.

**Concerns / watch-items:**
- The analyzer fires on ANY identifier whose name contains "planner" or "executor" — including things like `plannerCount`, `executorMaxIdle`. If a non-channel variable happens to have such a name AND appears inside a select's comm clause (very unlikely — selects only carry channel sends/receives), it could false-positive. The case has never arisen in practice for `*ast.CommClause` walks; the channel-only invariant of select clauses keeps the false-positive risk near zero. If a future PR hits this, switch to a type-aware check (require `cc.Comm` to actually be a `*ast.SendStmt` or a receive-shaped expression).
- The dynamic pool-pick case (`pickPool(spec).Acquire(ctx)`) is NOT detected — explicitly out of scope per 01-RESEARCH.md. PR review and the Pitfall 6 smell-test (any new `WorkerPool` type or generic-pool helper) is the backstop.

## Self-Check: PASSED

- Three task commits exist:
  - `fd8f60d` Task 1 RED (failing fixtures)
  - `5e5e4c3` Task 1 GREEN (analyzer impl)
  - `7f2c453` Task 2 (cmd/tide-lint + Makefile + ci.yaml)
- All claimed files present and verified:
  - `tools/analyzers/crosspool/{analyzer.go, analyzer_test.go}`
  - `tools/analyzers/crosspool/testdata/src/{valid,violation}/{go.mod, main.go}`
  - `cmd/tide-lint/main.go`
  - `.github/workflows/ci.yaml`
  - `Makefile` (contains `^tide-lint:` and `go run ./cmd/tide-lint`)
  - `.gitignore` (contains `/tide-lint` pattern)
- Verification commands all exit 0:
  - `go build ./...`
  - `go vet ./...`
  - `go test ./pkg/dag/... ./tools/analyzers/... -count=1 -timeout 30s` (pkg/dag in 1.1s; crosspool in 6.2s; dagimports in 7.8s — all well under 30s budget)
  - `make tide-lint` (exits 0 — no violations in current codebase)
  - `make verify-dag-imports` (exits 0 — pkg/dag still clean)
- All acceptance-criteria grep checks pass:
  - `grep -q "var Analyzer"` ✓
  - `grep -q 'Name: "crosspool"'` ✓
  - `grep -q "POOL-03"` ✓
  - `grep -q "// want"` ✓
  - `grep -q "singlechecker.Main"` ✓
  - `grep -q "crosspool.Analyzer"` ✓
  - `grep -q "^tide-lint:"` ✓
  - `grep -q "go run ./cmd/tide-lint"` ✓
  - `grep -q "make tide-lint"` (in ci.yaml) ✓
  - `grep -q "make verify-dag-imports"` (in ci.yaml) ✓
  - `grep -q "go-version: '1.26'"` ✓
  - `grep -q "TEST-01"` ✓
- No `tide.io` references introduced (`grep -rn 'tide\.io'` returns empty across cmd/, tools/, pkg/, .github/, Makefile)

---
*Phase: 01-foundation-crds-pkg-dag-controller-scaffold*
*Completed: 2026-05-12*
