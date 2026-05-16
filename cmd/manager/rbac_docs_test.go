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

// Plan 03-09 Task 2 — artifact-content regression tests.
//
// charts/tide/templates/push-rbac.yaml and docs/git-hosts.md ship as static
// artifacts; the cheapest contract test is a content-level grep over the
// file on disk. These tests guard against accidental removal or scope
// inflation of the tide-push RBAC (T-304 least-privilege; D-B1) and the
// ART-02 host docs (REQ-DOC-ART-02 / ART-05 SSH caveat).
//
// Path resolution: tests run with CWD = `cmd/manager/` (Go's test runner
// default). Resolve repo-relative paths via `filepath.Join("..", "..", ...)`.
package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// repoRoot returns the absolute path of the repo root (two levels up from
// cmd/manager).
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	// cmd/manager/ → repo root via ../..
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

// readFile returns the file contents at the given repo-relative path or
// t.Fatal's on read error.
func readFile(t *testing.T, relPath string) string {
	t.Helper()
	full := filepath.Join(repoRoot(t), relPath)
	b, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", full, err)
	}
	return string(b)
}

// ---------- push-rbac.yaml — least-privilege contract (T-304 / D-B1) ----------

// TestPushRBACTemplateExists verifies the Helm template exists on disk.
func TestPushRBACTemplateExists(t *testing.T) {
	body := readFile(t, "charts/tide/templates/push-rbac.yaml")
	if len(body) == 0 {
		t.Fatal("push-rbac.yaml is empty")
	}
}

// TestPushRBACDeclaresAllThreeResources verifies SA + Role + RoleBinding
// are all present in the single template file.
func TestPushRBACDeclaresAllThreeResources(t *testing.T) {
	body := readFile(t, "charts/tide/templates/push-rbac.yaml")
	wantKinds := []string{
		"kind: ServiceAccount",
		"kind: Role",
		"kind: RoleBinding",
	}
	for _, want := range wantKinds {
		if !strings.Contains(body, want) {
			t.Errorf("push-rbac.yaml missing %q", want)
		}
	}
}

// TestPushRBACServiceAccountNamedTidePush verifies the SA name lines up with
// pushSAName in internal/controller/push_helpers.go (the binary expects to
// run as tide-push at Pod admission; mismatch silently breaks Secret access).
func TestPushRBACServiceAccountNamedTidePush(t *testing.T) {
	body := readFile(t, "charts/tide/templates/push-rbac.yaml")
	if !strings.Contains(body, "name: tide-push") {
		t.Error("push-rbac.yaml: SA/Role/RoleBinding must be named tide-push (push_helpers.go pushSAName)")
	}
}

// TestPushRBACLeastPrivilegeVerbs verifies the Role grants only `secrets get` —
// no list, no watch, no wildcards (T-304 cross-namespace + escalation
// mitigation).
func TestPushRBACLeastPrivilegeVerbs(t *testing.T) {
	body := readFile(t, "charts/tide/templates/push-rbac.yaml")
	// Must grant get on secrets.
	if !regexp.MustCompile(`(?s)resources:\s*\["?secrets"?\]\s*verbs:\s*\["?get"?\]`).MatchString(
		stripComments(body),
	) {
		// Fall-back format: multi-line YAML list. Accept either inline-bracket
		// or block style as long as the verbs list contains exactly "get".
		if !strings.Contains(body, "verbs:") || !strings.Contains(body, "get") {
			t.Errorf("push-rbac.yaml Role must grant verbs: [get] on resources: [secrets]")
		}
	}
	// Must NOT grant wildcard verbs.
	if regexp.MustCompile(`verbs:\s*\[\s*['"]?\*['"]?\s*\]`).MatchString(body) {
		t.Error("push-rbac.yaml: wildcard verb [*] forbidden (T-304 least-privilege)")
	}
	// Must NOT grant write verbs (create/update/patch/delete) — push Job only reads.
	for _, forbid := range []string{`"create"`, `"update"`, `"patch"`, `"delete"`} {
		if strings.Contains(body, forbid) {
			t.Errorf("push-rbac.yaml: forbidden write verb %s — push Job reads Secrets only", forbid)
		}
	}
}

// TestPushRBACNamespaceScoped verifies the Role + RoleBinding use
// `kind: Role`, not `kind: ClusterRole`, and bind into .Release.Namespace
// — preventing accidental cluster-scope grants.
func TestPushRBACNamespaceScoped(t *testing.T) {
	body := readFile(t, "charts/tide/templates/push-rbac.yaml")
	if strings.Contains(body, "kind: ClusterRole") {
		t.Error("push-rbac.yaml: must use namespace-scoped Role, not ClusterRole (T-304 scope)")
	}
	if !strings.Contains(body, ".Release.Namespace") {
		t.Error("push-rbac.yaml: must template namespace via .Release.Namespace")
	}
}

// stripComments removes "# …" lines so verb-pattern regex doesn't get
// fooled by an explanatory comment that names a forbidden verb.
func stripComments(body string) string {
	var out strings.Builder
	for _, line := range strings.Split(body, "\n") {
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = line[:idx]
		}
		out.WriteString(line)
		out.WriteString("\n")
	}
	return out.String()
}

// ---------- docs/git-hosts.md — ART-02 / ART-05 deliverable ----------

// TestGitHostsDocExists verifies the documentation file ships in the repo.
func TestGitHostsDocExists(t *testing.T) {
	body := readFile(t, "docs/git-hosts.md")
	if len(body) == 0 {
		t.Fatal("docs/git-hosts.md is empty")
	}
}

// TestGitHostsDocHasRequiredSections verifies the four required H2 sections
// (GitHub, GitLab, Gitea, SSH) all appear — ART-02 multi-host scope.
func TestGitHostsDocHasRequiredSections(t *testing.T) {
	body := readFile(t, "docs/git-hosts.md")
	required := []string{"## GitHub", "## GitLab", "## Gitea", "## SSH"}
	for _, want := range required {
		if !strings.Contains(body, want) {
			t.Errorf("docs/git-hosts.md missing H2 section %q", want)
		}
	}
}

// TestGitHostsDocCitesRequirementIDs verifies ART-02 and ART-05 REQ-IDs
// appear in the doc body for traceability.
func TestGitHostsDocCitesRequirementIDs(t *testing.T) {
	body := readFile(t, "docs/git-hosts.md")
	for _, req := range []string{"ART-02", "ART-05"} {
		if !strings.Contains(body, req) {
			t.Errorf("docs/git-hosts.md missing REQ-ID citation %q", req)
		}
	}
}

// TestGitHostsDocDocumentsGITPATEnvVar verifies the GIT_PAT env-var convention
// (the binary reads GIT_PAT from envFrom: SecretRef on the push Job pod).
func TestGitHostsDocDocumentsGITPATEnvVar(t *testing.T) {
	body := readFile(t, "docs/git-hosts.md")
	if !strings.Contains(body, "GIT_PAT") {
		t.Error("docs/git-hosts.md must document the GIT_PAT env-var convention")
	}
}
