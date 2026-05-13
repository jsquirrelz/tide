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

// Command manager is the TIDE controller's entry point.
// Per CTRL-01 + CTRL-03 + CTRL-04 + POOL-01 + POOL-02 + BOOT-01.
package main

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	ctrlmgr "sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	"github.com/jsquirrelz/tide/internal/budget"
	"github.com/jsquirrelz/tide/internal/config"
	"github.com/jsquirrelz/tide/internal/controller"
	"github.com/jsquirrelz/tide/internal/dispatch/podjob"
	"github.com/jsquirrelz/tide/internal/pool"
	webhookv1alpha1 "github.com/jsquirrelz/tide/internal/webhook/v1alpha1"
)

// preChargeTimeout bounds the POOL-02 PreCharge call at Manager startup.
// Best-effort — if the apiserver List blocks past this deadline the call
// returns context.DeadlineExceeded and we log non-fatally and continue.
const preChargeTimeout = 30 * time.Second

// decodeSigningKeyFromEnv reads TIDE_SIGNING_KEY from the environment,
// base64-decodes it, and verifies the key is at least 32 bytes long.
// Fail-fast: returns an error (caller must os.Exit(1)) if the env var is
// missing, malformed, or too short (HARN-03 requirement).
//
// The Secret data key is TIDE_SIGNING_KEY (env-friendly — no dashes) so
// `envFrom: [{secretRef: {name: tide-signing-key}}]` on both the Manager
// Deployment and the credproxy sidecar auto-populates this env var directly
// (Blocker #1 fix — matches the Helm template data key in signing-secret.yaml).
func decodeSigningKeyFromEnv() ([]byte, error) {
	raw := os.Getenv("TIDE_SIGNING_KEY")
	if raw == "" {
		return nil, fmt.Errorf("TIDE_SIGNING_KEY env var is required (HARN-03)")
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("TIDE_SIGNING_KEY base64 decode: %w", err)
	}
	if len(key) < 32 {
		return nil, fmt.Errorf("TIDE_SIGNING_KEY too short: %d bytes (need >= 32)", len(key))
	}
	return key, nil
}

