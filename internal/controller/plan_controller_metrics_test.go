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

// Phase 24 Plan 04: per-plan materializeWaves was removed (D-03 — single Wave writer).
// The three TestMaterializeWaves_* tests that called r.materializeWaves directly are
// removed here because the function no longer exists on PlanReconciler.
//
// WavesDispatchedTotal metric emission now lives in
// ProjectReconciler.deriveGlobalWaves (project_controller.go) and is covered by
// the envtest suite (global_wave_derivation_test.go BidirectionalIndex specs).
// The metrics registry arity tests remain in internal/metrics/registry_test.go.
package controller
