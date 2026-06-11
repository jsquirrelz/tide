---
phase: 03-up-stack-reconcilers-git-integration-real-subagent-resumptio
plan: 04
subsystem: security
tags: [gitleaks, secret-scanning, go-embed, viper, defense-in-depth, push-job-utility]

# Dependency graph
requires:
  - phase: 02-dispatch-plan-validation-innermost-reconcilers-harness
    provides: "internal/harness/redact (Phase 2 D-F4) — the FIRST line of secret-leak defense on subagent stdout; this plan ships the SECOND line at the push boundary"
provides:
  - "internal/gitleaks.ScanDiff(diff) — embedded gitleaks v8.30.1 default ruleset (150+ rules) scans a diff and returns (found, findings, err)"
  - "internal/gitleaks.ScanDiffWithConfig(diff, *detect.Detector) — accepts a pre-built detector (typically from LoadConfig with per-Project override)"
  - "internal/gitleaks.LoadConfig(configPath) — parses a TOML config via gitleaks/v8/config + viper; supports [extend] useDefault=true compose; empty path returns the default detector"
  - "internal/gitleaks.DefaultRulesTOML() — returns a copy of the embedded gitleaks v8.30.1 ruleset bytes for inspection/audit"
  - "internal/gitleaks.Finding — type alias for github.com/zricethezav/gitleaks/v8/report.Finding so consumers can import only this package"
affects:
  - "03-06 (cmd/tide-push) — consumes ScanDiff at the push boundary; ConfigMap-mount mechanics for the override TOML live in 03-06"
  - "03-08 (ProjectReconciler push-result observation) — exit-code reason='leak-detected' translates to Prometheus counter increment (ART-07)"

# Tech tracking
tech-stack:
  added:
    - "github.com/zricethezav/gitleaks/v8 v8.30.1 (Go library — defense-in-depth, NOT shell-out, NOT a separate container per CLAUDE.md anti-pattern)"
    - "github.com/spf13/viper v1.21.0 (gitleaks-required TOML config parser; isolated via viper.New() so the global is not polluted)"
  patterns:
    - "go:embed for auditable vendored config (default_rules.toml is the TIDE-side audit copy of gitleaks v8.30.1 upstream config/gitleaks.toml; gitleaks itself embeds the same content)"
    - "Type aliasing third-party result types (Finding = report.Finding) so consumer packages do not need to import gitleaks/v8/report directly"
    - "Private viper.Viper for config parsing (avoids contaminating the global viper used by gitleaks/v8/detect.NewDetectorDefaultConfig)"

key-files:
  created:
    - "internal/gitleaks/scanner.go — ScanDiff + ScanDiffWithConfig + go:embed default_rules.toml + DefaultRulesTOML accessor"
    - "internal/gitleaks/config.go — LoadConfig (empty-path → default; TOML path → viper + ViperConfig.Translate + detect.NewDetector)"
    - "internal/gitleaks/doc.go — package godoc citing D-B3 and ART-07; mirrors internal/harness/redact/doc.go style"
    - "internal/gitleaks/default_rules.toml — vendored verbatim from gitleaks v8.30.1 upstream config/gitleaks.toml + 6-line audit header"
    - "internal/gitleaks/scanner_test.go — 6 test cases (3 ScanDiff scenarios + 2 LoadConfig scenarios + 1 embed sanity)"
  modified:
    - "go.mod / go.sum — gitleaks/v8 + spf13/viper added; incidental promotion of k8s.io/utils from indirect → direct (already used in internal/dispatch/podjob/jobspec.go)"

