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

// Package owner — see owner.go for package overview.
//
// This file provides the canonical project-label helper (CUTS-01 / D-01).
// Every child CR create site must call StampProjectLabel before c.Create()
// so that tideproject.k8s/project is present from birth, enabling
// label-filtered discovery by `tide approve` and the dashboard.
package owner

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LabelProject is the canonical TIDE project label key.
// All child CRs created by the reporter or a reconciler must carry this
// label so that `tide approve` label-filtered discovery finds them on the
// first call (CUTS-01 run-1 finding-6).
const LabelProject = "tideproject.k8s/project"

// LabelWavePaused is stamped on Tasks by PlanReconciler's wave-pause gate
// (value is the paused wave index) and read by the Task dispatch gate and
// git_writer's cumulative-branch scan to hold dispatch/integration at an
// unapproved wave boundary. Cleared by PlanReconciler on wave-approve.
const LabelWavePaused = "tideproject.k8s/wave-paused"

// LabelWaveIndex is stamped on Tasks by the global wave aggregator
// (WaveReconciler/ProjectReconciler) and read by the project status
// roll-up, the dashboard waves/execution-dag API, and the tide CLI to
// group Tasks by their derived global wave.
const LabelWaveIndex = "tideproject.k8s/wave-index"

// LabelAttempt is stamped on dispatch Jobs by the podjob jobspec builder
// (value is the current attempt number) and read by TaskReconciler's
// retry-accounting scan to compute the next attempt.
const LabelAttempt = "tideproject.k8s/attempt"

// StampProjectLabel stamps the canonical tideproject.k8s/project label on obj.
// Must be called at every child CR create site BEFORE c.Create().
//
// Semantics:
//   - No-op when projectName is empty (fail-open: don't prevent creation when
//     the parent label is absent — RESEARCH Pitfall 1).
//   - Lazily initialises the labels map when nil.
//   - Overwrites any pre-existing tideproject.k8s/project value, including
//     LLM-authored labels (Tampering mitigation T-15-01: the authoritative
//     parent label wins, same doctrine as stampParentRef).
func StampProjectLabel(obj metav1.Object, projectName string) {
	if projectName == "" {
		return
	}
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[LabelProject] = projectName
	obj.SetLabels(labels)
}
