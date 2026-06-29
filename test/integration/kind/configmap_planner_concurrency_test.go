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

// Package kind_integration — PREFLIGHT-01 helm-template render contract test.
//
// TestConfigMapPlannerConcurrency verifies that rendering the Helm chart with
// default values produces a ConfigMap with plannerConcurrency: 4 (not 16).
// This is the helm-template-render half of Success Criterion #1: the stale
// `| default 16` fallback must not be present in the chart, so an operator
// values override that omits plannerConcurrency cannot silently restore a
// 16-wide planner dispatch that would OOM a single-node cluster.
package kind_integration

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestConfigMapPlannerConcurrencyDefaultIsFour renders the Helm chart with
// default values and asserts the rendered ConfigMap carries plannerConcurrency: 4
// and does NOT contain plannerConcurrency: 16 (PREFLIGHT-01, Success Criterion #1).
//
// This is a plain go-test (not Ginkgo) that shells out to `helm template`. It
// mirrors the render-test shape used by TestHelmDeploymentTemplateBudgetReserveDefaultArg
// in projects_pvc_test.go — CombinedOutput over exec.Command("helm", "template", ...).
func TestConfigMapPlannerConcurrencyDefaultIsFour(t *testing.T) {
	chartDir := filepath.Join("..", "..", "..", "charts", "tide")
	cmd := exec.Command("helm", "template", "tide", chartDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, out)
	}

	outStr := string(out)

	// PREFLIGHT-01: rendered configmap must contain plannerConcurrency: 4.
	const wantEntry = "plannerConcurrency: 4"
	if !strings.Contains(outStr, wantEntry) {
		t.Errorf("helm template default render must contain %q (PREFLIGHT-01); got configmap section:\n%s",
			wantEntry, extractConfigMapSection(outStr))
	}

	// Absence check: plannerConcurrency must not resolve to 16.
	const notWantEntry = "plannerConcurrency: 16"
	if strings.Contains(outStr, notWantEntry) {
		t.Errorf("helm template default render must NOT contain %q (PREFLIGHT-01 stale default must be gone); got configmap section:\n%s",
			notWantEntry, extractConfigMapSection(outStr))
	}
}

// extractConfigMapSection returns the portion of the rendered output containing
// the tide-config ConfigMap, for cleaner failure messages.
func extractConfigMapSection(rendered string) string {
	start := strings.Index(rendered, "kind: ConfigMap")
	if start < 0 {
		return "(no ConfigMap found in render output)"
	}
	// Grab up to the next YAML document separator or end of string.
	section := rendered[start:]
	if end := strings.Index(section, "\n---"); end >= 0 {
		section = section[:end]
	}
	return section
}
