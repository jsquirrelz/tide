// Command tide-lint runs TIDE's custom go/analysis Passes against the
// module. Phase 1 shipped crosspool (POOL-03 / Pitfall 6 prevention).
// Phase 2 added providerfirewall.Analyzer (SUB-05 / Pitfall 14 prevention);
// this is the multichecker form. Phase 4 added metriccardinality.Analyzer
// (OBS-02 / Pitfall 17 / D-X4 prevention). Phase 04.1 added dagimports.Analyzer
// (DAG-05 fixture mirror; weaker than `make verify-dag-imports` transitive check
// but provides IDE-time fast feedback). New analyzers register by
// appending to multichecker.Main(...).
//
// Invocation:
//
//	go run ./cmd/tide-lint ./...
//	make tide-lint            # convenience target (Phase 1 back-compat)
//	make verify-import-firewall  # SUB-05-named alias for CI gating
//
// CI gate: .github/workflows/ci.yaml runs `make tide-lint` and fails
// the PR on any reported diagnostic. `make verify-import-firewall`
// is the dedicated Phase 2 alias that also invokes this binary.
//
// Why multichecker (not singlechecker): multichecker accepts multiple
// Analyzer registrations in a single invocation; singlechecker is
// single-analyzer only. Phase 1 used singlechecker; Phase 2 flipped
// to multichecker to add the providerfirewall SUB-05 gate alongside
// the existing crosspool POOL-03 gate without a second binary.
package main

import (
	"golang.org/x/tools/go/analysis/multichecker"

	"github.com/jsquirrelz/tide/tools/analyzers/crosspool"
	"github.com/jsquirrelz/tide/tools/analyzers/dagimports"
	"github.com/jsquirrelz/tide/tools/analyzers/metriccardinality"
	"github.com/jsquirrelz/tide/tools/analyzers/providerfirewall"
)

func main() {
	multichecker.Main(
		crosspool.Analyzer,
		providerfirewall.Analyzer,
		metriccardinality.Analyzer,
		dagimports.Analyzer, // Phase 04.1 P2.1 — wired alongside the shell-based verify-dag-imports
		// Makefile target as a redundant fast-feedback IDE-time gate.
	)
}
