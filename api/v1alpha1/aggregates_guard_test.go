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

// TestAggregatesGuard_PreservesPhase1Denylist asserts that the Phase 2 schema
// additions (BudgetConfig, BudgetStatus, ProviderConfig, PlanAdmissionConfig,
// Caps, TaskDev, ValidationState, etc.) do NOT introduce any of the PERSIST-02
// / Pitfall 4 forbidden tokens into *_types.go files.  The check walks the
// real api/v1alpha1/*_types.go tree programmatically — same corpus that
// `make verify-no-aggregates` inspects.
func TestAggregatesGuard_PreservesPhase1Denylist(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// Locate the api/v1alpha1 directory by walking up to go.mod, then descending.
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
	typesDir := filepath.Join(root, "api", "v1alpha1")
	re := regexp.MustCompile(aggregatesPattern)

	var violations []string
	err = filepath.Walk(typesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), "_types.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if re.Match(data) {
			violations = append(violations, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", typesDir, err)
	}
	if len(violations) > 0 {
		t.Errorf("PERSIST-02 denylist matched in Phase 2 *_types.go files (forbidden aggregate tokens present):\n%s",
			strings.Join(violations, "\n"))
	}
}

// TestAggregatesGuard_BudgetStatusIsNotAggregate is a positive-shape assertion:
// it verifies that (a) the BudgetStatus struct was added to project_types.go and
// (b) the BudgetStatus struct body does NOT contain any PERSIST-02 forbidden
// tokens — confirming it is a tally object, not an aggregate schedule.
func TestAggregatesGuard_BudgetStatusIsNotAggregate(t *testing.T) {
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

	projectTypesPath := filepath.Join(root, "api", "v1alpha1", "project_types.go")
	data, err := os.ReadFile(projectTypesPath)
	if err != nil {
		t.Fatalf("read project_types.go: %v", err)
	}
	content := string(data)

	// Positive assertion: BudgetStatus must be present.
	if !strings.Contains(content, "BudgetStatus") {
		t.Fatalf("BudgetStatus not found in project_types.go; Phase 2 addition is missing")
	}

	// Extract the BudgetStatus struct body between "type BudgetStatus struct {" and its closing "}".
	startMarker := "type BudgetStatus struct {"
	startIdx := strings.Index(content, startMarker)
	if startIdx < 0 {
		t.Fatalf("type BudgetStatus struct { not found in project_types.go")
	}
	// Find the matching closing brace.
	bodyStart := startIdx + len(startMarker)
	depth := 1
	closeIdx := bodyStart
	for closeIdx < len(content) && depth > 0 {
		switch content[closeIdx] {
		case '{':
			depth++
		case '}':
			depth--
		}
		closeIdx++
	}
	structBody := content[bodyStart:closeIdx]

	re := regexp.MustCompile(aggregatesPattern)
	if re.MatchString(structBody) {
		t.Errorf("BudgetStatus struct body contains PERSIST-02 forbidden token(s):\n%s\n\n"+
			"BudgetStatus must be a tally object — it must not contain schedule/wave/indegree fields.", structBody)
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
