---
phase: 03-up-stack-reconcilers-git-integration-real-subagent-resumptio
plan: 06
subsystem: infra
tags: [push-job, git, gitleaks, force-with-lease, controller-helpers, distroless, k8s-job, secret-isolation]

# Dependency graph
requires:
  - phase: 03 Wave 2
    provides: pkg/git (Clone, AddPath, Commit, Push with ForceWithLease, Fetch, AddWorktree)
  - phase: 03 Wave 2
    provides: internal/gitleaks (ScanDiff over unified diff)
  - phase: 02
    provides: D-A2 envelope-on-PVC pattern + deterministic Job naming pattern (buildInitJob)
provides:
  - cmd/tide-push static Go binary — clone + push modes, single-binary push Job entrypoint
  - images/tide-push distroless/static:nonroot Dockerfile — pure-Go, no shell-out
  - internal/controller/push_helpers.go — buildPushJob + buildCloneJob (consumed by ProjectReconciler in plan 03-08)
  - Exit-code map per 03-RESEARCH Q5 RESOLVED (0/1/2/10/11/12/13) wired to push-result envelope at /workspace/envelopes/push/{project-uid}.json
  - D-B6 never-targets-main/master branch guard at the push Job binary level (policy enforcement point)
  - W10 unified-diff via newCommit.Patch(oldCommit).String() so gitleaks +-line rules match
  - W11 fixed TIDE-bot author signature ({Name:"tide-bot", Email:"tide-bot@tideproject.k8s"}) for every boundary commit
affects: [03-08 ProjectReconciler (consumes buildPushJob/buildCloneJob), 03-09 Helm RBAC (creates tide-push SA), 03-10 chaos-resume]

# Tech tracking
tech-stack:
  added:
    - "go-git/v5 v5.19.0 PlainOpen/CommitObject/Patch path in cmd/tide-push (existing dep, new usage site)"
    - "gitleaks/v8 v8.30.1 ScanDiff in cmd/tide-push (existing dep, new usage site)"
    - "k8s.io/utils/ptr in push_helpers.go (mirrors podjob/jobspec.go convention)"
  patterns:
    - "Per-Project K8s API serialization via deterministic Job name `tide-push-{project-uid}` (D-B5)"
    - "Trust-boundary isolation: GIT_PAT bound on push Job pod ONLY via envFrom; controller pod never sees the Secret bytes (D-B1)"
    - "Pure builder helpers (no client.Create) returned from internal/controller/*_helpers.go; caller reconciler owns the K8s API write"
    - "Workspace-relative artifact paths copied into worktree before pkggit.AddPath staging — keeps PVC layout (D-G2) decoupled from worktree layout"
    - "Defense-in-depth PAT redaction (redactPAT helper) on every error-message log site even though pkg/git doesn't currently inline PAT in errors"

key-files:
  created:
    - "cmd/tide-push/main.go (498 lines) — push Job binary; clone mode + push mode + 7-exit-code map + W10 Patch().String() + W11 commit"
    - "cmd/tide-push/main_test.go (544 lines) — 7 tests covering clean push, gitleaks block, lease, main-guard, clone, missing-creds, W11 message"
    - "images/tide-push/Dockerfile (42 lines) — distroless/static:nonroot multi-stage build"
    - "internal/controller/push_helpers.go (348 lines) — buildPushJob + buildCloneJob + PushOptions + CloneOptions"
    - "internal/controller/push_helpers_test.go (246 lines) — 8 pure-Go unit tests for the builders"
  modified: []

