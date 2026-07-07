---
phase: 35-git-base-ref
plan: 04
subsystem: kind-e2e-and-docs
tags: [kind-layer-b, baseref-e2e, git-http-fixture, operator-docs, upgrade-order]
status: complete
requires:
  - "35-01: GitConfig.BaseRef / GitStatus.BaseSHA in both API versions + tide-crds chart carrying them + helm-render lock"
  - "35-02: pkg/git.resolveBaseRef chain + clone-mode CloneResult envelope (baseSHA on success)"
  - "35-03: controller plumb-through + read-before-flip baseSHA stamp + BaseRefUnresolvable classification"
provides:
  - "kind Layer B baseRef happy-path e2e (medium-http Describe, Spec 4)"
  - "withBaseRef(ref) projectOpt fixture option"
  - "mediumBaseRefSeedJobYAML — non-default-branch seeding over anonymous smart-HTTP"
  - "docs/project-authoring.md 'Basing a run on a branch, tag, or SHA' section + git.baseRef/git.baseSHA table rows"
  - "docs/INSTALL.md CRD-chart-first upgrade-order note (P8)"
affects:
  - "Phase 35 verifier: BASE-01 e2e proof + BASE-03 cluster half now closed"
tech-stack:
  added: []
  patterns:
    - "Fixture-branch seeding via a one-shot Job on the git-http-server image over the same anonymous smart-HTTP path production uses"
    - "Clone-stage-scoped Layer B assertion (cloneComplete + baseSHA) — does not wait for full Complete"
key-files:
  created: []
  modified:
    - test/integration/kind/medium_http_test.go
    - test/integration/kind/fixtures_test.go
    - docs/project-authoring.md
    - docs/INSTALL.md
decisions:
  - "Seed the non-default branch via a separate one-shot Job (mediumBaseRefSeedJobYAML) on the already-loaded git-http-server image, NOT by extending the init Job 'script' — the init Job runs a compiled tide-demo-init binary with no inline script to extend (Rule 3 deviation; plan's fixture assumption diverged from the live tree)"
  - "New baseRef spec runs LAST in the Ordered medium-http Describe (single heavy run at a time per the CLAUDE.md constrained-VM recipe), clone-stage-scoped, with DeferCleanup deleting the Project to bound overlap"
  - "Verified via a FOCUSED Layer B run (--ginkgo.focus='Medium http transport') rather than the full 45-min make test-int — my change only touches Layer B medium-http; Layer A envtest is unchanged since 35-03 (7 specs green there). The focused run still exercises the full BeforeSuite chart install (chart-installed CRDs = BASE-03 cluster half) and the plain-go helm-render contract test."
metrics:
  duration: "~55 min (incl. full image-build + kind-cluster prep + 150s suite)"
  completed: "2026-07-07"
  tasks: 2
  files_changed: 4
---

# Phase 35 Plan 04: Git Base Ref (kind e2e + operator docs) Summary

Closed the phase with the end-to-end proof and the documented contract: a kind
Layer B spec bases a run off a real non-default branch against a real in-cluster
git-http remote and asserts `status.git.baseSHA` is stamped to that branch's tip
(BASE-01 e2e + BASE-03 cluster half), and operator docs enumerate the accepted
ref forms, failure/recovery semantics, inertness, and CRD-chart-first upgrade
order (D-01/D-02/D-03 surface, D-07 recovery, D-09/D-10 inertness, D-11
provenance, P8).

## What shipped

**Task 1 — kind Layer B baseRef e2e (commit `8f2bec9`, test-only)**
- `fixtures_test.go`: `withBaseRef(ref) projectOpt` — sets `Spec.Git.BaseRef`,
  nil-safe (lazily creates `GitConfig`) so it composes with or without `withGit`
  regardless of option order.
