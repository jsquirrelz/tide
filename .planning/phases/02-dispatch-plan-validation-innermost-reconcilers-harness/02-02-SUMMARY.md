---
phase: 2
plan: 2
subsystem: tools/analyzers
tags: [lint, analyzer, provider-firewall, SUB-05, pitfall-14, multichecker]
dependency_graph:
  requires: ["02-01"]
  provides: ["SUB-05 lint gate: LLM SDK import firewall for orchestrator-side packages"]
  affects: ["cmd/tide-lint", "Makefile lint aggregate"]
tech_stack:
  added: []
  patterns:
    - "go/analysis Analyzer with path-predicate scope + import-prefix denylist (mirrors dagimports)"
    - "analysistest GOPATH-style fixture layout: testdata/src/{valid,violation,shared-stubs}/"
    - "multichecker.Main for multi-analyzer CI gate binaries"
key_files:
  created:
    - tools/analyzers/providerfirewall/analyzer.go
    - tools/analyzers/providerfirewall/analyzer_test.go
    - tools/analyzers/providerfirewall/testdata/src/valid/pkg/dispatch/safe.go
    - tools/analyzers/providerfirewall/testdata/src/violation/pkg/controller/forbidden.go
    - tools/analyzers/providerfirewall/testdata/src/github.com/anthropics/anthropic-sdk-go/sdk.go
    - tools/analyzers/providerfirewall/testdata/src/valid/internal/subagent/anthropic/allowed.go
  modified:
    - cmd/tide-lint/main.go
    - Makefile
decisions:
  - "Shared testdata stub location: github.com/anthropics/anthropic-sdk-go stub placed at testdata/src/github.com/anthropics/anthropic-sdk-go/ (shared root, not per-fixture) — matches dagimports pattern for k8s.io/apimachinery stub"
  - "inFirewalledScope out-of-scope guard: strings.Contains(path, 'internal/subagent/') catches all harness-adapter subpaths without requiring an exact suffix match"
  - "// want regexp: parentheses in diagnostic message use . (regex wildcard) not \\( literal-paren — avoids regexp parse errors while still matching the diagnostic"
metrics:
  duration: "4min"
  completed: "2026-05-12"
  tasks: 2
  files: 8
---

# Phase 2 Plan 2: Provider Firewall Analyzer (SUB-05) Summary

Provider-firewall lint analyzer for SUB-05/Pitfall 14 prevention: LLM SDK imports rejected in orchestrator-side packages at build time via `go/analysis`, plus `cmd/tide-lint` multichecker flip realizing the Phase 1 commitment.

## What Was Built

### Scope Predicate (firewalled boundaries)

The `inFirewalledScope` predicate in `tools/analyzers/providerfirewall/analyzer.go` accepts a package import path and returns true if it matches any of these six scope fragments:

| Fragment (contains) | Bare form (HasSuffix) |
|--------------------|-----------------------|
| `/pkg/controller/` | `pkg/controller` |
| `/pkg/dispatch/`   | `pkg/dispatch`   |
| `/pkg/dag/`        | `pkg/dag`        |
| `/internal/controller/` | `internal/controller` |
| `/internal/webhook/`    | `internal/webhook`    |
| `/internal/dispatch/`   | `internal/dispatch`   |

Out-of-scope by explicit guard: any path containing `internal/subagent/` (Phase 3 harness-adapter site) or `cmd/credproxy` (generic reverse proxy). These are checked before the scope predicate is applied.

### forbiddenPrefixes denylist (LLM SDK roots)

```go
var forbiddenPrefixes = []string{
    "github.com/anthropics/",
    "github.com/openai/",
    "github.com/sashabaranov/go-openai",
    "github.com/google/generative-ai-go",
}
```

Currently NOT covered (future tightening): `github.com/Azure/azure-sdk-for-go` OpenAI extension, `cloud.google.com/go/vertexai`, any future Mistral/Cohere/Llama cloud SDKs. These can be added to `forbiddenPrefixes` with a corresponding testdata fixture pair — the How-to-extend note lives in the package doc-comment.

### cmd/tide-lint flip (before/after)

