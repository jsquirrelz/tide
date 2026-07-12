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

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/internal/budget"
	"github.com/jsquirrelz/tide/internal/config"
	"github.com/jsquirrelz/tide/internal/controller"
	"github.com/jsquirrelz/tide/internal/credproxy"
	"github.com/jsquirrelz/tide/internal/dispatch/podjob"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"

	// Phase 4 D-O2: blank-import the central metric registry so its init()
	// registers all 7 Phase 4 counters/histograms on
	// sigs.k8s.io/controller-runtime/pkg/metrics.Registry at Manager start.
	// The registry is then surfaced via the existing --metrics-bind-address
	// flag (default :8443). See internal/metrics/doc.go for the inventory.
	_ "github.com/jsquirrelz/tide/internal/metrics"
	// Phase 4 D-O3: OTel TracerProvider lifecycle. NewTracerProvider returns
	// a no-op TP when OTEL_EXPORTER_OTLP_ENDPOINT is unset so `kind` clusters
	// without a collector still work; otherwise constructs the real SDK TP
	// with the OTLP gRPC exporter. The deferred Shutdown flushes the batch
	// span processor before the binary exits.
	"github.com/jsquirrelz/tide/internal/otelinit"
	"github.com/jsquirrelz/tide/internal/pool"

	// All webhooks serve v1alpha3 (Phase 40 crank, plan 40-03) — the prior
	// schema-revision package was renamed, not kept alongside it (D-01
	// reinstall-only: no transitional multi-version webhook registration).
	webhookv1alpha3 "github.com/jsquirrelz/tide/internal/webhook/v1alpha3"
	// +kubebuilder:scaffold:imports
)

// preChargeTimeout bounds the POOL-02 PreCharge call at Manager startup.
// Best-effort — if the apiserver List blocks past this deadline the call
// returns context.DeadlineExceeded and we log non-fatally and continue.
const preChargeTimeout = 30 * time.Second

// decodeSigningKeyFromEnv reads TIDE_SIGNING_KEY from the environment and
// returns the raw bytes after verifying the key is at least 32 bytes long.
// Fail-fast: returns an error (caller must os.Exit(1)) if the env var is
// missing or too short (HARN-03 requirement).
//
// WR-04: the Helm template renders the data key as
// `randAlphaNum 64 | b64enc | quote`. K8s base64-decodes Secret `data:`
// values once on its way to envFrom, so by the time this binary sees
// TIDE_SIGNING_KEY the value is the plaintext 64-char alphanum string —
// already the signing key bytes. An additional base64.DecodeString
// (previously here) would treat the alphanum as base64 and produce a
// truncated, partly-random byte slice whose length and entropy were
// undefined. Use the env value directly.
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
	key := []byte(raw)
	if len(key) < 32 {
		return nil, fmt.Errorf("TIDE_SIGNING_KEY too short: %d bytes (need >= 32)", len(key))
	}
	return key, nil
}

// hmacSelfTest signs a probe token with the manager's in-process signing
// key and verifies the round-trip via the same credproxy.Sign +
// credproxy.Verify code path the dispatcher uses. Catches in-process key
// corruption (e.g. the historical env-var-decode regression where the
// Helm-rendered alphanum key was double-decoded as base64 and silently
// truncated) at boot, before the first dispatch fails with a confusing
// "auth" error per task.
//
// WR-11 scope note: this self-test cannot detect Manager↔credproxy chart
// misconfiguration — credproxy runs as a sidecar of dispatched task Pods
// and is not reachable at manager-startup. What it CAN prove is that the
// key bytes the manager will hand to the dispatcher do round-trip
// correctly through the canonical Sign/Verify pair. A future plan that
// adds a reachable health endpoint on credproxy can extend this with a
// real on-wire probe.
func hmacSelfTest(signingKey []byte) error {
	const probeTaskUID = "manager-startup-probe"
	token, err := credproxy.Sign(signingKey, probeTaskUID, time.Minute)
	if err != nil {
		return fmt.Errorf("hmac self-test: Sign failed: %w", err)
	}
	if err := credproxy.Verify(signingKey, token, probeTaskUID); err != nil {
		return fmt.Errorf("hmac self-test: Verify failed (signing-key integrity broken): %w", err)
	}
	return nil
}

