---
phase: 05-distribution-self-hosting-acceptance
plan: 06
subsystem: examples
tags: [examples, fixture, scaffold, mit-license, go-module, demo, d-b3]

# Dependency graph
requires:
  - phase: 05-distribution-self-hosting-acceptance
    provides: D-B3 lock — fixture lives in THIS repo, MIT-licensed inline, distinct from TIDE's Apache-2.0; canonical layout README + go.mod + go.sum + main.go + main_test.go (RESEARCH §P4.4)
provides:
  - examples/tide-demo-fixture/ — 5-file MIT-licensed standalone Go module (module github.com/jsquirrelz/tide-demo-fixture, go 1.26, stdlib-only)
  - Greeting(name string) string — the modifiable function the medium-sample outcome prompt operates on
  - TestGreeting — table-driven unit test (3 cases) for TIDE to extend
  - Source-of-truth seed content for Plan 05-12's //go:embed into cmd/tide-demo-init
affects: [05-12 (cmd/tide-demo-init //go:embed of this directory), 05-medium-sample (examples/projects/medium/ local-only git remote bootstrap)]

# Tech tracking
tech-stack:
  added: []  # stdlib-only — no new deps
  patterns:
    - "MIT-licensed fixture content embedded in Apache-2.0 TIDE repo with explicit demarcation (D-B3): SPDX-License-Identifier on each .go file + 'distinct from TIDE's Apache-2.0' note in README"
    - "Standalone Go module (not in TIDE root workspace) — fixture is content TIDE clones from file://, not a build-time dep"
    - "Table-driven _test.go shape for TIDE-extensible scaffolds"

key-files:
  created:
    - examples/tide-demo-fixture/README.md
    - examples/tide-demo-fixture/go.mod
    - examples/tide-demo-fixture/go.sum
    - examples/tide-demo-fixture/main.go
    - examples/tide-demo-fixture/main_test.go
  modified: []

key-decisions:
  - "Followed PLAN.md canonical values (module github.com/jsquirrelz/tide-demo-fixture, go 1.26, func Greeting) over additional_context's stale variant (example.com path, go 1.21, func Hello). PLAN.md is the authoritative artifact and acceptance_criteria grep enforces the canonical values."
  - "Kept fixture as a STANDALONE Go module — NOT added to TIDE root go.mod workspace or replace directives. The fixture is content TIDE clones via file:// URL at sample-apply time, not a build-time dep of TIDE itself."
  - "go.sum created as a zero-byte placeholder for forward-compat. Stdlib-only means no entries needed; go mod tidy would remove it but we keep it for the explicit 5-file layout match to RESEARCH §P4.4."
  - "Added 3 table-driven test cases (alice / world / empty) instead of just one, to demonstrate the _test.go shape TIDE will extend. Strictly stronger than the plan's 'one passing test' minimum; the acceptance_criteria grep only requires func TestGreeting which is present."

patterns-established:
  - "MIT-inline fixture pattern: SPDX-License-Identifier comment at top of each .go file + full MIT block + 'distinct from Apache-2.0' note in README. Replicable for any future TIDE-distributed fixture content that should remain license-distinct from the controller code."
  - "Standalone fixture-module pattern: separate go.mod with its own module path, not added to root workspace, intended for clone-via-file:// at runtime."

requirements-completed: [DIST-04]

# Metrics
duration: 1.2min
completed: 2026-05-22
---

# Phase 5 Plan 06: examples/tide-demo-fixture/ Scaffold Summary

**MIT-licensed 5-file standalone Go module (`github.com/jsquirrelz/tide-demo-fixture`) shipped as the source-of-truth seed for the medium sample's local-only git remote (D-B3) — Greeting(name string) string with table-driven test, ready for //go:embed into cmd/tide-demo-init in Plan 05-12.**

## Performance

- **Duration:** 1.2 min
- **Started:** 2026-05-22T16:35:09Z
- **Completed:** 2026-05-22T16:36:21Z
- **Tasks:** 1
- **Files created:** 5

## Accomplishments

- `examples/tide-demo-fixture/` directory created with exactly the 5 files mandated by RESEARCH §P4.4: README.md, go.mod, go.sum, main.go, main_test.go.
- Fixture content is MIT-licensed inline (full MIT License block in README + `SPDX-License-Identifier: MIT` on each .go file) — explicitly distinct from TIDE's Apache-2.0 distribution per D-B3. Acceptance grep verifies absence of Apache header.
- `Greeting(name string) string` returns deterministic `"Hello, " + name + "!"` — the function the medium-sample outcome prompt will modify.
- `TestGreeting` is table-driven with 3 cases (Alice, world, empty); `go test ./...` exits 0 in 0.353s.
- Fixture stands alone as `module github.com/jsquirrelz/tide-demo-fixture` go 1.26 — NOT added to TIDE root workspace or replace directives, since TIDE clones it via `file://` at runtime, not as a build-time dep.

## Task Commits

Each task was committed atomically:

1. **Task 1: Create examples/tide-demo-fixture/ scaffold** — `31fc1f1` (feat)

_No final-metadata commit yet — this SUMMARY itself will be the next commit per the executor protocol._

## Files Created

- `examples/tide-demo-fixture/README.md` (1525 bytes, 29 lines) — `# tide-demo-fixture` heading + one-paragraph framing + `## License` MIT block + explicit "distinct from TIDE's Apache-2.0 distribution per Phase 5 D-B3" note
- `examples/tide-demo-fixture/go.mod` (56 bytes, 3 lines) — `module github.com/jsquirrelz/tide-demo-fixture` + `go 1.26`
- `examples/tide-demo-fixture/go.sum` (0 bytes, 0 lines) — empty placeholder for the canonical 5-file layout (stdlib-only means `go mod tidy` is a no-op or removes it; we keep an empty file for layout match)
- `examples/tide-demo-fixture/main.go` (440 bytes, 17 lines) — `package main`, `SPDX-License-Identifier: MIT` + `Copyright (c) 2026 The TIDE Authors`, `Greeting(name string) string`, `func main() { fmt.Println(Greeting("world")) }`
- `examples/tide-demo-fixture/main_test.go` (568 bytes, 27 lines) — `package main`, same SPDX header, table-driven `TestGreeting` with 3 cases

## Verification Output

```
$ cd examples/tide-demo-fixture && go test -v ./...
=== RUN   TestGreeting
=== RUN   TestGreeting/alice
=== RUN   TestGreeting/world
=== RUN   TestGreeting/empty
--- PASS: TestGreeting (0.00s)
    --- PASS: TestGreeting/alice (0.00s)
    --- PASS: TestGreeting/world (0.00s)
    --- PASS: TestGreeting/empty (0.00s)
PASS
ok  	github.com/jsquirrelz/tide-demo-fixture	0.353s
```

All 13 acceptance_criteria checks passed (12 positive greps + 1 negative grep for absence of Apache header).

## Decisions Made

- **Followed PLAN.md canonical values over `additional_context`'s stale variant.** The orchestrator's `additional_context` block specified `example.com/tide-demo-fixture` module path, `go 1.21`, and function `Hello`; PLAN.md's acceptance_criteria mandate `module github.com/jsquirrelz/tide-demo-fixture`, `^go 1.26`, and `func Greeting`. PLAN.md is the authoritative artifact (it's the planner-resolved D-B3 specification) and its acceptance grep enforces its values. The `additional_context` appears to have been authored against a draft of the plan before the planner finalized D-B3's specifics; following PLAN.md was the only path that satisfies the success_criteria as written.
- **Standalone Go module, not in root workspace.** Per PLAN.md NOTE at the end of the action block: "It is NOT added to the TIDE root go.mod's workspace or replace directives — it stands alone." Confirmed root `go.mod` is untouched; `go test ./...` from the fixture dir resolves to its own module-local `Greeting` symbol.
- **3-case table-driven test instead of 1-case.** PLAN.md's `<action>` says "One test: `func TestGreeting(t *testing.T) { got := Greeting("Alice"); want := "Hello, Alice!"; ... }`" and the acceptance_criteria require `grep -q "func TestGreeting"` (presence only, no shape constraint). I shipped 3 sub-cases (`Alice`, `world`, `empty`) to demonstrate the table-driven shape TIDE will extend when the medium-sample outcome prompt fires. Strictly stronger than the floor; all 3 cases pass.
- **Empty `go.sum` kept as 0-byte file.** PLAN.md File 3 instruction: "Empty file (zero bytes). Stdlib-only means no entries needed. If the executor's `go mod tidy` removes it, that's acceptable — the file is a placeholder for forward-compat." Did NOT run `go mod tidy` (would have removed the file and broken the 5-file layout match to RESEARCH §P4.4). `go test ./...` passes without it.

## Deviations from Plan

None — plan executed exactly as written. The PLAN.md vs `additional_context` discrepancy on module path / Go version / function name was resolved in PLAN.md's favor (it is the authoritative planner-resolved artifact and its acceptance_criteria grep enforces its values); this is not a deviation from the plan, it is a deviation from a stale prompt-context block.

## Issues Encountered

None.

## Self-Check: PASSED

**Files created (all 5 present, sizes as expected):**

```
$ ls -la examples/tide-demo-fixture/
-rw-r--r--  1 user staff  1525 May 22 12:35 README.md
-rw-r--r--  1 user staff    56 May 22 12:35 go.mod
-rw-r--r--  1 user staff     0 May 22 12:35 go.sum
-rw-r--r--  1 user staff   440 May 22 12:35 main.go
-rw-r--r--  1 user staff   568 May 22 12:35 main_test.go
```

**Commit landed:** `git log --oneline -1` shows `31fc1f1 feat(05-06): scaffold examples/tide-demo-fixture (D-B3 medium-sample seed)` on branch `worktree-agent-a97c2371df3a5fa8e`. (See "Plan Closeout Commit" below for the SUMMARY.md commit SHA that will be appended after this file is committed.)

**Acceptance grep matrix (all 13 checks PASS):**

| # | Check | Result |
|---|-------|--------|
| 1 | `test -d examples/tide-demo-fixture` | exit 0 |
| 2 | `test -s examples/tide-demo-fixture/README.md` | exit 0 (1525 bytes) |
| 3 | `grep -q "MIT License" .../README.md` | exit 0 |
| 4 | `grep -q "Copyright (c) 2026 The TIDE Authors" .../README.md` | exit 0 |
| 5 | `grep -q "module github.com/jsquirrelz/tide-demo-fixture" .../go.mod` | exit 0 |
| 6 | `grep -q "^go 1.26" .../go.mod` | exit 0 |
| 7 | `grep -q "SPDX-License-Identifier: MIT" .../main.go` | exit 0 |
| 8 | `grep -q "package main" .../main.go` | exit 0 |
| 9 | `grep -q "func Greeting" .../main.go` | exit 0 |
| 10 | `grep -q "func main()" .../main.go` | exit 0 |
| 11 | `grep -q "func TestGreeting" .../main_test.go` | exit 0 |
| 12 | `(cd examples/tide-demo-fixture && go test ./...)` | exit 0 (PASS) |
| 13 | `grep -q "Apache License" .../main.go` | exit 1 (NEGATIVE — MUST be absent) |

## Next Phase Readiness

- Fixture is ready to be embedded via `//go:embed examples/tide-demo-fixture/*` (or equivalent) into Plan 05-12's `cmd/tide-demo-init/` binary. The 5-file layout, the `Greeting` function (modifiable target for the outcome prompt), and the passing `TestGreeting` provide everything Plan 05-12 needs.
- The fixture's separate Go module status means Plan 05-12 will need to embed the directory as raw files (the embedded content is the seed for a fresh local-only git remote, not Go source the parent module compiles against). This is the intended D-B3 mechanic: TIDE clones the embedded content from a `file://` URL after the init binary materializes it onto the project PVC.
- No blockers. No carry-forward items.

## Plan Closeout Commit

_Will be appended after the SUMMARY.md is committed (final commit of this plan)._

---
*Phase: 05-distribution-self-hosting-acceptance*
*Plan: 06*
*Completed: 2026-05-22*
