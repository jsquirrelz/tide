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

// Phase 3 Plan 02 Task 1: schema-foundation tests for
//   - ProjectSpec.Subagent (D-C2)         + Levels milestone/phase/plan/task
//   - ProjectSpec.Git      (D-B6)         + CEL Pattern on repoURL
//   - ProjectStatus.Git    (D-B6)
//   - Phase constants PhasePushLeaseFailed / PhaseComplete
//   - Condition constants ConditionCloned / ConditionAuthoringPlanner / ConditionPushLeaseFailed
//
// These tests mirror the static-analysis convention of aggregates_guard_test.go:
// they parse the source files and the generated CRD YAML, rather than spinning
// up an envtest harness — the envtest contract belongs in internal/controller
// (Layer A) and the kind contract in test/integration (Layer B). Static tests
// here keep the api/v1alpha1 TEST-01 budget tight.
package v1alpha1_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
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

// readProjectTypes reads api/v1alpha1/project_types.go.
func readProjectTypes(t *testing.T) string {
	t.Helper()
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "api", "v1alpha1", "project_types.go"))
	if err != nil {
		t.Fatalf("read project_types.go: %v", err)
	}
	return string(data)
}

// readSharedTypes reads api/v1alpha1/shared_types.go.
func readSharedTypes(t *testing.T) string {
	t.Helper()
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "api", "v1alpha1", "shared_types.go"))
	if err != nil {
		t.Fatalf("read shared_types.go: %v", err)
	}
	return string(data)
}

// readProjectCRD reads config/crd/bases/tideproject.k8s_projects.yaml.
func readProjectCRD(t *testing.T) string {
	t.Helper()
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "config", "crd", "bases", "tideproject.k8s_projects.yaml"))
	if err != nil {
		t.Fatalf("read project CRD yaml: %v", err)
	}
	return string(data)
}

// TestSubagentConfigTypeDeclared verifies SubagentConfig + LevelOverrides + LevelConfig
// exist in project_types.go per D-C2.
func TestSubagentConfigTypeDeclared(t *testing.T) {
	src := readProjectTypes(t)
	for _, want := range []string{
		"type SubagentConfig struct",
		"type LevelOverrides struct",
		"type LevelConfig struct",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("project_types.go missing type declaration %q", want)
		}
	}
}

// TestGitConfigTypeDeclared verifies GitConfig + GitStatus exist per D-B6.
func TestGitConfigTypeDeclared(t *testing.T) {
	src := readProjectTypes(t)
	for _, want := range []string{
		"type GitConfig struct",
		"type GitStatus struct",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("project_types.go missing type declaration %q", want)
		}
	}
}

// TestProjectSpecHasSubagentField verifies Subagent SubagentConfig is wired into ProjectSpec.
func TestProjectSpecHasSubagentField(t *testing.T) {
	src := readProjectTypes(t)
	// Match either `Subagent SubagentConfig` (with optional whitespace).
	re := regexp.MustCompile(`Subagent\s+SubagentConfig`)
	if !re.MatchString(src) {
		t.Errorf("ProjectSpec missing `Subagent SubagentConfig` field wiring")
	}
}

// TestProjectSpecHasGitField verifies Git *GitConfig is wired into ProjectSpec.
// Pointer (not value) so omitempty fully elides absent GitConfig — value-type
// would serialize as `git: {repoURL: ""}` and trip RepoURL's pattern validator
// on Phase 2 test fixtures.
func TestProjectSpecHasGitField(t *testing.T) {
	src := readProjectTypes(t)
	re := regexp.MustCompile(`Git\s+\*GitConfig`)
	if !re.MatchString(src) {
		t.Errorf("ProjectSpec missing `Git *GitConfig` field wiring (pointer required)")
	}
}

// TestProjectStatusHasGitField verifies Git GitStatus is wired into ProjectStatus.
func TestProjectStatusHasGitField(t *testing.T) {
	src := readProjectTypes(t)
	re := regexp.MustCompile(`Git\s+GitStatus`)
	if !re.MatchString(src) {
		t.Errorf("ProjectStatus missing `Git GitStatus` field wiring")
	}
}

