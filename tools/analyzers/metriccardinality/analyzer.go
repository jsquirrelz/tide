/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package metriccardinality

import (
	"go/ast"
	"go/token"
	"strconv"

	"golang.org/x/tools/go/analysis"
)

// vecConstructors is the set of prometheus.New*Vec constructors that take a
// label-slice argument. Only these constructors are inspected; the singular
// forms (NewCounter, NewHistogram, …) have no label slice and are out of
// scope by construction.
var vecConstructors = map[string]struct{}{
	"NewCounterVec":   {},
	"NewHistogramVec": {},
	"NewGaugeVec":     {},
	"NewSummaryVec":   {},
}

// forbiddenLabels is the D-06-locked set of label names that must never
// appear in a prometheus.New*Vec label slice. "task" is the original
// Pitfall-17 entry; the remaining eight are run-ID-shaped names — an
// agent-controlled or per-attempt string reaching any of these as a
// Prometheus label multiplies series cardinality without bound (OBS-02 /
// D-06). Loop-native run detail belongs in traces, not metrics (LOOP-03) —
// metrics stay aggregate with bounded enum labels only (e.g.
// terminal_reason, exit_reason, loop_kind, evaluator_type, risk_tier, which
// this analyzer deliberately does NOT reject).
var forbiddenLabels = map[string]struct{}{
	"task":        {},
	"run_id":      {},
	"loop_run_id": {},
	"run":         {},
	"attempt":     {},
	"attempt_id":  {},
	"trace_id":    {},
	"task_uid":    {},
	"uid":         {},
}

// Analyzer rejects any of the forbiddenLabels label names appearing in the
// label slice argument of any prometheus.New*Vec call. The reported
// diagnostic is positioned at the offending string literal so analysistest
// `// want` directives sit on the label-slice line, not the call expression.
var Analyzer = &analysis.Analyzer{
	Name: "metriccardinality",
	Doc: `rejects run-ID-shaped label literals in prometheus.New*Vec calls: ` +
		`task, run_id, loop_run_id, run, attempt, attempt_id, trace_id, ` +
		`task_uid, uid (OBS-02 / D-06 / Pitfall 17)`,
	Run: run,
}

func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkgIdent, ok := sel.X.(*ast.Ident)
			if !ok || pkgIdent.Name != "prometheus" {
				return true
			}
			if _, ok := vecConstructors[sel.Sel.Name]; !ok {
				return true
			}
			// Look for a composite literal of type []string in the call's args.
			// The label slice is conventionally the second arg, but scan all
			// args so callers that pass through builders are still covered.
			for _, arg := range call.Args {
				lit, ok := arg.(*ast.CompositeLit)
				if !ok {
					continue
				}
				if !isStringSliceType(lit.Type) {
					continue
				}
				for _, elt := range lit.Elts {
					bl, ok := elt.(*ast.BasicLit)
					if !ok || bl.Kind != token.STRING {
						continue
					}
					unquoted, err := strconv.Unquote(bl.Value)
					if err != nil {
						continue
					}
					if _, forbidden := forbiddenLabels[unquoted]; forbidden {
						pass.Reportf(bl.Pos(),
							"metriccardinality: %q label forbidden in prometheus.%s(...) — run-ID-shaped "+
								"labels add unbounded per-attempt cardinality (OBS-02 / D-06 / Pitfall 17)",
							unquoted, sel.Sel.Name)
					}
				}
			}
			return true
		})
	}
	return nil, nil
}

// isStringSliceType returns true if expr describes the Go type []string.
// Both `[]string{...}` and the rarer named-alias forms are handled.
func isStringSliceType(expr ast.Expr) bool {
	arr, ok := expr.(*ast.ArrayType)
	if !ok {
		return false
	}
	if arr.Len != nil {
		return false // fixed-size array, not slice
	}
	elt, ok := arr.Elt.(*ast.Ident)
	if !ok {
		return false
	}
	return elt.Name == "string"
}
