---
status: resolved
trigger: "v1.0.7-rc.1 release gate: dry-run workflow failed — small-project never reaches status.phase=Complete within 10m on the DinD cluster; final v1.0.7 tag is HELD on this"
created: 2026-07-15T13:05:00Z
updated: 2026-07-15T17:00:00Z
---

## Symptoms

DATA_START
- expected: `make dry-run-v1` (DinD external-operator simulation, $0 stub-subagent path) drives the small-sample Project to `status.phase=Complete` within 10m — green at every release since v1.0.0; last green on v1.0.6 (2026-06-29).
- actual: GH run 29414804326 (tag v1.0.7-rc.1, 2026-07-15): install fully clean — cert-manager rolled out, tide-crds + tide charts deployed (chart 1.0.7), `deployment/tide-controller-manager condition met`, fixture applied (ns tide-sample-small, PVC tide-projects, SAs tide-subagent/tide-reporter, pvc-warmup pod, project small-project, secret tide-signing-key) — then `error: timed out waiting for the condition on projects/small-project` after exactly the 10m `kubectl wait --for=jsonpath='{.status.phase}'=Complete` budget (dry-run-v1.sh:214). Exit 2.
- errors: none besides the wait timeout; the transcript (evidence artifact `dry-run-evidence-v1.0.7-rc.1`, saved locally at ~/.claude/jobs/8b19abd1/tmp/dryrun-ev/tide-dry-run-1784118000/transcript.log) is console-only — NO controller logs, NO kubectl describe, NO events. The script captures no failure diagnostics (secondary finding worth fixing).
- timeline: first rc gate since v1.0.6. Everything in v1.0.7 (193 commits, Phases 34-41) is in the suspect window. Layer A envtest 56/56 and Layer B kind 26/26 are GREEN on this exact tree (local run 2026-07-15, MAKE_EXIT=0) — the failure is exclusive to the $0 chart-defaults dry-run surface.
- repro: `make dry-run-v1` locally (DinD in ubuntu:24.04, creates its own kind cluster inside the container). OPERATIONAL CONSTRAINTS: stop the `tide-dogfood-control-plane` docker container first (8 GiB VM, two clusters OOM — exit 137 history) and restart it after; expect ~15-25 min wall; script tees to /tmp/tide-dry-run-<ts>/transcript.log.
DATA_END

## Evidence

- timestamp: 2026-07-15T12:34Z
  checked: "GH run 29414804326 dry-run job log + downloaded transcript artifact"
  found: "Install path 100% clean; stall is strictly post-apply. Project object accepted by API server (v1alpha3 apply succeeded), controller Deployment Available. No further Project progress observable from outside (transcript has no status output)."
  implication: "Not an install/CRD/chart-render failure. The cascade stalls somewhere between Project admission and Complete."

- timestamp: 2026-07-15T12:50Z
  checked: "git diff v1.0.6..v1.0.7 on the $0 surface (examples/projects/small/, images/stub-subagent/, hack/scripts/dry-run-v1.sh, chart)"
  found: "(1) stub-subagent reworked (PR #3 / Phase 34): honors real executor git contract WHEN envelope Branch set. (2) sample bumped v1alpha2→v1alpha3 + schemaRevision (40-03). (3) dry-run-v1.sh unchanged. (4) Phase 34 branch stamping + integration gate; Phase 41 REFAC-11 --workspaces-pvc-name."
  implication: "Initial suspects A (git-path routing), B (boundary/Complete gating), C (PVC-name plumbing)."

- timestamp: 2026-07-15T13:40Z
  checked: "cmd/stub-subagent/main.go ensureExecutorWorktree + repoWaitTimeout"
  found: "Stub repo-wait is BOUNDED (30s) with a non-git fallback (return 0, proceed without git). Cannot account for a 10m stall."
  implication: "Hypothesis A (stub repo-wait hang) refuted for the CURRENT stub. Note: reconcilePhase3Lifecycle Step 1 stamps Status.Git.BranchName unconditionally, but the stub tolerates it."

- timestamp: 2026-07-15T13:45Z
  checked: "internal/controller: clone dispatch (project_controller.go:627), wave-integration gate (plan_controller.go:946), boundary push (boundary_push.go:97,230; project_controller.go:806)"
  found: "EVERY git-path entry point is guarded on `project.Spec.Git != nil && Spec.Git.RepoURL != \"\"`. The small sample has no spec.git block (only targetRepo) → clone Job never dispatches, wave-integration gate falls through, boundary push no-ops. Explicit comment at plan_controller.go:940-948: no-git projects must NOT block wave dispatch."
  implication: "Hypothesis B (Phase 34 integration/boundary gates hold Complete for no-git projects) refuted statically."

