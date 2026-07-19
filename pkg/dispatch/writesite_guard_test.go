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

package dispatch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

// writeSiteFiles are the three REAL production EnvelopeOut write sites
// (RESEARCH.md's dead-Harness.Run correction — internal/harness/harness.go's
// Harness.Run/buildEnvelopeOut is orphaned scaffolding with zero production
// call sites and is deliberately NOT in this list; see 50-RESEARCH.md
// Pitfall 1). Every populated EnvelopeOut{...} literal constructed in these
// files MUST set TerminalReason explicitly (D-02, EXEC-02).
var writeSiteFiles = []string{
	"cmd/claude-subagent/main.go",
	"internal/subagent/anthropic/subagent.go",
	"cmd/stub-subagent/main.go",
}

// minPopulatedLiteralInventory is the RESEARCH Pitfall 1 warning-sign floor:
// the three write-site files together construct at least this many populated
// EnvelopeOut{...} literals today (a grep for `pkgdispatch.EnvelopeOut{`
// across cmd/ and internal/subagent/ turns up ~15). If a future refactor
// drops the discovered count below this floor, the guard's own inventory has
// silently shrunk — narrowing what this test actually protects — so the
// test fails loudly with a distinct message rather than passing quietly on a
// smaller file set.
const minPopulatedLiteralInventory = 15

// TestEnvelopeOutWriteSites_AlwaysSetTerminalReason is the EXEC-02 "never a
// silent default" structural guard (D-02). Go's type system cannot make a
// struct field mandatory, so this is a source-level AST walk rather than a
// compile-time check — mirroring the fail-closed *discipline* of
// [ClassifyVerdict] (the zero value is never a silent default), not its
// bare-return classifier shape: TerminalReason is set at each Go call site,
// not classified from external input.
//
// Rule: every pkgdispatch.EnvelopeOut{...} composite literal in
// [writeSiteFiles] that has at least one element MUST contain a
// TerminalReason key. Zero-element literals (EnvelopeOut{}) are EXEMPT —
// they are dispatch-level error placeholders (e.g. the vendor-mismatch/
// params-allow-list/prompt-template-load early returns in
// internal/subagent/anthropic/subagent.go's Run(), which return
// `pkgdispatch.EnvelopeOut{}, err`) that are never written to out.json; the
// CALLER (cmd/claude-subagent/main.go's failEnvelope) wraps the error in its
// own populated EnvelopeOut, which DOES set TerminalReason and is itself
// covered by this same walk.
func TestEnvelopeOutWriteSites_AlwaysSetTerminalReason(t *testing.T) {
	root := findWriteSiteGuardRepoRoot(t)

	var populatedCount int
	for _, relPath := range writeSiteFiles {
		fset := token.NewFileSet()
		absPath := filepath.Join(root, relPath)
		file, err := parser.ParseFile(fset, absPath, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", relPath, err)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			sel, ok := lit.Type.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "EnvelopeOut" {
				return true
			}
			if len(lit.Elts) == 0 {
				// Exempt: a dispatch-level error placeholder, never written
				// to out.json — see the doc comment above.
				return true
			}
			populatedCount++

			hasTerminalReason := false
			for _, elt := range lit.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "TerminalReason" {
					hasTerminalReason = true
					break
				}
			}
			if !hasTerminalReason {
				pos := fset.Position(lit.Pos())
				t.Errorf("%s:%d: pkgdispatch.EnvelopeOut{...} literal omits TerminalReason (EXEC-02 violation)",
					relPath, pos.Line)
			}
			return true
		})
	}

	if populatedCount < minPopulatedLiteralInventory {
		t.Fatalf("write-site inventory shrank — found %d populated EnvelopeOut literals across %v, want >= %d; "+
			"update writeSiteFiles/minPopulatedLiteralInventory if a write site moved or was removed",
			populatedCount, writeSiteFiles, minPopulatedLiteralInventory)
	}
}

// findWriteSiteGuardRepoRoot walks up from the test's CWD until it finds
// go.mod. Mirrors pkg/otelai/attrs_test.go's findRepoRoot idiom.
func findWriteSiteGuardRepoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := cwd
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			return root
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatalf("go.mod not found from %s; cannot locate repo root", cwd)
		}
		root = parent
	}
}
