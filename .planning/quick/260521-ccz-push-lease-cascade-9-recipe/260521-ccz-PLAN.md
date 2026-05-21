---
id: 260521-ccz-push-lease-cascade-9-recipe
title: "Phase 03 push_lease Cascade 9: apply createNamespace recipe + drop SKIP gate"
type: quick
status: planned
created: 2026-05-21
phase: quick
plan: "260521-ccz"
wave: 1
depends_on: []
files_modified:
  - test/integration/kind/push_lease_test.go
  - test/integration/kind/suite_test.go
autonomous: true
requirements: [04.1-12-FOLLOWUP-2]
must_haves:
  truths:
    - "push_lease_test BeforeEach calls createNamespace(pushLeaseNS) BEFORE applyFile so the tide-subagent ServiceAccount and tide-signing-key Secret exist namespace-local for all 4 specs"
    - "SKIP_PHASE3_PUSH_LEASE_TESTS env-var gate is removed from push_lease_test.go — the 4 specs run unconditionally (subject only to skipIfCRDsOnlyMode)"
    - "test/integration/kind/push_lease_test.go compiles (go vet + go build clean) with no unused imports"
    - "suite_test.go's kindTestTimeout iter-5 rationale no longer references SKIP_PHASE3_PUSH_LEASE_TESTS — the 18m budget justification is preserved without the stale env-var framing"
  artifacts:
    - path: "test/integration/kind/push_lease_test.go"
      provides: "Push-lease Layer B spec with Cascade 9 recipe applied (createNamespace in BeforeEach, no SKIP gate)"
      contains: "createNamespace(pushLeaseNS)"
    - path: "test/integration/kind/suite_test.go"
      provides: "kindTestTimeout rationale without stale SKIP_PHASE3_PUSH_LEASE_TESTS reference"
  key_links:
    - from: "test/integration/kind/push_lease_test.go BeforeEach"
      to: "createNamespace(pushLeaseNS) in suite_test.go"
      via: "direct function call after skipIfCRDsOnlyMode()"
      pattern: "createNamespace\\(pushLeaseNS\\)"
---

<objective>
Close Phase 04.1 Plan 12 Outstanding Follow-up #2 (Cascade 9): apply the Cascade 6 recipe — `createNamespace(ns)` before `applyFile` — to the push_lease Layer B integration spec and drop the now-obsolete SKIP_PHASE3_PUSH_LEASE_TESTS env-var gate. This translates the locked recipe from `.planning/phases/04.1-pre-v1-audit-fixes-cross-phase-uat-closeout/04.1-12-SUMMARY.md` (line 153 — Outstanding Follow-up #2; line 96 — Cascade 6 closure shape) into source.

Purpose: enable Phase 03 push_lease tests to actually run end-to-end by providing the namespace-local `tide-subagent` ServiceAccount + helm-mirrored `tide-signing-key` Secret that the fixture YAML does NOT create inline (identical failure class to Cascade 6 on chaos_resume_test).

Output: minimal diff (≤ ~25 lines net deletion in push_lease_test.go; 2–4 lines in suite_test.go) that compiles clean under `go vet` and `go build`. Runtime gating (`make test-int`) is explicitly OUT of scope for this quick task per the user's constraint — code-shape correctness is the bar.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@CLAUDE.md
@.planning/STATE.md
@.planning/phases/04.1-pre-v1-audit-fixes-cross-phase-uat-closeout/04.1-12-SUMMARY.md
@test/integration/kind/push_lease_test.go
@test/integration/kind/chaos_resume_test.go
@test/integration/kind/suite_test.go

<interfaces>
<!-- Helpers the executor will call. All defined in suite_test.go — package-local, no import gymnastics. -->

