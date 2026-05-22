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

package podjob

import (
	"fmt"

	"k8s.io/apimachinery/pkg/types"
)

// JobName returns the deterministic Job name for a Task's nth dispatch attempt.
//
// Format: "tide-task-{taskUID}-{attempt}".
//
// SUB-03: This name IS the idempotency dedup key. When the TaskReconciler creates
// the Job and a watch-lag race causes a duplicate Create, the API server returns
// AlreadyExists — the caller (PodJobBackend.Run and TaskReconciler) treats
// AlreadyExists as success (Pitfall F — Job-create races against Job-status watch
// lag). AlreadyExists on duplicate Create == successful idempotent dispatch.
//
// The attempt number is incremented by TaskReconciler at Job-creation time
// (D-B5), not at Job-completion time. Retry cap: Project.Spec.maxAttemptsPerTask.
func JobName(taskUID types.UID, attempt int) string {
	return fmt.Sprintf("tide-task-%s-%d", taskUID, attempt)
}

// PlannerJobName returns the deterministic Job name for a planner dispatch at the
// given level.
//
// Format: "tide-{level}-{parentUID}-{attempt}".
//
// Matches the existing milestone_controller.go:176 name shape
// (tide-milestone-<uid>-1). The attempt number is single-shot per ROADMAP scope;
// retry semantics are tracked as CR-NN for a future plan.
// AlreadyExists on duplicate Create == successful idempotent dispatch (SUB-03).
//
// Phase 04.1 P1.2.
func PlannerJobName(level, parentUID string, attempt int) string {
	return fmt.Sprintf("tide-%s-%s-%d", level, parentUID, attempt)
}
