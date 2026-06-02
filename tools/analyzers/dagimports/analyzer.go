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

// Package dagimports implements an analyzer that rejects forbidden imports
// (k8s.io/*, sigs.k8s.io/*, github.com/anthropics/*) from any file whose
// package import path contains "/pkg/dag/" or ends with "/pkg/dag".
//
// This is the analyzer mirror of the `make verify-dag-imports` Makefile
// target. The Makefile target is the load-bearing CI gate (uses go list
// -deps for transitive coverage); this analyzer exists ONLY to provide a
// programmatic fixture proving the forbidden-import rule fires on a
// known-bad input, without requiring the executor to manually mutate
// pkg/dag at test time (revision Warning 4).
package dagimports

import (
	"strings"

	"golang.org/x/tools/go/analysis"
)

var forbiddenPrefixes = []string{
	"k8s.io/",
	"sigs.k8s.io/",
	"github.com/anthropics/",
}

// Analyzer rejects forbidden imports inside any package whose import path
// contains "/pkg/dag" (including the analysistest fixture packages, which
// have synthetic import paths like "valid/pkg/dag" and "violation/pkg/dag").
var Analyzer = &analysis.Analyzer{
	Name: "dagimports",
	Doc:  "rejects k8s.io/*, sigs.k8s.io/*, github.com/anthropics/* imports inside pkg/dag (DAG-05 fixture mirror)",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	path := pass.Pkg.Path()
	if !strings.Contains(path, "/pkg/dag") && !strings.HasSuffix(path, "pkg/dag") {
		return nil, nil
	}
	for _, f := range pass.Files {
		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, "\"")
			for _, prefix := range forbiddenPrefixes {
				if strings.HasPrefix(importPath, prefix) {
					pass.Reportf(imp.Pos(),
						"DAG-05 violation: forbidden import %q in pkg/dag (forbidden prefix %q)",
						importPath, prefix)
				}
			}
		}
	}
	return nil, nil
}
