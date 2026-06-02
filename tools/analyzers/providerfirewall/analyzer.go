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

// Package providerfirewall implements an analyzer that rejects LLM SDK imports
// (github.com/anthropics/*, github.com/openai/*, etc.) from files whose
// package import path falls within the orchestrator-side firewall boundary:
//   - pkg/controller/...
//   - pkg/dispatch/...
//   - pkg/dag/...
//   - internal/controller/...
//   - internal/webhook/...
//   - internal/dispatch/...
//
// SUB-05 / Pitfall 14 prevention: The Subagent interface is provider-agnostic
// by construction. A stray anthropic.NewClient() in the controller or dispatch
// packages would auto-load ANTHROPIC_API_KEY from the controller pod's
// environment (RESEARCH.md Pitfall D — Anthropic Go SDK env-var auto-load on
// controller startup). Defense is at the import graph — caught before merge.
//
// The explicitly out-of-scope site is internal/subagent/anthropic/... — Phase 3's
// harness-adapter site where the real Anthropic-backed Subagent implementation
// lives. cmd/credproxy/... is also out of scope; the credproxy is a generic
// HTTPS reverse proxy and never imports the Anthropic SDK.
//
// How to extend: to add a new firewalled boundary, append its path fragment to
// forbiddenScopes below and add a corresponding testdata/src fixture pair
// (valid/<new-boundary>/safe.go + violation/<new-boundary>/forbidden.go with
// a // want directive). The test will catch any drift.
package providerfirewall

import (
	"strings"

	"golang.org/x/tools/go/analysis"
)

// forbiddenPrefixes is the LLM SDK denylist. An import matching any of these
// prefixes is forbidden inside the firewalled boundaries. Add new providers
// here as new LLM SDK Go packages become relevant.
var forbiddenPrefixes = []string{
	"github.com/anthropics/",
	"github.com/openai/",
	"github.com/sashabaranov/go-openai",
	"github.com/google/generative-ai-go",
}

// forbiddenScopes is the set of package path fragments that constitute the
// orchestrator-side firewall boundary. A package import path containing any of
// these fragments (or ending with the bare form) is in scope.
//
// Out-of-scope by construction: internal/subagent/anthropic/... (harness-adapter
// site for real provider impls) and cmd/credproxy/... (generic reverse proxy).
var forbiddenScopes = []struct {
	contains  string
	hasSuffix string
}{
	{"/pkg/controller/", "pkg/controller"},
	{"/pkg/dispatch/", "pkg/dispatch"},
	{"/pkg/dag/", "pkg/dag"},
	{"/internal/controller/", "internal/controller"},
	{"/internal/webhook/", "internal/webhook"},
	{"/internal/dispatch/", "internal/dispatch"},
}

// Analyzer rejects LLM SDK imports inside the orchestrator-side firewall boundary
// (SUB-05 / Pitfall 14). The analysistest suite provides three fixtures:
//   - valid/pkg/dispatch   — stdlib only; must produce zero diagnostics
//   - violation/pkg/controller — imports github.com/anthropics/anthropic-sdk-go; must fire
//   - valid/internal/subagent/anthropic — same import at the allowed harness site; must NOT fire
var Analyzer = &analysis.Analyzer{
	Name: "providerfirewall",
	Doc: "rejects github.com/anthropics/*, github.com/openai/*, etc. imports inside the " +
		"orchestrator-side firewall boundary (SUB-05 / Pitfall 14)",
	Run: run,
}

func run(pass *analysis.Pass) (any, error) {
	path := pass.Pkg.Path()
	if !inFirewalledScope(path) {
		return nil, nil
	}
	for _, f := range pass.Files {
		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, "\"")
			for _, prefix := range forbiddenPrefixes {
				if strings.HasPrefix(importPath, prefix) {
					pass.Reportf(imp.Pos(),
						"SUB-05 violation: forbidden LLM SDK import %q in %s (Pitfall 14: vendor lock-in creep)",
						importPath, path)
				}
			}
		}
	}
	return nil, nil
}

// inFirewalledScope reports whether the given package import path falls within
// the orchestrator-side firewall boundary. It returns false for any package
// path containing "internal/subagent/" (the harness-adapter site) so the
// Phase 3 anthropic concrete impl can import the SDK freely.
func inFirewalledScope(path string) bool {
	// Harness-adapter and credproxy are explicitly out-of-scope.
	if strings.Contains(path, "internal/subagent/") || strings.Contains(path, "cmd/credproxy") {
		return false
	}
	for _, scope := range forbiddenScopes {
		if strings.Contains(path, scope.contains) || strings.HasSuffix(path, scope.hasSuffix) {
			return true
		}
	}
	return false
}
