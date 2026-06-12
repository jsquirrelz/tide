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
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type kindYAMLDoc struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Type       string `yaml:"type"`
	Metadata   struct {
		Name      string `yaml:"name"`
		Namespace string `yaml:"namespace"`
	} `yaml:"metadata"`
	Data map[string]string `yaml:"data"`
	Spec struct {
		AccessModes []string `yaml:"accessModes"`
		Resources   struct {
			Requests map[string]string `yaml:"requests"`
		} `yaml:"resources"`
		StorageClassName *string `yaml:"storageClassName"`
	} `yaml:"spec"`
}

func TestThreeTaskWaveFixtureIncludesProjectsPVC(t *testing.T) {
	docs := readKindYAMLDocs(t, filepath.Join("testdata", "three-task-wave.yaml"))

	var pvc *kindYAMLDoc
	for i := range docs {
		doc := &docs[i]
		if doc.Kind == "PersistentVolumeClaim" &&
			doc.Metadata.Name == "tide-projects" &&
			doc.Metadata.Namespace == "tide-int-test" {
			pvc = doc
			break
		}
	}
	if pvc == nil {
		t.Fatal("three-task wave fixture must create tide-projects PVC in tide-int-test")
	}
	assertProjectsPVCShape(t, pvc, "tide-int-test")
}

func TestProjectsPVCYAMLBuildsNamespaceLocalRWOClaim(t *testing.T) {
	docs := decodeKindYAMLDocs(t, []byte(projectsPVCYAML("caps-test")), "projectsPVCYAML")
	if len(docs) != 1 {
		t.Fatalf("projectsPVCYAML produced %d docs, want 1", len(docs))
	}
	pvc := &docs[0]
	if pvc.Kind != "PersistentVolumeClaim" {
		t.Fatalf("projectsPVCYAML kind = %q, want PersistentVolumeClaim", pvc.Kind)
	}
	if pvc.Metadata.Name != "tide-projects" {
		t.Fatalf("projectsPVCYAML name = %q, want tide-projects", pvc.Metadata.Name)
	}
	assertProjectsPVCShape(t, pvc, "caps-test")
}

func TestSigningKeySecretYAMLBuildsNamespaceLocalSecret(t *testing.T) {
	docs := decodeKindYAMLDocs(t, []byte(signingKeySecretYAML("credproxy-test", "dGVzdC1zaWduaW5nLWtleQ==")), "signingKeySecretYAML")
	if len(docs) != 1 {
		t.Fatalf("signingKeySecretYAML produced %d docs, want 1", len(docs))
	}
	secret := &docs[0]
	if secret.APIVersion != "v1" {
		t.Fatalf("Secret apiVersion = %q, want v1", secret.APIVersion)
	}
	if secret.Kind != "Secret" {
		t.Fatalf("signingKeySecretYAML kind = %q, want Secret", secret.Kind)
	}
	if secret.Metadata.Name != "tide-signing-key" {
		t.Fatalf("Secret name = %q, want tide-signing-key", secret.Metadata.Name)
	}
	if secret.Metadata.Namespace != "credproxy-test" {
		t.Fatalf("Secret namespace = %q, want credproxy-test", secret.Metadata.Namespace)
	}
	if secret.Type != "Opaque" {
		t.Fatalf("Secret type = %q, want Opaque", secret.Type)
	}
	if got, want := secret.Data["TIDE_SIGNING_KEY"], "dGVzdC1zaWduaW5nLWtleQ=="; got != want {
		t.Fatalf("Secret TIDE_SIGNING_KEY = %q, want %q", got, want)
	}
	if _, ok := secret.Data["ANTHROPIC_API_KEY"]; ok {
		t.Fatal("signing key secret must not include provider credentials")
	}
}

func TestHelmControllerArgsUpgradeInstallReusesExistingRelease(t *testing.T) {
	args := helmControllerArgs("/tmp/tide-chart", "nonce-123")
	if len(args) < 4 {
		t.Fatalf("helmControllerArgs length = %d, want at least 4", len(args))
	}
	if got, want := args[0], "upgrade"; got != want {
		t.Fatalf("helm arg 0 = %q, want %q", got, want)
	}
	if got, want := args[1], "--install"; got != want {
		t.Fatalf("helm arg 1 = %q, want %q", got, want)
	}
	if got, want := args[2], "tide"; got != want {
		t.Fatalf("helm release arg = %q, want %q", got, want)
	}
	if got, want := args[3], "/tmp/tide-chart"; got != want {
		t.Fatalf("helm chart arg = %q, want %q", got, want)
	}
	if !containsString(args, "--wait") {
		t.Fatal("helmControllerArgs must wait for rollout readiness")
	}
	if containsString(args, "--replace") {
		t.Fatal("helmControllerArgs must not use --replace; it fails when the release is currently deployed")
	}
}

