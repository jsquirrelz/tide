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

// TestHelmDeploymentTemplateRendersAgentIdentityEnv pins the SIGN-01 / D-03
// chart-tier contract: the manager Deployment must render TIDE_AGENT_NAME and
// TIDE_AGENT_EMAIL sourced from .Values.agent.name / .Values.agent.email. These
// env names byte-match cmd/manager/env.go's envOrDefault reads (Plan 36-02) and
// feed ProviderDefaults.AgentName/AgentEmail — the install-wide tier of the
// identity precedence chain (Project spec → chart agent.* → compiled default).
//
// This is a plain go-test (no cluster, no helm binary): it asserts against the
// committed template so a helmify regeneration that drops the
// phase36-agent-env-injected augment block fails CI — the exact drift class the
// CLAUDE.md Phase-7 warning describes (a dropped augment block that renders
// green in the Ginkgo summary but breaks the chart contract).
func TestHelmDeploymentTemplateRendersAgentIdentityEnv(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "charts", "tide", "templates", "deployment.yaml"))
	if err != nil {
		t.Fatalf("read deployment template: %v", err)
	}

	for _, want := range []string{
		"TIDE_AGENT_NAME",
		"TIDE_AGENT_EMAIL",
		".Values.agent.name",
		".Values.agent.email",
	} {
		if !bytes.Contains(data, []byte(want)) {
			t.Fatalf("deployment template must render %q (SIGN-01 / D-03 agent-identity env; regenerate via `make helm-controller` if the augment block was dropped)", want)
		}
	}
}
