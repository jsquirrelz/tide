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

// Package budget — see doc.go for package overview.
package budget

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// ProviderRateLimitHitsTotal counts the number of times the per-Secret-UID
// rate-limit bucket was exhausted, surfaced at the Project granularity.
//
// Cardinality discipline (Pitfall 17): the label set is {project} only.
// Per-Secret-UID dimension lives in the in-process sync.Map (bucket.go),
// NOT in a metric label. Adding a {secret_uid} label would produce O(project ×
// secret) cardinality — unacceptable on long-lived clusters.
//
// Callers: TaskReconciler (Plan 09) calls
//
//	budget.ProviderRateLimitHitsTotal.WithLabelValues(project.Name).Inc()
//
// when bucket.Reserve() returns a non-zero delay (i.e., the token bucket is
// exhausted and the dispatch must be requeued).
//
// Usage: the counter increments once per rate-limit hit, not once per delayed
// request. A batch of requests that are all rate-limited in the same reconcile
// loop produces one increment. Plan 09's RequeueAfter handles the pacing.
var ProviderRateLimitHitsTotal *prometheus.CounterVec

func init() {
	ProviderRateLimitHitsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tide_provider_rate_limit_hits_total",
			Help: "Count of times the per-Secret-UID rate-limit bucket was exhausted, surfaced by Project.",
		},
		[]string{"project"},
	)
	metrics.Registry.MustRegister(ProviderRateLimitHitsTotal)
}