key-decisions:
  - "Finding is a type alias for gitleaks/v8/report.Finding, not detect.Finding (the plan's interface block was wrong about the upstream type — DetectString returns []report.Finding). The local alias preserves the public-facing Finding name without forcing consumers to import a third gitleaks subpackage. Rule 1 deviation."
  - "Test fixture for the Anthropic key is a deterministic pseudo-random 93-char body that explicitly avoids the gitleaks v8 global stopword 'abcdefghijklmnopqrstuvwxyz'. Without this care, a naïvely 'A repeated' fixture is silently allow-listed and the test produces a false negative — exactly the failure mode that caught us in the first GREEN-phase iteration."
  - "Test fixture for the AWS key avoids 'AKIAIOSFODNN7EXAMPLE' (the AWS docs canonical example) because the upstream gitleaks rule allow-lists anything matching '.+EXAMPLE$'. A fresh 16-char tail in [A-Z2-7] that is not docs-shaped is the correct fixture."
  - "LoadConfig uses a fresh viper.New() rather than the global viper. gitleaks/v8/detect.NewDetectorDefaultConfig also uses the global viper internally; keeping our overrides isolated avoids cross-test contamination and is hygienic for the push-Job production caller (single binary, multiple ConfigMap loads over its lifetime)."
  - "default_rules.toml is the TIDE-side audit copy (98 KB, 3209 rules-lines). gitleaks v8 internally embeds the SAME content via config.DefaultConfig — so ScanDiff() with no LoadConfig hand-off uses gitleaks' internal embedded copy, and our own copy is exposed via DefaultRulesTOML() for inspection only. This is intentional duplication: it gives operators an in-tree audit trail without having to spelunk through go.mod cache."

patterns-established:
  - "Pattern A: Utility package wrapping a third-party scanner — mirrors internal/harness/redact (same role: thin Go-library wrapper, no LLM-SDK imports, no K8s API imports, table-driven tests)"
  - "Pattern B: Type alias for third-party result types — preserves the package's public surface even when the upstream library splits types across subpackages"
  - "Pattern C: go:embed + DefaultXxx() accessor — gives operators an in-tree audit copy of vendored upstream config without coupling the scan hot-path to the file IO"

requirements-completed:
  - ART-07

# Metrics
duration: 45min
completed: 2026-05-16
---

# Phase 3 Plan 4: internal/gitleaks Defense-in-Depth Scanner Summary

**`internal/gitleaks.ScanDiff(diff)` and `LoadConfig(path)` ship as a thin Go-library wrapper around `github.com/zricethezav/gitleaks/v8/detect` v8.30.1, with the upstream 150+-rule default ruleset embedded via `go:embed` and per-Project override composition via the gitleaks `[extend] useDefault=true` TOML directive — the push-boundary second line of secret-leak defense for D-B3 / ART-07.**

## Performance

- **Duration:** ~45 min (TDD RED + GREEN cycle including upstream API verification, three fixture iterations to dodge gitleaks allowlists/stopwords, and SUMMARY authoring)
- **Started:** 2026-05-15T23:29:00Z
- **Completed:** 2026-05-16T00:14:20Z
- **Tasks:** 1 (task type=auto, tdd=true)
- **Commits:** 2 (RED `33ad522` + GREEN `4de00d1`)
- **Files modified:** 6 (4 new files in `internal/gitleaks/` + go.mod + go.sum)

## Accomplishments

- `ScanDiff(diff)` + `ScanDiffWithConfig(diff, detector)` land with the embedded gitleaks v8.30.1 default ruleset. Both ship behind the same `internal/gitleaks.Finding` type-aliased return so consumer code in plan 03-06 (`cmd/tide-push`) imports only this package — not three gitleaks subpackages.
- `LoadConfig(configPath)` handles both the empty-path case (delegates to `detect.NewDetectorDefaultConfig`) and a TOML file path (viper + `config.ViperConfig.Translate()` + `detect.NewDetector`). The `[extend] useDefault = true` compose mechanism is end-to-end verified by Test 5 — a custom-rule TOML composes with embedded defaults so both the custom rule and the upstream Anthropic-key rule fire on a single detector.
- `default_rules.toml` (98 KB, 3209 lines) is vendored verbatim from gitleaks v8.30.1 upstream `config/gitleaks.toml` with a 6-line audit header documenting the source URL, version, and copy-only policy. The embedded byte slice is exposed via `DefaultRulesTOML()` for operator inspection.
- Six test cases PASS deterministically in 0.5s: Anthropic API key detection (≥1 finding), AWS access key detection (≥1 finding), clean-diff false-negative (0 findings), LoadConfig empty-path behavioral equivalence to `NewDetectorDefaultConfig`, `[extend] useDefault=true` compose (custom + embedded both fire on one detector), nil-detector rejection, and `go:embed` wire-up sanity.
- Package is K8s-API-free by construction — 0 imports from `k8s.io/*`, `sigs.k8s.io/*`, or `api/v1alpha1`. The ConfigMap-mount mechanics that surface override TOMLs live downstream in plan 03-06.

