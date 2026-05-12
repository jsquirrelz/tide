// Command tide-lint runs TIDE's custom go/analysis Passes against the
// module. Phase 1 ships exactly one analyzer — crosspool (POOL-03 /
// Pitfall 6 prevention, see tools/analyzers/crosspool). Phase 2+ may
// register additional analyzers; when that happens, swap
// singlechecker.Main for multichecker.Main and pass all Analyzers.
//
// Invocation:
//
//	go run ./cmd/tide-lint ./...
//	make tide-lint            # convenience target wiring the same call
//
// CI gate: .github/workflows/ci.yaml runs `make tide-lint` and fails
// the PR on any reported diagnostic.
//
// Why singlechecker (not unitchecker): singlechecker is the standalone
// "run this one analyzer over a module" entrypoint that accepts
// package patterns like ./... directly; unitchecker is the
// go-vet integration shape (invoked as `go vet -vettool=tide-lint`).
// Phase 1's use case is a top-level CI gate, which is singlechecker.
package main

import (
	"golang.org/x/tools/go/analysis/singlechecker"

	"github.com/jsquirrelz/tide/tools/analyzers/crosspool"
)

func main() {
	singlechecker.Main(crosspool.Analyzer)
}
