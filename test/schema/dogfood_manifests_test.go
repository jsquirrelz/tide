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

// Package schema relocates the dogfood-fixture strict-decode coverage that
// previously lived alongside the original API package's test suite. It moved
// here (Phase 40 Plan 40-05) when the two prior schema-revision packages were
// deleted — v1alpha3 is now the sole served+storage version, so the fixture
// set is single-version rather than mixed. Stays in the fast unit tier: `make test`
// selects `go list ./... | grep -v /e2e | grep -v /test/integration`, and
// test/schema matches neither exclusion.
package schema

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	sigsyaml "sigs.k8s.io/yaml"
)

// findRepoRoot walks up from cwd until it finds go.mod.
func findRepoRoot(t *testing.T) string {
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

// projectAPIVersion extracts the declared apiVersion from a Project doc so the
// strict-decode + required-field checks can validate each manifest against the
// schema it actually targets.
func projectAPIVersion(t *testing.T, doc []byte) string {
	t.Helper()
	var meta struct {
		APIVersion string `json:"apiVersion"`
	}
	if err := sigsyaml.Unmarshal(doc, &meta); err != nil {
		t.Fatalf("unmarshal apiVersion: %v", err)
	}
	return meta.APIVersion
}

// supportedProjectAPIVersions is the set of apiVersions a dogfood Project may
// declare. New schema versions get added here as they ship. v1alpha3 is the
// sole served+storage version as of Phase 40 (the two prior schema-revision
// versions were removed).
var supportedProjectAPIVersions = map[string]bool{
	"tideproject.k8s/v1alpha3": true,
}

// splitYAMLDocs splits a multi-document YAML file on "---" document separators,
// returning non-empty documents only.
func splitYAMLDocs(content []byte) [][]byte {
	var docs [][]byte
	for raw := range bytes.SplitSeq(content, []byte("\n---")) {
		trimmed := bytes.TrimSpace(raw)
		// Strip leading "---" from the first document if present.
		trimmed = bytes.TrimPrefix(trimmed, []byte("---"))
		trimmed = bytes.TrimSpace(trimmed)
		if len(trimmed) > 0 {
			docs = append(docs, trimmed)
		}
	}
	return docs
}

// isProjectDoc returns true if the YAML document has kind: Project.
func isProjectDoc(doc []byte) bool {
	return bytes.Contains(doc, []byte("kind: Project"))
}

// hasTopLevelKey returns true if the YAML document contains the given key as a
// top-level key (i.e., at the start of a line with no leading spaces).
func hasTopLevelKey(doc []byte, key string) bool {
	needle := []byte(key + ":")
	for line := range bytes.SplitSeq(doc, []byte("\n")) {
		stripped := bytes.TrimLeft(line, " \t")
		if bytes.HasPrefix(stripped, needle) {
			return true
		}
	}
	return false
}

// dogfoodGlob returns the paths of all 0*.yaml files under examples/projects/dogfood/.
// It locates the repo root by walking up from the test package's cwd.
func dogfoodGlob(t *testing.T) []string {
	t.Helper()
	root := findRepoRoot(t)
	pattern := filepath.Join(root, "examples", "projects", "dogfood", "0*.yaml")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob %q: %v", pattern, err)
	}
	return matches
}

// TestDogfoodManifests_GlobFindsThreeFiles verifies that exactly three numbered
// manifests exist under examples/projects/dogfood/.
func TestDogfoodManifests_GlobFindsThreeFiles(t *testing.T) {
	paths := dogfoodGlob(t)
	if len(paths) != 3 {
		t.Fatalf("expected 3 dogfood manifests, found %d: %v", len(paths), paths)
	}
	// Each file must be readable.
	for _, p := range paths {
		if _, err := os.ReadFile(p); err != nil {
			t.Errorf("cannot read %s: %v", p, err)
		}
	}
}

