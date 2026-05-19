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

// Package metrics centralizes Phase 4 Prometheus metric definitions for TIDE
// (Phase 4 D-O2 / D-W1 / D-X4).
//
// # Wiring
//
// cmd/manager/main.go blank-imports this package
//
//	import _ "github.com/jsquirrelz/tide/internal/metrics"
//
// to trigger the init() that registers every counter / histogram below on
// controller-runtime's metrics.Registry. The registry's /metrics endpoint
// (default :8443 in this codebase) is the Prometheus scrape surface.
//
// # Cardinality discipline
//
// Every metric in this package declares a bounded label set drawn from the
// following alphabet:
//
//	project, phase, plan, reason, outcome, vendor, level
//
// The `task` label is FORBIDDEN (D-X4 / Pitfall 17). Plan 04-02 ships a
// metriccardinality AST analyzer that walks `prometheus.NewCounterVec` /
// `NewHistogramVec` / `NewGaugeVec` / `NewSummaryVec` call sites and rejects
// any `[]string{...}` literal containing `"task"`. Until that analyzer lands,
// the grep test in registry_test.go enforces the same rule on this file.
//
// Why no task label: a long-lived cluster accumulates O(tasks) cardinality
// per metric, which DoSes Prometheus storage (Pitfall 17). Per-task quantities
// live in `Task.Status` and the OTel trace stream — both of which are
// time-bounded and don't aggregate across the Prometheus storage horizon.
//
// # Re-export of Phase 2 metric
//
// `internal/budget/metrics.go` already registers
// `tide_provider_rate_limit_hits_total{project}`. To give Phase 4 callers a
// single import path for every metric the orchestrator publishes, this
// package re-exports it as the package-level variable
// `ProviderRateLimitHitsTotal`. The registration ITSELF is NOT duplicated —
// the alias points at the same `*prometheus.CounterVec` instance that
// internal/budget's init() registered on the controller-runtime registry.
//
// # Counter / histogram inventory (v1.0)
//
//	tide_waves_dispatched_total{project, phase, plan}              counter
//	tide_tasks_completed_total{project, phase, plan}               counter
//	tide_tasks_failed_total{project, phase, plan, reason}          counter
//	tide_dispatch_latency_seconds{level}                           histogram
//	tide_secret_leak_blocked_total{project, phase, plan}           counter   (W-1 / D-W1)
//	tide_push_jobs_total{project, outcome}                         counter
//	tide_budget_overruns_total{project}                            counter
//	tide_provider_rate_limit_hits_total{project}                   counter   (re-exported from internal/budget)
//
// Histogram buckets for `tide_dispatch_latency_seconds` are sized for K8s
// API + LLM-inference latency reality — [0.1, 0.5, 1, 5, 10, 30, 60, 300,
// 1800] seconds (100 ms → 30 min). Default Prometheus buckets are too small
// for LLM workloads (CONTEXT.md "Claude's Discretion" §195).
//
// # Reason / outcome cardinality
//
// `reason` for tide_tasks_failed_total: {exit-1, gitleaks, lease, auth,
// internal, budget} — 6 values, bounded.
//
// `outcome` for tide_push_jobs_total: {success, leak, lease, auth, internal}
// — 5 values, bounded.
//
// `level` for tide_dispatch_latency_seconds: {milestone, phase, plan, task}
// — 4 values, bounded (the level dimension is intentional — distinguishing
// planner vs. executor latency profiles in Grafana is core to the spec's
// "planner fans out wide, executor fans out narrow" claim).
package metrics