// TestPhaseConstantsDeclared verifies PhasePushLeaseFailed + PhaseComplete are declared.
func TestPhaseConstantsDeclared(t *testing.T) {
	if got := tideprojectv1alpha1.PhasePushLeaseFailed; got != "PushLeaseFailed" {
		t.Errorf("PhasePushLeaseFailed = %q, want %q", got, "PushLeaseFailed")
	}
	if got := tideprojectv1alpha1.PhaseComplete; got != "Complete" {
		t.Errorf("PhaseComplete = %q, want %q", got, "Complete")
	}
}

// TestConditionConstantsDeclared verifies Phase 3 condition vocabulary additions.
func TestConditionConstantsDeclared(t *testing.T) {
	if got := tideprojectv1alpha1.ConditionCloned; got != "Cloned" {
		t.Errorf("ConditionCloned = %q, want %q", got, "Cloned")
	}
	if got := tideprojectv1alpha1.ConditionAuthoringPlanner; got != "AuthoringPlanner" {
		t.Errorf("ConditionAuthoringPlanner = %q, want %q", got, "AuthoringPlanner")
	}
	if got := tideprojectv1alpha1.ConditionPushLeaseFailed; got != "PushLeaseFailed" {
		t.Errorf("ConditionPushLeaseFailed = %q, want %q", got, "PushLeaseFailed")
	}
}

// TestConditionConstantsInSharedTypes asserts the source-of-truth declarations
// live in shared_types.go (not scattered).
func TestConditionConstantsInSharedTypes(t *testing.T) {
	src := readSharedTypes(t)
	for _, want := range []string{
		`ConditionCloned`,
		`ConditionAuthoringPlanner`,
		`ConditionPushLeaseFailed`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("shared_types.go missing constant %q", want)
		}
	}
}

// TestProjectCRDSchemaHasSubagent verifies `make manifests` regenerated the
// CRD YAML with the new spec.subagent block.
func TestProjectCRDSchemaHasSubagent(t *testing.T) {
	crd := readProjectCRD(t)
	if !strings.Contains(crd, "subagent:") {
		t.Errorf("project CRD YAML missing `subagent:` field — `make manifests` did not regenerate")
	}
	// All four per-level overrides must appear under `subagent.levels.{milestone,phase,plan,task}`.
	for _, want := range []string{"milestone:", "phase:", "plan:", "task:"} {
		if !strings.Contains(crd, want) {
			t.Errorf("project CRD YAML missing per-level field %q under subagent.levels", want)
		}
	}
}

// TestProjectCRDSchemaHasGitSpec verifies the spec.git block landed in the CRD.
func TestProjectCRDSchemaHasGitSpec(t *testing.T) {
	crd := readProjectCRD(t)
	for _, want := range []string{"repoURL:", "credsSecretRef:"} {
		if !strings.Contains(crd, want) {
			t.Errorf("project CRD YAML missing git field %q", want)
		}
	}
}

// TestProjectCRDSchemaHasGitStatus verifies the status.git block landed.
func TestProjectCRDSchemaHasGitStatus(t *testing.T) {
	crd := readProjectCRD(t)
	for _, want := range []string{"branchName:", "lastPushedSHA:", "leaseFailureCount:"} {
		if !strings.Contains(crd, want) {
			t.Errorf("project CRD YAML missing status.git field %q", want)
		}
	}
}

// TestProjectCRDSchemaHasRepoURLPattern verifies the URL pattern for the
// git.repoURL field landed in the regenerated CRD YAML. Pattern accepts
// http(s) (production) and SSH (git@) only; file:// is not a supported
// production transport (08-03: CEL marker tightened, file:// removed).
func TestProjectCRDSchemaHasRepoURLPattern(t *testing.T) {
	crd := readProjectCRD(t)
	re := regexp.MustCompile(`pattern:\s+\^\(https\?://\|git@\)\.\+`)
	if !re.MatchString(crd) {
		t.Errorf("project CRD YAML missing `pattern: ^(https?://|git@).+` validation on repoURL — kubebuilder marker missing or stale (08-03: file:// removed from pattern)")
	}
}