func main() {
	var configPath string
	var leaderElect bool
	var watchNamespace string
	var metricsAddr string
	var webhookCertPath string
	// Phase 2 flags (Plan 12 wiring).
	var subagentImage string
	var credproxyImage string
	var defaultFileTouchMode string
	var rateLimitDefaultRPM int
	var rateLimitDefaultBurst int
	// Phase 14 flags (Plan 14-05 wiring — D-02/D-05 operator tuning).
	var pricingOverridesJSON string
	var budgetReservePerDispatchCents int64

	flag.StringVar(&configPath, "config", "/etc/tide/config.yaml", "Path to runtime config YAML")
	flag.BoolVar(&leaderElect, "leader-elect", true, "Enable leader election (CTRL-03)")
	flag.StringVar(&watchNamespace, "watch-namespace", "",
		"Restrict watches to this namespace (AUTH-02). Empty = all namespaces.")
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8443",
		"Bind address for the metrics endpoint (controllerManager.manager.args in values.yaml)")
	flag.StringVar(&webhookCertPath, "webhook-cert-path", "/tmp/k8s-webhook-server/serving-certs",
		"Path to the webhook serving cert directory "+
			"(controller-runtime CertDir; controllerManager.manager.args in values.yaml)")
	// Phase 2 flags — bound from Helm values.yaml via the controller Deployment args.
	flag.StringVar(&subagentImage, "subagent-image", "",
		"Helm default-tier image for the subagent container. CRD fields (Spec.Subagent.Image, "+
			"Spec.Subagent.Levels.<level>.Image) take precedence over this flag (Phase 13 DISPATCH-01). "+
			"When set, overrides CLAUDE_SUBAGENT_IMAGE env so the unchanged chart (still passing "+
			"--subagent-image until 13-03) dispatches the stub in kind. "+
			"(images.stubSubagent in values.yaml)")
	flag.StringVar(&credproxyImage, "credproxy-image", "",
		"Image ref for the tide-credproxy sidecar (images.credProxy in values.yaml)")
	flag.StringVar(&defaultFileTouchMode, "default-file-touch-mode", "warn",
		"Cluster-level file-touch validation mode (planAdmission.fileTouchMode in values.yaml; warn|strict)")
	flag.IntVar(&rateLimitDefaultRPM, "rate-limit-default-rpm", 60,
		"Default requests-per-minute rate limit (rateLimits.defaults.requestsPerMinute in values.yaml)")
	flag.IntVar(&rateLimitDefaultBurst, "rate-limit-default-burst", 10,
		"Default burst size for rate-limit buckets (rateLimits.defaults.burst in values.yaml)")
	// Phase 14 flags — operator billing tuning. Flag-over-config chosen for
	// consistency with --rate-limit-default-rpm (RESEARCH Open Question 1 resolved:
	// flag mirrors the established Helm→flag pattern for per-operator tunables).
	flag.StringVar(&pricingOverridesJSON, "pricing-overrides-json", "",
		"JSON map of model-ID to price overrides in cents/MTok (Helm pricing.overrides, D-02); validated at startup")
	flag.Int64Var(&budgetReservePerDispatchCents, "budget-reserve-per-dispatch-cents", 100,
		"Flat per-dispatch reservation estimate in cents (Helm budget.reservePerDispatchCents, D-05); 0 disables pre-charge")

	// Phase 3 plan 03-09 — Helm env-var wiring. Set by the controller Deployment
	// env block from charts/tide/values.yaml. Helpers in cmd/manager/env.go.
	//
	//   TIDE_PUSH_IMAGE              → ProjectReconciler.TidePushImage
	//   CLAUDE_SUBAGENT_IMAGE        → ProviderDefaults.Image (D-C2 last fallback)
	//   TIDE_DEFAULT_MODEL_MILESTONE → ProviderDefaults.Models["milestone"]  D-C4
	//   TIDE_DEFAULT_MODEL_PHASE     → ProviderDefaults.Models["phase"]      D-C4
	//   TIDE_DEFAULT_MODEL_PLAN      → ProviderDefaults.Models["plan"]       D-C4
	//   TIDE_DEFAULT_MODEL_TASK      → ProviderDefaults.Models["task"]       D-C4
	//   TIDE_LEADER_LEASE_SECONDS    → ctrl.Options.LeaseDuration            D-D1
	//   TIDE_LEADER_RENEW_SECONDS    → ctrl.Options.RenewDeadline            D-D1
	//   TIDE_LEADER_RETRY_SECONDS    → ctrl.Options.RetryPeriod              D-D1
	// PROD_OVERRIDE_REQUIRED: dev default; production deployments must override
	// via Helm values.tidePushImage (which sets TIDE_PUSH_IMAGE on the controller
	// env). The :v0.1.0-dev tag tracks main and is NOT a release-stable placeholder.
	tidePushImage := envOrDefault("TIDE_PUSH_IMAGE", "ghcr.io/jsquirrelz/tide-push:v0.1.0-dev")

	// TIDE_REPORTER_IMAGE → four reconcilers' ReporterImage field (Phase 09 plan 09-06).
	// The tide-reporter reader Job reads out.json from the project-namespace PVC and
	// materializes child CRDs via the K8s API (Option C architecture). When empty,
	// the spawn site logs an Info message and skips Job creation (mirrors TIDE_PUSH_IMAGE
	// skip in boundary_push.go:80-88). Override via Helm values.images.tideReporter.
	// PROD_OVERRIDE_REQUIRED: dev default; production deployments must override via Helm.
	reporterImage := envOrDefault("TIDE_REPORTER_IMAGE", "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev")

	// TIDE_IMPORT_IMAGE → ImportController's ImportImage field (Phase 28 IMPORT-01).
	// The tide-import Job re-keys salvaged envelope trees from old-UID to new-UID paths.
	// When empty, ImportController skips Job creation (mirrors TIDE_REPORTER_IMAGE skip).
	// Injected by the chart (Phase 28 plan 01 tideImport block). PROD_OVERRIDE_REQUIRED.
	importImage := envOrDefault("TIDE_IMPORT_IMAGE", "ghcr.io/jsquirrelz/tide-import:v0.1.0-dev")

	// Single source for the cluster-wide workspace PVC name, read by both the
	// ProjectReconciler and the ImportReconciler so they cannot skew. The chart
	// exposes this as workspaces.pvc.name; the import Job mounts this PVC, so a
	// rename that reaches only one reconciler would leave the Job mounting a
	// non-existent claim and the pod stuck Pending (WR-03).
	// TODO: wire TIDE_WORKSPACES_PVC_NAME through the chart from
	// workspaces.pvc.name so an operator override propagates to both reconcilers.
	sharedPVCName := envOrDefault("TIDE_WORKSPACES_PVC_NAME", "tide-projects")

	// PROD_OVERRIDE_REQUIRED: dev default; production deployments must override
	// via Helm values.claudeSubagentImage (which sets CLAUDE_SUBAGENT_IMAGE on the
	// controller env). The :v0.1.0-dev tag tracks main and is NOT a release-stable placeholder.
	claudeSubagentImage := envOrDefault("CLAUDE_SUBAGENT_IMAGE", "ghcr.io/jsquirrelz/tide-claude-subagent:v0.1.0-dev")
	helmProviderDefaults := tideHelmProviderDefaults(claudeSubagentImage)
	leaderLease, leaderRenew, leaderRetry := resolveLeaderElectionTiming()

	opts := zap.Options{Development: false}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	// Phase 14 startup validation (T-14-01 / ASVS V5): validate flags before
	// constructing any component. Fail-fast: running with silently-broken billing
	// config is worse than failing to start (operator sees the error immediately).
	if pricingOverridesJSON != "" {
		if _, err := pkgdispatch.ParsePricingOverrides(pricingOverridesJSON); err != nil {
			fmt.Fprintf(os.Stderr, "invalid --pricing-overrides-json: %v\n", err)
			os.Exit(1)
		}
	}
	if budgetReservePerDispatchCents < 0 {
		fmt.Fprintf(os.Stderr, "--budget-reserve-per-dispatch-cents must be >= 0 (got %d)\n", budgetReservePerDispatchCents)
		os.Exit(1)
	}

	// Phase 13 DISPATCH-01 compatibility shim: if --subagent-image was passed (non-empty),
	// it overrides helmProviderDefaults.Image so the unchanged chart (still passing
	// --subagent-image={{ .Values.images.stubSubagent.* }} until plan 13-03 drops the arg)
	// continues to dispatch the stub in kind and Layer B. The flag is now the default tier
	// only — CRD fields (Spec.Subagent.Image, Spec.Subagent.Levels.<level>.Image) always
	// beat it via resolveImage in each reconciler.
	if subagentImage != "" {
		helmProviderDefaults.Image = subagentImage
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	//nolint:logcheck // controller-runtime logr idiom; klogr LoggerWithName helper not adopted
	setupLog := ctrl.Log.WithName("setup")

	// Establish the manager's parent context early — used by both OTel
	// init below and mgr.Start at the bottom. ctrl.SetupSignalHandler
	// returns a context that is cancelled on SIGINT/SIGTERM so the
	// manager (and the batch span processor) shut down cleanly.
	signalCtx := ctrl.SetupSignalHandler()

	// Phase 4 D-O3: OTel TracerProvider boot. Returns a no-op TP when
	// OTEL_EXPORTER_OTLP_ENDPOINT is unset so `kind` clusters work
	// without an OTLP collector; otherwise builds the real SDK TP
	// with an OTLP gRPC exporter. NewTracerProvider also calls
	// otel.SetTracerProvider so reconciler code using otel.Tracer(...)
	// resolves to the right TP. Pitfall 24: provider.go does NOT pass
	// WithSampler — OTEL_TRACES_SAMPLER env var governs.
	tp, otelShutdown, err := otelinit.NewTracerProvider(signalCtx)
	if err != nil {
		setupLog.Error(err, "otel init failed")
		os.Exit(1)
	}
	_ = tp // captured by the global otel handle; named here only so it
	// remains visible if a future caller wants to thread it explicitly.
	defer func() {
		// Bounded shutdown — the batch processor flushes outstanding
		// spans to the collector before the process exits. Use
		// context.Background() because signalCtx is already cancelled
		// when this defer runs.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := otelShutdown(shutdownCtx); err != nil {
			setupLog.Error(err, "otel shutdown failed")
		}
	}()

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

	// 2. Build scheme with v1alpha3 + corev1 + batchv1. v1alpha3 is the sole
	// served+storage version (Phase 40 D-01 reinstall-only removal) — nothing
	// prior is registered; checkSchemaRevisionGuard fail-closes any Project
	// authored under an earlier schema instead of relying on scheme decode.
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(tidev1alpha3.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme

	// 3. Construct the Manager (CTRL-01, CTRL-03).
	//    Phase 3 D-D1: leader-election timings come from Helm via the env-var
	//    helpers above (lease > renew > retry invariant enforced by
	//    resolveLeaderElectionTiming defaults; controller-runtime validates).
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		LeaderElection:         leaderElect,
		LeaderElectionID:       "tide-controller-leader.tideproject.k8s",
		LeaseDuration:          &leaderLease,
		RenewDeadline:          &leaderRenew,
		RetryPeriod:            &leaderRetry,
		HealthProbeBindAddress: ":8081",
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		WebhookServer:          webhook.NewServer(webhook.Options{Port: 9443, CertDir: webhookCertPath}),
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
	//    a.bis WR-11: Self-test the signing key by signing + verifying a probe
	//    token through the same credproxy.Sign/Verify code path the dispatcher
	//    uses. Catches in-process key corruption (e.g. the historical
	//    double-base64-decode regression) at boot rather than after the first
	//    dispatch lands and per-task auth errors start flooding the log.
	//    Does NOT prove on-wire credproxy reachability — credproxy is a
	//    per-Pod sidecar that does not exist at manager-startup time.
	if err := hmacSelfTest(signingKey); err != nil {
		setupLog.Error(err, "HMAC self-test failed at startup (WR-11)")
		os.Exit(1)
	}
	setupLog.Info("HMAC self-test passed", "key-bytes", len(signingKey))
	//    b. Budget store — in-process per-Secret-UID rate-limiter cache (D-D1).
	budgetStore := budget.NewStore()
	//    b.2 Phase 14 D-05: in-process reservation store for pre-dispatch cost
	//    estimation. Rederived at startup from in-flight Job labels by the runnable
	//    registered below (same pattern as budget.PreCharge for the rate-limiter).
	reservationStore := budget.NewReservationStore()
	//    c. Rate-limit defaults from Helm values (rateLimits.defaults.* in values.yaml).
	defaults := budget.Limits{
		RequestsPerMinute: rateLimitDefaultRPM,
		BurstSize:         rateLimitDefaultBurst,
	}
	//    d. EnvelopeOut reader — prefer the completed subagent container's
	//       termination message because Task PVCs are namespace-local; keep the
	//       filesystem reader as a same-namespace/local-test fallback.
	envReader := &podjob.PodStatusEnvelopeReader{
		Client:   mgr.GetAPIReader(),
		Fallback: &podjob.FilesystemEnvelopeReader{WorkspaceRoot: "/workspaces"},
	}
	//    e. Dispatcher — wires PodJobBackend into both Plan and Task reconcilers' Phase 2
	//       dispatch paths (cascade-8 fix per .planning/debug/credproxy-backoff-suppression.md).
	//       Without this, plan_controller.go:121 and task_controller.go:167 short-circuit
	//       and no Job is ever created in production.
	dispatcher := &podjob.PodJobBackend{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		// Phase 13: use post-shim default tier (CRD fields resolved inline in Run).
		SubagentImage:        helmProviderDefaults.Image,
		CredproxyImage:       credproxyImage,
		SigningKey:           signingKey,
		EnvReader:            envReader,
		PVCName:              sharedPVCName,
		PricingOverridesJSON: pricingOverridesJSON,
		// SIGN-01 / D-03: chart-tier agent identity for the fixture backend's
		// inline precedence mirror (same source as the controllers' HelmProviderDefaults).
		AgentName:  helmProviderDefaults.AgentName,
		AgentEmail: helmProviderDefaults.AgentEmail,
	}

	// 7. Register all six reconcilers (CTRL-01).
	//    Phase 3 plan 03-09: TidePushImage (Project) + HelmProviderDefaults
	//    (Milestone/Phase/Plan) are wired from Helm env vars (D-C4 / D-B5).
	//
	// plannerDeps carries the dispatch-tier deps shared by the four
	// planner-tier reconcilers (Milestone/Phase/Plan/Project), built once and
	// assigned four times below (plan 41-06 consolidation). A forgotten
	// wiring is now a one-place mistake instead of a per-reconciler one — the
	// cascade-8 class (never-assigned Dispatcher field; see
	// .planning/debug/credproxy-backoff-suppression.md) this closes.
	plannerDeps := controller.PlannerReconcilerDeps{
		Dispatcher:           dispatcher,
		EnvReader:            envReader,
		SigningKey:           signingKey,
		CredproxyImage:       credproxyImage,
		TidePushImage:        tidePushImage,
		ReporterImage:        reporterImage,
		HelmProviderDefaults: helmProviderDefaults,
		PricingOverridesJSON: pricingOverridesJSON,
	}
	if err := (&controller.ProjectReconciler{
		Client:                  mgr.GetClient(),
		Scheme:                  mgr.GetScheme(),
		MaxConcurrentReconciles: cfg.MaxConcurrentReconciles.Project,
		WatchNamespace:          watchNamespace,
		Deps:                    plannerDeps,
		// SharedPVCName reads the same sharedPVCName source as the other
		// reconcilers (mirrors ImportReconciler's WR-03 wiring). Previously
		// unwired here despite the field existing on ProjectReconciler — its
		// own sharedPVCName() accessor always fell back to
		// defaultSharedPVCName regardless of --workspaces-pvc-name.
		SharedPVCName: sharedPVCName,
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
		Deps:                    plannerDeps,
		// SharedPVCName reads the same sharedPVCName source as ProjectReconciler
		// so the planner dispatch Job mounts the PVC the rest of the controller
		// writes to (mirrors ImportReconciler's WR-03 wiring).
		SharedPVCName: sharedPVCName,
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
		Deps:                    plannerDeps,
		// SharedPVCName reads the same sharedPVCName source as ProjectReconciler
		// so the planner dispatch Job mounts the PVC the rest of the controller
		// writes to (mirrors ImportReconciler's WR-03 wiring).
		SharedPVCName: sharedPVCName,
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
		Deps:                    plannerDeps,
		// Phase 15 D-05: same cluster-level default as the webhook so the reconciler
		// gate uses the same baseline mode when no Project.Spec overrides it.
		DefaultFileTouchMode: defaultFileTouchMode,
		// SharedPVCName reads the same sharedPVCName source as ProjectReconciler
		// so the planner dispatch Job mounts the PVC the rest of the controller
		// writes to (mirrors ImportReconciler's WR-03 wiring).
		SharedPVCName: sharedPVCName,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Plan")
		os.Exit(1)
	}
	if err := (&controller.WaveReconciler{
		Client:                  mgr.GetClient(),
		Scheme:                  mgr.GetScheme(),
		MaxConcurrentReconciles: cfg.MaxConcurrentReconciles.Wave,
		WatchNamespace:          watchNamespace,
		// CR-01 fix (debug #16): Dispatcher must be assigned for the observational
		// roll-up to fire — wave_controller.go:125 gates reconcileObservational on
		// r.Dispatcher != nil. Without this the Wave never populates Status.Phase /
		// Status.TaskRefs (it only sets the "dispatcher not wired" scaffold Ready
		// condition), and the dashboard collapses every task into wave 0. The
		// roll-up is purely observational and never creates Jobs, so wiring the
		// real dispatcher here is safe — it only unblocks the gate.
		Dispatcher: dispatcher,
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
		// SharedPVCName reads the same sharedPVCName source as ProjectReconciler
		// so the executor dispatch Job mounts the PVC the rest of the controller
		// writes to (mirrors ImportReconciler's WR-03 wiring).
		SharedPVCName: sharedPVCName,
		// Phase 04.1 P3.2 — dispatch-tier deps consolidated into a carrier struct.
		// Mirrors HelmProviderDefaults precedent on Milestone/Phase/Plan reconcilers.
		Deps: controller.TaskReconcilerDeps{
			Dispatcher:           dispatcher,
			Budget:               budgetStore,
			Defaults:             defaults,
			SigningKey:           signingKey,
			CredproxyImage:       credproxyImage,
			EnvReader:            envReader,
			HelmProviderDefaults: helmProviderDefaults,
			// Phase 14 D-05: reservation store + per-dispatch estimate for pre-charge.
			Reservations:         reservationStore,
			ReserveEstimateCents: budgetReservePerDispatchCents,
			// Phase 14 D-02: pricing overrides forwarded opaquely to executor Jobs.
			PricingOverridesJSON: pricingOverridesJSON,
		},
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Task")
		os.Exit(1)
	}
	// Phase 28 IMPORT-01: ImportReconciler drives the one-shot UID-rewrite import
	// state machine for Projects carrying spec.importSource. No pool, dispatcher, or
	// signing key — the import Job reads the PVC directly via the tide-import binary.
	// SharedPVCName reads the same sharedPVCName source as ProjectReconciler.PVCName
	// so the import Job mounts the PVC the rest of the controller writes to (WR-03).
	if err := (&controller.ImportReconciler{
		Client:                  mgr.GetClient(),
		Scheme:                  mgr.GetScheme(),
		MaxConcurrentReconciles: cfg.MaxConcurrentReconciles.Project, // reuse Project concurrency
		WatchNamespace:          watchNamespace,
		ImportImage:             importImage,
		SharedPVCName:           sharedPVCName,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Import")
		os.Exit(1)
	}

	// 8. Register both webhooks (CRD-04, CRD-05).
	// defaultFileTouchMode drives cluster-level file-touch validation (Plan 11 / D-E3).
	// The Helm-chart default is "warn" (safe for fresh installs); operators opt in to
	// "strict" via --set planAdmission.fileTouchMode=strict which is passed through
	// the controller Deployment args to this --default-file-touch-mode flag.
	// Plan webhook serves v1alpha3 (Phase 40 crank, plan 40-03).
	if err := webhookv1alpha3.SetupPlanWebhookWithManager(mgr, defaultFileTouchMode); err != nil {
		setupLog.Error(err, "unable to create webhook", "kind", "Plan")
		os.Exit(1)
	}
	// Wave webhook re-registered for v1alpha3 (D-B1 re-registration).
	if err := webhookv1alpha3.SetupWaveWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "kind", "Wave")
		os.Exit(1)
	}
	if err := webhookv1alpha3.SetupProjectWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "kind", "Project")
		os.Exit(1)
	}

	// +kubebuilder:scaffold:builder

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

	// 9.b Register Phase 14 RederiveReservations as a Manager Runnable (Pitfall 3).
	//     Runs after cache sync (same NeedLeaderElection/cache-warm pattern as PreCharge
	//     above). Restores in-flight reservation estimates from the tideproject.k8s/estimated-cost
	//     labels on active Jobs so the ReservationStore is populated before the first reconcile.
	//     Pre-Phase-14 Jobs lacking the label are treated as 0 reserved (conservative; Pitfall 5).
	//     Best-effort: errors are logged but non-fatal (a clean restart re-arms on next reconcile).
	if err := mgr.Add(ctrlmgr.RunnableFunc(func(ctx context.Context) error {
		if err := budget.RederiveReservations(ctx, mgr.GetClient(), reservationStore); err != nil {
			setupLog.Error(err, "reservation rederivation failed (non-fatal, Pitfall 3)")
			return nil
		}
		setupLog.Info("reservation store rederived from in-flight jobs",
			"totalReservedCents", reservationStore.TotalReserved())
		return nil
	})); err != nil {
		setupLog.Error(err, "unable to register reservation rederive runnable")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(signalCtx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
