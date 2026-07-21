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
	"os/exec"
	"path/filepath"
	"strings"
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

// TestHelmDeploymentTemplateRendersVerifierModelEnv pins the CFG-01/D-02
// chart-tier contract for the verifier model scalar: the manager Deployment
// must render a TIDE_VERIFIER_MODEL env entry sourced from
// .Values.subagent.verify.model. This is a static grep against the committed
// template (no cluster, no helm binary) guarding the
// phase53-verify-model-env-injected augment block against a future helmify
// regeneration silently dropping it (the v1.0.8 release-cascade lesson).
func TestHelmDeploymentTemplateRendersVerifierModelEnv(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "charts", "tide", "templates", "deployment.yaml"))
	if err != nil {
		t.Fatalf("read deployment template: %v", err)
	}

	for _, want := range []string{
		"TIDE_VERIFIER_MODEL",
		".Values.subagent.verify.model",
	} {
		if !bytes.Contains(data, []byte(want)) {
			t.Fatalf("deployment template must render %q (CFG-01/D-02 verifier-model env; regenerate via `make helm-controller` if the augment block was dropped)", want)
		}
	}
}

// TestHelmDeploymentTemplateVerifyPostureInstallVsUpgrade pins the CFG-02/D-05
// install-vs-upgrade posture contract via real `helm template` invocations
// (no cluster — `lookup` always returns empty under `helm template`, so the
// posture-marker ConfigMap's guard collapses to a pure IsInstall/IsUpgrade
// check, RESEARCH.md Finding 1). CI pins helm v3.16.3, which has --is-upgrade.
//
// Every failure message names `make helm-controller` as the regeneration fix
// (the chart is generated from hack/helm/, never hand-edited — Pitfall 1).
func TestHelmDeploymentTemplateVerifyPostureInstallVsUpgrade(t *testing.T) {
	chartDir := filepath.Join("..", "..", "..", "charts", "tide")

	t.Run("plain install renders verify-levels-json and the posture marker", func(t *testing.T) {
		cmd := exec.Command("helm", "template", "tide", chartDir)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("helm template failed: %v\n%s", err, out)
		}
		outStr := string(out)
		if !strings.Contains(outStr, "--verify-levels-json") {
			t.Fatalf("plain install render (IsInstall=true, no marker) must contain --verify-levels-json; regenerate via `make helm-controller` if the augment block was dropped. Got:\n%s", outStr)
		}
		if !strings.Contains(outStr, "tide-verify-posture") {
			t.Fatalf("plain install render must contain the tide-verify-posture marker ConfigMap; regenerate via `make helm-controller` if the augment block was dropped. Got:\n%s", outStr)
		}
	})

	t.Run("is-upgrade renders neither verify-levels-json nor the marker", func(t *testing.T) {
		cmd := exec.Command("helm", "template", "tide", chartDir, "--is-upgrade")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("helm template --is-upgrade failed: %v\n%s", err, out)
		}
		outStr := string(out)
		if strings.Contains(outStr, "--verify-levels-json") {
			t.Fatalf("--is-upgrade render (no marker lookup possible, IsInstall=false) must NOT contain --verify-levels-json (CFG-02 least-surprise contract). Got:\n%s", outStr)
		}
		if strings.Contains(outStr, "tide-verify-posture") {
			t.Fatalf("--is-upgrade render must NOT render the tide-verify-posture marker ConfigMap. Got:\n%s", outStr)
		}
	})

	t.Run("explicit posture=enabled overrides is-upgrade", func(t *testing.T) {
		cmd := exec.Command("helm", "template", "tide", chartDir,
			"--is-upgrade", "--set", "subagent.verify.posture=enabled")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("helm template failed: %v\n%s", err, out)
		}
		outStr := string(out)
		if !strings.Contains(outStr, "--verify-levels-json") {
			t.Fatalf("subagent.verify.posture=enabled must force --verify-levels-json ON even under --is-upgrade (explicit override beats upgrade semantics, D-05). Got:\n%s", outStr)
		}
	})

	t.Run("explicit posture=disabled overrides install", func(t *testing.T) {
		cmd := exec.Command("helm", "template", "tide", chartDir,
			"--set", "subagent.verify.posture=disabled")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("helm template failed: %v\n%s", err, out)
		}
		outStr := string(out)
		if strings.Contains(outStr, "--verify-levels-json") {
			t.Fatalf("subagent.verify.posture=disabled must force --verify-levels-json OFF even on a plain install (explicit override beats install semantics, D-05). Got:\n%s", outStr)
		}
	})

	// WR-06 (Phase 53 code review): every posture value outside the
	// auto|enabled|disabled enum used to silently resolve to "auto" — a
	// "disable"/"Disabled"/"off" typo turned the spend-bearing verifier tier
	// ON, the opposite of the operator's intent. The template now render-fails
	// loudly (`fail`), mirroring ParseVerifyLevelDefaults' reject-unknown-
	// loudly discipline one layer down (T-53-03).
	t.Run("posture typo render-fails loudly instead of failing open to auto", func(t *testing.T) {
		for _, typo := range []string{"disable", "Disabled", "off"} {
			cmd := exec.Command("helm", "template", "tide", chartDir,
				"--set-string", "subagent.verify.posture="+typo)
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("subagent.verify.posture=%q must render-fail (fail-closed, WR-06), but the render succeeded", typo)
			}
			if !strings.Contains(string(out), "subagent.verify.posture must be auto|enabled|disabled") {
				t.Fatalf("posture=%q render failure must carry the enum message; got:\n%s", typo, out)
			}
		}
	})
}

// TestHelmDeploymentTemplateMarkerDerefIsNilSafe pins the WR-03 (Phase 53
// code review) regression class: the ARGS53 posture block must read the
// tide-verify-posture marker's data.posture through a `dig` chain, never a
// direct `$verifyMarker.data.posture` deref. A marker ConfigMap that EXISTS
// without a `data` map (an operator `kubectl create configmap
// tide-verify-posture`, or a patch that strips `data` — T-53-10 explicitly
// anticipates hand-edits) nil-pointers the direct deref and bricks every
// subsequent `helm upgrade` of the release with an opaque template error.
// `helm template` cannot exercise `lookup` (always empty without a cluster),
// so this is a static pin against the committed template — the live
// malformed-marker upgrade is covered by verify_posture_sticky_test.go's
// kind spec.
func TestHelmDeploymentTemplateMarkerDerefIsNilSafe(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "charts", "tide", "templates", "deployment.yaml"))
	if err != nil {
		t.Fatalf("read deployment template: %v", err)
	}

	if !bytes.Contains(data, []byte(`dig "data" "posture" "" ($verifyMarker | default dict)`)) {
		t.Fatal("deployment template must read the posture marker via the nil-safe dig chain (WR-03; regenerate via `make helm-controller` if the augment block was dropped)")
	}
	if bytes.Contains(data, []byte("$verifyMarker.data.posture")) {
		t.Fatal("deployment template must NOT deref $verifyMarker.data.posture directly — a marker ConfigMap without a data map nil-pointers the render and bricks every subsequent helm upgrade (WR-03)")
	}
}