- timestamp: 2026-07-15T13:50Z
  checked: "PVC name seam: cmd/manager/main.go:219 (TIDE_WORKSPACES_PVC_NAME default 'tide-projects'), charts/tide/values.yaml:435 (name: tide-projects), sample fixture PVC (tide-projects)"
  found: "All three ends agree on 'tide-projects' by default. Chart passes no override on the chart-defaults path."
  implication: "Hypothesis C (REFAC-11 PVC-name mismatch) refuted statically."

- timestamp: 2026-07-15T13:55Z
  checked: "Which stub image the dry-run actually dispatches: resolution chain (internal/dispatch/podjob/backend.go:281-287 — Levels.Task.Image → Spec.Subagent.Image → chart default) + examples/projects/small/project.yaml:181"
  found: "Spec.Subagent.Image IS live (v1.0.1 fix landed). Sample pins `ghcr.io/jsquirrelz/tide-stub-subagent:1.0.0` — the OLD published image, pulled from ghcr. Never bumped since commit 7d76316 (Phase 5). kind Layer B instead uses `--set subagent.defaults.image=ghcr.io/jsquirrelz/tide-stub-subagent:test` (suite_test.go:510) — the FRESHLY BUILT stub; bare-project.yaml sets no spec.subagent.image."
  implication: "THE DIFFERENTIAL: dry-run = new manager (1.0.7, kind-loaded local build) + OLD stub (1.0.0 from ghcr). kind suite = new manager + NEW stub. Any 34-41 manager↔subagent contract change breaks only the dry-run surface."

- timestamp: 2026-07-15T14:00Z
  checked: "git diff v1.0.6..HEAD -- pkg/dispatch/envelope.go + git log -S 'dispatch.tideproject.k8s/v1alpha1'"
  found: "Commit dfb8d58 (feat(40-02): decouple envelope contract group from CRD group, D-08) changed APIVersionV1Alpha1 from 'tideproject.k8s/v1alpha1' → 'dispatch.tideproject.k8s/v1alpha1'. ValidateAPIVersionKind (envelope.go:446) is STRICT equality — no dual-accept ('Consumers MUST reject envelopes whose apiVersion field does not match'). Verified stub:1.0.0 and 1.0.6 trees both compile the OLD constant."
  implication: "ROOT CAUSE. Manager 1.0.7 writes EnvelopeIn with the new group; stub:1.0.0's loadEnvelope→ValidateAPIVersionKind rejects it → exit 2 on EVERY dispatch → project-level planner Job fails through its backoff → no Milestone children ever materialize → Project never reaches Complete → 10m wait times out. Symmetric break on the read side too (reporter 1.0.7 would reject the old stub's out.json apiVersion)."

- timestamp: 2026-07-15T14:03Z
  checked: "Blast radius of the stale-pin class: grep tide-*-subagent: in examples/ docs/; chart default subagent.defaults.image; pod pull policy"
  found: "(1) medium + large samples pin claude-subagent:1.0.0 (same class — that image also compiles the old envelope group). (2) small/README.md documents pulling stub:1.0.0. (3) Chart default subagent.defaults.image is the CLAUDE image (Phase 13 D-01: stub never injected implicitly) — so the sample MUST keep an explicit stub pin for the $0 guarantee; removing the pin is not an option. (4) podjob backend sets no imagePullPolicy → IfNotPresent for versioned tags → a kind-loaded local :1.0.7 image is used at rc time (load-images-if-needed.sh already loads all 6 images at CHART_APP_VERSION). (5) dogfood examples are historical run artifacts, out of scope. (6) No existing appVersion-drift guard test."
  implication: "Fix = bump sample pins to track appVersion (1.0.7) + add a drift-guard contract test in test/integration/kind (plain go-test, runs under make test-int/CI) so the v1.0.1-class 'bump appVersion as step one' release step mechanically catches the samples too."

