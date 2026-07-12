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

// Phase 4 Plan 01 Task 3: confirm the cmd/manager package wires the
// internal/metrics blank import so init() fires when the manager binary
// starts (D-O2 / Pitfall 23 — must be crmetrics.Registry, not
// prometheus.DefaultRegisterer).
//
// Why static source-grep: a runtime test that imports internal/metrics
// directly would pass even WITHOUT the blank import on main.go, because the
// test file's own import would trigger init(). To prove the wire-up lives in
// main.go (and survives a future refactor that removes the test file), grep
// the production source for the exact blank-import literal. This mirrors the
// static-assertion pattern used by cmd/manager/rbac_docs_test.go (the
// aggregates-guard variant of this pattern was retired in Phase 40 — its
// concern is now carried by the hardened `make verify-no-aggregates` gate).
//
// A second test (TestMetricsFamilies) seeds and gathers via the
// controller-runtime registry to prove the metric NAMES match the contract
// — but that test does intentionally import internal/metrics, because the
// goal there is the contract-shape audit, not the wire-up audit.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	tidemetrics "github.com/jsquirrelz/tide/internal/metrics"
)

// TestMetricsBlankImportPresent asserts main.go contains the
// `_ "github.com/jsquirrelz/tide/internal/metrics"` blank import. This is
// the wire-up that triggers Phase 4 metric registration at Manager start.
func TestMetricsBlankImportPresent(t *testing.T) {
	root := findRepoRootFromCmdManager(t)
	data, err := os.ReadFile(filepath.Join(root, "cmd", "manager", "main.go"))
	if err != nil {
		t.Fatalf("read cmd/manager/main.go: %v", err)
	}
	src := string(data)
	want := `_ "github.com/jsquirrelz/tide/internal/metrics"`
	if !strings.Contains(src, want) {
		t.Errorf("cmd/manager/main.go missing Phase 4 metrics blank import %q — Phase 4 D-O2 registration will NOT fire at Manager start",
			want)
	}
}

// TestMetricsRegistered asserts every Phase 4 metric family is reachable via
// the controller-runtime registry (NOT prometheus.DefaultRegisterer — D-O2 /
// Pitfall 23). This test uses a direct internal/metrics import to make the
// assertion straightforward; the wire-up check above is what proves the
// production binary loads it.
func TestMetricsRegistered(t *testing.T) {
	// Seed one labeled child per metric so Gather emits the family.
	tidemetrics.WavesDispatchedTotal.WithLabelValues("__seed__", "ph", "pl").Add(0)
	tidemetrics.TasksCompletedTotal.WithLabelValues("__seed__", "ph", "pl").Add(0)
	tidemetrics.TasksFailedTotal.WithLabelValues("__seed__", "ph", "pl", "exit-1").Add(0)
	tidemetrics.DispatchLatency.WithLabelValues("__seed__")
	tidemetrics.SecretLeakBlockedTotal.WithLabelValues("__seed__", "ph", "pl").Add(0)
	tidemetrics.PushJobsTotal.WithLabelValues("__seed__", "success").Add(0)
	tidemetrics.BudgetOverrunsTotal.WithLabelValues("__seed__").Add(0)

	want := []string{
		"tide_waves_dispatched_total",
		"tide_tasks_completed_total",
		"tide_tasks_failed_total",
		"tide_dispatch_latency_seconds",
		"tide_secret_leak_blocked_total",
		"tide_push_jobs_total",
		"tide_budget_overruns_total",
	}
	for _, name := range want {
		got, err := testutil.GatherAndCount(crmetrics.Registry, name)
		if err != nil {
			t.Errorf("GatherAndCount(%q): %v", name, err)
			continue
		}
		if got < 1 {
			t.Errorf("metric family %q: GatherAndCount returned %d, want >= 1 (registry: crmetrics.Registry)",
				name, got)
		}
	}
}

// findRepoRootFromCmdManager walks up from cwd until it finds go.mod.
// (Named differently from env_test.go's helpers to avoid collision in the
// `package main` namespace.)
func findRepoRootFromCmdManager(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := cwd
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			return root
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatalf("go.mod not found from %s; cannot locate repo root", cwd)
		}
		root = parent
	}
}
