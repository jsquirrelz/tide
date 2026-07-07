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

// Phase 4 Plan 01 Task 2: registry tests.
//
// Each test exercises one of the seven Phase 4 metric registrations registered
// on controller-runtime's metrics.Registry via this package's init().
// `testutil.CollectAndCount` lets us assert label-arity at WithLabelValues call
// sites — passing too few / too many labels panics inside client_golang, so a
// successful Inc / Observe is itself the arity check.
package metrics_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	tidemetrics "github.com/jsquirrelz/tide/internal/metrics"
)

// TestRegistry_AllMetricFamiliesPresent asserts that all 7 Phase 4 metric
// families register on the controller-runtime registry as a side-effect of
// the package import. (`tide_provider_rate_limit_hits_total` lives in
// internal/budget and is intentionally NOT duplicated here — the alias
// variable `tidemetrics.ProviderRateLimitHitsTotal` re-exports it for
// callers, but the registration stays in internal/budget per plan 04-01
// Task 2 action.)
//
// Note on Prometheus vector metric semantics: an unobserved *CounterVec /
// *HistogramVec emits NO metric family from Gather() until at least one
// child is created via WithLabelValues. testutil.CollectAndCount handles
// this correctly because it walks the descriptor (Desc) chain rather than
// the gathered families — descriptors are registered at MustRegister time,
// before any observation. This is also why the analyzer of plan 04-02 walks
// AST (registration sites) rather than the runtime registry: registration
// commits the contract, observation surfaces the data.
func TestRegistry_AllMetricFamiliesPresent(t *testing.T) {
	// Seed one observation for each so they emit a family on Gather().
	// Using a sentinel project label keeps these out of any real bucket.
	tidemetrics.WavesDispatchedTotal.WithLabelValues("__seed__", "ph", "pl").Add(0)
	tidemetrics.TasksCompletedTotal.WithLabelValues("__seed__", "ph", "pl").Add(0)
	tidemetrics.TasksFailedTotal.WithLabelValues("__seed__", "ph", "pl", "exit-1").Add(0)
	tidemetrics.DispatchLatency.WithLabelValues("__seed__")
	tidemetrics.SecretLeakBlockedTotal.WithLabelValues("__seed__", "ph", "pl").Add(0)
	tidemetrics.PushJobsTotal.WithLabelValues("__seed__", "success").Add(0)
	tidemetrics.BudgetOverrunsTotal.WithLabelValues("__seed__").Add(0)
	// Phase 34 D-12: seed the integration-outcome counter.
	tidemetrics.IntegrationOutcomesTotal.WithLabelValues("__seed__", "success").Add(0)
	// Phase 16 TELEM-03: seed six new metric families ({project, phase, plan, wave} = 4 args).
	tidemetrics.TokensInputTotal.WithLabelValues("__seed__", "ph", "pl", "w").Add(0)
	tidemetrics.TokensOutputTotal.WithLabelValues("__seed__", "ph", "pl", "w").Add(0)
	tidemetrics.TokensCacheReadTotal.WithLabelValues("__seed__", "ph", "pl", "w").Add(0)
	tidemetrics.TokensCacheCreationTotal.WithLabelValues("__seed__", "ph", "pl", "w").Add(0)
	tidemetrics.CostCentsTotal.WithLabelValues("__seed__", "ph", "pl", "w").Add(0)
	tidemetrics.TaskDurationSeconds.WithLabelValues("__seed__", "ph", "pl", "w").Observe(0)
	// Phase 21 OBSV-02: seed realized-savings counter.
	tidemetrics.CacheSavingsCentsTotal.WithLabelValues("__seed__", "ph", "pl", "w").Add(0)

	families, err := crmetrics.Registry.Gather()
	if err != nil {
		t.Fatalf("Registry.Gather: %v", err)
	}
	seen := make(map[string]bool, len(families))
	for _, mf := range families {
		seen[mf.GetName()] = true
	}
	want := []string{
		"tide_waves_dispatched_total",
		"tide_tasks_completed_total",
		"tide_tasks_failed_total",
		"tide_dispatch_latency_seconds",
		"tide_secret_leak_blocked_total",
		"tide_push_jobs_total",
		"tide_budget_overruns_total",
		"tide_integration_outcomes_total",
		// Phase 16 TELEM-03 locked metrics:
		"tide_tokens_input_total",
		"tide_tokens_output_total",
		"tide_tokens_cache_read_total",
		"tide_tokens_cache_creation_total",
		"tide_cost_cents_total",
		"tide_task_duration_seconds",
		// Phase 21 OBSV-02:
		"tide_cache_savings_cents_total",
	}
	for _, name := range want {
		if !seen[name] {
			t.Errorf("metric family %q not registered on crmetrics.Registry", name)
		}
	}
}