- timestamp: 2026-07-15T16:25Z
  checked: "LIVE REPRODUCTION — local make dry-run-v1 run 1 (wrapper /tmp/tide-dryrun-verify/run.sh; ran BEFORE the fix was committed — the DinD script clones the host repo at HEAD, so the uncommitted fix did not ride along, making run 1 an exact rc.1 repro with diagnostics)"
  found: "Project stuck Running 9m43s, ZERO child CRs. Planner Job tide-project-<uid>-1 Failed 0/1 running image ghcr.io/jsquirrelz/tide-stub-subagent:1.0.0. Pod log verbatim: 'stub-subagent: envelope load: validate /workspace/envelopes/<uid>/in.json: envelope: unknown apiVersion \"dispatch.tideproject.k8s/v1alpha1\" (expected tideproject.k8s/v1alpha1)'. Termination message: exitCode 2, childCount 0. kubectl wait timed out at 10m, MAKE_EXIT=2 — byte-for-byte the rc.1 failure shape."
  implication: "Root cause CONFIRMED at runtime, not just statically. Also confirms the repro protocol: dry-run verification requires the fix to be COMMITTED (script pins clone to host HEAD)."

## Eliminated

- hypothesis: "Install/chart/CRD regression (v1alpha3 apply rejected, image tags wrong, manager crash)"
  evidence: "Transcript: charts deployed, CRDs served v1alpha3, project applied successfully, controller Deployment condition met"
  timestamp: 2026-07-15T12:34Z

- hypothesis: "(A) small-project routed down git path: clone Job against non-routable .example remote, or stub blocking on repo-wait"
  evidence: "Clone dispatch gated on Spec.Git (project_controller.go:627) — sample has none. Stub repo-wait bounded at 30s with non-git fallback (main.go:198). targetRepo alone never triggers clone."
  timestamp: 2026-07-15T13:45Z

- hypothesis: "(B) Phase 34 boundary-push/integration-completeness gate holds Complete for a no-git project"
  evidence: "wave gate (plan_controller.go:946), boundary push (boundary_push.go:97,230), reconcileBoundaryPush (project_controller.go:806) all early-exit on Spec.Git==nil with explicit no-git-must-not-block comments"
  timestamp: 2026-07-15T13:45Z

- hypothesis: "(C) REFAC-11 --workspaces-pvc-name plumbing mismatch on chart-default path"
  evidence: "Manager default TIDE_WORKSPACES_PVC_NAME='tide-projects' (main.go:219) == chart default (values.yaml:435) == fixture PVC name"
  timestamp: 2026-07-15T13:50Z

## Current Focus

hypothesis: "RESOLVED — human-verify checkpoint confirmed 2026-07-15 (session manager re-verified commit 4712437 on main, green transcript's condition-met on the exact rc.1 wait, byte-identical repro on pre-fix tree, guard test present, environment restored). Re-tagging v1.0.7-rc.2 is the orchestrator's job — NOT done here."
test: "n/a — session archived"
expecting: "n/a"
next_action: "none — archived to resolved/; knowledge-base entry written"

reasoning_checkpoint:
  hypothesis: "Manager 1.0.7 writes EnvelopeIn with apiVersion 'dispatch.tideproject.k8s/v1alpha1' (changed in dfb8d58 / 40-02 D-08); the sample-pinned stub:1.0.0 validates against its compiled-in 'tideproject.k8s/v1alpha1' via strict-equality ValidateAPIVersionKind and exits 2 on every dispatch, so the project-level planner Job fails forever and the Project never leaves the pre-Milestone state."
  confirming_evidence:
    - "git show v1.0.0:pkg/dispatch/envelope.go → constant 'tideproject.k8s/v1alpha1'; HEAD → 'dispatch.tideproject.k8s/v1alpha1'; ValidateAPIVersionKind is strict equality with MUST-reject contract"
    - "spec.subagent.image resolution chain is live (podjob/backend.go:281-287) and the sample pins stub:1.0.0, unbumped since Phase 5 (git log: only 7d76316 touched the tag)"
    - "Perfect differential match: kind Layer B green because suite_test.go:510 injects the tree-built :test stub; dry-run red because it dispatches the published :1.0.0 stub; v1.0.6 dry-run green because manager and stub then agreed on the old group"
  falsification_test: "If a local make dry-run-v1 with ONLY the sample tag bumped to 1.0.7 (image kind-loaded by load-images-if-needed.sh) still times out, the hypothesis is wrong or incomplete"
  fix_rationale: "The root defect is a version-skew landmine: a hardcoded published-image tag in the sample that must-match the manager's envelope contract but has no mechanical coupling to it. Bumping to 1.0.7 restores the matched pair on every surface (rc dry-run uses the kind-loaded local build via IfNotPresent; released operators pull the published 1.0.7); the drift-guard test makes the pin fail CI the moment appVersion bumps without it — same fix shape as the v1.0.1 'appVersion bump is step one' lesson. Dual-accepting the old group in ValidateAPIVersionKind would contradict Phase 40's deliberate clean-break posture and leave the rot mechanism in place for the NEXT contract change."
  blind_spots: "Static analysis cannot rule out a SECOND, downstream defect on the $0 path that the envelope rejection currently masks (e.g. a later-level gate specific to chart-defaults). The bare-project kind fixture is the closest analog and is green, but its helm args differ. The local make dry-run-v1 verification run is the falsifier for any residual defect."

