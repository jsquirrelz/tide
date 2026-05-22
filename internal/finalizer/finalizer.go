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

// Package finalizer provides a bounded-deadline finalizer recipe that
// prevents the "deletion stuck" failure mode (Pitfall 21).
//
// On deletion, HandleDeletion runs the caller's cleanup function under a
// derived context with the configured deadline. If cleanup succeeds, the
// finalizer is removed and garbage collection proceeds. If cleanup exceeds
// the deadline, the finalizer is FORCIBLY removed and a log warning is
// emitted — this prevents an indefinite leak where a failed external system
// holds the K8s object hostage in Terminating state forever.
//
// If cleanup returns a non-timeout error, the reconcile is requeued and the
// finalizer is NOT removed (transient failures get retried).
//
// Per CTRL-05 from .planning/REQUIREMENTS.md. The cleanup callback is the
// caller's responsibility to make idempotent — this helper only wraps the
// bounded deadline + finalizer-string removal mechanics.
package finalizer

import (
	"context"
	"errors"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// HandleDeletion runs the caller-supplied cleanup for `obj` under a bounded
// deadline derived from ctx + timeout.
//
// Return contract:
//   - finalizer absent → ctrl.Result{}, nil (idempotent; nothing to do)
//   - cleanup returns nil → finalizer removed, ctrl.Result{}, nil
//   - cleanup returns context.DeadlineExceeded → log loudly, finalizer
//     FORCIBLY removed, ctrl.Result{}, nil (Pitfall 21 prevention)
//   - cleanup returns any other error → ctrl.Result{Requeue: true}, err
//     (finalizer retained for next reconcile)
//
// The cleanup function is invoked exactly once per call. Callers must make
// cleanup idempotent if it may run multiple times across reconcile passes
// (e.g. transient errors that trigger requeue).
func HandleDeletion(
	ctx context.Context,
	c client.Client,
	obj client.Object,
	finalizerName string,
	cleanup func(context.Context) error,
	timeout time.Duration,
) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(obj, finalizerName) {
		return ctrl.Result{}, nil
	}

	cleanupCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := cleanup(cleanupCtx); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			log.FromContext(ctx).Error(err,
				"finalizer cleanup deadline exceeded; forcibly removing finalizer (Pitfall 21 prevention)",
				"namespace", obj.GetNamespace(),
				"name", obj.GetName(),
				"finalizer", finalizerName,
				"deadline", timeout.String())
			controllerutil.RemoveFinalizer(obj, finalizerName)
			return ctrl.Result{}, c.Update(ctx, obj)
		}
		// Non-timeout error: keep finalizer, requeue. Caller's controller
		// gets another chance once the transient condition clears.
		return ctrl.Result{Requeue: true}, err
	}

	controllerutil.RemoveFinalizer(obj, finalizerName)
	return ctrl.Result{}, c.Update(ctx, obj)
}
