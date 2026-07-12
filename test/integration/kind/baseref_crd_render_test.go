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

package kind_integration

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestHelmTideCRDsRenderBaseRef is the P8 chart-skew lock: a stale tide-crds
// chart silently prunes baseRef/baseSHA, and runs branch from HEAD with no
// error. This plain go-test renders the tide-crds subchart via `helm
// template` (no cluster required — same style as projects_pvc_test.go's
// helm-template contract tests) and asserts baseRef (spec.git) and baseSHA
// (status.git) each appear exactly once in the rendered Project CRD — the
// sole v1alpha3 version block (Phase 40 removed the two prior schema-revision
// blocks; v1alpha3 is the only served+storage version). A dropped field in
// that block, or a stale regenerated chart, fails here without a cluster.
func TestHelmTideCRDsRenderBaseRef(t *testing.T) {
	chartDir := filepath.Join("..", "..", "..", "charts", "tide-crds")
	cmd := exec.Command("helm", "template", "tide-crds", chartDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helm template tide-crds failed: %v\n%s", err, out)
	}
	rendered := string(out)

	if got := strings.Count(rendered, "baseRef:"); got != 1 {
		t.Errorf("rendered tide-crds baseRef: occurrences = %d, want 1 (sole Project CRD version block); chart regenerated stale via `make helm-crds`?", got)
	}
	if got := strings.Count(rendered, "baseSHA:"); got != 1 {
		t.Errorf("rendered tide-crds baseSHA: occurrences = %d, want 1 (sole Project CRD version block)", got)
	}

	// The charset Pattern must survive the helmify pass in the version block.
	const wantPattern = `pattern: ^[A-Za-z0-9][A-Za-z0-9._+@/-]*$`
	if got := strings.Count(rendered, wantPattern); got != 1 {
		t.Errorf("rendered tide-crds baseRef pattern %q occurrences = %d, want 1 (charset validation pruned or stale)", wantPattern, got)
	}
}
