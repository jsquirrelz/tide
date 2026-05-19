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

// Command dashboard is the TIDE read-only dashboard backend (Phase 4 D-D2).
//
// Architecture (CONTEXT D-D2):
//
//   - Browser ↔ this binary (HTTP/SSE on :8080)
//   - this binary ↔ apiserver (controller-runtime client + informer cache)
//   - browser NEVER talks to apiserver directly; this binary is the only
//     consumer of dashboard SA credentials
//
// The HTTP server is registered as a controller-runtime manager.Runnable
// so it shares the manager's signal handler + cache-sync gating. Health
// probes on :8081 mirror the controller-manager convention:
//
//   - :8081 /healthz  — gated on mgr.GetCache().WaitForCacheSync()
//   - :8081 /readyz   — ditto
//   - :8080 /healthz  — bare process liveness (no cache gate)
//   - :8080 /api/v1/* — REST API (read-only, DASH-05 enforced)
//   - :8080 /*        — SPA fallback (embedded Vite bundle)
//
// LeaderElection is OFF — the dashboard is a stateless read replica;
// multiple replicas are safe.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
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

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	dashboardapi "github.com/jsquirrelz/tide/cmd/dashboard/api"
	dashboardembed "github.com/jsquirrelz/tide/cmd/dashboard/embed"
	"github.com/jsquirrelz/tide/cmd/dashboard/hub"
)

// shutdownTimeout bounds the graceful HTTP shutdown when ctx is
// cancelled (SIGTERM from kubelet, etc.). 10s mirrors the manager's
// terminationGracePeriodSeconds in the Helm chart default; matches the
// "manager.Start returns within 10s" assertion in router_test.go.
const shutdownTimeout = 10 * time.Second

func main() {
	var apiAddr string
	var probeAddr string
	var metricsAddr string

	flag.StringVar(&apiAddr, "api-bind-address", ":8080", "Bind address for the dashboard HTTP API (browser-facing)")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "Bind address for the manager-level health probes (cache-sync-gated)")
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "Bind address for the manager metrics endpoint (0 disables; dashboard does not register custom metrics in v1.0)")

	opts := zap.Options{Development: false}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	setupLog := ctrl.Log.WithName("setup")

	signalCtx := ctrl.SetupSignalHandler()

	// 1. Build scheme with v1alpha1 + corev1 (needed for pods/log proxy
	//    in plan 04-11; included now so the cache primes the right
	//    informers from the first reconcile loop).
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(tidev1alpha1.AddToScheme(scheme))

	// 2. Construct the controller-runtime Manager. LeaderElection: false
	//    — the dashboard is a stateless read replica (D-D2). The
	//    informer cache is the dashboard's only data source.
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		LeaderElection:         false,
		HealthProbeBindAddress: probeAddr,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// 3. Construct the SSE fan-out hub. Plan 04-11 wires the informer
	//    cache's watch events into Publish() and the SSE handlers
	//    subscribe via Hub.Subscribe().
	pubsubHub := hub.NewHub(setupLog.WithName("hub"))

	// 4. Re-base the embedded SPA at the `dist/` sub-tree so HTTP file
	//    paths like `/index.html` map to `dist/index.html` inside the
	//    embed.FS. fs.Sub returns a *DirFS-equivalent view; errors at
	//    this point are programmer errors (the embed directive is
	//    compile-time-validated), so log + exit.
	spaFS, err := fs.Sub(dashboardembed.Dist, "dist")
	if err != nil {
		setupLog.Error(err, "unable to mount embedded SPA bundle")
		os.Exit(1)
	}

	// 5. Build the chi router. Same construction path tests use
	//    (RegisterRoutes in router.go) so the route table is identical
	//    between production and TestZeroMutationRoutes.
	router := RegisterRoutes(Dependencies{
		Client: mgr.GetClient(),
		Hub:    pubsubHub,
		Log:    setupLog.WithName("router"),
		SPAFS:  spaFS,
	})

	// 6. Register the HTTP server as a manager.Runnable. It shares the
	//    manager's signal handler + lifecycle; graceful shutdown
	//    triggered when signalCtx is cancelled.
	if err := mgr.Add(ctrlmgr.RunnableFunc(func(ctx context.Context) error {
		srv := &http.Server{
			Addr:              apiAddr,
			Handler:           router,
			ReadHeaderTimeout: 10 * time.Second,
		}

		// Goroutine waits for ctx cancel → triggers Shutdown which
		// drains in-flight requests up to shutdownTimeout and returns
		// http.ErrServerClosed from ListenAndServe.
		go func() {
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
			defer cancel()
			if sErr := srv.Shutdown(shutdownCtx); sErr != nil {
				setupLog.Error(sErr, "http server shutdown")
			}
		}()

		setupLog.Info("starting dashboard HTTP server", "addr", apiAddr)
		if lErr := srv.ListenAndServe(); lErr != nil && !errors.Is(lErr, http.ErrServerClosed) {
			return fmt.Errorf("dashboard ListenAndServe: %w", lErr)
		}
		return nil
	})); err != nil {
		setupLog.Error(err, "unable to add HTTP server runnable")
		os.Exit(1)
	}

	// 7. Wire the informer cache watch events into the Hub. The bridge
	//    is registered as another manager.Runnable so it ties into the
	//    manager lifecycle — Start() runs after the cache is up; ctx
	//    cancellation on shutdown propagates through.
	if err := mgr.Add(ctrlmgr.RunnableFunc(func(ctx context.Context) error {
		return dashboardapi.BridgeInformerToHub(ctx, mgr.GetCache(), mgr.GetClient(), pubsubHub, setupLog.WithName("informer-bridge"))
	})); err != nil {
		setupLog.Error(err, "unable to add informer bridge runnable")
		os.Exit(1)
	}

	// 8. Register the manager-port healthz that gates on informer-cache
	//    sync (per the truth spec line 25). The Helm chart's
	//    readinessProbe targets this so kubelet only routes traffic to
	//    the Pod once the cache is hot.
	if err := mgr.AddHealthzCheck("informer-synced", cacheSyncHealthz(mgr)); err != nil {
		setupLog.Error(err, "unable to register cache-sync healthz")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("informer-synced", cacheSyncHealthz(mgr)); err != nil {
		setupLog.Error(err, "unable to register cache-sync readyz")
		os.Exit(1)
	}
	// Liveness on the manager port — process is alive even before
	// cache sync (kubelet uses this to distinguish "starting" from
	// "deadlocked").
	if err := mgr.AddHealthzCheck("ping", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to register ping healthz")
		os.Exit(1)
	}

	setupLog.Info("starting dashboard manager")
	if err := mgr.Start(signalCtx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// cacheSyncHealthz returns a healthz.Checker that returns nil once the
// manager's informer cache has synced, otherwise an error. Per the
// plan's `must_haves.truths[1]`, /healthz returns 200 only after
// WaitForCacheSync(ctx) returns true. The checker uses a short-timeout
// context so a probe doesn't block indefinitely waiting on a cold cache.
func cacheSyncHealthz(mgr ctrlmgr.Manager) healthz.Checker {
	return func(_ *http.Request) error {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if !mgr.GetCache().WaitForCacheSync(ctx) {
			return errors.New("informer cache not yet synced")
		}
		return nil
	}
}