// TestRegistry_WavesDispatchedLabelArity asserts WavesDispatchedTotal accepts
// exactly 3 labels {project, phase, plan}.
func TestRegistry_WavesDispatchedLabelArity(t *testing.T) {
	// WithLabelValues panics on arity mismatch — that panic IS the test.
	tidemetrics.WavesDispatchedTotal.WithLabelValues("p", "ph", "pl").Inc()
	if got := testutil.ToFloat64(tidemetrics.WavesDispatchedTotal.WithLabelValues("p", "ph", "pl")); got < 1 {
		t.Errorf("WavesDispatchedTotal counter = %v, want >= 1", got)
	}
}

// TestRegistry_TasksCompletedLabelArity asserts arity {project, phase, plan} = 3.
func TestRegistry_TasksCompletedLabelArity(t *testing.T) {
	tidemetrics.TasksCompletedTotal.WithLabelValues("p", "ph", "pl").Inc()
	if got := testutil.ToFloat64(tidemetrics.TasksCompletedTotal.WithLabelValues("p", "ph", "pl")); got < 1 {
		t.Errorf("TasksCompletedTotal counter = %v, want >= 1", got)
	}
}

// TestRegistry_TasksFailedLabelArity asserts arity {project, phase, plan, reason} = 4.
func TestRegistry_TasksFailedLabelArity(t *testing.T) {
	tidemetrics.TasksFailedTotal.WithLabelValues("p", "ph", "pl", "exit-1").Inc()
	if got := testutil.ToFloat64(tidemetrics.TasksFailedTotal.WithLabelValues("p", "ph", "pl", "exit-1")); got < 1 {
		t.Errorf("TasksFailedTotal counter = %v, want >= 1", got)
	}
}

// TestRegistry_DispatchLatencyArity asserts arity {level} = 1 with the locked
// bucket slice. We can't read prometheus.Histogram buckets directly from the
// client_golang API, but observing a value in the histogram and reading it
// back via testutil.CollectAndCount confirms the registration succeeded.
func TestRegistry_DispatchLatencyArity(t *testing.T) {
	tidemetrics.DispatchLatency.WithLabelValues("milestone").Observe(2.5)
	count := testutil.CollectAndCount(tidemetrics.DispatchLatency)
	if count < 1 {
		t.Errorf("DispatchLatency series count = %d, want >= 1", count)
	}
}

// TestRegistry_DispatchLatencyBuckets asserts the exact locked bucket slice
// [0.1, 0.5, 1, 5, 10, 30, 60, 300, 1800] by reading the source file.
// Buckets are not exposed via the runtime API; grep is the audit.
func TestRegistry_DispatchLatencyBuckets(t *testing.T) {
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "internal", "metrics", "registry.go"))
	if err != nil {
		t.Fatalf("read registry.go: %v", err)
	}
	src := string(data)
	want := "[]float64{0.1, 0.5, 1, 5, 10, 30, 60, 300, 1800}"
	if !strings.Contains(src, want) {
		t.Errorf("registry.go missing locked histogram bucket slice %q", want)
	}
}

// TestRegistry_SecretLeakBlockedArity asserts arity {project, phase, plan} = 3
// (no task label — W-1 fires per push Job, not per Task; D-W1).
func TestRegistry_SecretLeakBlockedArity(t *testing.T) {
	tidemetrics.SecretLeakBlockedTotal.WithLabelValues("p", "ph", "pl").Inc()
	if got := testutil.ToFloat64(tidemetrics.SecretLeakBlockedTotal.WithLabelValues("p", "ph", "pl")); got < 1 {
		t.Errorf("SecretLeakBlockedTotal counter = %v, want >= 1", got)
	}
}

