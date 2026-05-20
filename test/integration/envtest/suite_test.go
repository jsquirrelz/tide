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

// Package envtest_integration holds Layer A integration tests that exercise the
// Phase 2 reconcilers and admission webhook in-process via envtest.
//
// This is a SEPARATE test binary from internal/controller/ (which runs as part
// of `make test` with the TEST-01 30s budget). Run via:
//
//	make test-int-fast
//
// Layer A covers: admission webhook (cycle/file-touch), indegree recomputation,
// attempt counters, owner-cascade, Wave roll-up, init Job lifecycle, budget cap,
// and rate-limit storm absorption (AC #4).
package envtest_integration

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	"github.com/jsquirrelz/tide/internal/budget"
	controller "github.com/jsquirrelz/tide/internal/controller"
	"github.com/jsquirrelz/tide/internal/dispatch"
	"github.com/jsquirrelz/tide/internal/dispatch/podjob"
	webhookv1alpha1 "github.com/jsquirrelz/tide/internal/webhook/v1alpha1"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	ctx       context.Context
	cancel    context.CancelFunc
	testEnv   *envtest.Environment
	cfg       *rest.Config
	k8sClient client.Client
	// mgrClient is the manager's cached client; it supports custom field indexers
	// registered via mgr.GetFieldIndexer(). Use mgrClient in tests that call
	// MatchingFields on custom indexes (e.g., .spec.planRef).
	mgrClient client.Client
)

// Phase 2: shared test values for reconciler field injection.
var (
	// testSigningKey is a 32-byte test signing key for HMAC token minting.
	testSigningKey = []byte("tide-test-signing-key-32-bytes!!")

	// testBudgetStore is the shared in-process Store used by dispatching tests.
	testBudgetStore = budget.NewStore()

	// testBudgetDefaults are the Helm-driven rate-limit defaults for tests.
	testBudgetDefaults = budget.Limits{RequestsPerMinute: 120, BurstSize: 10}

	// testSubagentImage and testCredproxyImage are placeholder images for tests.
	testSubagentImage  = "tide-stub-subagent:test"
	testCredproxyImage = "tide-credproxy:test"
)

// mapEnvReader is an in-memory EnvelopeReader implementation for tests.
// It maps task UID → EnvelopeOut; tests pre-populate it before reconciling.
type mapEnvReader struct {
	byUID map[string]pkgdispatch.EnvelopeOut
	errs  map[string]error
}

func newMapEnvReader() *mapEnvReader {
	return &mapEnvReader{
		byUID: make(map[string]pkgdispatch.EnvelopeOut),
		errs:  make(map[string]error),
	}
}

func (m *mapEnvReader) SetOut(taskUID string, out pkgdispatch.EnvelopeOut) {
	m.byUID[taskUID] = out
}

func (m *mapEnvReader) SetErr(taskUID string, err error) {
	m.errs[taskUID] = err
}

func (m *mapEnvReader) ReadOut(_ context.Context, _, taskUID string) (pkgdispatch.EnvelopeOut, error) {
	if err, ok := m.errs[taskUID]; ok {
		return pkgdispatch.EnvelopeOut{}, err
	}
	if out, ok := m.byUID[taskUID]; ok {
		return out, nil
	}
	return pkgdispatch.EnvelopeOut{}, fmt.Errorf("no envelope out for task UID %q", taskUID)
}

// stubDispatcher satisfies dispatch.Dispatcher so the reconciler's Dispatcher
// field is non-nil, enabling the Phase 2 dispatch seam without actual subagent calls.
type stubDispatcher struct{}

func (s *stubDispatcher) Run(_ context.Context, in pkgdispatch.EnvelopeIn) (pkgdispatch.EnvelopeOut, error) {
	return pkgdispatch.EnvelopeOut{TaskUID: in.TaskUID}, nil
}

var _ dispatch.Dispatcher = (*stubDispatcher)(nil)
var _ podjob.EnvelopeReader = (*mapEnvReader)(nil)

