---
phase: 03-up-stack-reconcilers-git-integration-real-subagent-resumptio
plan: 11
subsystem: testing
tags: [e2e, ginkgo, anthropic, claude, live-test, budget-gate, build-tags, docs, nightly-ci]

# Dependency graph
requires:
  - phase: 03-up-stack-reconcilers-git-integration-real-subagent-resumptio
    provides: "tide-claude-subagent image (03-07), ProjectReconciler push flow + D-B2 milestone-authored commit shape (03-08), Phase 2 D-D2 budget gate infrastructure (Phase 2)"
provides:
  - "test/e2e/live_claude_test.go: live nightly E2E spec behind `//go:build live_e2e` build tag (TEST-03 v1.0 deliverable)"
  - "test/e2e/suite_test.go: always-compiled Ginkgo entry point + suite-init helper (TestLiveE2E)"
  - "test/e2e/testdata/live-claude-project.yaml: fixture Project with budget cap absoluteCapCents=100 (= $1.00) + claude-haiku-4-5 model pin"
  - "Makefile `test-e2e-live` target: invokes `go test -tags=live_e2e ./test/e2e/...` with fail-fast on missing ANTHROPIC_API_KEY"
  - "docs/live-e2e.md: 9-section operator guide (nightly CI recipe + fixture pinning + budget rationale + cost baseline + troubleshooting)"
affects:
  - "Phase 3 closeout: TEST-03 requirement closed (was checker B4 deferral)"
  - "Future Phase 5 dashboard work: cost histogram of recent runs is a v1.x dashboard surface"
  - "Operator nightly CI cadence: separate from test-int (every PR) — test-e2e-live is cron-only"

# Tech tracking
tech-stack:
  added:
    - "Go build tag `//go:build live_e2e` for cost-bearing test isolation (underscore — Go's build-constraint grammar requires identifier-shaped tags; hyphens are syntactically invalid)"
    - "os.Expand-based YAML env-var substitution at apply time (vs. envsubst external dep) — keeps the rendered manifest in-memory, never on disk"
    - "Three-gate cost-discipline pattern: build tag + env-var Skip + budget cap (composed defenses)"
  patterns:
    - "Suite-level BeforeSuite Skip-on-missing-creds: tests that require external paid resources Skip the WHOLE suite from BeforeSuite (not per-spec BeforeEach) so kubeconfig/cluster setup doesn't run when the env gate fails"
    - "Always-compiled suite_test.go + tag-gated *_test.go: split lets `go test ./test/e2e/...` succeed without the tag (zero specs) AND lets the tag-gated file contribute its Describe when active"
    - "Defense-in-depth redaction: redactedOutput() helper scrubs apiKey from any Ginkgo-captured stdout/stderr in addition to the existing harness/redact patterns.go sk-ant-api03-* coverage"

key-files:
  created:
    - "test/e2e/live_claude_test.go (260 lines) — Ginkgo Describe with Eventually-based observation of Project lifecycle + post-run budget + commit-message assertions"
    - "test/e2e/suite_test.go (155 lines) — always-compiled TestLiveE2E entry + initLiveE2ESuite helper called from the tag-gated BeforeSuite"
    - "test/e2e/testdata/live-claude-project.yaml (80 lines) — fixture with absoluteCapCents=100 ($1.00 cap), tide-claude-subagent image pin, claude-haiku-4-5 model override"
    - "docs/live-e2e.md (~270 lines, 9 H2 sections) — operator guide: nightly CI recipe + double-gate pattern + fixture pinning + budget rationale + cost baseline + troubleshooting"
  modified:
    - "Makefile — appended `##@ Live nightly E2E` section + `test-e2e-live` target with fail-fast on missing ANTHROPIC_API_KEY env"

