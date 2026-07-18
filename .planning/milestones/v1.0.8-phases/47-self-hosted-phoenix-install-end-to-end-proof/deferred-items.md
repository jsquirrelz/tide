# Deferred Items — Phase 47

Out-of-scope discoveries logged during plan execution (not fixed; scope boundary per executor protocol).

## Plan 47-07

- **`internal/controller/span_emission_unit_test.go:897`** — `golangci-lint` (modernize/slicescontains) flags a loop that could use `slices.Contains`. Pre-existing (introduced in commit `565daae`, Phase 46 WR-01), not touched by plan 47-07's five spawn-site edits. Out of scope for this gap-closure plan.