From test/integration/kind/suite_test.go:
```go
// createNamespace creates the Namespace and namespace-local Task Job
// dependencies (tide-subagent SA, tide-projects PVC, tide-signing-key Secret).
// Idempotent — kubectl apply tolerates re-creates.
func createNamespace(ns string)

// skipIfCRDsOnlyMode Skip()s the spec when no controller deployment is live.
// Called FIRST in BeforeEach so we don't waste kubectl work on CRDs-only runs.
func skipIfCRDsOnlyMode()

// deleteNamespace deletes a namespace from the kind cluster (called in AfterEach).
func deleteNamespace(ns string)
```

From test/integration/kind/chaos_resume_test.go (reference for Cascade 6 recipe shape):
```go
// chaos_resume_test.go:128–137 — the locked recipe pattern.
// Note: chaos-resume has ONE It block, so the createNamespace call lives
// inside the It. push_lease has FOUR It blocks sharing one BeforeEach, so
// the call belongs in BeforeEach.
By("Ensure namespace-local SA + signing-key Secret (Phase 04.1 P12 iter-4 Cascade 6)")
// ... (8-line comment block — DO NOT replicate verbatim in push_lease;
//      keep the push_lease comment tight per the user's constraint)
createNamespace(chaosResumeNS)
```

From test/integration/kind/push_lease_test.go (current state — lines 54–80):
```go
var _ = Describe("Push lease semantics (ART-06 / D-B5 / D-B6)", Label("kind"), func() {
    const pushLeaseNS = "push-lease-test"

    BeforeEach(func() {
        // 12-line scope-defer comment block — REMOVE (lines 57–68)
        if os.Getenv("SKIP_PHASE3_PUSH_LEASE_TESTS") == "true" {  // REMOVE
            Skip("SKIP_PHASE3_PUSH_LEASE_TESTS=true; ...")           // REMOVE
        }
        skipIfCRDsOnlyMode()
        // ADD: createNamespace(pushLeaseNS) here, after skipIfCRDsOnlyMode
    })

    AfterEach(func() {
        deleteNamespace(pushLeaseNS)  // already correct — no change
        // ...
    })
```

From test/integration/kind/suite_test.go (current state — lines 87–93):
```go
//   - Phase 04.1-12 iter-5 bump → 18m: iter-4 observed 908s inner wall
//     barely over 900s (15m), causing chaos_resume + caps_test to
//     SKIP at the tail. push_lease specs (4 × ~103s = 415s) are the
//     dominant budget consumer; iter-5 scope-defers them via
//     SKIP_PHASE3_PUSH_LEASE_TESTS=true. 18m gives headroom for       <-- scrub env-var ref
//     unexpected first-run delays without re-tripping the cascade.
kindTestTimeout = 18 * time.Minute
```
</interfaces>

**Cascade 6 → Cascade 9 mapping** (per 04.1-12-SUMMARY.md line 96 + line 153):
- Same failure class: fixture YAML creates Namespace + PVC + provider Secret inline, but NOT the `tide-subagent` SA (chart-templated only in `tide-system`) or the helm-mirrored `tide-signing-key` Secret.
- Same closure: call `createNamespace(ns)` BEFORE `applyFile`. The helper bundles `ensureSubagentSA + ensureProjectsPVC + ensureSigningKeySecret`, all idempotent.
- Structural delta: chaos_resume has 1 It → recipe lives in the It block. push_lease has 4 It blocks sharing one BeforeEach → recipe lives in BeforeEach (covers all 4 uniformly, plays well with the existing `AfterEach { deleteNamespace(pushLeaseNS) }`).

**Out of scope** (do NOT touch in this quick task):
- Running `make test-int` — runtime gating is not the bar; code-shape correctness via `go vet` + `go build` is.
- `chaos_resume_test.go` line 230 second-stage failure (Cascade 10 — separate Phase 03 follow-up).
- Plan-pod credproxy `ANTHROPIC_API_KEY` missing (Cascade 7 — separate follow-up).
- Item 2 Layer B 429 storm spec authoring.
- Adjusting `kindTestTimeout` value (18m stays correct; only the stale comment reference gets scrubbed).
</context>