func TestHelmControllerArgsForcesManagerRollout(t *testing.T) {
	args := helmControllerArgs("/tmp/tide-chart", "nonce-123")
	want := "controllerManager.manager.podAnnotations.tideproject\\.k8s/restart-nonce=nonce-123"
	if !containsString(args, "--set-string") || !containsString(args, want) {
		t.Fatalf("helmControllerArgs must set a manager pod restart nonce; args=%v", args)
	}
}

// TestHelmControllerArgsStubOptIn verifies the harness explicitly opts into the
// stub subagent via subagent.defaults.image (Phase 13 D-01/D-02). The chart no
// longer injects --subagent-image implicitly; test installs must declare the
// stub so CLAUDE_SUBAGENT_IMAGE points at the kind-loaded stub image rather than
// the real claude subagent (which is unavailable in the kind cluster).
func TestHelmControllerArgsStubOptIn(t *testing.T) {
	args := helmControllerArgs("/tmp/tide-chart", "nonce-123")
	want := "subagent.defaults.image=ghcr.io/jsquirrelz/tide-stub-subagent:test"
	if !containsString(args, want) {
		t.Fatalf("helmControllerArgs must opt into the stub via subagent.defaults.image; args=%v", args)
	}
}

func TestHelmDeploymentTemplateRendersManagerPodAnnotations(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "charts", "tide", "templates", "deployment.yaml"))
	if err != nil {
		t.Fatalf("read deployment template: %v", err)
	}
	if !bytes.Contains(data, []byte(".Values.controllerManager.manager.podAnnotations")) {
		t.Fatal("deployment template must render controllerManager.manager.podAnnotations")
	}
}

// TestHelmDeploymentTemplateDropsSubagentImageFlag verifies the chart no longer
// injects --subagent-image as a hard-coded flag. Phase 13 D-01: the flag was
// silently forcing the stub image in every v1.0.0 install; it has been removed
// so production installs dispatch the real subagent via CLAUDE_SUBAGENT_IMAGE.
func TestHelmDeploymentTemplateDropsSubagentImageFlag(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "charts", "tide", "templates", "deployment.yaml"))
	if err != nil {
		t.Fatalf("read deployment template: %v", err)
	}
	if bytes.Contains(data, []byte("--subagent-image=")) {
		t.Fatal("deployment template must NOT contain --subagent-image= (Phase 13 D-01: flag dropped; subagent image flows via subagent.defaults.image → CLAUDE_SUBAGENT_IMAGE env)")
	}
}

// TestHelmDeploymentTemplateSubagentImageEnvFromDefaults verifies the chart
// sources CLAUDE_SUBAGENT_IMAGE from .Values.subagent.defaults.image (D-01)
// rather than the old images.claudeSubagent path that was wired pre-Phase 13.
func TestHelmDeploymentTemplateSubagentImageEnvFromDefaults(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "charts", "tide", "templates", "deployment.yaml"))
	if err != nil {
		t.Fatalf("read deployment template: %v", err)
	}
	if !bytes.Contains(data, []byte("CLAUDE_SUBAGENT_IMAGE")) {
		t.Fatal("deployment template must contain CLAUDE_SUBAGENT_IMAGE env var")
	}
	if !bytes.Contains(data, []byte(".Values.subagent.defaults.image")) {
		t.Fatal("deployment template must source CLAUDE_SUBAGENT_IMAGE from .Values.subagent.defaults.image (Phase 13 D-01)")
	}
}