## Task Commits

Each TDD phase was committed atomically:

1. **Task 1 RED — `test(03-04): add failing tests`** — `33ad522` (test)
   - Files: `internal/gitleaks/{scanner_test.go, scanner.go (placeholder), config.go (placeholder), default_rules.toml (vendored)}`
   - State: 5 of 6 tests fail with "not yet implemented" errors; embed sanity test passes (defaultRulesTOML non-empty).
2. **Task 1 GREEN — `feat(03-04): implement internal/gitleaks ScanDiff + LoadConfig`** — `4de00d1` (feat)
   - Files: `internal/gitleaks/{scanner.go, config.go, doc.go, scanner_test.go (fixture corrections)}` + `go.mod` + `go.sum`
   - State: all 6 tests PASS; `go vet ./internal/gitleaks/...` clean; `go build ./...` clean.

No REFACTOR commit was needed — the GREEN implementation is already small and idiomatic; no duplication, no excessive nesting.

**Plan metadata commit:** _to be authored by the orchestrator after merging this worktree (parallel-executor mode — STATE/ROADMAP/SUMMARY are committed by the merge-back step)._

## Files Created/Modified

- `internal/gitleaks/scanner.go` (NEW, 96 lines) — `ScanDiff`, `ScanDiffWithConfig`, `DefaultRulesTOML`, `go:embed default_rules.toml`, `type Finding = report.Finding`, shared inner `scanWithDetector` helper.
- `internal/gitleaks/config.go` (NEW, 69 lines) — `LoadConfig` with empty-path fast path + file-path path through a fresh `viper.New()` to avoid global-viper contamination.
- `internal/gitleaks/doc.go` (NEW, 50 lines) — package godoc citing D-B3 (gitleaks as Go library, not shell-out) and ART-07 (push-boundary secret-leak prevention + Prometheus counter wired downstream in 03-08).
- `internal/gitleaks/default_rules.toml` (NEW, 3215 lines / 98 KB) — vendored from gitleaks v8.30.1 upstream `config/gitleaks.toml` + 6-line audit header.
- `internal/gitleaks/scanner_test.go` (NEW, 200 lines) — 6 test cases; fixtures hand-crafted to dodge gitleaks v8 global stopwords + the upstream `.+EXAMPLE$` AWS allowlist.
- `go.mod` / `go.sum` (MODIFIED) — `gitleaks/v8 v8.30.1` + `spf13/viper v1.21.0` added as direct deps; ~30 transitive indirect deps absorbed via `go mod tidy`; incidental promotion of `k8s.io/utils` from indirect → direct (already imported by `internal/dispatch/podjob/jobspec.go`).

## Decisions Made

See `key-decisions` in the frontmatter. The five non-obvious choices:

1. **`Finding = report.Finding` alias.** Plan's interface block said `[]detect.Finding`; gitleaks v8.30.1's `DetectString` actually returns `[]report.Finding`. The alias preserves the package's public-facing type name (`gitleaks.Finding`) without leaking the gitleaks subpackage split to callers. Rule 1 deviation, documented below.
2. **Stopword-aware test fixtures.** The first GREEN iteration's Anthropic fixture used a body containing `abcdefghijklmnopqrstuvwxyz` — a gitleaks v8 global stopword — and silently produced zero findings (the rule fires but the allowlist eats the finding). Switched to a deterministic pseudo-random 93-char body. The AWS fixture similarly avoids the upstream `.+EXAMPLE$` allowlist by using a fresh 16-char tail that is not docs-shaped.
3. **Private `viper.New()` for config parsing.** gitleaks v8's `NewDetectorDefaultConfig` uses the package-global `viper.Viper`; our `LoadConfig` uses `viper.New()` so the override doesn't leak into a subsequent default-config build (and so cross-test contamination is impossible).
4. **Audit-copy `default_rules.toml` + `DefaultRulesTOML()` accessor.** The byte slice is intentionally duplicated relative to gitleaks' internally-embedded copy. The scan hot-path uses gitleaks' internal copy; our copy is for operator/auditor inspection in the tree.
5. **No REFACTOR commit.** GREEN implementation is small enough that a third commit would have been ceremony. The two-commit RED/GREEN shape is honest about the cycle.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 — Bug] ScanDiff signature `[]detect.Finding` → `[]Finding` (alias for `report.Finding`)**