<tasks>

<task type="auto">
  <name>Task 1: Apply Cascade 9 recipe to push_lease_test.go (drop SKIP gate, add createNamespace)</name>
  <files>test/integration/kind/push_lease_test.go</files>
  <action>
In `test/integration/kind/push_lease_test.go`, surgically transform `BeforeEach` (current lines 57–73):

1. DELETE the entire 12-line comment block at lines 57–68 (the "Phase 04.1 Plan 12 iter-5: scope-defer..." rationale through the "...separate Phase 03 fix plan." closing line). The upstream summary at `.planning/phases/04.1-pre-v1-audit-fixes-cross-phase-uat-closeout/04.1-12-SUMMARY.md` Outstanding Follow-up #2 carries this rationale — do NOT replicate it in source.

2. DELETE the 3-line `if os.Getenv("SKIP_PHASE3_PUSH_LEASE_TESTS") == "true" { Skip(...) }` block (current lines 69–71). After this delete, `skipIfCRDsOnlyMode()` becomes the first line of `BeforeEach`.

3. ADD `createNamespace(pushLeaseNS)` immediately AFTER `skipIfCRDsOnlyMode()` (NOT before — running `skipIfCRDsOnlyMode` first means CRDs-only runs short-circuit before any kubectl work). Prefix with a tight 2-line comment matching chaos_resume_test.go:128–137 STYLE (terse — single By-prefixed sentence) but NOT length. Recommended comment (one By line, no multi-paragraph block): `By("Ensure namespace-local SA + signing-key Secret (Phase 04.1 P12 Cascade 9 — same shape as Cascade 6)")`. This is a Ginkgo `By` call (not a comment) so it surfaces in the spec report; alternatively a single-line `//` comment if the executor prefers — author's discretion.

4. REMOVE the `"os"` import from the import block at line 38. This is the ONLY `os.` reference in the file (verified via `grep -nE "\bos\." test/integration/kind/push_lease_test.go` → only line 69, which Task 1 step 2 deleted). Leaving it triggers `imported and not used: "os"` at compile time.

Final BeforeEach shape (target — for executor reference, NOT a code block to copy literally; preserve existing AfterEach and the rest of the Describe untouched):
- Line 1: `BeforeEach(func() {`
- Line 2: `    skipIfCRDsOnlyMode()`
- Line 3: `    By("Ensure namespace-local SA + signing-key Secret (Phase 04.1 P12 Cascade 9 — same shape as Cascade 6)")`
- Line 4: `    createNamespace(pushLeaseNS)`
- Line 5: `})`

Do NOT touch AfterEach (lines 75–80), the four It blocks (lines 82–190), any helpers below line 192, or any other file. Atomic single-file commit.

Commit message (executor task protocol): `fix(test): apply Cascade 9 recipe to push_lease_test BeforeEach — createNamespace(pushLeaseNS) + drop SKIP gate`
  </action>
  <verify>
    <automated>cd /Users/justinsearles/Projects/tide &amp;&amp; go vet ./test/integration/kind/... &amp;&amp; go build ./test/integration/kind/... &amp;&amp; grep -c 'createNamespace(pushLeaseNS)' test/integration/kind/push_lease_test.go | grep -q '^1$' &amp;&amp; ! grep -q 'SKIP_PHASE3_PUSH_LEASE_TESTS' test/integration/kind/push_lease_test.go &amp;&amp; ! grep -qE '^\s*"os"\s*$' test/integration/kind/push_lease_test.go</automated>
  </verify>
  <done>
