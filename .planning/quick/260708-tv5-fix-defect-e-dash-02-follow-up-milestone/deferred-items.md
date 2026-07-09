# Deferred Items — quick-260708-tv5

Out-of-scope discoveries surfaced during execution. NOT fixed by this plan
(files untouched by the Defect E controller fix; pre-existing debt).

## Pre-existing `make lint` findings (8) — in files this plan did not touch

`make lint` exits 2 solely on pre-existing golangci-lint debt in the tide-push
CLI and the dashboard gitfetch tests. All three files this plan modified
(`push_helpers.go`, `project_controller.go`, `project_boundary_push_test.go`)
are lint-clean. The offending files were last modified by commits `e6a913c`
(37-02) and `123448f` (37-03), not by tv5.

| File | Line | Linter | Finding |
|------|------|--------|---------|
| cmd/tide-push/main.go | 418 | gocyclo | `runPush` cyclomatic complexity 34 (>30) |
| cmd/tide-push/main.go | 753 | lll | line 134 chars (>120) |
| cmd/tide-push/main.go | 773 | lll | line 131 chars (>120) |
| cmd/tide-push/main.go | 787 | lll | line 121 chars (>120) |
| cmd/tide-push/main.go | 790 | lll | line 133 chars (>120) |
| cmd/tide-push/main.go | 148 | modernize (stringscut) | `strings.Index` → `strings.Cut` |
| cmd/dashboard/gitfetch/store_test.go | 218 | modernize (rangeint) | `for` loop → range over int |
| cmd/dashboard/gitfetch/gitfetch_test.go | 293 | prealloc | preallocate `out` with `len(m)` |

These predate the plan and are unrelated to the boundary-push supersede fix.
Recommend a separate lint-debt cleanup pass on the tide-push CLI + dashboard.