// TestIntegrationEnvtest is the entry point for the Layer A envtest suite.
func TestIntegrationEnvtest(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Integration Envtest Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	var err error
	err = tideprojectv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// admissionregistration/v1 is required so envtest can install
	// ValidatingWebhookConfiguration objects from config/webhook.
	utilruntime.Must(admissionv1.AddToScheme(scheme.Scheme))

	By("bootstrapping envtest environment for Layer A integration tests")
	testEnv = &envtest.Environment{
		// Path is three levels up from test/integration/envtest/
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		WebhookInstallOptions: envtest.WebhookInstallOptions{
			Paths: []string{filepath.Join("..", "..", "..", "config", "webhook")},
		},
	}

	// Locate pre-downloaded envtest binaries from KUBEBUILDER_ASSETS or bin/k8s/.
	if binDir := getEnvTestBinaryDir(); binDir != "" {
		testEnv.BinaryAssetsDirectory = binDir
	}

	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	// Start a webhook server bound to the envtest-provisioned host/port/certDir
	// so the cluster's ValidatingWebhookConfiguration can reach in-process handlers.
	webhookInstallOptions := &testEnv.WebhookInstallOptions
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:  scheme.Scheme,
		Metrics: metricsserver.Options{BindAddress: "0"},
		WebhookServer: webhook.NewServer(webhook.Options{
			Host:    webhookInstallOptions.LocalServingHost,
			Port:    webhookInstallOptions.LocalServingPort,
			CertDir: webhookInstallOptions.LocalServingCertDir,
		}),
		LeaderElection: false,
	})
	Expect(err).NotTo(HaveOccurred())

	// Register the Plan admission webhook (Phase 2 stateful form from Plan 11).
	// "warn" is the cluster default per the Helm chart default.
	Expect(webhookv1alpha1.SetupPlanWebhookWithManager(mgr, "warn")).To(Succeed())
	Expect(webhookv1alpha1.SetupWaveWebhookWithManager(mgr)).To(Succeed())
	// Phase 04.1 P4.2 — Project AllowedRoutes denylist webhook.
	Expect(webhookv1alpha1.SetupProjectWebhookWithManager(mgr)).To(Succeed())

	mgrClient = mgr.GetClient()

	// The .spec.planRef field indexer is registered by TaskReconciler.SetupWithManager
	// (see internal/controller/task_controller.go); registering it here would cause an
	// indexer conflict at BeforeSuite.

	// Wire all six reconcilers with Phase 2 field injections.
	Expect(newPhase2ReconcilersForTest(mgr)).To(Succeed())

	go func() {
		defer GinkgoRecover()
		err := mgr.Start(ctx)
		Expect(err).NotTo(HaveOccurred())
	}()

	// Wait for the webhook server to be reachable over TLS.
	dialer := &net.Dialer{Timeout: time.Second}
	addrPort := fmt.Sprintf("%s:%d", webhookInstallOptions.LocalServingHost, webhookInstallOptions.LocalServingPort)
	Eventually(func() error {
		conn, err := tls.DialWithDialer(dialer, "tcp", addrPort, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec // envtest self-signed cert
		if err != nil {
			return err
		}
		return conn.Close()
	}, 30*time.Second, time.Second).Should(Succeed())
})

var _ = AfterSuite(func() {
	By("tearing down envtest environment")
	cancel()
	Eventually(func() error {
		return testEnv.Stop()
	}, time.Minute, time.Second).Should(Succeed())
})

// newPhase2ReconcilersForTest registers all six Phase 2 reconcilers with the
// manager, injecting test-friendly stubs and shared test values.
func newPhase2ReconcilersForTest(mgr ctrl.Manager) error {
	envReader := newMapEnvReader()

	if err := (&controller.MilestoneReconciler{
		Client:         mgr.GetClient(),
		Scheme:         mgr.GetScheme(),
		EnvReader:      envReader,
		Dispatcher:     &stubDispatcher{},
		SubagentImage:  testSubagentImage,
		CredproxyImage: testCredproxyImage,
		SigningKey:      testSigningKey,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("MilestoneReconciler: %w", err)
	}

	if err := (&controller.PhaseReconciler{
		Client:         mgr.GetClient(),
		Scheme:         mgr.GetScheme(),
		Dispatcher:     &stubDispatcher{},
		SubagentImage:  testSubagentImage,
		CredproxyImage: testCredproxyImage,
		SigningKey:     testSigningKey,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("PhaseReconciler: %w", err)
	}

	if err := (&controller.PlanReconciler{
		Client:         mgrClient,
		Scheme:         mgr.GetScheme(),
		Dispatcher:     &stubDispatcher{},
		SubagentImage:  testSubagentImage,
		CredproxyImage: testCredproxyImage,
		SigningKey:     testSigningKey,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("PlanReconciler: %w", err)
	}

	if err := (&controller.TaskReconciler{
		Client: mgrClient,
		Scheme: mgr.GetScheme(),
		Deps: controller.TaskReconcilerDeps{
			Dispatcher:     &stubDispatcher{},
			Budget:         testBudgetStore,
			Defaults:       testBudgetDefaults,
			SigningKey:     testSigningKey,
			SubagentImage:  testSubagentImage,
			CredproxyImage: testCredproxyImage,
			EnvReader:      envReader,
		},
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("TaskReconciler: %w", err)
	}

	if err := (&controller.WaveReconciler{
		Client:     mgrClient,
		Scheme:     mgr.GetScheme(),
		Dispatcher: &stubDispatcher{},
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("WaveReconciler: %w", err)
	}

	if err := (&controller.ProjectReconciler{
		Client:                  mgr.GetClient(),
		Scheme:                  mgr.GetScheme(),
		Dispatcher:              &stubDispatcher{},
		MaxConcurrentReconciles: 1,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("ProjectReconciler: %w", err)
	}

	return nil
}

// getEnvTestBinaryDir locates the envtest binary directory from KUBEBUILDER_ASSETS
// env var or from the local bin/k8s/ directory (populated by `make setup-envtest`).
func getEnvTestBinaryDir() string {
	// KUBEBUILDER_ASSETS is set by the Makefile test-int / test-int-fast targets.
	if dir := os.Getenv("KUBEBUILDER_ASSETS"); dir != "" {
		return dir
	}

	// Fall back to bin/k8s/ (three levels up from test/integration/envtest/).
	basePath := filepath.Join("..", "..", "..", "bin", "k8s")
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}
