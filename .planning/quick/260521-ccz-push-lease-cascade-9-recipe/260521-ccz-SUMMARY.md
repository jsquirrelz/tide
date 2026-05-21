---
id: 260521-ccz-push-lease-cascade-9-recipe
title: "Phase 03 push_lease Cascade 9 — apply createNamespace recipe + drop SKIP gate"
type: quick
status: complete
created: 2026-05-21
completed: 2026-05-21
phase: quick
plan: "260521-ccz"
wave: 1
depends_on: []
files_modified:
  - test/integration/kind/push_lease_test.go
  - test/integration/kind/suite_test.go
requirements: [04.1-12-FOLLOWUP-2]
key-files:
  modified:
    - path: test/integration/kind/push_lease_test.go
      change: "BeforeEach now calls createNamespace(pushLeaseNS) after skipIfCRDsOnlyMode; SKIP_PHASE3_PUSH_LEASE_TESTS env-var gate + 12-line scope-defer comment removed; \"os\" import dropped"
      diff_lines: "+2 / -15 (net -13)"
    - path: test/integration/kind/suite_test.go
      change: "kindTestTimeout iter-5 rationale rewritten to point at Cascade 9 recipe (04.1-12-SUMMARY.md Outstanding Follow-up #2) instead of the now-obsolete SKIP_PHASE3_PUSH_LEASE_TESTS env-var; 18m constant value preserved"
      diff_lines: "+4 / -3 (net +1)"
commits:
  - hash: 5e1db67
    type: fix(test)
    message: "apply Cascade 9 recipe to push_lease_test BeforeEach — createNamespace(pushLeaseNS) + drop SKIP gate"
    files: [test/integration/kind/push_lease_test.go]
  - hash: bc3ed29
    type: docs(test)
    message: "scrub stale SKIP_PHASE3_PUSH_LEASE_TESTS reference from suite_test.go kindTestTimeout rationale"
    files: [test/integration/kind/suite_test.go]
decisions:
  - "Cascade 9 closure shape mirrors Cascade 6 exactly: createNamespace(<ns>) before applyFile bundles ensureSubagentSA + ensureProjectsPVC + ensureSigningKeySecret idempotently. push_lease's 4 It blocks share one BeforeEach (vs chaos_resume's 1 It block), so the recipe call lives in BeforeEach to cover all four specs uniformly."
  - "Source-recipe comment kept terse (one By line, no multi-paragraph block) — the locked rationale lives in 04.1-12-SUMMARY.md Outstanding Follow-up #2, not in source. Avoids replicating the chaos_resume_test.go:128–137 multi-line comment block per the user's quick-task constraint."
  - "kindTestTimeout = 18 * time.Minute UNCHANGED. Only the env-var sentence in the iter-5 history paragraph was edited; the iter-4 → iter-5 wall-time delta history (908s > 900s; push_lease 415s budget consumer) is preserved."
tags: [phase-03, cascade-9, push-lease, kind-integration, layer-b, test-infra]
---

# Quick Task 260521-ccz: push_lease Cascade 9 Recipe Summary

**One-liner:** Applied the Cascade 6 namespace-local SA + signing-key Secret recipe to push_lease Layer B specs by calling `createNamespace(pushLeaseNS)` in BeforeEach and removed the now-obsolete SKIP_PHASE3_PUSH_LEASE_TESTS env-var gate; closes 04.1-12 Outstanding Follow-up #2 at the source-recipe level.

## Objective Recap

Close Phase 04.1 Plan 12 Outstanding Follow-up #2 (Cascade 9) by translating the locked recipe from `.planning/phases/04.1-pre-v1-audit-fixes-cross-phase-uat-closeout/04.1-12-SUMMARY.md` (line 96 closure shape; line 153 Outstanding Follow-up #2) into source. Same failure class as Cascade 6 on chaos_resume_test: the fixture YAML creates Namespace + PVC + provider Secret inline but NOT the `tide-subagent` ServiceAccount or the helm-mirrored `tide-signing-key` Secret. Closure is the same — call `createNamespace(ns)` (idempotent helper that bundles ensureSubagentSA + ensureProjectsPVC + ensureSigningKeySecret) before `applyFile`.

## Tasks Completed

