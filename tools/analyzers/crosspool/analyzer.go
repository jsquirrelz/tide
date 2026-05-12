// Package crosspool implements a golang.org/x/tools/go/analysis Pass
// that rejects any select statement waiting on both the planner and
// executor pool channels in the same case set.
//
// POOL-03 / Pitfall 6 prevention: the TIDE spec mandates two
// separately-sized parallelism semaphores — plannerPool (sized for
// planning-DAG fan-out, default 16) and executorPool (sized for
// execution-DAG fan-out, default 4). A select that waits on both is
// the canonical "I'm about to unify them" smell, and unification is
// the named Pitfall 6 the requirements traceability table maps to
// Phase 1. Detection happens at scaffold time so the mistake cannot
// bake in alongside the pools themselves.
//
// Detection target: the analyzer walks every *ast.SelectStmt and
// inspects each *ast.CommClause for any identifier whose name
// contains "planner" or "executor" (case-insensitive). If a single
// select contains both, the Pass calls Reportf with a diagnostic
// citing POOL-03 / Pitfall 6 — the v1 detection is intentionally
// identifier-based (not type-based) so the analyzer fires before the
// internal/pool.Pool type even exists. The dynamic pool-pick case
// (e.g. pickPool(spec).Acquire(ctx) where pickPool returns *Pool
// chosen between the two) is OUT OF SCOPE for v1 — that pattern is
// left to PR review and the WorkerPool-type-named smell test.
//
// The Pass is registered via cmd/tide-lint's singlechecker.Main; the
// `make tide-lint` Makefile target is the load-bearing CI gate, and
// .github/workflows/ci.yaml fails the PR on any violation.
package crosspool

import (
	"go/ast"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Analyzer is the registered crosspool Pass. cmd/tide-lint passes this
// to singlechecker.Main; future plans may switch to multichecker.Main
// when additional TIDE-custom analyzers land (for example, an
// import-firewall analyzer for the SUB-05 provider-firewall boundary).
var Analyzer = &analysis.Analyzer{
	Name: "crosspool",
	Doc:  "rejects select statements that wait on both planner and executor pools (POOL-03 / Pitfall 6 prevention)",
	Run:  run,
}

func run(pass *analysis.Pass) (interface{}, error) {
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectStmt)
			if !ok {
				return true
			}
			hasPlanner := false
			hasExecutor := false
			for _, commStmt := range sel.Body.List {
				cc, ok := commStmt.(*ast.CommClause)
				if !ok {
					continue
				}
				if matchPoolIdent(cc.Comm, "planner") {
					hasPlanner = true
				}
				if matchPoolIdent(cc.Comm, "executor") {
					hasExecutor = true
				}
			}
			if hasPlanner && hasExecutor {
				pass.Reportf(sel.Pos(),
					"cross-pool wait: select waits on both planner and executor pools (POOL-03 / Pitfall 6 violation)")
			}
			return true
		})
	}
	return nil, nil
}

// matchPoolIdent reports whether the given comm-clause statement
// references an identifier whose name contains `want`
// (case-insensitive). It walks the entire AST subtree rooted at stmt,
// which handles both *ast.SendStmt (channel sends like
// `plannerPool.sem <- struct{}{}`) and *ast.ExprStmt-wrapped channel
// receives (like `<-executorPool.done`). A nil stmt is the default
// case in a select, which can never match either pool.
func matchPoolIdent(stmt ast.Stmt, want string) bool {
	if stmt == nil {
		return false
	}
	found := false
	ast.Inspect(stmt, func(n ast.Node) bool {
		if found {
			return false
		}
		ident, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		if strings.Contains(strings.ToLower(ident.Name), want) {
			found = true
			return false
		}
		return true
	})
	return found
}
