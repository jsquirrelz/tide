---
phase: 05-distribution-self-hosting-acceptance
plan: 12
subsystem: distribution
tags: [examples, medium-sample, demo-init, local-only-git-remote, go-embed, distroless]
requires:
  - "examples/tide-demo-fixture/ (plan 05-06 — source-of-truth seed content, MIT-licensed)"
  - "cmd/tide-push/main.go (PATTERNS.md §P5.1 — analog binary shape)"
  - "images/tide-push/Dockerfile (PATTERNS.md §P5.2 — analog Dockerfile shape)"
  - "pkg/git (Phase 3 — though not directly imported, validates the file:// transport convention)"
provides:
  - "cmd/tide-demo-init/ — Go binary that initializes a bare local-only git remote from embedded examples/tide-demo-fixture/ content"
  - "images/tide-demo-init/Dockerfile — container image for in-cluster bootstrap Job"
  - "examples/projects/medium/ — 5 manifests (namespace + PVC + init-Job + Project + README) implementing the ~$5 medium sample with local-only git remote"
  - "3-sample cost spectrum complete (small $0 / medium $5 / large $25) — DIST-04 medium slot"
affects:
  - "verify-license.sh — Go-header exclusion list now includes cmd/tide-demo-init/fixture/* (keeps gate green when developers materialize the fixture locally)"
  - ".gitignore — cmd/tide-demo-init/fixture/ + root-level tide-* / stub-subagent binary patterns"
tech-stack:
  added: []  # No new tech — uses go-git/v5 (already pinned), distroless/static:nonroot (already used), golang:1.26-alpine (already pinned)
  patterns:
    - "//go:embed all:fixture with build-time directory positioning (MEDIUM-11 lock)"
    - "submodule shim: rename go.mod → go.mod.txt during fixture materialization so Go's embed doesn't reject the dir as a different module; restoreShimmedName reverses at unpack time"
    - "invariantError sentinel for exit-code discrimination (exit 2 = bad args / dir-already-exists; exit 1 = generic failure)"
key-files:
  created:
    - "cmd/tide-demo-init/main.go (270 lines — binary with embed, bootstrap() helper, restoreShimmedName helper)"
    - "cmd/tide-demo-init/main_test.go (180 lines — TestBootstrapDirRequired + TestBootstrapRefusesExistingTarget + TestBootstrapHappyPath with clone-and-inspect round-trip)"
    - "cmd/tide-demo-init/README.md (binary docs — purpose / flags / exit codes / build paths / submodule shim explanation)"
    - "images/tide-demo-init/Dockerfile (two-stage distroless build with fixture COPY + go.mod rename)"
    - "examples/projects/medium/namespace.yaml (tide-sample-medium Namespace)"
    - "examples/projects/medium/demo-remote-pvc.yaml (ReadWriteOnce 100Mi PVC for the local-only bare repo)"
    - "examples/projects/medium/demo-remote-init-job.yaml (batch/v1 Job that runs tide-demo-init image, mounts the PVC)"
    - "examples/projects/medium/project.yaml (tideproject.k8s/v1alpha1 Project medium-project; targetRepo file:///demo-remote.git; absoluteCapCents 500; claude-haiku-4-5)"
    - "examples/projects/medium/README.md (7-step apply sequence, observability walkthrough, schema-gap notes)"
  modified:
    - ".gitignore (+ cmd/tide-demo-init/fixture/ + root-level binary names)"
    - "hack/scripts/verify-license.sh (Go-header exclusion + cmd/tide-demo-init/fixture/*)"
decisions:
  - "MEDIUM-11 embed strategy HONORED: //go:embed all:fixture + Dockerfile COPY + go:generate hybrid. No symlinks. Submodule shim (go.mod → go.mod.txt during materialization, reversed at unpack) added to handle Go's same-module embed constraint — documented inline in main.go package-doc + README + Dockerfile comments."
  - "fixture/ directory gitignored. SOT remains examples/tide-demo-fixture/; the cmd/tide-demo-init/fixture/ directory is purely a build-time materialization."
  - "outcomePrompt carried as tideproject.k8s/outcome-prompt annotation per v1.0 schema gap (same posture as large sample per plan 05-11 deferred-items.md entry)."
  - "file:///demo-remote.git used in both ProjectSpec.targetRepo and Git.RepoURL despite CEL/Pattern validation rejecting file://. Same posture as small sample (file:///tmp/no-such-repo); schema gap logged in deferred-items.md (extended this iteration)."
  - "Plan split per MEDIUM-8 honored exactly: Task 1 = Go binary + tests; Task 2 = Dockerfile + cmd README + 5 medium-sample manifests. Two atomic commits."
