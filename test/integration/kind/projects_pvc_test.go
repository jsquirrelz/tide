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
	"path/filepath"
	"slices"
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

func TestHelmDeploymentTemplateRendersManagerPodAnnotations(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "charts", "tide", "templates", "deployment.yaml"))
	if err != nil {
		t.Fatalf("read deployment template: %v", err)
	}
	if !bytes.Contains(data, []byte(".Values.controllerManager.manager.podAnnotations")) {
		t.Fatal("deployment template must render controllerManager.manager.podAnnotations")
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