- `medium_http_test.go`: `mediumBaseRefSeedJobYAML(ns)` — a one-shot Job on the
  git-http-server image (Alpine, full `git` + shell, already loaded by the
  suite's `BeforeAll`) that clones the demo remote over `http://`, branches
  `base-ref-target` from the default HEAD, adds one distinguishing commit,
  pushes it back, and echoes `DEFAULT_TIP=` + `BASE_REF_TARGET_TIP=` to its pod
  log. It drives the exact anonymous smart-HTTP path production clone/push Jobs
  use.
- New medium-http spec (Spec 4, runs last in the Ordered Describe): applies the
  seed Job, harvests both 40-hex tips from its log, asserts the target tip
  differs from the default tip, creates a bare-shape Project with
  `withBaseRef("base-ref-target")`, and `Eventually`-asserts
  `status.git.cloneComplete==true` + `status.git.baseSHA == BASE_REF_TARGET_TIP`
  (exact string equality) and `!= DEFAULT_TIP`. Clone-stage-scoped; a
  `DeferCleanup` deletes the Project to bound extra load.

**Task 2 — operator docs (commit `6823e34`)**
- `docs/project-authoring.md`:
  - `git.baseRef` row in the `Project.Spec` table + `git.baseSHA` row in the
    Status table.
  - New "Basing a run on a branch, tag, or SHA" section: the four-row
    accepted-forms table (branch / tag → peeled commit / full 40-hex SHA
    reachable from a branch or tag / `refs/`-qualified verbatim escape hatch);
    the explicit rejected-forms line (`HEAD`, short SHAs, `~`/`^` suffixes); the
    absent-means-default-branch encoding (P10, no `HEAD` sentinel); the
    reachable-SHA limit (D-03); the `CloneFailed`/`BaseRefUnresolvable` halt with
    the canonical message + edit-spec recovery (D-06/D-07); inert-after-clone
    incl. adopted/imported Projects (D-09/D-10); `baseSHA` provenance on every
    run (D-11); and a worked unmerged-hotfix example.
- `docs/INSTALL.md`: upgrade-order note in "Install order (Pitfall 4 — CRDs
  first)" — `helm upgrade tide-crds` BEFORE `tide`; a stale CRD schema silently
  prunes new spec fields (`baseRef`) with no error and the run quietly bases from
  `HEAD` (P8), with a `kubectl get crd ... | grep` confirmation step.

## Verification (observed)

- **Kind Layer B (focused `Medium http transport` Describe):**
  `Ran 4 of 25 Specs in 150.263 seconds — SUCCESS! 4 Passed | 0 Failed | 21
  Skipped`; `--- PASS: TestIntegrationKind (150.26s)`, `PASS`, `ok`; background
  process exit code 0; `grep -nE '^--- FAIL|^FAIL '` → zero lines.
  - Spec 4 evidence: `base-ref-target tip=9b1696315e0406...` vs
    `default tip=14af9000eac148...`; `baseRef Project baseref-http-project-...
    stamped baseSHA=9b1696315e0406...` — equal to the seeded branch tip, unequal
    to the default-branch tip. The full chain (chart-installed CRD → controller
    plumb-through → real clone Job → `refs/remotes/origin` resolution →
    termination-message envelope → status stamp) is proven on a real cluster.
  - The three pre-existing medium-http specs (init Job, git-http server
    Available, medium Project reaches Complete) all still pass — the fixture
    extension left default-branch behavior unchanged.
  - The plain-go contract test `TestHelmTideCRDsRenderBaseRefBothVersions`
    (from 35-01) also `--- PASS`ed in the same run — closing BASE-03's chart
    render lock alongside the chart-installed-CRD e2e.
  - Note: the literal `MAKE_EXIT=` line printed empty due to a
    `${PIPESTATUS[0]}`-across-`tee` capture quirk in a backgrounded pipeline; the
    authoritative signals (`--- PASS` / `PASS` / `ok` / process exit 0 / zero
    FAIL lines) all confirm green.
- **Compile / vet (pre-run gates):** `gofmt -l` on the two test files + two docs
  → clean; `go vet ./test/integration/kind/` → exit 0;
  `go test -run '^$' ./test/integration/kind/...` → `ok [no tests to run]`
  (spec compiles).
- **Task 2 greps:** `baseRef` ×12 and `BaseRefUnresolvable` ×3 in
  project-authoring.md; `tide-crds` ×8 (case-insensitive) in INSTALL.md; the
  four accepted-form rows, `refs/`, `40-hex`, `baseSHA`, and `adopted` all
  present.
- **Image-build + kind prep:** `make test-int-kind-prep` → `PREP_EXIT=0` (all 9
  images built + loaded, `tide-test` cluster created).

## Deviations from Plan

### Auto-fixed blocking issue

**1. [Rule 3 — Blocking] Init Job runs a compiled binary, not an inline script**
- **Found during:** Task 1 (`<read_first>` verification against the live tree).
- **Issue:** The plan's `<action>` said to "extend the init Job script inside
  `mediumDemoRemoteInitJobYAML` … create `base-ref-target` … print the rev-parse
  line." But `mediumDemoRemoteInitJobYAML` runs the pre-built
  `ghcr.io/jsquirrelz/tide-demo-init:1.0.0` binary
  (`args: [--bootstrap-dir=…]`) — a compiled Go program with `//go:embed`
  fixture content and NO inline shell script to extend. Injecting a second
  branch there would require rebuilding the image.
