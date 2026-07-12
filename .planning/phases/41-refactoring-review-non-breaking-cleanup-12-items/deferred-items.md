# Deferred Items — Phase 41 Plan 03

Out-of-scope discoveries logged per executor SCOPE BOUNDARY rule. Not fixed as part of this plan.

## `cmd/tide-demo-init` missing embedded `fixture/` directory

`go build ./...` fails with:

```
cmd/tide-demo-init/main.go:112:12: pattern all:fixture: no matching files found
```

`cmd/tide-demo-init/main.go` declares `//go:embed all:fixture` but no `fixture/` directory
exists at `cmd/tide-demo-init/` in this checkout. Confirmed unrelated to Plan 41-03 (no files
in `cmd/tide-demo-init/` were touched by this plan; the directory listing shows only
`main.go`, `main_test.go`, `README.md` — no `.gitignore` entry excludes it either). Pre-existing
environmental gap, not introduced by this plan. Plan verification instead uses the scoped
commands `go build ./... && go vet ./internal/controller/...` (Task 1/2) and
`go test ./internal/controller/... ./cmd/manager/... -count=1` (Task 3), consistent with the
plan's own `<verify>` blocks — both pass clean on the touched packages.
