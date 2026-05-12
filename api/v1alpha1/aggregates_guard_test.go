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

package v1alpha1_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// aggregatesRegex is the canonical PERSIST-02 / Pitfall 4 denylist mirrored
// from `make verify-no-aggregates`. Tests below assert this regex matches
// forbidden patterns and stays silent on the real Phase 1 types.
const aggregatesPattern = `Schedule|Waves *\[\]|IndegreeMap|CachedDag|DerivedDag`

// TestAggregatesGuardCatchesViolation asserts the PERSIST-02 / Pitfall 4
// regex used by `make verify-no-aggregates` flags a forbidden aggregate field
// when one is present. The test writes a temp file containing the forbidden
// pattern and runs the same grep regex against it — no mutation of real
// api/v1alpha1/*_types.go files occurs.
//
// Replaces the previous revision's manual "insert + revert" negative-test
// recipe (Warning 4).
func TestAggregatesGuardCatchesViolation(t *testing.T) {
	dir := t.TempDir()
	badFile := filepath.Join(dir, "bad_types.go")
	badContent := `package v1alpha1
type PlanStatus struct {
    Schedule []string ` + "`json:\"schedule\"`" + `
}
`
	if err := os.WriteFile(badFile, []byte(badContent), 0o600); err != nil {
		t.Fatalf("write temp: %v", err)
	}

	// Same regex the Makefile target uses.
	re := regexp.MustCompile(aggregatesPattern)
	data, err := os.ReadFile(badFile)
	if err != nil {
		t.Fatalf("read temp: %v", err)
	}
	if !re.Match(data) {
		t.Fatalf("PERSIST-02 regex did NOT flag forbidden field in bad fixture; rule is broken")
	}
}

// TestAggregatesGuardSilentOnCleanFile asserts the regex does NOT flag a
// clean PlanStatus struct (the actual Phase 1 shape).
func TestAggregatesGuardSilentOnCleanFile(t *testing.T) {
	dir := t.TempDir()
	goodFile := filepath.Join(dir, "good_types.go")
	goodContent := `package v1alpha1
type PlanStatus struct {
    Phase string ` + "`json:\"phase,omitempty\"`" + `
}
`
	if err := os.WriteFile(goodFile, []byte(goodContent), 0o600); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	re := regexp.MustCompile(aggregatesPattern)
	data, err := os.ReadFile(goodFile)
	if err != nil {
		t.Fatalf("read temp: %v", err)
	}
	if re.Match(data) {
		t.Fatalf("PERSIST-02 regex falsely flagged clean fixture")
	}
}

// TestMakeVerifyNoAggregatesPassesOnRealTypes runs the actual Makefile target
// against the real api/v1alpha1/*_types.go files and asserts exit 0. This is
// the integration-level check — the real types files must satisfy the rule.
func TestMakeVerifyNoAggregatesPassesOnRealTypes(t *testing.T) {
	// Skip if make is unavailable.
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make not found on PATH; skipping integration check")
	}
	// Find repo root by walking up until go.mod is found.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := cwd
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Skipf("go.mod not found from %s; cannot locate repo root", cwd)
		}
		root = parent
	}
	cmd := exec.Command("make", "-C", root, "verify-no-aggregates")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make verify-no-aggregates failed against real types:\n%s\nerr: %v", out, err)
	}
	if !strings.Contains(string(out), "OK") {
		t.Fatalf("expected 'OK' in output, got:\n%s", out)
	}
}