// TestDogfoodManifests_StrictDecode verifies that each 0*.yaml file contains a
// Project document that strict-decodes cleanly into v1alpha3.Project.
// UnmarshalStrict rejects unknown field names, so typos in field names fail here
// without needing a live cluster — this is the schema-validity proof.
func TestDogfoodManifests_StrictDecode(t *testing.T) {
	for _, path := range dogfoodGlob(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			docs := splitYAMLDocs(content)
			found := false
			for _, doc := range docs {
				if !isProjectDoc(doc) {
					continue
				}
				found = true
				switch av := projectAPIVersion(t, doc); av {
				case "tideproject.k8s/v1alpha3":
					var proj tideprojectv1alpha3.Project
					if err := sigsyaml.UnmarshalStrict(doc, &proj); err != nil {
						t.Errorf("UnmarshalStrict (v1alpha3) failed for Project doc in %s: %v", path, err)
					}
				default:
					t.Errorf("Project doc in %s declares unsupported apiVersion %q", path, av)
				}
			}
			if !found {
				t.Errorf("no `kind: Project` document found in %s", path)
			}
		})
	}
}

// TestDogfoodManifests_RequiredFields asserts per-Project field invariants:
//   - apiVersion is a supported tideproject.k8s version
//   - spec.targetRepo == "https://github.com/jsquirrelz/tide.git"
//   - spec.outcomePrompt is non-empty
//   - spec.providerSecretRef is non-empty
//   - spec.git.credsSecretRef is non-empty
func TestDogfoodManifests_RequiredFields(t *testing.T) {
	for _, path := range dogfoodGlob(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			docs := splitYAMLDocs(content)
			for _, doc := range docs {
				if !isProjectDoc(doc) {
					continue
				}

				var apiVersion, targetRepo, outcomePrompt, providerSecretRef, gitCredsRef string
				gitNil := true
				switch av := projectAPIVersion(t, doc); av {
				case "tideproject.k8s/v1alpha3":
					var proj tideprojectv1alpha3.Project
					if err := sigsyaml.UnmarshalStrict(doc, &proj); err != nil {
						t.Fatalf("UnmarshalStrict (v1alpha3): %v", err)
					}
					apiVersion, targetRepo, outcomePrompt, providerSecretRef = proj.APIVersion, proj.Spec.TargetRepo, proj.Spec.OutcomePrompt, proj.Spec.ProviderSecretRef
					if proj.Spec.Git != nil {
						gitNil, gitCredsRef = false, proj.Spec.Git.CredsSecretRef
					}
				default:
					t.Fatalf("Project doc declares unsupported apiVersion %q", av)
				}

				if !supportedProjectAPIVersions[apiVersion] {
					t.Errorf("apiVersion = %q, want a supported tideproject.k8s version", apiVersion)
				}
				if targetRepo != "https://github.com/jsquirrelz/tide.git" {
					t.Errorf("spec.targetRepo = %q, want %q", targetRepo, "https://github.com/jsquirrelz/tide.git")
				}
				if strings.TrimSpace(outcomePrompt) == "" {
					t.Errorf("spec.outcomePrompt is empty")
				}
				if providerSecretRef == "" {
					t.Errorf("spec.providerSecretRef is empty")
				}
				if gitNil || gitCredsRef == "" {
					t.Errorf("spec.git.credsSecretRef is empty or git is nil")
				}
			}
		})
	}
}

// TestDogfoodManifests_NoInlineSecrets asserts that no raw document in any
// dogfood manifest file contains a `stringData` or `data` top-level key.
// Inline Secrets would expose credential material in a public repo.
func TestDogfoodManifests_NoInlineSecrets(t *testing.T) {
	for _, path := range dogfoodGlob(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			for i, doc := range splitYAMLDocs(content) {
				if hasTopLevelKey(doc, "stringData") {
					t.Errorf("doc %d in %s has top-level `stringData:` key — inline Secret material forbidden", i, path)
				}
				if hasTopLevelKey(doc, "data") {
					t.Errorf("doc %d in %s has top-level `data:` key — inline Secret material forbidden", i, path)
				}
			}
		})
	}
}
