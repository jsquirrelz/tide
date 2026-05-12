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
