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
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestHelmDeploymentTemplateRendersVerifierEnv pins the CFG-01 chart-tier
// contract: the manager Deployment must render TIDE_VERIFIER_IMAGE sourced
// from .Values.images.tideLanggraphVerifier.repository/.tag. This env name
// byte-matches cmd/manager/main.go's envOrDefault read and feeds
// TaskReconciler.VerifierImage — the chart tier of the Task loop's read-only
// LangGraph verifier image (Phase 51 the Task loop).
//
// This is a plain go-test (no cluster, no helm binary): it asserts against
// the committed template so a helmify regeneration that drops the
// phase53-verifier-image-env-injected augment block fails CI — the exact
// dropped-augment-block drift class the v1.0.8 release cascade surfaced.
func TestHelmDeploymentTemplateRendersVerifierEnv(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "charts", "tide", "templates", "deployment.yaml"))
	if err != nil {
		t.Fatalf("read deployment template: %v", err)
	}

	for _, want := range []string{
		"TIDE_VERIFIER_IMAGE",
		".Values.images.tideLanggraphVerifier.repository",
		".Values.images.tideLanggraphVerifier.tag",
	} {
		if !bytes.Contains(data, []byte(want)) {
			t.Fatalf("deployment template must render %q (CFG-01 verifier-image env; regenerate via `make helm-controller` if the augment block was dropped)", want)
		}
	}
}
