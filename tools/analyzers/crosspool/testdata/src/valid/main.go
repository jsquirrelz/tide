// Synthetic single-pool fixture for the crosspool analyzer. The select
// statement below waits on plannerPool only; the analyzer must NOT
// produce any diagnostic. The valid fixture exercises the negative
// path: a properly bounded planner-side wait that does not unify the
// two parallelism budgets.
package main

import "context"

// Pool is a minimal stand-in for the real internal/pool.Pool type that
// Plan 01-04 will introduce. The crosspool analyzer matches on
// identifier name (not type), so the type shape here is irrelevant —
// only the variable names plannerPool / executorPool drive detection.
type Pool struct {
	sem chan struct{}
}

func main() {
	plannerPool := &Pool{sem: make(chan struct{}, 16)}
	ctx := context.Background()

	// VALID: select waits on plannerPool only. No diagnostic expected.
	select {
	case plannerPool.sem <- struct{}{}:
	case <-ctx.Done():
	}
}
