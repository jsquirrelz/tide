// Package dag is a pure-Go, stdlib-only implementation of Kahn's algorithm
// in layered form. It is consumed twice in TIDE:
//
//  1. Planning DAG: nodes are artifacts to author (MILESTONE.md, phase briefs,
//     PLAN.md). Edges are "this artifact's authoring requires another
//     artifact's interface." Used by Milestone/Phase reconcilers (Phase 2/3).
//
//  2. Execution DAG: nodes are Tasks. Edges are declared task dependencies.
//     Used by the Plan admission webhook (Phase 2) and the WaveReconciler
//     (Phase 2).
//
// Per DAG-03, callers wrap pkg/dag outputs in their respective domain types
// (PlanningWave, ExecutionWave) rather than passing the raw [][]NodeID
// around. This keeps the two DAGs typed apart at the API level while sharing
// the algorithm. Phase 1 ships pkg/dag as a leaf package; Phase 2 introduces
// the wrappers.
//
// Per DAG-05, this package MUST NOT import:
//   - k8s.io/*       (any)
//   - sigs.k8s.io/*  (any)
//   - github.com/anthropics/* (any)
//
// Enforced by the `make verify-dag-imports` Makefile target wired into CI.
//
// Worked example (DAG-04 regression fixture, matches README.md spec):
//
//	Nodes: alpha,beta,gamma,delta,epsilon,zeta,eta,theta
//	Edges: alpha->delta, beta->delta, gamma->eta, zeta->eta,
//	       delta->epsilon, eta->theta
//	Waves: [{alpha,beta,gamma,zeta}, {delta,eta}, {epsilon,theta}]
//
// Complexity: O(V + E) — re-derived on every reconcile per the spec's
// "schedule is derived, not cached" principle.
package dag