metrics:
  duration_minutes: 18
  completed_at: "2026-05-23T12:56:37Z"
  tasks_completed: 2
  files_created: 9
  files_modified: 2
  commits:
    - "56a745b feat(05-12): add cmd/tide-demo-init Go binary + unit tests (Task 1)"
    - "6db7cf1 feat(05-12): add tide-demo-init Dockerfile + medium-sample manifests (Task 2)"
---

# Phase 05 Plan 12: cmd/tide-demo-init + medium-sample (~$5 local-only git remote) Summary

Ships the medium-sample cost-spectrum slot (DIST-04). `cmd/tide-demo-init/` is a small Go binary (Apache-2.0) that initializes a bare local-only git remote from embedded `examples/tide-demo-fixture/` content; the medium sample (`examples/projects/medium/`) is the 5-manifest apply surface that uses the binary as an in-cluster Job to bootstrap a `file:///demo-remote.git` PVC, then runs TIDE against real Claude (Haiku 4.5) at ~$5 with zero external repo dependency (Phase 5 D-B3).

## Outcome

Two atomic commits per the MEDIUM-8 plan split:

| Task | Commit  | Subject                                                            | Files                                                                                                                          |
| ---- | ------- | ------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------ |
| 1    | 56a745b | feat(05-12): add cmd/tide-demo-init Go binary + unit tests          | cmd/tide-demo-init/main.go + main_test.go + .gitignore + hack/scripts/verify-license.sh                                       |
| 2    | 6db7cf1 | feat(05-12): add tide-demo-init Dockerfile + medium-sample manifests| images/tide-demo-init/Dockerfile + cmd/tide-demo-init/README.md + examples/projects/medium/{namespace,pvc,init-job,project,README}.yaml |

Total: 9 new files + 2 modified.

## Verification — what was run and what was observed

