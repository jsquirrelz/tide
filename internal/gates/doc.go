/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package gates encapsulates every gate-policy decision and annotation
// handshake the Phase 4 reconcilers consume at level boundaries. It is the
// shared seam between the gate-policy code path AND the W-2 mid-stack push
// trigger — both call BoundaryDetected on the same children.
//
// # Vocabulary
//
// Three policy values live in the api/v1alpha3.Gates CRD field, CEL-validated
// at admission time to exactly this set:
//
//   - auto    — advance immediately; today's behavior. Default for phase,
//     plan, task.
//   - approve — pause at the boundary; resume when the annotation
//     "tideproject.k8s/approve-<level>": "true" arrives (D-G3).
//     Default for milestone.
//   - pause   — halt at the boundary; resume requires explicit `tide resume`
//     (D-G2). Stronger than approve: no annotation polling.
//
// # Default policy (D-G1)
//
//	milestone:          approve
//	phase / plan / task: auto
//	pauseBetweenWaves:   false
//
// DefaultGates() returns this struct. EvaluatePolicy applies the per-level
// default whenever the operator omitted the corresponding Gates field.
//
// # Annotations
//
// The package exports three annotation key constants (annotation.go):
//
//   - AnnotationApprovePrefix     = "tideproject.k8s/approve-"      (level
//     suffix: milestone | phase | plan | task)
//   - AnnotationApproveWavePrefix = "tideproject.k8s/approve-wave-" (integer
//     suffix per D-G3)
//   - AnnotationReject            = "tideproject.k8s/reject"        (carries
//     a human-readable reason as the value per D-G4)
//
// Approval is one-shot per D-G2 / threat T-04-G2 mitigation: ConsumeApprove
// returns a NEW annotation map with the approve-* key removed; the caller is
// responsible for the Patch. This mirrors budget.ConsumeBypass exactly.
//
// # Boundary detection
//
// BoundaryDetected(ctx, c, parent, childKind) returns true iff every
// child CRD of childKind under parent has Status.Phase=Succeeded. Empty
// children list is NOT a boundary (returns false) — at least one child must
// exist for "all children Succeeded" to be meaningful. The function is
// idempotent and pure-over-state (no writes); caller is responsible for
// loop-prevention against a false return (threat T-04-W2 mitigation).
//
// # References
//
//   - .planning/phases/04-gates-observability-dashboard-cli/04-CONTEXT.md
//     decisions D-G1 (default policy), D-G2 (consult at every boundary),
//     D-G3 (wave-approve annotation), D-G4 (reject + resume).
//   - .planning/phases/04-gates-observability-dashboard-cli/04-RESEARCH.md
//     §1300-1303 — co-location decision (this package over
//     internal/controller/push_helpers.go) for the shared seam.
//   - internal/budget/cap.go + internal/budget/precharge.go — structural
//     analogs for policy + annotation handshake (this package mirrors the
//     pure-func + ConsumeBypass shape).
package gates
