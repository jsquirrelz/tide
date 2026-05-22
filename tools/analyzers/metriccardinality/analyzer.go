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

// Analyzer rejects the literal string "task" appearing in the label slice
// argument of any prometheus.New*Vec call. The reported diagnostic is
// positioned at the offending string literal so analysistest `// want`
// directives sit on the label-slice line, not the call expression.
var Analyzer = &analysis.Analyzer{
	Name: "metriccardinality",
	Doc:  `rejects "task" label literal in prometheus.New*Vec calls (OBS-02 / Pitfall 17 / D-X4)`,
	Run:  run,
}

func run(pass *analysis.Pass) (interface{}, error) {
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
					if unquoted == "task" {
						pass.Reportf(bl.Pos(),
							"metriccardinality: %q label forbidden in prometheus.%s(...) — adds unbounded task-axis cardinality (Pitfall 17 / D-X4)",
							"task", sel.Sel.Name)
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
