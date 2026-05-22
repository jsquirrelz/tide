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

// Package budget implements TIDE's two-cache budget model:
//
//  1. In-memory token-bucket rate limiter, keyed by Secret UID (FAIL-03).
//     Backed by a sync.Map of *rate.Limiter values. Lives in-process only
//     (D-D1: per-second update granularity would crush etcd via Status churn).
//     Pre-charged from active Jobs on Manager restart (best-effort per Pitfall C).
//
//  2. Per-Project budget tally, durable via Project.Status.Budget (FAIL-04).
//     One Status Patch per Task completion (D-D2). Halt is structural:
//     TaskReconciler (Plan 09) checks IsCapExceeded before every dispatch.
//
// The two homes are intentional per the two-cache split documented in
// 02-CONTEXT.md "Specifics" — do not unify them.
//
// Pitfall 9 prevention: rate-limit hits are absorbed by returning
// rate.Reservation.Delay() to the TaskReconciler, which schedules a
// ctrl.Result{RequeueAfter: delay} rather than blocking the Reconcile goroutine.
//
// Pitfall 17 discipline: the Prometheus counter tide_provider_rate_limit_hits_total
// carries only a {project} label. Per-Secret-UID cardinality stays in the
// in-process sync.Map, never in a metric label.
package budget