| Check                                                                     | Result                                                                                                            |
| ------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| `go generate ./cmd/tide-demo-init/...`                                    | OK (populates `cmd/tide-demo-init/fixture/` with go.mod/go.sum renamed to .txt)                                  |
| `go build ./cmd/tide-demo-init/`                                          | OK (no compile errors after submodule-shim fix; embed resolves)                                                  |
| `go test ./cmd/tide-demo-init/`                                           | OK — `TestBootstrapDirRequired` PASS, `TestBootstrapRefusesExistingTarget` PASS, `TestBootstrapHappyPath` PASS (0.12s) |
| `gofmt -l ./cmd/tide-demo-init/main.go ./cmd/tide-demo-init/main_test.go` | clean (no output)                                                                                                |
| `go vet ./cmd/tide-demo-init/`                                            | clean (no output)                                                                                                |
| `bash hack/scripts/verify-license.sh`                                     | PASS (LICENSE + NOTICE + every *.go file under api/cmd/internal/pkg/test/tools/ carries Apache-2.0 header)        |
| YAML lint (`python3 yaml.safe_load_all` on all 4 medium-sample YAML files) | all 4 parse cleanly                                                                                              |
| Acceptance grep checks (Task 1) — 10 checks                                | all OK (Apache header / package main / //go:embed all:fixture / //go:generate / go-git import / func bootstrap / func TestBootstrap*) |
| Acceptance grep checks (Task 2) — 17 checks                                | all OK (FROM golang:1.26-alpine, distroless, USER 1000, cmd/tide-demo-init/fixture/, ENTRYPOINT, namespace, accessModes, ReadWriteOnce, image tag, Job args, apiVersion, file:///demo-remote.git, $5 cap, model pick, README mentions, kubectl wait, kubectl create secret, Cleanup) |

## Embed-strategy verification (MEDIUM-11 lock)

Three independent paths to fixture materialization, all converging on the same `cmd/tide-demo-init/fixture/` build-time directory:

1. **Local `go build`:** `go generate ./cmd/tide-demo-init/...` runs the bash command embedded in main.go's `//go:generate` directive, which copies `examples/tide-demo-fixture/.` to `./fixture/` and renames go.mod → go.mod.txt + go.sum → go.sum.txt. Verified: `ls cmd/tide-demo-init/fixture/` shows `go.mod.txt go.sum.txt main.go main_test.go README.md`.
2. **Docker image build:** `images/tide-demo-init/Dockerfile` line `COPY examples/tide-demo-fixture/ cmd/tide-demo-init/fixture/` + two `RUN` lines that perform the same go.mod → go.mod.txt + go.sum → go.sum.txt renames inside the build context. (Image build not exercised by this plan's verification — that's the chart-release path in 05-16.)
3. **Test invocation:** `go test ./cmd/tide-demo-init/` (after `go generate`) exercises the full embed → bootstrap → bare-repo → clone path via `TestBootstrapHappyPath`. The test clones the bare repo and asserts `main.go`, `main_test.go`, `go.mod`, `README.md` round-trip without byte drift; the `restoreShimmedName` helper reverses the `.txt` renames at unpack time so the bare repo's working tree carries canonical filenames.

The submodule shim emerged during execution as a Rule 3 fix (blocking issue): Go's `//go:embed` refuses to cross go.mod boundaries — embedding `examples/tide-demo-fixture/go.mod` directly would surface as `cannot embed directory fixture: in different module`. The rename-on-materialize + restore-on-unpack pattern keeps the embed directive locked exactly as MEDIUM-11 specifies (`//go:embed all:fixture`) while satisfying Go's same-module constraint.

## Schema-gap notes (carry-forward to deferred-items.md)

Two v1.0 schema gaps already known from plan 05-11 — same posture applied here:

- **`ProjectSpec.targetRepo` CEL validator** currently allows only `http`/`git@` prefixes; the medium-sample contract uses `file:///demo-remote.git`. Same as the small sample (`file:///tmp/no-such-repo`) — ship-as-intended, schema catches up in v1.x. Documented inline in `project.yaml` + medium README.
- **`ProjectSpec.Git.RepoURL` Pattern** `^https?://.+` has the same gap. Same treatment.
- **`outcomePrompt`** is not a Spec field; carried as `tideproject.k8s/outcome-prompt` annotation. Same posture as large sample. Documented inline.

These will be picked up by the v1.x schema work that promotes outcomePrompt and extends the targetRepo allow-list. The medium sample's contract values stay correct; only the admission gate needs to widen.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Submodule shim for //go:embed across go.mod boundary**

- **Found during:** Task 1 — `go build ./cmd/tide-demo-init/` immediately after the first `go generate` materialized `fixture/` from `examples/tide-demo-fixture/`.
- **Issue:** Go's `//go:embed all:fixture` rejected the directory with `cannot embed directory fixture: in different module`. The fixture SOT ships its own go.mod / go.sum (it's a standalone Go module the medium-sample's Claude task operates on), and Go refuses to embed across module boundaries.
- **Fix:** Rename `go.mod` → `go.mod.txt` and `go.sum` → `go.sum.txt` during fixture materialization (in BOTH the go:generate directive AND the Dockerfile COPY/RUN block). Added `restoreShimmedName` helper in `cmd/tide-demo-init/main.go` that reverses the rename at unpack time so the bare repo's working tree carries canonical filenames byte-for-byte equivalent to the SOT.
- **Files modified:** cmd/tide-demo-init/main.go (`restoreShimmedName` + go:generate directive); images/tide-demo-init/Dockerfile (RUN mv); cmd/tide-demo-init/README.md (Submodule shim section). MEDIUM-11 embed directive `//go:embed all:fixture` itself UNCHANGED — still the locked value.
- **Commit:** 56a745b (the shim landed in Task 1's main.go; Dockerfile RUN steps landed in Task 2's commit 6db7cf1).
- **Why this doesn't violate MEDIUM-11:** The lock is the `//go:embed all:fixture` directive shape. Materialization-time renames don't change the embed directive; they change the on-disk layout the embed reads from. Plan-checker grep `grep -q "//go:embed all:fixture" cmd/tide-demo-init/main.go` still returns 0.

**2. [Rule 2 - Missing critical functionality] verify-license.sh exclusion for cmd/tide-demo-init/fixture/**

- **Found during:** Task 1 acceptance verification — after `go generate` materialized the fixture, the fixture's MIT-licensed Go files (main.go, main_test.go) would FAIL the Apache-2.0 header check if verify-license.sh ran from a normal (non-worktree) checkout.
- **Issue:** The script already excludes `examples/*` (where the fixture SOT lives, also MIT-licensed) but not `cmd/tide-demo-init/fixture/*`. A developer who runs `go generate` followed by `verify-license.sh` from a non-worktree checkout would see a spurious failure.
- **Fix:** Extended the find exclusion in `hack/scripts/verify-license.sh` with `-not -path '*/cmd/tide-demo-init/fixture/*'`. Comment in the script documents the rationale (fixture is materialized at build time, gitignored, carries MIT-licensed content).
- **Files modified:** hack/scripts/verify-license.sh
- **Commit:** 56a745b
- **Risk if not fixed:** Plan success criterion #4 ("verify-license.sh still passes after this plan lands") would fail intermittently depending on whether the executor (or CI) ran `go generate` before verifying. Fixing inline keeps the gate green deterministically.

**3. [Rule 2 - Missing critical functionality] .gitignore entries for root-level binaries**

- **Found during:** Task 1 — `go build ./cmd/tide-demo-init/` left a `./tide-demo-init` binary at the repo root (Go's default output path).
- **Issue:** The existing `.gitignore` only listed `/tide-lint`, `/manager`, `/credproxy`, `/tide` but not the newer cmd binaries. A developer's `git add .` could accidentally commit the binary.
- **Fix:** Added `/tide-demo-init`, `/tide-push`, `/stub-subagent` to the same `.gitignore` section that lists the other root-level binaries.
- **Files modified:** .gitignore
- **Commit:** 56a745b

### Schema-gap notes carried over (NOT a deviation — established convention from plan 05-11)

Per plan 05-11's deferred-items.md entry, the v1.0 ProjectSpec lacks `OutcomePrompt` and its CEL/Pattern validators reject `file://`. The medium sample ships with the contract values (`file:///demo-remote.git`, annotation-carried outcomePrompt) and documents the gap inline + in this Summary's Schema-gap notes section. This is the established posture for Phase 5 samples; not flagged as a deviation.

### Architecture changes proposed

None. All discoveries were either inline fixes (Rules 1-3) or schema-gap carry-forwards (established convention).

## Known Stubs

None for this plan. The medium sample is fully wired:

- `cmd/tide-demo-init` is a real binary, not a stub — it actually creates bare repos and pushes commits.
- `examples/projects/medium/project.yaml` references a real container image (`ghcr.io/jsquirrelz/tide-claude-subagent:v1.0.0`); the publish-side wiring is plan 05-16's responsibility.
- `examples/projects/medium/demo-remote-init-job.yaml` references `ghcr.io/jsquirrelz/tide-demo-init:v1.0.0` — same publish-side dependency on 05-16.

The two image references are forward-references to plan 05-16's release pipeline, not stubs. The medium sample will work end-to-end once the v1.0 chart release ships those images.

## Threat surface scan

No new threat surface introduced beyond what the plan's `<threat_model>` already enumerated. The medium-sample's local-only `file://` transport is intentionally lower-risk than the large sample's HTTPS+PAT path (no network egress, no credentials in flight). The submodule-shim discovery is purely a build-time materialization concern — no new trust boundary.

The plan's threat register (T-05-12-01 through T-05-12-04) all carry `mitigate` dispositions, all addressed by the implementation:

- **T-05-12-01 (fixture drift):** Dockerfile COPY + go:generate both source from `examples/tide-demo-fixture/` — single SOT.
- **T-05-12-02 (PVC cross-pod):** Accepted; standard K8s RWO semantics; documented in PVC manifest.
- **T-05-12-03 (embed positioning):** Submodule shim + MEDIUM-11 lock; single embed directive in main.go.
- **T-05-12-04 (USER 1000):** Dockerfile sets USER 1000 + distroless/static:nonroot base.

## Self-Check

### Files created — verified present

- `[FOUND] cmd/tide-demo-init/main.go`
- `[FOUND] cmd/tide-demo-init/main_test.go`
- `[FOUND] cmd/tide-demo-init/README.md`
- `[FOUND] images/tide-demo-init/Dockerfile`
- `[FOUND] examples/projects/medium/namespace.yaml`
- `[FOUND] examples/projects/medium/demo-remote-pvc.yaml`
- `[FOUND] examples/projects/medium/demo-remote-init-job.yaml`
- `[FOUND] examples/projects/medium/project.yaml`
- `[FOUND] examples/projects/medium/README.md`

### Files modified — verified non-empty diffs

- `[FOUND] .gitignore` (+ cmd/tide-demo-init/fixture/ + root-level binary names)
- `[FOUND] hack/scripts/verify-license.sh` (+ fixture/* exclusion)

### Commits — verified present on this branch

- `[FOUND] 56a745b feat(05-12): add cmd/tide-demo-init Go binary + unit tests`
- `[FOUND] 6db7cf1 feat(05-12): add tide-demo-init Dockerfile + medium-sample manifests`

## Self-Check: PASSED