- `go vet ./test/integration/kind/...` passes (exit 0).
- `go build ./test/integration/kind/...` passes (exit 0).
- `grep -c 'createNamespace(pushLeaseNS)' test/integration/kind/push_lease_test.go` returns exactly `1`.
- `grep 'SKIP_PHASE3_PUSH_LEASE_TESTS' test/integration/kind/push_lease_test.go` returns 0 matches.
- The `"os"` import is removed (file has no `os.` references and import block does not contain a bare `"os"` line).
- Diff scope: ONLY `test/integration/kind/push_lease_test.go` modified.
- `git diff --stat test/integration/kind/push_lease_test.go` shows roughly net `-15` to `-17` lines (12-line comment + 3-line if-block + 1-line `os` import; minus 2-line additive recipe).
  </done>
</task>

<task type="auto">
  <name>Task 2: Scrub stale SKIP_PHASE3_PUSH_LEASE_TESTS reference from suite_test.go kindTestTimeout rationale</name>
  <files>test/integration/kind/suite_test.go</files>
  <action>
In `test/integration/kind/suite_test.go`, surgically edit the kindTestTimeout iter-5 comment paragraph (current lines 87–93) to remove the stale `SKIP_PHASE3_PUSH_LEASE_TESTS=true` env-var reference while preserving the 18m budget justification.

Current paragraph (lines 87–93):
```
//   - Phase 04.1-12 iter-5 bump → 18m: iter-4 observed 908s inner wall
//     barely over 900s (15m), causing chaos_resume + caps_test to
//     SKIP at the tail. push_lease specs (4 × ~103s = 415s) are the
//     dominant budget consumer; iter-5 scope-defers them via
//     SKIP_PHASE3_PUSH_LEASE_TESTS=true. 18m gives headroom for
//     unexpected first-run delays without re-tripping the cascade.
```

Replace with (target — adapt as needed to preserve adjacent paragraph wrapping):
```
//   - Phase 04.1-12 iter-5 bump → 18m: iter-4 observed 908s inner wall
//     barely over 900s (15m), causing chaos_resume + caps_test to
//     SKIP at the tail. push_lease specs (4 × ~103s = 415s) are the
//     dominant budget consumer (now run unconditionally per the
//     quick-task Cascade 9 recipe — see 04.1-12-SUMMARY.md Outstanding
//     Follow-up #2). 18m gives headroom for unexpected first-run
//     delays without re-tripping the cascade.
```

Key edit: replace the single sentence "iter-5 scope-defers them via SKIP_PHASE3_PUSH_LEASE_TESTS=true." with a forward-looking pointer to the recipe-application quick task. Preserve every other word in the paragraph and the surrounding iteration-history paragraphs (lines 78–86). Do NOT change the `kindTestTimeout = 18 * time.Minute` constant value (line 93) — the budget remains correct with push_lease running.

Also REMOVE the residual `SKIP_PHASE3_PUSH_LEASE_TESTS` token if it appears anywhere else in the file. Verified during planning that line 91 is the only occurrence in `suite_test.go`, but re-grep before commit as a defensive check.

Do NOT touch any other file, the rest of `suite_test.go`, or the kindTestTimeout value. Atomic single-file commit.

Commit message (executor task protocol): `docs(test): scrub stale SKIP_PHASE3_PUSH_LEASE_TESTS reference from suite_test.go kindTestTimeout rationale`
  </action>
  <verify>
    <automated>cd /Users/justinsearles/Projects/tide &amp;&amp; go vet ./test/integration/kind/... &amp;&amp; go build ./test/integration/kind/... &amp;&amp; ! grep -q 'SKIP_PHASE3_PUSH_LEASE_TESTS' test/integration/kind/suite_test.go &amp;&amp; grep -qE 'kindTestTimeout\s*=\s*18\s*\*\s*time\.Minute' test/integration/kind/suite_test.go</automated>
  </verify>
  <done>
