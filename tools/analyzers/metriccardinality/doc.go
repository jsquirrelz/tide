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

// Package metriccardinality implements a go/analysis Analyzer that rejects
// the literal string "task" as a label name in any of the four cardinality-
// generating Prometheus *Vec constructors:
//
//   - prometheus.NewCounterVec
//   - prometheus.NewHistogramVec
//   - prometheus.NewGaugeVec
//   - prometheus.NewSummaryVec
//
// OBS-02 / Pitfall 17 / D-X4 prevention. The TIDE orchestrator runs hundreds
// to thousands of K8s Tasks per phase; adding "task" to a Prometheus label
// slice would multiply every series by the active Task count, producing
// unbounded cardinality growth (a known Prometheus operational hazard). The
// approved bounded-cardinality label set is {project, phase, plan} plus the
// optional dimensions {reason, outcome, vendor, level}. "task" is never
// permitted as a label; per-task observability flows through OTel traces
// (one span per Task) instead.
//
// The analyzer is registered into the cmd/tide-lint multichecker so any PR
// that introduces a "task" label literal anywhere in the module fails CI at
// `make tide-lint`. The companion fixtures under testdata/src/{badlabels,
// goodlabels}/ assert positive and negative cases per analysistest's
// GOPATH-style resolver convention.
//
// # Known limitation — WR-15
//
// The analyzer matches *ast.BasicLit nodes of kind STRING. A caller that
// names the label via an identifier or named constant bypasses the check:
//
//	const taskLabel = "task"
//	prometheus.NewCounterVec(opts, []string{taskLabel}) // NOT caught
//
//	var taskLabel = "task"
//	prometheus.NewCounterVec(opts, []string{taskLabel}) // NOT caught
//
// Pattern-matching analyzers (without go/types resolution) can only see
// literal string nodes. Reviewers MUST treat this as a literal-only
// guardrail rather than a complete cardinality oracle. If the codebase
// needs identifier-aware detection later, extend the analyzer with
// go/types to resolve const declarations to their string value
// (significant rework — judge cost/benefit; the literal form is the
// idiomatic Prometheus label-slice shape so the literal check catches
// the common case).
package metriccardinality
