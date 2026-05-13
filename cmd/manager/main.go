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
	"flag"
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
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	"github.com/jsquirrelz/tide/internal/config"
	"github.com/jsquirrelz/tide/internal/controller"
	"github.com/jsquirrelz/tide/internal/pool"
	webhookv1alpha1 "github.com/jsquirrelz/tide/internal/webhook/v1alpha1"
)

// preChargeTimeout bounds the POOL-02 PreCharge call at Manager startup.
// Best-effort — if the apiserver List blocks past this deadline the call
// returns context.DeadlineExceeded and we log non-fatally and continue.
const preChargeTimeout = 30 * time.Second

func main() {
	var configPath string
	var leaderElect bool
	var watchNamespace string
	flag.StringVar(&configPath, "config", "/etc/tide/config.yaml", "Path to runtime config YAML")
	flag.BoolVar(&leaderElect, "leader-elect", true, "Enable leader election (CTRL-03)")
	flag.StringVar(&watchNamespace, "watch-namespace", "", "Restrict watches to this namespace (AUTH-02). Empty = all namespaces.")
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

	// 6. Register all six reconcilers (CTRL-01).
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
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Task")
		os.Exit(1)
	}

	// 7. Register both webhooks (CRD-04, CRD-05).
	// defaultFileTouchMode is the cluster-level file-touch validation mode (Plan 11 / D-E3).
	// "warn" is the safe Helm-chart default — operators opt into "strict" via Project.Spec
	// or Plan annotation. A future Helm value (e.g. --set planAdmission.fileTouchMode=strict)
	// will plumb through config; for Phase 2 the cluster default is hard-coded to "warn".
	const defaultFileTouchMode = "warn"
	if err := webhookv1alpha1.SetupPlanWebhookWithManager(mgr, defaultFileTouchMode); err != nil {
		setupLog.Error(err, "unable to create webhook", "kind", "Plan")
		os.Exit(1)
	}
	if err := webhookv1alpha1.SetupWaveWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "kind", "Wave")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