func main() {
	var configPath string
	var leaderElect bool
	var watchNamespace string
	// Phase 2 flags (Plan 12 wiring).
	var subagentImage string
	var credproxyImage string
	var defaultFileTouchMode string
	var rateLimitDefaultRPM int
	var rateLimitDefaultBurst int

	flag.StringVar(&configPath, "config", "/etc/tide/config.yaml", "Path to runtime config YAML")
	flag.BoolVar(&leaderElect, "leader-elect", true, "Enable leader election (CTRL-03)")
	flag.StringVar(&watchNamespace, "watch-namespace", "", "Restrict watches to this namespace (AUTH-02). Empty = all namespaces.")
	// Phase 2 flags — bound from Helm values.yaml via the controller Deployment args.
	flag.StringVar(&subagentImage, "subagent-image", "", "Image ref for the subagent container (images.stubSubagent in values.yaml)")
	flag.StringVar(&credproxyImage, "credproxy-image", "", "Image ref for the tide-credproxy sidecar (images.credProxy in values.yaml)")
	flag.StringVar(&defaultFileTouchMode, "default-file-touch-mode", "warn", "Cluster-level file-touch validation mode (planAdmission.fileTouchMode in values.yaml; warn|strict)")
	flag.IntVar(&rateLimitDefaultRPM, "rate-limit-default-rpm", 60, "Default requests-per-minute rate limit (rateLimits.defaults.requestsPerMinute in values.yaml)")
	flag.IntVar(&rateLimitDefaultBurst, "rate-limit-default-burst", 10, "Default burst size for rate-limit buckets (rateLimits.defaults.burst in values.yaml)")

	opts := zap.Options{Development: false}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	setupLog := ctrl.Log.WithName("setup")

	// 1. Load runtime config (CTRL-04).
	cfg, err := config.Load(configPath)
	if err != nil {
		setupLog.Error(err, "failed to load config", "path", configPath)
		os.Exit(1)
	}
	setupLog.Info("loaded config",
		"plannerConcurrency", cfg.PlannerConcurrency,
		"executorConcurrency", cfg.ExecutorConcurrency,
		"maxConcurrentReconciles", cfg.MaxConcurrentReconciles)

	// 2. Build scheme with v1alpha1 + corev1 + batchv1.
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(tidev1alpha1.AddToScheme(scheme))

	// 3. Construct the Manager (CTRL-01, CTRL-03).
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		LeaderElection:         leaderElect,
		LeaderElectionID:       "tide-controller-leader.tideproject.k8s",
		HealthProbeBindAddress: ":8081",
		Metrics:                metricsserver.Options{BindAddress: ":8080"},
		WebhookServer:          webhook.NewServer(webhook.Options{Port: 9443}),
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	// 4. Construct both parallelism pools (POOL-01).
	plannerPool := pool.New(cfg.PlannerConcurrency, "planner")
	executorPool := pool.New(cfg.ExecutorConcurrency, "executor")

	// 5. Pre-charge pools from live Jobs on restart (POOL-02).
	//    Best-effort with a 30-second deadline; errors are logged but non-fatal.
	preChargeCtx, preChargeCancel := context.WithTimeout(context.Background(), preChargeTimeout)
	defer preChargeCancel()
	if err := plannerPool.PreCharge(preChargeCtx, mgr.GetClient(), "tideproject.k8s/role=planner"); err != nil {
		setupLog.Error(err, "planner pool pre-charge failed (non-fatal)")
	}
	if err := executorPool.PreCharge(preChargeCtx, mgr.GetClient(), "tideproject.k8s/role=executor"); err != nil {
		setupLog.Error(err, "executor pool pre-charge failed (non-fatal)")
	}

	// 6. Phase 2 component wiring (Plan 12).
	//    a. HMAC signing key — fail-fast; running without it breaks HMAC validation (HARN-03).
	signingKey, err := decodeSigningKeyFromEnv()
	if err != nil {
		setupLog.Error(err, "signing key required (HARN-03)")
		os.Exit(1)
	}
	//    b. Budget store — in-process per-Secret-UID rate-limiter cache (D-D1).
	budgetStore := budget.NewStore()
	//    c. Rate-limit defaults from Helm values (rateLimits.defaults.* in values.yaml).
	defaults := budget.Limits{
		RequestsPerMinute: rateLimitDefaultRPM,
		BurstSize:         rateLimitDefaultBurst,
	}
	//    d. EnvelopeOut reader — reads from the shared tide-projects PVC at /workspaces
	//       (Blocker #2/#3 fix — single-shared-PVC + subPath architecture, RESEARCH.md OQ#2 RESOLVED).
	envReader := &podjob.FilesystemEnvelopeReader{WorkspaceRoot: "/workspaces"}

	// 7. Register all six reconcilers (CTRL-01).
	if err := (&controller.ProjectReconciler{
		Client:                  mgr.GetClient(),
		Scheme:                  mgr.GetScheme(),
		MaxConcurrentReconciles: cfg.MaxConcurrentReconciles.Project,
		WatchNamespace:          watchNamespace,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Project")
		os.Exit(1)
	}
	if err := (&controller.MilestoneReconciler{
		Client:                  mgr.GetClient(),
		Scheme:                  mgr.GetScheme(),
		MaxConcurrentReconciles: cfg.MaxConcurrentReconciles.Milestone,
		PlannerPool:             plannerPool,
		WatchNamespace:          watchNamespace,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Milestone")
		os.Exit(1)
	}
	if err := (&controller.PhaseReconciler{
		Client:                  mgr.GetClient(),
		Scheme:                  mgr.GetScheme(),
		MaxConcurrentReconciles: cfg.MaxConcurrentReconciles.Phase,
		PlannerPool:             plannerPool,
		WatchNamespace:          watchNamespace,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Phase")
		os.Exit(1)
	}
	if err := (&controller.PlanReconciler{
		Client:                  mgr.GetClient(),
		Scheme:                  mgr.GetScheme(),
		MaxConcurrentReconciles: cfg.MaxConcurrentReconciles.Plan,
		PlannerPool:             plannerPool,
		WatchNamespace:          watchNamespace,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Plan")
		os.Exit(1)
	}
	if err := (&controller.WaveReconciler{
		Client:                  mgr.GetClient(),
		Scheme:                  mgr.GetScheme(),
		MaxConcurrentReconciles: cfg.MaxConcurrentReconciles.Wave,
		ExecutorPool:            executorPool,
		WatchNamespace:          watchNamespace,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Wave")
		os.Exit(1)
	}
	if err := (&controller.TaskReconciler{
		Client:                  mgr.GetClient(),
		Scheme:                  mgr.GetScheme(),
		MaxConcurrentReconciles: cfg.MaxConcurrentReconciles.Task,
		ExecutorPool:            executorPool,
		WatchNamespace:          watchNamespace,
		// Phase 2 fields (Plan 12 wiring).
		Budget:         budgetStore,
		Defaults:       defaults,
		SigningKey:     signingKey,
		SubagentImage:  subagentImage,
		CredproxyImage: credproxyImage,
		EnvReader:      envReader,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Task")
		os.Exit(1)
	}

	// 8. Register both webhooks (CRD-04, CRD-05).
	// defaultFileTouchMode drives cluster-level file-touch validation (Plan 11 / D-E3).
	// The Helm-chart default is "warn" (safe for fresh installs); operators opt in to
	// "strict" via --set planAdmission.fileTouchMode=strict which is passed through
	// the controller Deployment args to this --default-file-touch-mode flag.
	if err := webhookv1alpha1.SetupPlanWebhookWithManager(mgr, defaultFileTouchMode); err != nil {
		setupLog.Error(err, "unable to create webhook", "kind", "Plan")
		os.Exit(1)
	}
	if err := webhookv1alpha1.SetupWaveWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "kind", "Wave")
		os.Exit(1)
	}

	// 9. Register Phase 2 budget.PreCharge as a Manager Runnable (D-D1 / Pitfall C).
	//    Runs after Manager.Start completes leader-election + cache sync so the
	//    informer cache is warm when PreCharge calls client.List (plan comment: "uncached
	//    client" note in precharge.go is addressed by calling via the manager's Runnable
	//    path — the cache-backed client is warm by this point). Best-effort: errors are
	//    logged but non-fatal per Pitfall C (timestamps are not persisted per-Job, so
	//    a slight count over/under is accepted for v1).
	if err := mgr.Add(ctrlmgr.RunnableFunc(func(ctx context.Context) error {
		if err := budget.PreCharge(ctx, mgr.GetClient(), budgetStore, defaults, 60*time.Second); err != nil {
			setupLog.Error(err, "budget pre-charge failed (non-fatal, Pitfall C)")
		}
		return nil
	})); err != nil {
		setupLog.Error(err, "unable to register budget pre-charge runnable")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