// TestRegistry_PushJobsArity asserts arity {project, outcome} = 2.
func TestRegistry_PushJobsArity(t *testing.T) {
	tidemetrics.PushJobsTotal.WithLabelValues("p", "success").Inc()
	if got := testutil.ToFloat64(tidemetrics.PushJobsTotal.WithLabelValues("p", "success")); got < 1 {
		t.Errorf("PushJobsTotal counter = %v, want >= 1", got)
	}
}

// TestRegistry_BudgetOverrunsArity asserts arity {project} = 1.
func TestRegistry_BudgetOverrunsArity(t *testing.T) {
	tidemetrics.BudgetOverrunsTotal.WithLabelValues("p").Inc()
	if got := testutil.ToFloat64(tidemetrics.BudgetOverrunsTotal.WithLabelValues("p")); got < 1 {
		t.Errorf("BudgetOverrunsTotal counter = %v, want >= 1", got)
	}
}

// TestRegistry_IntegrationOutcomesArity asserts arity {project, outcome} = 2
// and that it increments per outcome label independently (Phase 34 D-12).
func TestRegistry_IntegrationOutcomesArity(t *testing.T) {
	tidemetrics.IntegrationOutcomesTotal.WithLabelValues("p", "miss").Inc()
	if got := testutil.ToFloat64(tidemetrics.IntegrationOutcomesTotal.WithLabelValues("p", "miss")); got < 1 {
		t.Errorf("IntegrationOutcomesTotal counter = %v, want >= 1", got)
	}
	tidemetrics.IntegrationOutcomesTotal.WithLabelValues("p", "conflict").Inc()
	if got := testutil.ToFloat64(tidemetrics.IntegrationOutcomesTotal.WithLabelValues("p", "conflict")); got < 1 {
		t.Errorf("IntegrationOutcomesTotal counter (conflict) = %v, want >= 1", got)
	}
}

// TestRegistry_TokensInputLabelArity asserts arity {project, phase, plan, wave} = 4.
func TestRegistry_TokensInputLabelArity(t *testing.T) {
	tidemetrics.TokensInputTotal.WithLabelValues("p", "ph", "pl", "w").Add(1)
	if got := testutil.ToFloat64(tidemetrics.TokensInputTotal.WithLabelValues("p", "ph", "pl", "w")); got < 1 {
		t.Errorf("TokensInputTotal counter = %v, want >= 1", got)
	}
}

// TestRegistry_TokensOutputLabelArity asserts arity {project, phase, plan, wave} = 4.
func TestRegistry_TokensOutputLabelArity(t *testing.T) {
	tidemetrics.TokensOutputTotal.WithLabelValues("p", "ph", "pl", "w").Add(1)
	if got := testutil.ToFloat64(tidemetrics.TokensOutputTotal.WithLabelValues("p", "ph", "pl", "w")); got < 1 {
		t.Errorf("TokensOutputTotal counter = %v, want >= 1", got)
	}
}

// TestRegistry_TokensCacheReadLabelArity asserts arity {project, phase, plan, wave} = 4.
func TestRegistry_TokensCacheReadLabelArity(t *testing.T) {
	tidemetrics.TokensCacheReadTotal.WithLabelValues("p", "ph", "pl", "w").Add(1)
	if got := testutil.ToFloat64(tidemetrics.TokensCacheReadTotal.WithLabelValues("p", "ph", "pl", "w")); got < 1 {
		t.Errorf("TokensCacheReadTotal counter = %v, want >= 1", got)
	}
}

// TestRegistry_TokensCacheCreationLabelArity asserts arity {project, phase, plan, wave} = 4.
func TestRegistry_TokensCacheCreationLabelArity(t *testing.T) {
	tidemetrics.TokensCacheCreationTotal.WithLabelValues("p", "ph", "pl", "w").Add(1)
	if got := testutil.ToFloat64(tidemetrics.TokensCacheCreationTotal.WithLabelValues("p", "ph", "pl", "w")); got < 1 {
		t.Errorf("TokensCacheCreationTotal counter = %v, want >= 1", got)
	}
}