- **Found during:** Task 1 GREEN-phase compile.
- **Issue:** The plan's `<interfaces>` block and `acceptance_criteria` grep both specify `func ScanDiff(diffContent string) (found bool, findings []detect.Finding, err error)`. In gitleaks v8.30.1 the type `Finding` does NOT live in the `detect` package — `detector.DetectString(content)` returns `[]report.Finding` from `github.com/zricethezav/gitleaks/v8/report`. The plan author conflated `detect.Finding` with `report.Finding`. The 03-RESEARCH.md Pattern 4 snippet has the same bug; both pre-date this implementation.
- **Fix:** Defined `type Finding = report.Finding` in `scanner.go` so the package's public surface is `internal/gitleaks.Finding` (consumers do not need to import a third gitleaks subpackage). `ScanDiff` returns `[]Finding` which is structurally identical to `[]report.Finding`. The `report` package is still imported (the alias requires it) but only `scanner.go` knows that.
- **Files modified:** `internal/gitleaks/scanner.go` (the alias declaration + ScanDiff signature).
- **Verification:** `go build ./...` clean; `go vet ./internal/gitleaks/...` clean; the alias is transparent to callers — `g, _, _ := gitleaks.ScanDiff(diff); for _, f := range g { fmt.Println(f.RuleID, f.Match) }` works without importing `gitleaks/v8/report`.
- **Acceptance grep impact:** The exact grep `grep -nE 'func ScanDiff\(diffContent string\) \(found bool, findings \[\]detect\.Finding, err error\)' internal/gitleaks/scanner.go` returns 0 hits (the literal plan-author signature). The realistic grep `grep -nE 'func ScanDiff\(diffContent string\) \(found bool, findings \[\]Finding, err error\)'` returns 1 hit. The behavioral contract (named-return tuple shape with `found`, `findings`, `err`) is preserved.
- **Committed in:** `4de00d1` (Task 1 GREEN commit).

**2. [Rule 1 — Bug] Test fixtures rebuilt to dodge gitleaks v8 global stopwords / allowlists**

- **Found during:** Task 1 GREEN-phase test run (first iteration).
- **Issue:** The plan's example Anthropic fixture (`sk-ant-api03-AAAAAAA...AAAA`, 93 A's + AA) and AWS fixture (`AKIAIOSFODNN7EXAMPLE`) both produce zero findings against the real gitleaks v8.30.1 default ruleset, because (a) the AWS canonical-doc example is on the upstream `aws-access-token` rule's allowlist (`.+EXAMPLE$`), and (b) any candidate Anthropic body that contains the substring `abcdefghijklmnopqrstuvwxyz` is on the global stopwords list and is silently dropped post-match.
- **Fix:** Replaced fixtures with a deterministic pseudo-random 93-char Anthropic body (`odJFCrnl2edlBDdz1C5Jau2RJtBRnlWmTSHf6pWkLUyifDLkDmWJ6UuVTAIjvFu7WICPhDeOZIiBOB-Y6sHrFH2ZUCr-l`) and a fresh AWS 16-char tail (`AKIAJ3K7H2RAQGVF6XBP`) that does not end in `EXAMPLE`. Added inline comments in scanner_test.go documenting which gitleaks v8 stopword/allowlist each fixture choice avoids.
- **Files modified:** `internal/gitleaks/scanner_test.go` (fixture string literals + comments).
- **Verification:** All 3 ScanDiff subtests + LoadConfig_EmptyPath + LoadConfig_ExtendUseDefault now PASS.
- **Committed in:** `4de00d1` (Task 1 GREEN commit; fixtures were rewritten inline as part of the GREEN cycle).

**3. [Rule 3 — Blocking, incidental] `go mod tidy` promoted `k8s.io/utils` from indirect to direct.**

- **Found during:** Post-`go get gitleaks/v8` `go mod tidy`.
- **Issue:** Adding gitleaks pulled in viper, which pulled in a chain of deps, and `go mod tidy`'s graph-walk noticed that `internal/dispatch/podjob/jobspec.go` already imports `k8s.io/utils/ptr` directly — so the indirect marker on `k8s.io/utils` in go.mod was wrong (pre-existing mis-categorization from Phase 1 or 2). Tidy corrected it.
- **Fix:** Accepted the promotion. It is a correctness change, not a scope change.
- **Files modified:** `go.mod` (one-line move from indirect to direct require block).
- **Verification:** `grep -rE '"k8s\.io/utils' internal/dispatch/podjob/jobspec.go` confirms the direct import predates this plan; `go build ./...` exits 0.
- **Committed in:** `4de00d1` (Task 1 GREEN commit, as a side-effect of the go.mod update).

