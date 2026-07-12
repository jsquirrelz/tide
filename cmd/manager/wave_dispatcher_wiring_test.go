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

// Regression guard for debug #16 (dashboard DAG collapses every task into
// wave 0).
//
// Root cause: main() constructed WaveReconciler WITHOUT a Dispatcher, while
// every other reconciler that gates its Phase-2 body on `r.Dispatcher != nil`
// wires `Dispatcher: dispatcher`. With Dispatcher nil, WaveReconciler.Reconcile
// falls through to the scaffold branch and NEVER runs reconcileObservational,
// so Wave.Status.{Phase,TaskRefs} stay empty and the dashboard plan handler
// maps every task to waveIndex 0.
//
// Why the existing TestReconcilerWiringComplete did NOT catch this: that test
// builds a fresh struct literal with the field set by hand
// (`&controller.WaveReconciler{Dispatcher: dispatcher}`) and asserts it's
// non-nil — tautological. It inspects nothing about what main() actually wires,
// and Wave was never even in its matrix. The same omission could recur on any
// future Dispatcher-gated reconciler.
//
// This guard instead parses main.go's AST and asserts that every reconciler
// constructed in main() includes a `Dispatcher:` field in its composite
// literal. Any future reconciler wired without a Dispatcher (the #16 bug
// class) fails this test. AST-based so it is robust to formatting/comments.
package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// reconcilersRequiringDispatcher are the reconciler type names whose Reconcile
// body gates its Phase-2 (dispatch / observational roll-up) work on
// `r.Dispatcher != nil`. Each MUST be wired with a Dispatcher in main(), or the
// production path silently short-circuits to a scaffold branch.
//
// TaskReconciler is intentionally excluded: it carries its Dispatcher inside a
// nested Deps struct (TaskReconcilerDeps), a different literal shape covered by
// TestReconcilerWiringComplete's Task.Deps.Dispatcher case.
//
// Since plan 41-06, ProjectReconciler/MilestoneReconciler/PhaseReconciler/
// PlanReconciler ALSO carry Dispatcher inside a nested Deps struct
// (PlannerReconcilerDeps, built once as `plannerDeps` and assigned via
// `Deps: plannerDeps`) — see resolveDispatcherPresence below for how this
// test follows that indirection back to the literal that sets Dispatcher.
var reconcilersRequiringDispatcher = []string{
	"ProjectReconciler",
	"MilestoneReconciler",
	"PhaseReconciler",
	"PlanReconciler",
	"WaveReconciler",
}

// TestMainWiresDispatcherOnGatedReconcilers asserts that the WaveReconciler
// (and the other Dispatcher-gated reconcilers) are constructed in main.go with
// a `Dispatcher:` field (directly, or indirectly via a `Deps:` struct field
// whose value resolves to a composite literal setting Dispatcher). This is
// the regression guard for debug #16: before the fix, the WaveReconciler
// composite literal omitted Dispatcher, so this test fails on the buggy tree
// and passes once the field is wired.
func TestMainWiresDispatcherOnGatedReconcilers(t *testing.T) {
	mainPath := mainGoPath(t)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, mainPath, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", mainPath, err)
	}

	// First pass: collect every composite literal assigned to a variable
	// (`:=` or `=`), keyed by variable name — resolves the `Deps: plannerDeps`
	// indirection (plan 41-06) back to the PlannerReconcilerDeps literal that
	// actually carries Dispatcher.
	varLiterals := map[string]*ast.CompositeLit{}
	ast.Inspect(file, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, rhs := range assign.Rhs {
			if i >= len(assign.Lhs) {
				continue
			}
			lit, ok := rhs.(*ast.CompositeLit)
			if !ok {
				continue
			}
			ident, ok := assign.Lhs[i].(*ast.Ident)
			if !ok {
				continue
			}
			varLiterals[ident.Name] = lit
		}
		return true
	})

	// Collect, per reconciler type, whether main() constructs it with a
	// Dispatcher field (directly or via Deps). A type may be constructed
	// once; record presence.
	constructed := map[string]bool{}
	hasDispatcher := map[string]bool{}

	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		typeName, ok := compositeLitTypeName(lit)
		if !ok {
			return true
		}
		if !contains(reconcilersRequiringDispatcher, typeName) {
			return true
		}
		constructed[typeName] = true
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok {
				continue
			}
			switch key.Name {
			case "Dispatcher":
				hasDispatcher[typeName] = true
			case "Deps":
				if depsLiteralHasDispatcher(kv.Value, varLiterals) {
					hasDispatcher[typeName] = true
				}
			}
		}
		return true
	})

	for _, name := range reconcilersRequiringDispatcher {
		if !constructed[name] {
			t.Errorf("%s is not constructed in main.go — the Dispatcher-gated "+
				"reconciler set in this test is stale; update it", name)
			continue
		}
		if !hasDispatcher[name] {
			t.Errorf("%s is constructed in main.go WITHOUT a Dispatcher field. "+
				"Its Reconcile body gates Phase-2 work on r.Dispatcher != nil, so a "+
				"nil Dispatcher silently short-circuits to the scaffold branch "+
				"(debug #16: WaveReconciler omission collapsed the dashboard DAG "+
				"into wave 0). Wire `Dispatcher: dispatcher` in the struct literal.", name)
		}
	}
}

// depsLiteralHasDispatcher reports whether a `Deps:` field's value literal
// sets a Dispatcher field — either inline (`Deps: controller.XReconcilerDeps{
// Dispatcher: dispatcher}`) or indirectly through a variable reference
// (`Deps: plannerDeps`, resolved via varLiterals to the plannerDeps := ...
// composite literal). Plan 41-06 introduced the indirect form for the four
// planner-tier reconcilers.
func depsLiteralHasDispatcher(expr ast.Expr, varLiterals map[string]*ast.CompositeLit) bool {
	var lit *ast.CompositeLit
	switch v := expr.(type) {
	case *ast.CompositeLit:
		lit = v
	case *ast.Ident:
		lit = varLiterals[v.Name]
	}
	if lit == nil {
		return false
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Dispatcher" {
			return true
		}
	}
	return false
}

// compositeLitTypeName returns the type name of a composite literal whose type
// is either `controller.TypeName` (qualified) or `TypeName` (bare).
func compositeLitTypeName(lit *ast.CompositeLit) (string, bool) {
	switch t := lit.Type.(type) {
	case *ast.SelectorExpr:
		return t.Sel.Name, true
	case *ast.Ident:
		return t.Name, true
	default:
		return "", false
	}
}

func contains(haystack []string, needle string) bool {
	return slices.Contains(haystack, needle)
}

// mainGoPath resolves main.go next to this test file, independent of the test
// runner's CWD.
func mainGoPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	dir := filepath.Dir(thisFile)
	if !strings.HasSuffix(dir, filepath.Join("cmd", "manager")) {
		// Fall back to CWD-relative when run from the package dir.
		return "main.go"
	}
	return filepath.Join(dir, "main.go")
}