- `go vet ./test/integration/kind/...` passes (exit 0).
- `go build ./test/integration/kind/...` passes (exit 0).
- `grep 'SKIP_PHASE3_PUSH_LEASE_TESTS' test/integration/kind/suite_test.go` returns 0 matches.
- `kindTestTimeout = 18 * time.Minute` constant value is UNCHANGED.
- The iter-5 paragraph still references the 18m bump rationale (chaos_resume + caps_test SKIP at the tail, push_lease ~415s budget consumer) — only the env-var sentence was replaced.
- Diff scope: ONLY `test/integration/kind/suite_test.go` modified.
- `git diff --stat test/integration/kind/suite_test.go` shows roughly net `+1` to `+3` lines (comment text reflow only).
  </done>
</task>

</tasks>

<verification>

**Repo-wide stale-reference sweep** (run after both tasks land):

```bash
# Should return 0 matches anywhere in the source tree (planning artifacts intentionally retain it for cascade history):
grep -rn "SKIP_PHASE3_PUSH_LEASE_TESTS" /Users/justinsearles/Projects/tide/test /Users/justinsearles/Projects/tide/cmd /Users/justinsearles/Projects/tide/internal /Users/justinsearles/Projects/tide/pkg /Users/justinsearles/Projects/tide/Makefile 2>/dev/null

# Should compile cleanly (no `imported and not used` errors):
cd /Users/justinsearles/Projects/tide && go vet ./test/integration/kind/...
cd /Users/justinsearles/Projects/tide && go build ./test/integration/kind/...

# Cascade 6 recipe shape parity check (both files should have the same recipe-call pattern, one each):
grep -c "createNamespace(chaosResumeNS)" /Users/justinsearles/Projects/tide/test/integration/kind/chaos_resume_test.go
grep -c "createNamespace(pushLeaseNS)" /Users/justinsearles/Projects/tide/test/integration/kind/push_lease_test.go
# Both should return 1
```

**Optional bonus** (not blocking):

```bash
# goimports / gofmt parity (recommended but not gating):
cd /Users/justinsearles/Projects/tide && gofmt -l test/integration/kind/push_lease_test.go test/integration/kind/suite_test.go
# Should output nothing
```

</verification>

<success_criteria>

1. `test/integration/kind/push_lease_test.go`:
   - `createNamespace(pushLeaseNS)` called in `BeforeEach` AFTER `skipIfCRDsOnlyMode()`.
   - `SKIP_PHASE3_PUSH_LEASE_TESTS` env-var gate removed.
   - 12-line scope-defer comment block removed.
   - `"os"` import removed (no `os.` references remain in the file).
   - Tight 1-line `By(...)` or `//` comment naming the Cascade 9 / Cascade 6 recipe — NOT a multi-paragraph block.

2. `test/integration/kind/suite_test.go`:
   - `SKIP_PHASE3_PUSH_LEASE_TESTS` token removed.
   - `kindTestTimeout = 18 * time.Minute` value UNCHANGED.
   - 18m budget rationale preserved (iter-4 → iter-5 wall-time history intact).

3. Compilation: `go vet ./test/integration/kind/...` and `go build ./test/integration/kind/...` both pass with exit 0.

4. Two atomic commits on `main` (or worktree branch, per executor protocol) — one per file, each with the commit message stub provided in the task action.

5. `make test-int` was NOT run as part of this quick task (per the user's explicit constraint). The user runs it separately if they want the runtime gate on Phase 03 push_lease.

</success_criteria>

<output>
After completion, create `.planning/quick/260521-ccz-push-lease-cascade-9-recipe/260521-ccz-SUMMARY.md` per the standard quick-task summary template. The summary should record:

- Both commit SHAs (one per task).
- The before/after diff line-count for each file.
- A note that Cascade 9 from Plan 04.1-12 Outstanding Follow-up #2 is now CLOSED at the source-recipe level (runtime verification deferred to the user's next `make test-int` run).
- An explicit "Out of scope" footer listing the remaining Phase 03 follow-ups (Cascade 7 plan-pod credproxy, Cascade 10 chaos_resume line 230, Item 2 Layer B 429 storm) so the cascade map stays auditable.
</output>
