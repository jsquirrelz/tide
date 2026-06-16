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

// Package e2e holds the live-cluster end-to-end tests for TIDE.
//
// Two disjoint test surfaces live under `package e2e`:
//
//  1. Kubebuilder-scaffolded `TestE2E` in e2e_suite_test.go + e2e_test.go,
//     gated by `//go:build e2e`. Run via `make test-e2e`. No real Anthropic
//     API call — exercises controller deployment + metrics + webhook CAs.
//
//  2. Live-Claude nightly `TestLiveE2E` (in live_suite_entry_test.go) + the
//     Ginkgo Describe in live_claude_test.go, gated by `//go:build live_e2e`.
//     Run via `make test-e2e-live`. Makes ONE real Anthropic API call;
//     skipped by default; double-gated by build tag AND ANTHROPIC_API_KEY env.
//     The build tag uses an underscore (Go's build-constraint grammar
//     requires identifier-shaped tags; hyphens are illegal). The Makefile
//     target keeps the operator-friendly hyphenated name.
//
// This suite_test.go file is INTENTIONALLY un-tagged (compiled in every
// build of test/e2e/...) so the package always has at least one compilable
// .go file under NO build tags. golangci-lint typechecks the tree under no
// tags; without this anchor it would hit "build constraints exclude all Go
// files in test/e2e" and break the Lint gate. It carries the live-E2E suite
// state (vars + initLiveE2ESuite/teardownLiveE2ESuite/resolveKubeconfigPath
// helpers) but NO Test entry point of its own — under no tags `go test`
// reports "no tests to run" / ok, which is correct.
//
// The RunSpecs entry point `TestLiveE2E` lives in live_suite_entry_test.go
// behind `//go:build live_e2e`, deliberately separated so it is NOT compiled
// into the kind_e2e binary alongside TestKindE2E's own RunSpecs (two RunSpecs
// callers in one binary trips Ginkgo's "Rerunning Suite" guard — Failure 7).
//
// When `-tags=live_e2e` is set, live_claude_test.go contributes its Describe
// block to this same `package e2e`; the Describe registers a spec which
// TestLiveE2E's RunSpecs invocation will pick up.
//
// Prerequisite: a pre-existing Kubernetes cluster with TIDE installed
// (helm install tide ./charts/tide -n tide-system). Unlike the Layer B
// integration suite in test/integration/kind/, this E2E suite does NOT
// spin up a kind cluster — that's the operator's responsibility per
// docs/live-e2e.md "Nightly CI Recipe".
package e2e

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
)

const (
	// liveE2ETestTimeout bounds the BeforeSuite + spec execution context.
	// Live Claude milestone runs are O(minutes); 15m matches the Makefile
	// `test-e2e-live` target's `-timeout=15m`. The in-test Eventually
	// timeouts (10m → 13m) fit comfortably inside this envelope.
	liveE2ETestTimeout = 15 * time.Minute //nolint:unused // consumed only under -tags=live_e2e (live_claude_test.go)

	// liveE2EControllerNamespace is the namespace TIDE installs into.
	// Mirrors `kindControllerNamespace` in test/integration/kind/suite_test.go.
	liveE2EControllerNamespace = "tide-system" //nolint:unused // consumed only under -tags=live_e2e (live_claude_test.go)
)

var (
	// liveE2EClient is the kube client wired by the BeforeSuite in
	// live_claude_test.go (only when `-tags=live_e2e` is active).
	// Without the tag, this stays nil — TestLiveE2E runs zero specs
	// and exits cleanly.
	liveE2EClient client.Client //nolint:unused // consumed only under -tags=live_e2e (live_claude_test.go)

	// liveE2ECtx carries the BeforeSuite timeout to every Eventually
	// inside the live spec.
	liveE2ECtx    context.Context    //nolint:unused // consumed only under -tags=live_e2e
	liveE2ECancel context.CancelFunc //nolint:unused // consumed only under -tags=live_e2e

	// liveE2EKubeconfigPath is the path to the kubeconfig in use.
	// Defaults to ~/.kube/config (via KUBECONFIG env or clientcmd defaults).
	liveE2EKubeconfigPath string //nolint:unused // consumed only under -tags=live_e2e
)

// initLiveE2ESuite is invoked by live_claude_test.go's BeforeSuite (which
// itself only registers when `-tags=live_e2e` is set). Factored out so the
// non-tagged suite_test.go stays Ginkgo-symbol-free at the package level —
// no var _ = BeforeSuite(...) here.
//
// Always-compiled helper. Safe to leave un-tagged: it's only CALLED from
// tagged code, never registered as a Ginkgo node directly.
//
// Skip-on-missing-creds is enforced at the SUITE level (before any kube
// client is built) — without ANTHROPIC_API_KEY, the BeforeSuite Skips and
// the whole suite reports "Ran 0 of 0 Specs". This is the second of the
// three live-E2E gates (build tag → env → budget cap).
func initLiveE2ESuite() { //nolint:unused // called only from live_claude_test.go under -tags=live_e2e
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		Skip("ANTHROPIC_API_KEY not set — skipping live E2E (T-311 / T-312 gate)")
	}

	liveE2ECtx, liveE2ECancel = context.WithTimeout(context.Background(), liveE2ETestTimeout)

	By("Registering TIDE CRD types in the runtime scheme")
	Expect(tideprojectv1alpha2.AddToScheme(scheme.Scheme)).To(Succeed())

	By("Resolving kubeconfig path (KUBECONFIG env or ~/.kube/config)")
	liveE2EKubeconfigPath = resolveKubeconfigPath()

	By("Building k8s client from kubeconfig")
	cfg, err := clientcmd.BuildConfigFromFlags("", liveE2EKubeconfigPath)
	Expect(err).NotTo(HaveOccurred(), "Live E2E requires a working kubeconfig — see docs/live-e2e.md")
	liveE2EClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())

	GinkgoWriter.Println("Live Claude E2E suite ready; kubeconfig: " + liveE2EKubeconfigPath)
}

// teardownLiveE2ESuite is invoked from live_claude_test.go's AfterSuite.
func teardownLiveE2ESuite() { //nolint:unused // called only from live_claude_test.go under -tags=live_e2e
	if liveE2ECancel != nil {
		liveE2ECancel()
	}
}

// resolveKubeconfigPath honors KUBECONFIG env, then falls back to ~/.kube/config.
func resolveKubeconfigPath() string { //nolint:unused // called only from initLiveE2ESuite under -tags=live_e2e
	if kc := os.Getenv("KUBECONFIG"); kc != "" {
		return kc
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".kube", "config")
}
