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

package dag

import "fmt"

// CycleError is returned by ComputeWaves when the input graph contains a cycle.
// InvolvedNodes lists every node whose indegree never reached zero — i.e.,
// every node involved in the unresolvable cyclic state. Sorted lexicographically
// for deterministic output. Per DAG-02.
type CycleError struct {
	InvolvedNodes []NodeID
}

func (e *CycleError) Error() string {
	return fmt.Sprintf("cyclic DAG: nodes with unresolvable indegrees: %v", e.InvolvedNodes)
}