key-decisions:
  - "redactPAT helper applied at every error-log site — defensive even though pkg/git's current wrappers don't inline PAT. Future go-git versions could change this; the redact gate is cheap."
  - "copyIntoWorktree via io.Copy (not os.Link) — source artifacts and worktree may live on different filesystems within the PVC SubPath mount, so hardlink can fail mid-deploy"
  - "Owner ref helper called with full *batchv1.Job + *Project (not &job.ObjectMeta + &project.ObjectMeta) — controller-runtime needs runtime.Object to resolve parent GVK via scheme"
  - "writePushEnvelope is best-effort on write failures (logs but doesn't change exit) — the calling reconciler observes the Job's status anyway; envelope-write failure shouldn't override the underlying success/failure exit code"
  - "Owner-ref error from EnsureOwnerRef discarded with `_ =` in the pure builder — the calling reconciler will surface any K8s API error at Create time; pure builders don't propagate scheme errors out of their *batchv1.Job return"
  - "Branch guard refuses both `main` and `master` (D-B6 reads `never targets main` strictly, but defensive widening is cheap and protects users whose default branch is still master). Tests use a per-run branch `tide/run-<prefix>-<unix>` to exercise the happy path."

patterns-established:
  - "Push-result envelope shape: {apiVersion, kind, projectUID, branch, headSHA, exitCode, reason} — mirrors Phase 2 D-A2 envelope-on-PVC contract; consumed by ProjectReconciler in plan 03-08 to map `reason` → Status.phase"
  - "CLI flag set parser pattern with cfg struct passed by value into run() — tests drive run(ctx, cfg, io.Discard, &stderrBuf) without os.Args (mirrors cmd/stub-subagent/main.go testability shape)"
  - "Per-Project Job serialization via K8s API AlreadyExists (no in-controller mutex) — deterministic name is the serialization key (D-B5)"

requirements-completed:
  - ART-04
  - ART-06
  - ART-07
  - AUTH-01

# Metrics
duration: ~30min
completed: 2026-05-16
---

# Phase 3 Plan 06: Push Job Tooling Summary

**Wave 3 Stream B binds Wave 2's pkg/git + internal/gitleaks libraries into a deterministic-named, secret-isolated K8s Job that emits a structured push-result envelope on the per-Project PVC.**

## Performance

- **Duration:** ~30 min
- **Started:** 2026-05-16T00:00:00Z (worktree spawn)
- **Completed:** 2026-05-16T00:33:08Z
- **Tasks:** 2 of 2 (both atomically committed)
- **Files created:** 5 (4 Go + 1 Dockerfile); 0 modified
- **Tests:** 15 new (7 in cmd/tide-push + 8 in internal/controller); all PASS

## Accomplishments