// TestProjectCRDGroupUnchanged enforces the CLAUDE.md domain rule:
// API group MUST remain `tideproject.k8s` (never `tide.io` or placeholders).
func TestProjectCRDGroupUnchanged(t *testing.T) {
	crd := readProjectCRD(t)
	re := regexp.MustCompile(`(?m)^\s+group:\s+tideproject\.k8s`)
	if !re.MatchString(crd) {
		t.Errorf("project CRD group is not `tideproject.k8s` — CLAUDE.md domain rule violated")
	}
}

// TestSubagentConfigRoundTrip verifies the Go types can be instantiated, copied,
// and read back without data loss — proxy for envtest round-trip semantics.
func TestSubagentConfigRoundTrip(t *testing.T) {
	original := tideprojectv1alpha1.SubagentConfig{
		Image: "ghcr.io/foo/bar:v1",
		Model: "claude-sonnet-4-6",
		Levels: tideprojectv1alpha1.LevelOverrides{
			Task: &tideprojectv1alpha1.LevelConfig{
				Model:  "claude-haiku-4-5",
				Params: map[string]string{"temperature": "0.2"},
			},
		},
	}
	copied := original.DeepCopy()
	if copied.Image != "ghcr.io/foo/bar:v1" {
		t.Errorf("Image copy mismatch: got %q", copied.Image)
	}
	if copied.Model != "claude-sonnet-4-6" {
		t.Errorf("Model copy mismatch: got %q", copied.Model)
	}
	if copied.Levels.Task == nil {
		t.Fatalf("Levels.Task pointer not copied")
	}
	if copied.Levels.Task.Model != "claude-haiku-4-5" {
		t.Errorf("Levels.Task.Model copy mismatch: got %q", copied.Levels.Task.Model)
	}
	if copied.Levels.Task.Params["temperature"] != "0.2" {
		t.Errorf("Levels.Task.Params copy mismatch: got %v", copied.Levels.Task.Params)
	}
	// Mutate copied; original must not change (deep-copy independence).
	copied.Levels.Task.Params["temperature"] = "0.9"
	if original.Levels.Task.Params["temperature"] != "0.2" {
		t.Errorf("DeepCopy did NOT isolate Params map; mutation of copy bled into original")
	}
}

// TestGitStatusRoundTrip verifies GitStatus carries branchName/lastPushedSHA/leaseFailureCount.
func TestGitStatusRoundTrip(t *testing.T) {
	original := tideprojectv1alpha1.GitStatus{
		BranchName:        "tide/run-foo-1234567890",
		LastPushedSHA:     "abc1234",
		LeaseFailureCount: 2,
	}
	copied := original.DeepCopy()
	if copied.BranchName != "tide/run-foo-1234567890" {
		t.Errorf("BranchName copy mismatch: got %q", copied.BranchName)
	}
	if copied.LastPushedSHA != "abc1234" {
		t.Errorf("LastPushedSHA copy mismatch: got %q", copied.LastPushedSHA)
	}
	if copied.LeaseFailureCount != 2 {
		t.Errorf("LeaseFailureCount copy mismatch: got %d", copied.LeaseFailureCount)
	}
}

// TestGitConfigRoundTrip verifies GitConfig field shape.
func TestGitConfigRoundTrip(t *testing.T) {
	original := tideprojectv1alpha1.GitConfig{
		RepoURL:        "https://github.com/owner/repo.git",
		CredsSecretRef: "git-creds",
		LeaksConfigRef: "gitleaks-rules",
	}
	copied := original.DeepCopy()
	if copied.RepoURL != "https://github.com/owner/repo.git" {
		t.Errorf("RepoURL copy mismatch: got %q", copied.RepoURL)
	}
	if copied.CredsSecretRef != "git-creds" {
		t.Errorf("CredsSecretRef copy mismatch: got %q", copied.CredsSecretRef)
	}
	if copied.LeaksConfigRef != "gitleaks-rules" {
		t.Errorf("LeaksConfigRef copy mismatch: got %q", copied.LeaksConfigRef)
	}
}
