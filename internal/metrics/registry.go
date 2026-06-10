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

// Package metrics — see doc.go for package overview, cardinality discipline,
// and the v1.0 counter/histogram inventory.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/jsquirrelz/tide/internal/budget"
)

// dispatchLatencyBuckets is the locked bucket slice for
// tide_dispatch_latency_seconds (CONTEXT.md "Claude's Discretion" §195).
// 100 ms → 30 min, sized for K8s API plus LLM-inference latency reality.
// Default Prometheus buckets cluster around sub-second values and would
// quantize LLM responses into a single bucket, defeating the histogram.
var dispatchLatencyBuckets = []float64{0.1, 0.5, 1, 5, 10, 30, 60, 300, 1800}

// WavesDispatchedTotal counts the number of Waves the orchestrator dispatched
// to the executor pool, surfaced by Project / Phase / Plan (D-O2).
//
// Label arity: {project, phase, plan} = 3. The Wave name is intentionally
// elided — wave count rolls up to the parent Plan for at-a-glance triage in
// Grafana, and per-wave detail lives in Wave.Status.Phase + the OTel trace.
var WavesDispatchedTotal *prometheus.CounterVec

// TasksCompletedTotal counts Tasks that reached terminal success, surfaced
// by Project / Phase / Plan (D-O2). NO `task` label — Pitfall 17.
var TasksCompletedTotal *prometheus.CounterVec

// TasksFailedTotal counts Tasks that reached terminal failure, broken down by
// the failure class. The `reason` set is bounded — {exit-1, gitleaks, lease,
// auth, internal, budget} — six values total.
//
// Reason taxonomy (D-O2):
//
//	exit-1   — subagent CLI exited non-zero without a more specific reason
//	gitleaks — push Job exit-10 (leak detected; also fires SecretLeakBlockedTotal)
//	lease    — push Job exit-11 (--force-with-lease rejection)
//	auth     — credproxy denied a request (HARN-03 HMAC validation failure)
//	internal — TIDE controller bug surfaced via Status.Conditions[Failed].Reason
//	budget   — Project absolute / rolling cap hit (Phase 2 D-D2)
var TasksFailedTotal *prometheus.CounterVec

// DispatchLatency observes the wall-clock latency of a dispatch round trip,
// broken down by level. `level` ∈ {milestone, phase, plan, task}. The locked
// bucket slice [0.1, 0.5, 1, 5, 10, 30, 60, 300, 1800] is sized for the
// full planner-vs-executor latency spread (planning calls take minutes;
// task executions can run for tens of minutes on Sonnet 4.6).
var DispatchLatency *prometheus.HistogramVec

// SecretLeakBlockedTotal — W-1 / D-W1. Increments when ProjectReconciler
// observes a push Job exit code of 10 (envelope.reason="leak-detected").
// Distinct from PushJobsTotal{outcome="leak"} because operators correlate
// secret-leak incidents to Project / Phase / Plan, not to the per-push
// outcome buckets. Label arity {project, phase, plan} = 3.
var SecretLeakBlockedTotal *prometheus.CounterVec

// PushJobsTotal counts every terminal push Job, broken down by outcome.
// `outcome` ∈ {success, leak, lease, auth, internal, dispatched, exhausted}.
// Bounded cardinality because outcomes are a closed enum surfaced by Phase 3
// D-B2 + debug defect #13b (dispatched = a boundary-push attempt was created;
// exhausted = the bounded boundary-push retry budget was reached).
var PushJobsTotal *prometheus.CounterVec

// BudgetOverrunsTotal counts the number of times a Project exceeded its
// absolute cost cap. Phase 2 D-D2 already tracks the data via
// BudgetStatus.CostSpentCents; this counter surfaces overruns as discrete
// events for Prometheus alerting. Label arity {project} = 1.
var BudgetOverrunsTotal *prometheus.CounterVec

// ProviderRateLimitHitsTotal is re-exported from internal/budget for callers
// that want a single metrics package import. The underlying
// *prometheus.CounterVec instance is the same one that
// internal/budget's init() registered on metrics.Registry — DO NOT call
// metrics.Registry.MustRegister on this alias (would panic on duplicate
// registration). See package doc.go "Re-export of Phase 2 metric".
var ProviderRateLimitHitsTotal *prometheus.CounterVec

func init() {
	WavesDispatchedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tide_waves_dispatched_total",
			Help: "Count of Waves dispatched to the executor pool, surfaced by Project, Phase, and Plan (Phase 4 D-O2).",
		},
		[]string{"project", "phase", "plan"},
	)

	TasksCompletedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tide_tasks_completed_total",
			Help: "Count of Tasks that reached terminal success, surfaced by Project, Phase, and Plan (Phase 4 D-O2).",
		},
		[]string{"project", "phase", "plan"},
	)

	TasksFailedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tide_tasks_failed_total",
			Help: "Count of Tasks that reached terminal failure, with reason ∈ {exit-1, gitleaks, lease, auth, internal, budget} (Phase 4 D-O2).",
		},
		[]string{"project", "phase", "plan", "reason"},
	)

	DispatchLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "tide_dispatch_latency_seconds",
			Help:    "Wall-clock latency of a dispatch round trip, with level ∈ {milestone, phase, plan, task}. Buckets sized for LLM-inference reality (Phase 4 D-O2).",
			Buckets: dispatchLatencyBuckets,
		},
		[]string{"level"},
	)

	SecretLeakBlockedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tide_secret_leak_blocked_total",
			Help: "Count of push Jobs blocked by gitleaks (exit code 10 / envelope.reason=leak-detected), surfaced by Project, Phase, and Plan (Phase 4 D-W1).",
		},
		[]string{"project", "phase", "plan"},
	)

	PushJobsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tide_push_jobs_total",
			Help: "Count of terminal push Jobs, with outcome ∈ {success, leak, lease, auth, internal} (Phase 4 D-O2).",
		},
		[]string{"project", "outcome"},
	)

	BudgetOverrunsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tide_budget_overruns_total",
			Help: "Count of times a Project exceeded its absolute cost cap. Tracked at the Project granularity (Phase 4 D-O2).",
		},
		[]string{"project"},
	)

	metrics.Registry.MustRegister(
		WavesDispatchedTotal,
		TasksCompletedTotal,
		TasksFailedTotal,
		DispatchLatency,
		SecretLeakBlockedTotal,
		PushJobsTotal,
		BudgetOverrunsTotal,
	)

	// Re-export Phase 2's ProviderRateLimitHitsTotal. The variable is a
	// pointer to the same *prometheus.CounterVec instance that internal/budget
	// already registered; assignment here just gives callers a single import
	// path. We deliberately do NOT call MustRegister — duplicate registration
	// panics on a controller-runtime registry.
	ProviderRateLimitHitsTotal = budget.ProviderRateLimitHitsTotal
}