| # | Task | File | Commit | Diff |
|---|------|------|--------|------|
| 1 | Apply Cascade 9 recipe to push_lease_test.go BeforeEach | test/integration/kind/push_lease_test.go | 5e1db67 | +2 / -15 (net -13) |
| 2 | Scrub stale SKIP_PHASE3_PUSH_LEASE_TESTS reference from suite_test.go kindTestTimeout rationale | test/integration/kind/suite_test.go | bc3ed29 | +4 / -3 (net +1) |

Two atomic single-file commits. Both committed on `worktree-agent-a65ea6fc8e1e1eafe`; SHAs above.

## Verification Evidence

### Task 1 automated verify (passed, exit 0)

```
cd /Users/justinsearles/Projects/tide && \
  go vet ./test/integration/kind/... && \
  go build ./test/integration/kind/... && \
  grep -c 'createNamespace(pushLeaseNS)' test/integration/kind/push_lease_test.go | grep -q '^1$' && \
  ! grep -q 'SKIP_PHASE3_PUSH_LEASE_TESTS' test/integration/kind/push_lease_test.go && \
  ! grep -qE '^\s*"os"\s*$' test/integration/kind/push_lease_test.go
# exit 0
```

### Task 2 automated verify (passed, exit 0)

```
cd /Users/justinsearles/Projects/tide && \
  go vet ./test/integration/kind/... && \
  go build ./test/integration/kind/... && \
  ! grep -q 'SKIP_PHASE3_PUSH_LEASE_TESTS' test/integration/kind/suite_test.go && \
  grep -qE 'kindTestTimeout\s*=\s*18\s*\*\s*time\.Minute' test/integration/kind/suite_test.go
# exit 0
```

### Repo-wide stale-reference sweep (passed)

```
grep -rn "SKIP_PHASE3_PUSH_LEASE_TESTS" test/ cmd/ internal/ pkg/ Makefile
# 0 matches outside .planning/ (planning artifacts intentionally retain it for cascade history)
```

### Cascade 6 / Cascade 9 recipe parity (passed)

```
grep -c "createNamespace(chaosResumeNS)" test/integration/kind/chaos_resume_test.go  # 1
grep -c "createNamespace(pushLeaseNS)"   test/integration/kind/push_lease_test.go    # 1
```

### gofmt parity (passed, no formatting drift)

```
gofmt -l test/integration/kind/push_lease_test.go test/integration/kind/suite_test.go
# (empty output)
```

## Before / After Shape

### push_lease_test.go BeforeEach

**Before (lines 57–73 — 17 lines):**

```go
BeforeEach(func() {
    // Phase 04.1 Plan 12 iter-5: scope-defer Phase 03 push_lease tests
    // when SKIP_PHASE3_PUSH_LEASE_TESTS=true so they don't burn the suite
    // budget while the Phase 02 UAT closeout (plan 04.1-12) executes.
    // Without this skip, the 4 push_lease specs each fail at ~103s
    // (90s Eventually + cleanup), consuming ~415s of the suite ctx and
    // starving the trailing chaos_resume + caps_test specs (which then
    // SKIP via skipIfCRDsOnlyMode on ctx.DeadlineExceeded).
    //
    // The push_lease failures are a Phase 03 cascade — likely a
    // namespace-local SA/Secret mirroring gap similar to chaos-resume
    // (Cascade 6). They belong to a separate Phase 03 fix plan.
    if os.Getenv("SKIP_PHASE3_PUSH_LEASE_TESTS") == "true" {
        Skip("SKIP_PHASE3_PUSH_LEASE_TESTS=true; deferring Phase 03 push_lease tests during Phase 02 UAT closeout (Phase 04.1 Plan 12 iter-5)")
    }
    skipIfCRDsOnlyMode()
})
```

**After (4 lines):**

```go
BeforeEach(func() {
    skipIfCRDsOnlyMode()
    By("Ensure namespace-local SA + signing-key Secret (Phase 04.1 P12 Cascade 9 — same shape as Cascade 6)")
    createNamespace(pushLeaseNS)
})
```

Also dropped the now-unused `"os"` import (only `os.` reference in the file was on the deleted SKIP line).

### suite_test.go kindTestTimeout iter-5 paragraph

**Before:** `iter-5 scope-defers them via SKIP_PHASE3_PUSH_LEASE_TESTS=true. 18m gives headroom for unexpected first-run delays without re-tripping the cascade.`

**After:** `dominant budget consumer (now run unconditionally per the quick-task Cascade 9 recipe — see 04.1-12-SUMMARY.md Outstanding Follow-up #2). 18m gives headroom for unexpected first-run delays without re-tripping the cascade.`