// TestRegistry_CostCentsLabelArity asserts arity {project, phase, plan, wave} = 4.
func TestRegistry_CostCentsLabelArity(t *testing.T) {
	tidemetrics.CostCentsTotal.WithLabelValues("p", "ph", "pl", "w").Add(1)
	if got := testutil.ToFloat64(tidemetrics.CostCentsTotal.WithLabelValues("p", "ph", "pl", "w")); got < 1 {
		t.Errorf("CostCentsTotal counter = %v, want >= 1", got)
	}
}

// TestRegistry_CacheSavingsCentsLabelArity asserts arity {project, phase, plan, wave} = 4
// for the Phase 21 OBSV-02 realized-savings counter.
//
// Also serves as the OBSV-01 audit guard: the savings counter carries the same
// four-label set that makes per-level PromQL slicing work without extra
// instrumentation.
func TestRegistry_CacheSavingsCentsLabelArity(t *testing.T) {
	tidemetrics.CacheSavingsCentsTotal.WithLabelValues("p", "ph", "pl", "w").Add(1)
	if got := testutil.ToFloat64(tidemetrics.CacheSavingsCentsTotal.WithLabelValues("p", "ph", "pl", "w")); got < 1 {
		t.Errorf("CacheSavingsCentsTotal counter = %v, want >= 1", got)
	}
}

// TestRegistry_TaskDurationSecondsArity asserts arity {project, phase, plan, wave} = 4.
func TestRegistry_TaskDurationSecondsArity(t *testing.T) {
	tidemetrics.TaskDurationSeconds.WithLabelValues("p", "ph", "pl", "w").Observe(90)
	count := testutil.CollectAndCount(tidemetrics.TaskDurationSeconds)
	if count < 1 {
		t.Errorf("TaskDurationSeconds series count = %d, want >= 1", count)
	}
}

// TestRegistry_TaskDurationBuckets asserts the exact locked bucket slice
// [30, 60, 120, 300, 600, 1200, 1800, 3600, 7200] (D-11) by reading the source file.
// Buckets are not exposed via the runtime API; grep of the source is the audit.
func TestRegistry_TaskDurationBuckets(t *testing.T) {
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "internal", "metrics", "registry.go"))
	if err != nil {
		t.Fatalf("read registry.go: %v", err)
	}
	src := string(data)
	want := "[]float64{30, 60, 120, 300, 600, 1200, 1800, 3600, 7200}"
	if !strings.Contains(src, want) {
		t.Errorf("registry.go missing locked D-11 histogram bucket slice %q", want)
	}
}

// TestRegistry_NoTaskLabel asserts no Phase 4 metric uses a `task` label.
// Pitfall 17 mitigation — anticipates the AST analyzer of plan 04-02.
// The source-grep here is the compile-time guardrail; the analyzer of 04-02
// is the codebase-wide enforcement.
func TestRegistry_NoTaskLabel(t *testing.T) {
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "internal", "metrics", "registry.go"))
	if err != nil {
		t.Fatalf("read registry.go: %v", err)
	}
	src := string(data)
	// Look only inside NewCounterVec / NewHistogramVec label slices.
	// Crude but effective: walk every line that contains `prometheus.New`
	// (the registration constructor call) and the lines immediately following
	// up to the matching closing brace; flag any `"task"` literal in the label
	// slice region. Implementation: substring search for the exact label
	// literal `"task"` anywhere in the file is the conservative check —
	// any occurrence is suspicious because this file should never reference
	// a task-scoped quantity.
	if strings.Contains(src, `"task"`) {
		// Allow `task` to appear in a comment (// task ...) but reject the
		// quoted-string literal anywhere — a label slice literal is the only
		// place a quoted "task" should land.
		t.Errorf(`registry.go contains the literal "task" — Pitfall 17 violation; do not add a task-scoped metric label`)
	}
}

// findRepoRoot walks up from cwd until it finds go.mod.
func findRepoRoot(t *testing.T) string {
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
