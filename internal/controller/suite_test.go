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

package controller

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

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
	"github.com/jsquirrelz/tide/internal/budget"
	webhookv1alpha2 "github.com/jsquirrelz/tide/internal/webhook/v1alpha2"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
	// +kubebuilder:scaffold:imports
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
	// registered via mgr.GetFieldIndexer(). Use mgrClient in reconcilers that
	// call MatchingFields on custom indexes (e.g., taskPlanRefIndexKey).
	mgrClient client.Client
)

// Phase 2: shared test values for TaskReconciler field injection.
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

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	var err error
	err = tideprojectv1alpha2.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// admissionregistration/v1 is required so envtest can install
	// ValidatingWebhookConfiguration objects from config/webhook (Plan 07).
	utilruntime.Must(admissionv1.AddToScheme(scheme.Scheme))

	// +kubebuilder:scaffold:scheme

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		// WebhookInstallOptions installs the ValidatingWebhookConfiguration
		// emitted by `make manifests` into the envtest apiserver and provisions
		// self-signed certs for the webhook server. Plan 07 / revision Warning
		// 9: webhook envtest folds into THIS shared BeforeSuite (single envtest
		// cold-start; TEST-01 budget protection).
		WebhookInstallOptions: envtest.WebhookInstallOptions{
			Paths: []string{filepath.Join("..", "..", "config", "webhook")},
		},
	}

	// Retrieve the first found binary directory to allow running tests from IDEs
	if getFirstFoundEnvTestBinaryDir() != "" {
		testEnv.BinaryAssetsDirectory = getFirstFoundEnvTestBinaryDir()
	}

	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	// Start a webhook server bound to the envtest-provisioned host/port/certDir
	// so the cluster's ValidatingWebhookConfiguration (installed above) can
	// reach the in-process webhook handlers. Plan 07 / revision Warning 9: this
	// runs inside the controller suite — no second envtest cold-start.
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

	// Register webhooks (Plan 07 Task 1 scaffolding; Plan 11 fills the body).
	// SetupPlanWebhookWithManager now accepts the cluster-default file-touch mode
	// (Phase 2 — Plan 11). Pass "warn" as the cluster default per the Helm chart default.
	// Plan and Wave webhooks moved to v1alpha2 (Spring Tide breaking change, Plan 23-02).
	Expect(webhookv1alpha2.SetupPlanWebhookWithManager(mgr, "warn")).To(Succeed())
	Expect(webhookv1alpha2.SetupWaveWebhookWithManager(mgr)).To(Succeed())
	// Phase 04.1 P4.2 — Project AllowedRoutes denylist webhook (moved to v1alpha2, Plan 23-02).
	Expect(webhookv1alpha2.SetupProjectWebhookWithManager(mgr)).To(Succeed())

	// mgrClient is the manager's cached client; supports custom field indexers.
	mgrClient = mgr.GetClient()

	// Phase 2: Register .spec.planRef field indexer.
	// This is shared by TaskReconciler (listSiblingTasks) and WaveReconciler
	// (taskToWaveMapper via field-indexed list). Registered once here per
	// PATTERNS.md "Single envtest BeforeSuite".
	Expect(mgr.GetFieldIndexer().IndexField(context.Background(),
		&tideprojectv1alpha2.Task{},
		taskPlanRefIndexKey,
		func(obj client.Object) []string {
			task := obj.(*tideprojectv1alpha2.Task) //nolint:forcetypeassert
			return []string{task.Spec.PlanRef}
		},
	)).To(Succeed())

	// +kubebuilder:scaffold:webhook

	go func() {
		defer GinkgoRecover()
		err := mgr.Start(ctx)
		Expect(err).NotTo(HaveOccurred())
	}()

	// Wait for the webhook server to be reachable over TLS before any spec
	// attempts to Create/Update objects (otherwise admission would fail with
	// a connection refused).
	dialer := &net.Dialer{Timeout: time.Second}
	addrPort := fmt.Sprintf("%s:%d", webhookInstallOptions.LocalServingHost, webhookInstallOptions.LocalServingPort)
	Eventually(func() error {
		conn, err := tls.DialWithDialer(dialer, "tcp", addrPort, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec // envtest uses self-signed cert
		if err != nil {
			return err
		}
		return conn.Close()
	}, 30*time.Second, time.Second).Should(Succeed())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	Eventually(func() error {
		return testEnv.Stop()
	}, time.Minute, time.Second).Should(Succeed())
})

// getFirstFoundEnvTestBinaryDir locates the first binary in the specified path.
// ENVTEST-based tests depend on specific binaries, usually located in paths set by
// controller-runtime. When running tests directly (e.g., via an IDE) without using
// Makefile targets, the 'BinaryAssetsDirectory' must be explicitly configured.
//
// This function streamlines the process by finding the required binaries, similar to
// setting the 'KUBEBUILDER_ASSETS' environment variable. To ensure the binaries are
// properly set up, run 'make setup-envtest' beforehand.
func getFirstFoundEnvTestBinaryDir() string {
	basePath := filepath.Join("..", "..", "bin", "k8s")
	entries, err := os.ReadDir(basePath)
	if err != nil {
		logf.Log.Error(err, "Failed to read directory", "path", basePath)
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}
