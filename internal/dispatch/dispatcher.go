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

package dispatch

import (
	"context"

	pkg "github.com/jsquirrelz/tide/pkg/dispatch"
)

// Dispatcher is the runtime injection seam for dispatching a Task subagent.
//
// The Phase 1 placeholder at internal/dispatch/doc.go (type Dispatcher interface{})
// is replaced by this real body in Phase 2. All Phase 1 reconciler structs that
// declared a `Dispatcher dispatch.Dispatcher` field continue to compile unchanged
// — the import path and the field name are unchanged; only the interface body has
// been filled.
//
// Subtle but important: Run is NOT called from inside Reconcile(). Calling Run
// from Reconcile would block the reconciler goroutine on I/O (Pitfall 1 — forbidden
// by controller-runtime's single-reconcile-goroutine contract). Instead, the
// executor path for Phase 2 is split:
//
//   - TaskReconciler.createDispatchJob: calls BuildJobSpec + client.Create (sync, fast) from
//     inside Reconcile.
//   - handleJobCompletion: reacts to Owns-watch events when the Job reaches terminal
//     state; reads EnvelopeOut from the PVC.
//
// Run is exposed for:
//  1. Test fixtures that need a single call to drive a Job end-to-end.
//  2. Phase 3+ planner-dispatch callers that run in a goroutine spawned outside
//     Reconcile (e.g., the PlanReconciler spawning a planner subagent for the
//     planning-DAG wave).
type Dispatcher interface {
	// Run creates the Task's Job (idempotent — AlreadyExists is success), watches it
	// to terminal state, reads the EnvelopeOut from the per-Project PVC, and returns
	// it. Must NOT be called from inside Reconcile() — see type doc.
	Run(ctx context.Context, in pkg.EnvelopeIn) (pkg.EnvelopeOut, error)
}