The 18m bump rationale (iter-4 observed 908s > 900s; push_lease 4 × ~103s = 415s dominant consumer; chaos_resume + caps_test SKIP at the tail in iter-4) is preserved verbatim. Only the env-var sentence was rewritten as a forward-looking pointer to the recipe quick task. `kindTestTimeout = 18 * time.Minute` constant value UNCHANGED.

## Cascade Closure Status

**Cascade 9 (Phase 03 push_lease ×4 namespace-local SA/Secret gap):** CLOSED at source-recipe level.

The `createNamespace(pushLeaseNS)` call in BeforeEach makes the `tide-subagent` ServiceAccount and the helm-mirrored `tide-signing-key` Secret namespace-local before any push_lease spec applies its fixture YAML. This is the identical closure shape that Cascade 6 used on chaos_resume_test (commit 624aafb, line 96 of the upstream summary).

**Runtime verification deferred** to the user's next `make test-int` run. This quick task explicitly scoped runtime gating out per the user's constraint — code-shape correctness (go vet + go build clean + grep assertions) is the bar.

## Deviations from Plan

None. Both tasks executed exactly as written:

- Task 1: dropped 12-line comment + 3-line if-block + 1-line `"os"` import; added one-line `By(...)` + one-line `createNamespace(pushLeaseNS)` after `skipIfCRDsOnlyMode()`.
- Task 2: replaced one sentence in the iter-5 paragraph (env-var reference → recipe pointer); preserved every adjacent paragraph and the `kindTestTimeout = 18 * time.Minute` constant.

Diff stats matched plan predictions (push_lease: predicted -15 to -17, actual -13; suite_test: predicted +1 to +3, actual +1).

## Out of Scope (cascade map remains auditable)

Per the plan's `<context>` block — these were NOT touched and remain follow-ups:

| Cascade | Where | Status |
|---------|-------|--------|
| Cascade 7 (Plan-pod credproxy ANTHROPIC_API_KEY missing when `opts.Project nil`) | `internal/dispatch/podjob/jobspec.go` BuildJobSpec path | DEFERRED — Phase 04.2 or Phase 03 follow-up |
| Cascade 10 (chaos_resume_test second-stage failure at line 230 post-iter-5) | `test/integration/kind/chaos_resume_test.go:230` | DEFERRED — Phase 03 cascade |
| Item 2 (Layer B 429 rate-limit storm spec, FAIL-03 / AC#4) | No Layer B spec authored | OPEN — decide spec author vs accept Layer A coverage |

This quick task closes only Cascade 9; the remaining three Phase 03 follow-ups continue to live in 04.1-12-SUMMARY.md "Outstanding Follow-ups".

## Self-Check

- [x] `git log --oneline -2` shows `bc3ed29 docs(test): scrub stale SKIP_PHASE3_PUSH_LEASE_TESTS reference...` and `5e1db67 fix(test): apply Cascade 9 recipe to push_lease_test BeforeEach...` (verified)
- [x] `grep -c 'createNamespace(pushLeaseNS)' test/integration/kind/push_lease_test.go` returns `1` (verified)
- [x] `grep 'SKIP_PHASE3_PUSH_LEASE_TESTS' test/integration/kind/push_lease_test.go` returns 0 matches (verified)
- [x] `grep 'SKIP_PHASE3_PUSH_LEASE_TESTS' test/integration/kind/suite_test.go` returns 0 matches (verified)
- [x] `grep -E 'kindTestTimeout\s*=\s*18\s*\*\s*time\.Minute' test/integration/kind/suite_test.go` returns the constant line (verified — line 94)
- [x] No `"os"` import in push_lease_test.go (verified — `! grep -qE '^\s*"os"\s*$'` exits 0)
- [x] `go vet ./test/integration/kind/...` exits 0 (verified)
- [x] `go build ./test/integration/kind/...` exits 0 (verified)
- [x] Repo-wide `grep -rn "SKIP_PHASE3_PUSH_LEASE_TESTS"` outside `.planning/` returns 0 matches (verified)
- [x] `gofmt -l` returns no formatting drift (verified — empty output)
- [x] Two atomic commits, one per file (verified — 5e1db67 and bc3ed29)

## Self-Check: PASSED

All claims grounded in observed `go vet` / `go build` / `grep` output. No runtime gate was run (explicitly out of scope); next `make test-int` will surface whether the Cascade 9 closure holds end-to-end on real cluster state.