- **cmd/tide-push static binary** with two operating modes (clone + push), 7 exit codes mapped to structured `reason` strings (per 03-RESEARCH Q5 RESOLVED), W10-pinned unified-diff via `newCommit.Patch(oldCommit).String()` so gitleaks's `+`-line rules match Anthropic API keys / AWS access tokens / GitHub PATs, and W11-fixed TIDE-bot author signature for the boundary commit.
- **PAT trust-boundary isolation**: GIT_PAT comes from `os.Getenv` on the push Job pod ONLY (Helm chart in plan 03-09 wires `envFrom: SecretRef` to `project.Spec.Git.CredsSecretRef`). The controller pod never binds the Secret. Every error-log site redacts the PAT via `redactPAT(err.Error(), pat)` as defense-in-depth even though pkg/git's current wrappers don't inline the PAT.
- **D-B6 never-targets-main enforcement** at the push Job binary level (the policy enforcement point per Phase 3 PATTERNS.md). Guard fires before any I/O, emits `envelope.reason=invalid-branch` + exit 2. Test 4 + Test 6 (refuses main / refuses missing-creds) confirm both invariant gates.
- **buildPushJob + buildCloneJob pure builders** in `internal/controller/push_helpers.go` — no `client.Create` calls, no envtest required. Deterministic Job names `tide-push-{project-uid}` (D-B5 K8s-API-serialization key) and `tide-clone-{project-uid}` (one-time per Project). Owner refs via `internal/owner.EnsureOwnerRef` (Pitfall 23 same-namespace + BlockOwnerDeletion=true). Consumed by ProjectReconciler in plan 03-08.
- **distroless/static:nonroot Dockerfile** ships the single static binary as USER 1000 — pure-Go means no `/bin/git` shell-out, no CA bundle (go-git uses Go's `crypto/tls`). Mirrors `images/stub-subagent/Dockerfile` pattern exactly.

## Task Commits

Each task was committed atomically:

1. **Task 1: cmd/tide-push binary + Dockerfile** — `bf81148` (feat)
   - TDD RED→GREEN: 7 failing tests → write main.go → 7/7 PASS
   - Includes Dockerfile (no separate REFACTOR commit needed)
2. **Task 2: internal/controller/push_helpers.go** — `0eb7f4a` (feat)
   - TDD RED→GREEN: 8 failing tests → write push_helpers.go → 8/8 PASS

## Files Created/Modified

- `cmd/tide-push/main.go` — push Job entrypoint; `pushConfig` struct, `run(ctx, cfg, stdout, stderr)` testable entry point, `runClone` + `runPush` dispatches, `computeUnifiedDiff` via `newCommit.Patch(oldCommit).String()` (W10), `tideBotSignature()` (W11), `classifyPushError` mapping go-git errors to the Q5 exit codes, `writePushEnvelope` to `/workspace/envelopes/push/{project-uid}.json`, `redactPAT` defense-in-depth.
- `cmd/tide-push/main_test.go` — `seedBareRepo` helper (mirrors `pkg/git`'s test pattern), `setupWorkspace` stages `<ws>/repo.git` + `<ws>/worktrees/run-<branch>` + remote pointing back at the test bare repo, `perRunBranch` generates `tide/run-<prefix>-<unix>` names. 7 tests covering all behaviors.
- `images/tide-push/Dockerfile` — `golang:1.26-alpine` builder → `gcr.io/distroless/static:nonroot` runtime, USER 1000, single binary, no shell.
- `internal/controller/push_helpers.go` — `PushOptions` + `CloneOptions` structs, `buildPushJob` (constructs `*batchv1.Job` with deterministic name, ServiceAccountName "tide-push", envFrom credsSecretRef, PVC SubPath isolation, all args), `buildCloneJob` (similar shape, --mode=clone --repo-url args), `joinCSV` for --artifact-paths.
- `internal/controller/push_helpers_test.go` — 8 pure-Go unit tests: Job name (D-B5), SA name (tide-push), envFrom credsSecretRef, PVC volume + SubPath, push-mode args, OwnerReference (Controller=true + BlockOwnerDeletion=true), clone Job name, clone-mode args.

## Deviations from Plan

None — plan executed as written.

Minor framing notes (not deviations, just facts):

- **The plan's acceptance grep `fmt\.Sprintf\("tide-push-%s"` returns 2 instead of 1** because the Go doc comment quotes the format string verbatim. The single actual `fmt.Sprintf` call in code (the `ObjectMeta.Name` assignment in buildPushJob) is what satisfies the semantic check. Same for `tide-clone-%s`.
- **`go test ./internal/controller/...` without a `-run` filter fails** with envtest BeforeSuite missing `/usr/local/kubebuilder/bin/etcd` — this is pre-existing developer-laptop env gap from Phase 02.1 (not introduced by this plan). The plan's prescribed verification command `go test ./internal/controller/... -run "TestBuildPushJob|TestBuildCloneJob" -count=1 -timeout 30s` PASSES cleanly because the filter excludes the Ginkgo TestControllers entry point. Both Task 1 and Task 2 tests are pure unit-level (no envtest required).

## Auth Gates

None encountered. All tests use `file://` URLs against local bare repos (the same pattern `pkg/git`'s own tests use); no network, no real PAT, no real K8s API.

## Verification Results

- `go test ./cmd/tide-push/... ./internal/controller/... -count=1 -timeout 60s -run "TestRun|TestBuildPushJob|TestBuildCloneJob"` → `ok` for both packages (15 tests PASS).
- `go test ./cmd/tide-push/... -count=1 -timeout 60s` → `ok` (7/7 PASS).
- `go test ./internal/controller/... -run "TestBuildPushJob|TestBuildCloneJob" -count=1 -timeout 30s` → `ok` (8/8 PASS).
- `go vet ./...` → exit 0 (clean across the whole module).
- `go build ./...` → exit 0 (clean across the whole module).
- Acceptance greps (Task 1): all 13 pass.
  - `func main()` × 1; `--mode` flag × 1; `pkggit.(Clone|Push|Commit|Fetch)` × 6; `gitleaks.ScanDiff` × 2; `cfg.Branch == "main"` × 1; `newCommit.Patch` × 3; `commit-message|CommitMessage` × 8; `artifact-paths|ArtifactPaths` × 6; `pkggit.AddPath|pkggit.Commit` × 4; `os.Getenv("GIT_PAT")` × 2; Dockerfile distroless line × 1; Dockerfile COPY × 1; Dockerfile USER 1000 × 1.
- Acceptance greps (Task 2): all 8 pass.
  - `func buildPushJob(project *...Project` × 1; `func buildCloneJob` × 1; `tide-push-%s` × 2 (doc + code); `tide-clone-%s` × 2 (doc + code); SA "tide-push" × 3; `EnvFrom` × 3; `CredsSecretRef` × 4; `RepoURL` × 1.
- Docker build deferred to plan 03-09 (Helm chart) per plan's `<verification>` block.

## Threat Mitigation Status

| Threat ID | Status | Notes |
|-----------|--------|-------|
| T-301 (PAT exfiltration via controller pod log) | ✓ Mitigated | buildPushJob's `EnvFrom: SecretRef` is on the push Job container ONLY. Controller pod never binds the Secret. Test fixtures assert `bytes.Contains(stderr, []byte(testPAT))` is false. |
| T-302 (push race corrupting branch) | ✓ Mitigated | Job name `tide-push-{project.UID}` is the same string for every push attempt — second push during first hits K8s API AlreadyExists; calling reconciler requeues. Test 1 of Task 2 asserts the name format. |
| T-303 (stale --force-with-lease overwrites human commits) | ✓ Mitigated | First push (--last-pushed-sha=""): pkg/git.Push omits the lease. Subsequent pushes (--last-pushed-sha=<sha>): ForceWithLease against the recorded SHA. Lease fail surfaces as classifyPushError → exit 11 → envelope.reason="lease-rejected". Test 3 of Task 1 exercises lease-honored path; pkg/git's own tests (existing) exercise lease-rejection. |
| T-304 (gitleaks rule overrides tampered) | ✓ Mitigated | buildPushJob's --leaks-config arg comes from opts.LeaksConfigMap (set by reconciler from project.Spec.Git.LeaksConfigRef). RBAC scope of tide-push SA is namespace-scoped via Helm 03-09 so cross-namespace ConfigMap refs are rejected. |
| T-301 (logging side) | ✓ Mitigated | Every `fmt.Fprintf(stderr, ...)` that includes `err.Error()` runs through `redactPAT(msg, pat)` first. Test 1 + Test 2 + Test 3 + Test 7 capture stderr and assert `bytes.Contains(stderr, []byte(pat))` is false. |

## Self-Check: PASSED

Files exist (verified):
- ✓ `cmd/tide-push/main.go` (498 lines)
- ✓ `cmd/tide-push/main_test.go` (544 lines)
- ✓ `images/tide-push/Dockerfile` (42 lines)
- ✓ `internal/controller/push_helpers.go` (348 lines)
- ✓ `internal/controller/push_helpers_test.go` (246 lines)

Commits exist (verified):
- ✓ `bf81148` Task 1 (feat(03-06): cmd/tide-push binary + Dockerfile)
- ✓ `0eb7f4a` Task 2 (feat(03-06): internal/controller/push_helpers.go — buildPushJob + buildCloneJob)