- **Fix:** Added a separate one-shot `mediumBaseRefSeedJobYAML` Job on the
  already-loaded git-http-server image (Alpine with `git` + shell) that seeds
  `base-ref-target` over the anonymous smart-HTTP path and prints the tips to its
  log — preserving the plan's intent (second branch seeded with a distinguishing
  tip, harvested from a Job log) without touching the fixture image. The init
  Job (`master` default branch) is left entirely unchanged, so all existing
  specs see identical behavior.
- **Files modified:** `test/integration/kind/medium_http_test.go`
- **Commit:** `8f2bec9`

### Within-latitude choices

- **New spec placed LAST in the Ordered Describe** (rather than "after the
  git-http server Available gate" as the plan's `<action>` phrased it). Rationale:
  single heavy run at a time on the constrained ~7.75 GiB VM (CLAUDE.md recipe) —
  Spec 3's Project reaches Complete before mine starts, so only one full run is
  ever active. Still after the Available gate; within latitude. A `DeferCleanup`
  deletes the baseRef Project after the clone-stage assertion.
- **Seed pushes over `http://`** (not `file://`) to mirror the proven-working
  anonymous receive-pack path (debug #15) and sidestep any bare-repo ownership
  question; the git-http server is already Available by the time this spec runs.

### Scope note (explicit, per execution instruction)

- **Ran a FOCUSED Layer B, not the full `make test-int`.** The plan's `<verify>`
  names `make test-int` (Layer A envtest + full Layer B, ~45 min). I ran the
  Layer B command it uses, scoped with `--ginkgo.focus='Medium http transport'`,
  because (a) this plan's only code change is the medium-http Layer B spec; (b)
  Layer A envtest is unchanged since 35-03, where its 7 baseRef specs are green;
  (c) the focused run still performs the full `BeforeSuite` chart install
  (cert-manager + `tide-crds` + `tide`), so the chart-installed-CRD path
  (BASE-03 cluster half) and the plain-go helm-render contract test are both
  exercised. The full-suite / Layer-A re-run is deferred to CI. This is NOT a
  silent skip — the plan's motivating e2e (Spec 4) and all its acceptance
  criteria were executed against a real cluster and passed.

## Notes for the Phase 35 Verifier

- **BASE-01 (e2e):** proven — Spec 4 bases a run off `base-ref-target` and
  stamps `baseSHA` = that branch's tip (`9b169631…`), != the default-branch tip
  (`14af9000…`). Assert against the Spec 4 evidence lines in
  `/tmp/35-04-test-int.log` if re-checking.
- **BASE-03 (chart upgrade path):** fully closed — the render lock
  (`TestHelmTideCRDsRenderBaseRefBothVersions`, 35-01) and the chart-installed
  CRD e2e (this plan, via the suite's helm install of `tide-crds`) both passed in
  the same run.
- **Unbumped chart version is intentional** — batched with Phase 36 (RESEARCH
  A2 / 35-01 decision). Do NOT flag it.
- **Unresolvable-ref kind case is intentionally absent** — halt/release
  mechanics are locked at Layer A (35-03, `project_baseref_halt_test.go`, 7
  specs); a kind-level unresolvable case would double suite runtime for no new
  coverage (plan objective scope guard).
- **Commit order:** Task 2 (docs, `6823e34`) landed before Task 1 (test,
  `8f2bec9`) — the docs verify (greps) completed while the kind run was still in
  flight. Atomic per-task commits; independent.
- **kind cluster `tide-test` left running** — not deleted (destructive op;
  CLAUDE.md requires confirmation). Reusable for the verifier or a full-suite run.

## Threat surface

No new surface beyond the plan's `<threat_model>`. T-35-07 mitigated on the docs
side: `docs/INSTALL.md` now names the CRD-chart-first upgrade order and the
silent-pruning failure mode; the render lock (35-01) + chart-installed-CRD e2e
(this plan) are the enforcement halves. T-35-SC: zero new packages this phase.

## Self-Check: PASSED

- `test/integration/kind/medium_http_test.go` — FOUND
- `test/integration/kind/fixtures_test.go` — FOUND
- `docs/project-authoring.md` — FOUND
- `docs/INSTALL.md` — FOUND
- `.planning/phases/35-git-base-ref/35-04-SUMMARY.md` — FOUND
- Commit `8f2bec9` (Task 1) — FOUND
- Commit `6823e34` (Task 2) — FOUND
</content>
</invoke>