**Phase 1 (singlechecker):**
```go
import "golang.org/x/tools/go/analysis/singlechecker"
import "github.com/jsquirrelz/tide/tools/analyzers/crosspool"
func main() { singlechecker.Main(crosspool.Analyzer) }
```

**Phase 2 (multichecker):**
```go
import "golang.org/x/tools/go/analysis/multichecker"
import "github.com/jsquirrelz/tide/tools/analyzers/crosspool"
import "github.com/jsquirrelz/tide/tools/analyzers/providerfirewall"
func main() { multichecker.Main(crosspool.Analyzer, providerfirewall.Analyzer) }
```

Phase 2 downstream plans (04-11) should treat `make verify-import-firewall` and `make tide-lint` as equivalent CI gates covering both POOL-03 (crosspool) and SUB-05 (providerfirewall) in a single invocation.

### Direct vs. transitive imports

The analyzer fires on **direct imports only** — it inspects `f.Imports` in the AST of files within the firewalled package. Transitive leakage (a helper outside the firewall boundary imports the SDK, and a firewalled package imports that helper) is NOT caught by this analyzer.

This is acceptable for Phase 2 because:
1. All existing helpers (`internal/dispatch`, `internal/pool`, `pkg/dag`) are themselves stdlib-only — verified by `make verify-dag-imports` and `make verify-dispatch-imports` using `go list -deps` for transitive coverage.
2. The `go list -deps` Makefile gates provide the transitive coverage backstop.
3. Downstream plans (04-11) add new packages; if any of those import the SDK directly, this analyzer catches it. Transitive chains through a new helper would need a dedicated `go list -deps` gate or a new `verify-*` Makefile target.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed // want regexp trailing backslash parse error**
- **Found during:** Task 1 GREEN phase first test run
- **Issue:** Original plan specified the // want directive as ``// want \`...\` `` (with backslash before closing backtick). The trailing backslash is invalid regexp syntax — `regexp.MustCompile` fails with "trailing backslash at end of expression".
- **Fix:** Removed the trailing backslash; used `.` (regexp wildcard) for parentheses in `(Pitfall 14: vendor lock-in creep)` as the plan's want-comment spec already indicated (the plan text uses `.` for the parens).
- **Files modified:** `tools/analyzers/providerfirewall/testdata/src/violation/pkg/controller/forbidden.go`
- **Commit:** dc262ee (GREEN phase, same commit as analyzer implementation)

**2. [Rule 2 - Missing dep.go] Shared stub placement**
- The plan listed `violation/pkg/controller/dep.go` as a separate file. The dagimports pattern places the stub at the shared testdata/src root (e.g., `k8s.io/apimachinery/pkg/runtime/runtime.go` is NOT under `violation/`). Following the same convention, the anthropic-sdk-go stub lives at `testdata/src/github.com/anthropics/anthropic-sdk-go/sdk.go` (shared, not per-fixture). Both the violation fixture and the allowed fixture reference the same stub. No `dep.go` needed.

## TDD Gate Compliance

| Gate | Commit | Status |
|------|--------|--------|
| RED (test commit) | bd22532 | test(02-02): add failing tests for providerfirewall analyzer (RED) |
| GREEN (impl commit) | dc262ee | feat(02-02): implement providerfirewall analyzer (GREEN) |
| REFACTOR | — | Not needed; implementation is clean |

## Self-Check

### Files Created
- tools/analyzers/providerfirewall/analyzer.go — FOUND
- tools/analyzers/providerfirewall/analyzer_test.go — FOUND
- tools/analyzers/providerfirewall/testdata/src/valid/pkg/dispatch/safe.go — FOUND
- tools/analyzers/providerfirewall/testdata/src/violation/pkg/controller/forbidden.go — FOUND
- tools/analyzers/providerfirewall/testdata/src/github.com/anthropics/anthropic-sdk-go/sdk.go — FOUND
- tools/analyzers/providerfirewall/testdata/src/valid/internal/subagent/anthropic/allowed.go — FOUND

### Files Modified
- cmd/tide-lint/main.go — FOUND
- Makefile — FOUND

### Commits
- bd22532: test(02-02) RED — FOUND
- dc262ee: feat(02-02) GREEN analyzer — FOUND
- 426e58c: feat(02-02) multichecker flip — FOUND

## Self-Check: PASSED