// TestHelmDeploymentTemplateEmptyImageFailsRender verifies the required guard
// added for WR-04: rendering the deployment template with an empty
// subagent.defaults.image must fail with the named error message rather than
// silently producing the garbage value ":<appVersion>" (InvalidImageName at
// runtime — Phase 13 WR-04).
func TestHelmDeploymentTemplateEmptyImageFailsRender(t *testing.T) {
	chartDir := filepath.Join("..", "..", "..", "charts", "tide")
	cmd := exec.Command("helm", "template", "tide", chartDir,
		"--set", "subagent.defaults.image=",
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("helm template with empty subagent.defaults.image must fail (required guard); it succeeded instead")
	}
	const wantFragment = "subagent.defaults.image must be a non-empty image ref"
	if !strings.Contains(string(out), wantFragment) {
		t.Fatalf("helm template error output must contain %q; got:\n%s", wantFragment, out)
	}
}

// TestHelmDeploymentTemplateBudgetReserveDefaultArg verifies that helm template
// with default values renders --budget-reserve-per-dispatch-cents=100 in the
// manager args and does NOT render --pricing-overrides-json (D-02/D-05, Phase 14-05).
func TestHelmDeploymentTemplateBudgetReserveDefaultArg(t *testing.T) {
	chartDir := filepath.Join("..", "..", "..", "charts", "tide")
	cmd := exec.Command("helm", "template", "tide", chartDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, out)
	}
	outStr := string(out)
	const wantArg = "--budget-reserve-per-dispatch-cents=100"
	if !strings.Contains(outStr, wantArg) {
		t.Fatalf("default render must contain %q; got output (args section):\n%s",
			wantArg, outStr)
	}
	const notWantArg = "--pricing-overrides-json"
	if strings.Contains(outStr, notWantArg) {
		t.Fatalf("default render must NOT contain %q (pricing.overrides is empty by default); got:\n%s",
			notWantArg, outStr)
	}
}

// TestHelmDeploymentTemplatePricingOverridesArg verifies that helm template with
// pricing.overrides set renders --pricing-overrides-json containing the model key
// (D-02, Phase 14-05).
func TestHelmDeploymentTemplatePricingOverridesArg(t *testing.T) {
	chartDir := filepath.Join("..", "..", "..", "charts", "tide")
	cmd := exec.Command("helm", "template", "tide", chartDir,
		"--set", "pricing.overrides.claude-test-model.inputCentsPerMTok=300",
		"--set", "pricing.overrides.claude-test-model.outputCentsPerMTok=1500",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helm template with pricing.overrides failed: %v\n%s", err, out)
	}
	outStr := string(out)
	const wantKey = "claude-test-model"
	const wantFlag = "--pricing-overrides-json"
	if !strings.Contains(outStr, wantFlag) {
		t.Fatalf("render with pricing.overrides must contain %q; got:\n%s", wantFlag, outStr)
	}
	if !strings.Contains(outStr, wantKey) {
		t.Fatalf("render with pricing.overrides must contain model key %q; got:\n%s", wantKey, outStr)
	}
}

func readKindYAMLDocs(t *testing.T, path string) []kindYAMLDoc {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return decodeKindYAMLDocs(t, data, path)
}

func decodeKindYAMLDocs(t *testing.T, data []byte, source string) []kindYAMLDoc {
	t.Helper()
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var docs []kindYAMLDoc
	for {
		var doc kindYAMLDoc
		err := dec.Decode(&doc)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("decode %s: %v", source, err)
		}
		if doc.Kind == "" {
			continue
		}
		docs = append(docs, doc)
	}
	return docs
}

func containsString(values []string, want string) bool {
	return slices.Contains(values, want)
}

func assertProjectsPVCShape(t *testing.T, pvc *kindYAMLDoc, ns string) {
	t.Helper()

	if pvc.APIVersion != "v1" {
		t.Fatalf("PVC apiVersion = %q, want v1", pvc.APIVersion)
	}
	if pvc.Metadata.Namespace != ns {
		t.Fatalf("PVC namespace = %q, want %q", pvc.Metadata.Namespace, ns)
	}
	if pvc.Spec.StorageClassName != nil {
		t.Fatalf("PVC storageClassName = %q, want it omitted", *pvc.Spec.StorageClassName)
	}
	if got, want := pvc.Spec.AccessModes, []string{"ReadWriteOnce"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("PVC accessModes = %v, want %v", got, want)
	}
	if got, want := pvc.Spec.Resources.Requests["storage"], "1Gi"; got != want {
		t.Fatalf("PVC storage request = %q, want %q", got, want)
	}
}