## Resolution

root_cause: "Plan 40-02 (D-08, commit dfb8d58) changed the envelope contract apiVersion group from 'tideproject.k8s/v1alpha1' to 'dispatch.tideproject.k8s/v1alpha1' with strict-equality validation and no dual-accept. examples/projects/small/project.yaml pins spec.subagent.image to the published ghcr.io/jsquirrelz/tide-stub-subagent:1.0.0 (never bumped since Phase 5), and spec.subagent.image is a live override — so the v1.0.7-rc.1 dry-run dispatched a stub compiled with the OLD group against a manager writing the NEW group. Every subagent dispatch exits 2 at envelope validation; the project-level planner Job fails through its backoff; no child CRs are ever created; the Project never reaches Complete and the 10m kubectl wait times out. Kind Layer B never sees this because the suite injects the tree-built stub via subagent.defaults.image=:test."
fix: "Matched-pair restore + rot guard: (1) bumped examples/projects/small/project.yaml stub-subagent pin 1.0.0→1.0.7 with a comment documenting the must-equal-appVersion invariant; (2) bumped the same-class claude-subagent:1.0.0 pins in examples/projects/medium/ and large/ to 1.0.7; (3) updated examples/projects/small/README.md pull/load instructions 1.0.0→1.0.7; (4) added test/integration/kind/examples_image_pin_test.go (TestExamplesSubagentImagePinsMatchChartAppVersion) — plain go-test in the make test-int/CI package that scans examples/projects/ (dogfood excluded — historical artifacts) for versioned tide-{stub,claude}-subagent refs in YAML+MD and asserts every tag equals charts/tide/Chart.yaml appVersion. Verified RED against the pre-fix tree (7 drift errors on the 1.0.0 pins via git stash) and GREEN post-fix. Deliberately did NOT dual-accept the old envelope group in ValidateAPIVersionKind: Phase 40's clean-break posture is intentional, and dual-accept would leave the rot mechanism armed for the next contract change."
verification: "GREEN. (1) Run 1 (pre-commit tree → exact rc.1 repro): planner Job on stub:1.0.0 Failed with pod log 'unknown apiVersion \"dispatch.tideproject.k8s/v1alpha1\" (expected tideproject.k8s/v1alpha1)', zero child CRs, 10m wait timeout, MAKE_EXIT=2 — reproduces AND explains rc.1. (2) Run 2 (fix committed as 4712437; report tideVersion v1.0.7-rc.1-2-g4712437-dirty proves the clone carried it): planner Job on stub-subagent:1.0.7, stub-milestone-1 materialized within ~45s of apply, 'project.tideproject.k8s/small-project condition met', 'PASS: dry-run completed in 800s (under 1800s cap)', DRY_RUN_MAKE_EXIT=0. Transcripts: /tmp/tide-dryrun-verify/run1-repro/ (repro) and /tmp/tide-dryrun-verify/run/ (green). (3) Guard test RED on pre-fix tree (7 drift errors), GREEN post-fix; gofmt/vet/golangci-lint clean. (4) Environment restored: tide-dry-run cluster removed, tide-dogfood-control-plane running. Commit 4712437 on main (code files only; this session file committed at archive)."
files_changed:
  - examples/projects/small/project.yaml
  - examples/projects/medium/project.yaml
  - examples/projects/large/project.yaml
  - examples/projects/small/README.md
  - test/integration/kind/examples_image_pin_test.go
deferred_followups:
  - "Stale `chartVersions: 1.0.1` in dry-run-report.json — route through /gsd:quick (not fixed in this session)"
  - "dry-run-v1.sh captures zero failure diagnostics at wait timeout (no controller logs / describe / events on failure) — route through /gsd:quick (not fixed in this session)"
