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

package controller_test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestRBACWildcardGuardCatchesViolation asserts the AUTH-03 / Pitfall 15
// regex used by `make verify-no-rbac-wildcards` flags a wildcard verb when
// one is present. The test writes a temp role.yaml containing a wildcard and
// runs the same grep regex against it — no mutation of real config/rbac/
// manifests occurs.
//
// Replaces the previous revision's manual "insert + revert" recipe (Warning 4).
func TestRBACWildcardGuardCatchesViolation(t *testing.T) {
	dir := t.TempDir()
	badFile := filepath.Join(dir, "role.yaml")
	badContent := `apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: bad-role
rules:
  - apiGroups: ["tideproject.k8s"]
    resources: ["projects"]
    verbs: ["*"]
`
	if err := os.WriteFile(badFile, []byte(badContent), 0o600); err != nil {
		t.Fatalf("write temp: %v", err)
	}

	// Same regex the Makefile target uses (verb wildcard pattern).
	re := regexp.MustCompile(`verbs:.*"?\*"?`)
	data, err := os.ReadFile(badFile)
	if err != nil {
		t.Fatalf("read temp: %v", err)
	}
	if !re.Match(data) {
		t.Fatalf("AUTH-03 regex did NOT flag wildcard in bad fixture; rule is broken")
	}
}

// TestRBACWildcardGuardSilentOnCleanFile asserts the regex does NOT flag an
// enumerated-verbs Role (the actual Phase 1 shape).
func TestRBACWildcardGuardSilentOnCleanFile(t *testing.T) {
	dir := t.TempDir()
	goodFile := filepath.Join(dir, "role.yaml")
	goodContent := `apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: good-role
rules:
  - apiGroups: ["tideproject.k8s"]
    resources: ["projects"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
`
	if err := os.WriteFile(goodFile, []byte(goodContent), 0o600); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	re := regexp.MustCompile(`verbs:.*"?\*"?`)
	data, err := os.ReadFile(goodFile)
	if err != nil {
		t.Fatalf("read temp: %v", err)
	}
	if re.Match(data) {
		t.Fatalf("AUTH-03 regex falsely flagged clean fixture")
	}
}

// TestRBACMarkerDisciplineRegexCatchesViolation mirrors the source-marker
// check used by `make verify-rbac-marker-discipline`.
func TestRBACMarkerDisciplineRegexCatchesViolation(t *testing.T) {
	badMarker := `// +kubebuilder:rbac:groups=tideproject.k8s,resources=projects,verbs=*`
	re := regexp.MustCompile(`kubebuilder:rbac.*verbs=\*|kubebuilder:rbac.*resources=\*`)
	if !re.MatchString(badMarker) {
		t.Fatalf("AUTH-03 marker-discipline regex did NOT flag wildcard marker; rule is broken")
	}
}
