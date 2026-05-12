package dag

// NodeID is the unique identifier of a node in the DAG. Generic strings —
// callers project domain identifiers (Task names, artifact names) into this
// type.
type NodeID = string

// Edge expresses "From must complete before To."
type Edge struct {
	From NodeID
	To   NodeID
}

// ComputeWaves runs layered Kahn's algorithm over nodes and edges.
//
// Returns the layered topological sort as [][]NodeID where each inner slice
// is one wave (set of nodes whose upstream dependencies are all satisfied
// once previous waves have completed). Within each wave, NodeIDs are sorted
// lexicographically for deterministic output.
//
// Returns *CycleError if the graph contains a cycle.
//
// Complexity: O(V + E).
func ComputeWaves(nodes []NodeID, edges []Edge) ([][]NodeID, error) {
	panic("not yet implemented — Task 2 fills this body")
}
