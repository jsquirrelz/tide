// Synthetic cross-pool fixture for the crosspool analyzer. The select
// statement below waits on BOTH plannerPool and executorPool channels
// in the same case set — the canonical POOL-03 / Pitfall 6 violation
// shape. The directive on the select line drives analysistest's
// expectation that the analyzer produces a diagnostic citing the rule.
package main

import "context"

type Pool struct {
	sem chan struct{}
}

func main() {
	plannerPool := &Pool{sem: make(chan struct{}, 16)}
	executorPool := &Pool{sem: make(chan struct{}, 4)}
	ctx := context.Background()

	// VIOLATION: select waits on both pools. POOL-03 / Pitfall 6.
	select { // want `cross-pool wait`
	case plannerPool.sem <- struct{}{}:
	case executorPool.sem <- struct{}{}:
	case <-ctx.Done():
	}
}