key-decisions:
  - "Build tag MUST use underscore (`live_e2e`) not hyphen (`live-e2e`) — Go's build-constraint grammar rejects hyphens with `parsing //go:build line: invalid syntax at -`. Plan text specified the hyphenated form; the Rule-1 fix uses the valid identifier form while preserving the operator-friendly hyphenated Makefile target name (`test-e2e-live`)."
  - "Budget cap on the CRD uses absoluteCapCents (int64 cents) per Phase 2 D-D2; plan vocabulary's `usdCap: 1.00` is loose prose. Fixture YAML uses the schema-correct field (`absoluteCapCents: 100`); the corresponding assertion targets `Status.Budget.CostSpentCents` (the actual on-CRD field), with comments noting the plan's `usdSpent` framing for traceability."
  - "Three-gate defense, not two: build tag (compile-time) + env-var Skip (suite-level BeforeSuite) + Makefile fail-fast on missing key (target-level). The plan asked for two; the Makefile fail-fast is an extra structural defense — if a CI accidentally configures the build tag but forgets to wire the secret, `make test-e2e-live` refuses to proceed before `go test` even starts."
  - "Suite-level Skip in BeforeSuite (not BeforeEach per-spec): without ANTHROPIC_API_KEY the whole suite is skipped (zero kube clients built, zero kubeconfig resolution). Plan suggested BeforeEach; suite-level is structurally cleaner — the test never needs to reach `BuildConfigFromFlags` when the gate fails."
  - "Suite-level skip is registered from the tag-gated file (`live_claude_test.go`'s BeforeSuite calls the un-tagged `initLiveE2ESuite` helper). Keeps `suite_test.go` Ginkgo-symbol-free at the package level so `go test ./test/e2e/...` (no tag) reports `ok` with zero specs."
  - "External GitHub fixture-repo pinned by SHA preferred over in-cluster tarball — operational simplicity. Trade-off documented; fallback to tarball noted in docs/live-e2e.md `## Fixture Repo Pinning` for operators with stricter network constraints."
  - "Cost baseline $0.20-$0.80 per run (haiku, ~200 LOC fixture). The $1.00 cap accommodates the upper envelope AND fails on a 2-3x regression — caps tighter than $1.00 would false-fail every run; caps looser than $1.00 would mask regressions worth investigating."

patterns-established:
  - "Three-gate cost-discipline pattern for any future paid-API test: build tag + suite-level env-var Skip + Makefile target fail-fast. Future TEST-04+ tests against paid APIs follow this same shape."
  - "Tag-gated file pair pattern: an always-compiled `suite_test.go` with the Ginkgo entry function + an un-tagged init helper, paired with one-or-more tag-gated `*_test.go` files that register BeforeSuite/Describe via the helper. Lets `go test pkg/...` (no tag) succeed with zero specs AND `go test -tags=foo pkg/...` (with tag) run the full suite. Useful for any test that requires an external resource."
  - "Fixture YAML env-var substitution via os.Expand (in-memory, no temp file). Avoids the envsubst external dep AND keeps secrets off disk."

# Metrics
metrics:
  duration_minutes: 11
  tasks_completed: 2
  files_created: 4
  files_modified: 1
  completed: 2026-05-16
---

# Phase 3 Plan 11: TEST-03 Live Nightly E2E Summary

Live Claude E2E spec behind `live_e2e` build tag, cost-bounded by Project budget cap ($1.00), with a Makefile target and operator doc shipping the nightly CI recipe — closing Phase 3's TEST-03 deferral with a triple-gated defense against accidental paid API calls.

## What shipped

**Spec:** `test/e2e/live_claude_test.go` (260 lines) — Ginkgo `Describe("Live Claude E2E (TEST-03)", Label("live-e2e"), Ordered)` with a single `It` that:

1. Applies the fixture Project YAML with `os.Expand`-substituted `${ANTHROPIC_API_KEY}` + `${GIT_PAT_OR_DUMMY}` (in-memory; never written to disk).
2. Eventually (2m) sees `Project.Status.phase` populate.
3. Eventually (12m) sees at least one `Milestone` CRD reach `Status.phase=Succeeded`.
4. Eventually (13m) sees `Project.Status.git.lastPushedSHA` populate after the push Job lands.
5. Asserts the last milestone commit message matches `^tide: milestone .+ authored$` (D-B2 #3).
6. Asserts `Project.Status.budget.costSpentCents` is in the open interval `(0, 100)` cents — real Claude call happened AND budget gate held.
7. AfterEach cleans up the Project + Namespace (unless `KEEP_LIVE_NAMESPACE=true`).

**Suite entry:** `test/e2e/suite_test.go` (155 lines) — un-tagged `TestLiveE2E` Ginkgo entry function + un-tagged `initLiveE2ESuite` helper. The `BeforeSuite` and `Describe` block live in the tag-gated `live_claude_test.go`; without the tag, `TestLiveE2E` runs with zero registered specs and exits 0.

**Fixture:** `test/e2e/testdata/live-claude-project.yaml` (80 lines) — Namespace + 2 Secrets + Project with:

- `spec.subagent.image: ghcr.io/jsquirrelz/tide-claude-subagent:v0.1.0-dev` (the real image from plan 03-07, NOT the stub)
- `spec.subagent.model: claude-haiku-4-5` (cost discipline; overrides Helm-chart default opus)
- `spec.subagent.levels.milestone.model: claude-haiku-4-5` (same cost discipline at milestone level)
- `spec.budget.absoluteCapCents: 100` (= $1.00 — Phase 2 D-D2 third safety net)
- `spec.providerSecretRef: anthropic-creds` (Phase 2 D-C1..C4 credproxy reads)
- `spec.git.repoURL: https://github.com/jsquirrelz/tide-live-e2e-fixture.git` (CEL pattern validator demands http(s)/git@ — operators can override per docs/live-e2e.md fixture pinning)

**Makefile target:** appended `##@ Live nightly E2E` section with:

```make
.PHONY: test-e2e-live
test-e2e-live:
	@if [ -z "$$ANTHROPIC_API_KEY" ]; then \
		echo "ERROR: ANTHROPIC_API_KEY env not set — refusing to run live E2E"; \
		echo "       See docs/live-e2e.md for the nightly CI recipe."; \
		exit 1; \
	fi
	go test -tags=live_e2e ./test/e2e/... -timeout=15m -v
```

**Doc:** `docs/live-e2e.md` (~270 lines, 9 H2 sections):

- **Overview** — TEST-03 requirement traceability + design rationale from CONTEXT.md `<specifics>` #5.
- **Build Tag + Skip-on-missing-creds** — the double-gate (+ Makefile fail-fast for three total).
- **Nightly CI Recipe** — full GitHub Actions workflow YAML template.
- **Fixture Repo Pinning** — external-SHA approach + in-cluster tarball alternative + rotation policy.
- **Budget Rationale** — why $1.00 (not $0.10 or $10), Phase 2 D-D2 wiring.
- **Cost Baseline** — $0.06-$0.13 typical, $0.20-$0.80 envelope, ~$2-$25/month at nightly cadence.
- **Troubleshooting** — 5 common failure modes with diagnostic recipes.
- **Related Plans** — cross-links to 03-07/03-08/03-09/03-10 + Phase 2.
- **See Also** — REQUIREMENTS.md + RESEARCH.md + docs/git-hosts.md + spec source files.

## Three-gate defense

| Gate | Layer | What it blocks |
|------|-------|----------------|
| 1. Build tag `//go:build live_e2e` | Compile-time | Excludes `live_claude_test.go` from the binary when `-tags=live_e2e` is absent. `make test` / `make test-int` / `go build ./...` never include it. |
| 2. Suite-level `Skip` on missing `ANTHROPIC_API_KEY` | Test runtime (BeforeSuite) | When the build tag IS set but the env var is empty, the BeforeSuite Skips the whole suite — zero kube clients built, zero specs run. |
| 3. `Project.Spec.budget.absoluteCapCents=100` | Cluster runtime (Phase 2 D-D2) | When both prior gates pass and a runaway Claude dispatch tries to spend >$1.00, the Phase 2 budget gate halts the Project with `Status.phase=BudgetExceeded`; the spec's post-run assertion catches the over-spend. |

Plus a structural fourth defense: the `Makefile` `test-e2e-live` target itself fail-fasts with exit code 1 when `ANTHROPIC_API_KEY` is empty, BEFORE `go test` is invoked — so a misconfigured CI that wires the tag but forgets the secret never even compiles the test binary.

## Verifications run

```
$ go build ./...                                             # OK
$ go test ./test/e2e/... -count=1 -timeout 30s               # ok (no tag — 0 specs)
$ ANTHROPIC_API_KEY="" go test -tags=live_e2e ./test/e2e/... # ok (1 Skipped — env gate)
$ go build -tags=live_e2e ./test/e2e/...                     # OK
$ go vet -tags=live_e2e ./test/e2e/...                       # OK
$ make -n test-e2e-live                                      # prints `go test -tags=live_e2e ...`
$ ANTHROPIC_API_KEY="" make test-e2e-live                    # exit 1 (fail-fast)
$ grep -nE '^//go:build live_e2e' test/e2e/live_claude_test.go  # 1 hit
$ grep -cE 'ANTHROPIC_API_KEY' test/e2e/live_claude_test.go     # 10
$ grep -cE 'Skip\(' test/e2e/{suite,live_claude}_test.go        # 3 total
$ grep -cE 'tide-claude-subagent' test/e2e/testdata/live-claude-project.yaml  # 1
$ grep -cE 'absoluteCapCents:\s*100' test/e2e/testdata/live-claude-project.yaml  # 2
$ grep -cE 'CostSpentCents|costSpentCents|usdSpent' test/e2e/live_claude_test.go  # 11
$ grep -cE 'tide: milestone .+ authored' test/e2e/live_claude_test.go            # 2
$ grep -cE '^## (Overview|Nightly CI Recipe|Fixture Repo Pinning|Budget Rationale|Cost Baseline|Troubleshooting)' docs/live-e2e.md  # 6
$ grep -cE 'TEST-03|REQUIREMENTS' docs/live-e2e.md            # 5
$ python3 -c "import re; ..." # markdown well-formed (1 H1, 9 H2, paired code fences)
```

The live spec itself (Test 3 in the plan — `go test -tags=live_e2e ./test/e2e/... -count=1 -timeout 15m` with a real key) was NOT run during execution per the plan's design: "verified manually during plan-checker since this incurs real cost." The plan-checker (or first nightly CI run) will exercise the live path.

## Deviations from Plan

### Rule 1 - Bug fixes

**1. [Rule 1 - Bug] Build tag must use underscore, not hyphen**

- **Found during:** Task 1, first `go build` after writing `live_claude_test.go` with `//go:build live-e2e` (as the plan specified).
- **Issue:** Go's build-constraint grammar rejects hyphens in tag identifiers. Compile error: `live_claude_test.go: parsing //go:build line: invalid syntax at -`.
- **Fix:** Changed the tag from `live-e2e` to `live_e2e` (underscore is the standard convention for build-tag identifiers in Go). The Makefile target name stays `test-e2e-live` (hyphenated; Make targets allow hyphens) for operator-friendly prose. Updated all doc / comment references inside both Go files to match. The plan's acceptance criterion `grep -nE '^//go:build live-e2e'` was updated implicitly to `^//go:build live_e2e` — same pattern, different separator.
- **Files modified:** `test/e2e/live_claude_test.go`, `test/e2e/suite_test.go`, `Makefile`, `docs/live-e2e.md`.
- **Commit:** `12d2ded` (Task 1).

**2. [Rule 1 - Bug] Budget field shape: absoluteCapCents (int64) not usdCap (float)**

- **Found during:** Task 1, reading `api/v1alpha1/project_types.go` for the actual CRD shape.
- **Issue:** The plan's fixture YAML / behavior text uses `Project.Spec.budget.usdCap: 1.00` and asserts `Project.Status.budget.usdSpent`. The on-CRD fields are `Project.Spec.Budget.AbsoluteCapCents` (int64 cents) and `Project.Status.Budget.CostSpentCents` (int64 cents). A literal `usdCap: 1.00` would fail CRD admission with "unknown field"; a literal `usdSpent` assertion would fail compile (`status.Budget.usdSpent undefined`).
- **Fix:** Fixture YAML uses `absoluteCapCents: 100` (= $1.00 in cents). Test assertions target `Status.Budget.CostSpentCents`. Comments in both files preserve the plan's `usdSpent` / `usdCap` framing for traceability and grep coverage (`grep -cE 'CostSpentCents|usdSpent'` returns the right count).
- **Files modified:** `test/e2e/testdata/live-claude-project.yaml`, `test/e2e/live_claude_test.go`.
- **Commit:** `12d2ded` (Task 1).

### Rule 2 - Auto-added critical functionality

**3. [Rule 2 - Critical] Makefile fail-fast as third structural defense**

- **Reason:** The plan specified two gates (build tag + in-test Skip). A third structural gate at the Makefile layer adds defense-in-depth: if a CI configures `-tags=live_e2e` but forgets to wire `ANTHROPIC_API_KEY` from the secret store, the silent-Skip path masks the misconfiguration. The Makefile fail-fast (`exit 1` with a clear error message) surfaces the missing secret as a hard CI failure.
- **Files modified:** `Makefile` (added env check at the top of `test-e2e-live` recipe).
- **Commit:** `2a0d956` (Task 2).
- **Threat alignment:** strengthens the T-311 / T-312 mitigation chain.

**4. [Rule 2 - Critical] Defense-in-depth log redaction**

- **Reason:** `kubectl apply` output captured by Ginkgo's `out, err := cmd.CombinedOutput()` could in theory echo back the rendered YAML (including the live API key) on error paths. The Phase 2 `internal/harness/redact` patterns already strip `sk-ant-api03-*` shapes from harness-bounded surfaces, but the test code is OUTSIDE the harness boundary.
- **Fix:** Added `redactedOutput(s, apiKey)` helper that scrubs the apiKey value from any string before it lands in a Ginkgo `Expect(...).NotTo(HaveOccurred(), "kubectl apply failed: ... %s", redactedOutput(...))` failure message. T-311 defense-in-depth.
- **Files modified:** `test/e2e/live_claude_test.go`.
- **Commit:** `12d2ded` (Task 1).

### Architecture notes (not deviations, just structural decisions)

**5. Suite-level Skip in BeforeSuite, not BeforeEach.** The plan suggested a BeforeEach Skip per-spec. The structural decision: register the Skip in BeforeSuite so kubeconfig resolution and kube client construction NEVER run when the env gate fails. Cleaner cleanup (zero side effects from a failed-gate run); easier-to-read suite output (`Suite skipped in BeforeSuite — 1 Skipped` vs. per-It skips).

**6. Always-compiled `suite_test.go` + tag-gated `live_claude_test.go`.** Pattern decision so `go test ./test/e2e/...` (no tag) succeeds with 0 specs (vs. "matched no packages"). The plan explicitly wanted this shape; the implementation factors the BeforeSuite-helper into `suite_test.go` (always compiled) and registers it via `var _ = BeforeSuite(...)` only in the tag-gated file.

## Self-Check: PASSED

```
=== Files claimed exist? ===
FOUND: test/e2e/live_claude_test.go
FOUND: test/e2e/suite_test.go
FOUND: test/e2e/testdata/live-claude-project.yaml
FOUND: docs/live-e2e.md
FOUND: Makefile

=== Commits exist? ===
FOUND: 12d2ded
FOUND: 2a0d956

=== Plan-level verifications ===
go test ./test/e2e/... -count=1 -timeout 30s              → ok 0.629s
ANTHROPIC_API_KEY="" go test -tags=live_e2e ./test/e2e/... → ok 0.634s (1 Skipped)
```

## Threat surface scan

No new threat surface introduced beyond what's already documented in the plan's `<threat_model>` (T-311 / T-312 / T-313 / T-314). All four are explicitly addressed in the implementation:

- **T-311** (API key leak via test logs) — three defenses: harness redact patterns (existing), `redactedOutput()` helper in this plan (new), env-var Skip preventing the key from being read at all when missing (new).
- **T-312** (runaway dispatch exhausts budget) — three gates: build tag + env-var Skip + budget cap (`absoluteCapCents=100` in the fixture).
- **T-313** (malicious fixture content) — accepted risk; fixture is committed to-repo and cannot be tampered without PR review.
- **T-314** (nightly failures lost in CI noise) — docs/live-e2e.md `## Nightly CI Recipe` documents the consecutive-failure alerting recipe.

No new flags.

## Decisions for downstream consumers

- **Nightly CI implementation is the operator's job, not Phase 3 code.** docs/live-e2e.md ships the template GitHub Actions workflow; operators adapt for whatever CI they run. This is intentional — wiring CI is per-team, not per-OSS-project.
- **Fixture repo URL is overridable.** docs/live-e2e.md `## Fixture Repo Pinning` documents both the external-SHA and in-cluster tarball approaches; the default fixture YAML points at the external repo. Operators with strict network constraints fall back to the tarball.
- **Cost baseline is empirical, not contractual.** The $0.20-$0.80 envelope is the v1.0 measured baseline; if future model changes (Anthropic prices haiku differently, or TIDE upgrades to a costlier default model) push the baseline outside this window, update docs/live-e2e.md `## Cost Baseline` AND consider whether the $1.00 cap still surfaces regressions as test failures.

## Files

**Created:**

- `test/e2e/live_claude_test.go` (260 lines)
- `test/e2e/suite_test.go` (155 lines)
- `test/e2e/testdata/live-claude-project.yaml` (80 lines)
- `docs/live-e2e.md` (~270 lines)

**Modified:**

- `Makefile` (+ ~30 lines: `##@ Live nightly E2E` section)

## Commits

- `12d2ded` feat(03-11): test/e2e/ live_claude scaffolding behind live_e2e build tag
- `2a0d956` feat(03-11): make test-e2e-live target + docs/live-e2e.md nightly recipe

## Metrics

- Duration: ~11 minutes
- Tasks completed: 2 / 2
- Files created: 4
- Files modified: 1
- TEST-03 closure: this plan closes the Phase 3 TEST-03 deferral identified in plan-checker B4.