---

**Total deviations:** 3 auto-fixed (2 Rule 1 bugs in plan text + research doc, 1 Rule 3 incidental go-mod hygiene).
**Impact on plan:** All three fixes are mandatory for correctness. The Rule 1 signature deviation makes the code compile against the real upstream API; the Rule 1 fixture deviation makes the tests actually exercise the embedded ruleset; the Rule 3 go.mod fix is a correctness side-effect of tidy. None expand the plan's scope; all stay within `internal/gitleaks/`.

## Issues Encountered

- **Three GREEN-phase iterations before all tests passed.** The first iteration failed with `[]detect.Finding` vs `[]report.Finding` (caught at compile time before commit; never committed). The second iteration compiled but Anthropic+AWS fixtures produced 0 findings — debugged by running the gitleaks default-config detector against the raw fixtures and comparing to a hand-rolled `regexp.MustCompile` of the same pattern; that proved the regex matched in isolation, so the filter had to be allowlist/stopword. Direct read of `gitleaks/v8/config/config.go` confirmed the global stopwords list and the `.+EXAMPLE$` AWS rule allowlist; fixtures were rewritten in iteration three. Net cost: ~10 minutes; root-causes were knowable from the upstream config TOML if read carefully — a CLAUDE.md "Observe First" reminder that reading the actual library config beats hypothesizing about regex semantics.

## User Setup Required

None — no external service configuration required. The gitleaks v8 library is pure-Go; nothing to install at the host level.

## Next Phase Readiness

- **Plan 03-06 (`cmd/tide-push`) is unblocked.** The push-Job binary imports `internal/gitleaks` and calls `ScanDiff(diff)` on the staged diff before invoking go-git's `Push`. The exit-code map (`reason="leak-detected"` → exit 10) and Prometheus counter wiring are downstream concerns (plan 03-08).
- **Plan 03-06 ConfigMap-mount mechanics** for per-Project rule overrides will mount the override TOML at `/etc/tide/gitleaks-config.toml` on the push Job pod and pass that path to `gitleaks.LoadConfig`. The loader is content-agnostic; the threat-boundary enforcement (override ConfigMap MUST be in the Project's namespace) lives on the push Job spec per the plan's `<threat_model>` T-304 mitigation.
- **Phase 4 ART-07 Prometheus counter** (`tide_secret_leak_blocked_total`) consumes the push Job's exit code via the ProjectReconciler observation in plan 03-08; this plan is the data-producing side and is complete.

## Self-Check: PASSED

- File `internal/gitleaks/scanner.go` — FOUND.
- File `internal/gitleaks/config.go` — FOUND.
- File `internal/gitleaks/doc.go` — FOUND.
- File `internal/gitleaks/default_rules.toml` — FOUND (98242 bytes).
- File `internal/gitleaks/scanner_test.go` — FOUND.
- Commit `33ad522` — FOUND on `worktree-agent-ac04f48cec3aad3b4` (RED).
- Commit `4de00d1` — FOUND on `worktree-agent-ac04f48cec3aad3b4` (GREEN).
- `go test ./internal/gitleaks/... -count=1 -timeout 30s` — PASS (6/6 tests, 0.5s).
- `go vet ./internal/gitleaks/...` — exit 0.
- `go build ./...` — exit 0.
- `grep -rcE 'k8s\.io|sigs\.k8s\.io|api/v1alpha1' internal/gitleaks/*.go` — returns 0 on every file (import firewall clean).
- `grep -nE 'github\.com/zricethezav/gitleaks/v8 v8\.30\.1' go.mod` — 1 hit.
- `grep -nE '//go:embed default_rules\.toml' internal/gitleaks/scanner.go` — 1 hit.
- `grep -cE 'NewDetectorDefaultConfig' internal/gitleaks/{scanner,config}.go` — ≥2.

---
*Phase: 03-up-stack-reconcilers-git-integration-real-subagent-resumptio*
*Completed: 2026-05-16*
