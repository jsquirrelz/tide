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
