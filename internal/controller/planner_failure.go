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

// planner_failure.go — shared D4 false-leaf guard for phase and milestone planner
// succession sites (Phase 33, PLANFAIL-01/02).
//
// A planner Job that exits nonzero with zero children would otherwise be misclassified
// as a genuine leaf (exitCode==0, childCount==0) and falsely advance its parent via
// patchXSucceeded, corrupting the planning DAG. This predicate is the single source
// of truth for the D4 false-leaf contract; it must be inserted before the
// expected == 0 → patchXSucceeded branch at both succession sites.
//
// Plan and project are DELIBERATELY excluded. Those levels succeed only via
// gates.BoundaryDetected (returns matched > 0, false on zero children), so a
// zero-child failed planner cannot drive them to Succeeded. See 33-CONTEXT.md D-02
// for the full reasoning — do not "complete the set" by adding the guard there.
package controller

import pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"

// isPlannerFailure returns true when a completed planner Job exited nonzero with
// zero children — the "false-leaf" condition that would otherwise cause patchXSucceeded
// to corrupt the planning DAG by falsely advancing the parent level.
//
// The check is envReadOK && ExitCode != 0 && ChildCount == 0.
//
//   - !envReadOK → envelope unavailable (transient read error); caller handles
//     separately. The zero-value EnvelopeOut has ExitCode==0, so this branch is
//     safe even without the envReadOK guard — but the guard makes intent explicit.
//   - ExitCode == 0 → planner succeeded; ChildCount==0 is a genuine leaf → Succeed.
//   - ExitCode != 0, ChildCount > 0 → planner failed but has already authored children;
//     leave Running, let the reporter drain. (Unusual; not the D4 false-leaf target.)
//   - ExitCode != 0, ChildCount == 0 → false-leaf; mark Failed (this guard fires).
//
// Called only at phase and milestone succession sites. NOT called at plan or project
// level (see package doc and 33-CONTEXT.md D-02).
func isPlannerFailure(out pkgdispatch.EnvelopeOut, envReadOK bool) bool {
	return envReadOK && out.ExitCode != 0 && out.ChildCount == 0
}
