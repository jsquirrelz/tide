---
phase: 04
plan: 08
subsystem: cli
tags: [cobra, cli-write-back, annotation-handshake, pods-log-stream, foreground-cascade-delete, d-g3, d-g4, d-c3, d-c5, pitfall-25]
dependency_graph:
  requires:
    - "04-04 — internal/gates package (AnnotationApprovePrefix, AnnotationApproveWavePrefix, AnnotationReject, ConsumeReject) — annotation key constants + Consume helpers re-used by tide approve / reject / resume"
    - "04-07 — cmd/tide cobra skeleton (rootCmd, registerSubcommands, K8sClient, RESTConfig, resolveNamespace, scheme) + the five plan-04-08 stubs whose error wording was honest about implementation state"
    - "04-05 — TaskReconciler + up-stack reconciler gate-policy hook (read-side consumer of the annotations this plan writes); validates the contract end-to-end"
  provides:
    - "cmd/tide approve <project> [--wave plan/N] — annotation writer for the canonical approve handshake (D-G3)"
    - "cmd/tide reject <project> [--reason ...] — annotation writer for the reject halt (D-G4)"
    - "cmd/tide resume <project> — clears the reject annotation via gates.ConsumeReject + client.Patch"
    - "cmd/tide cancel <project> --force [--dry-run] — destructive cascade delete with Foreground propagation; --dry-run enumerates owner-ref'd children for operator review"
    - "cmd/tide tail <task> [--container -c] [--tail] [--timestamps -t] — pod-log streaming via clientset.CoreV1().Pods(ns).GetLogs(name, opts).Stream(ctx) with ctx-aware cancellation (Pitfall 25)"
    - "tailPodPicker + tailStreamer function-var seams — tests inject deterministic resolvers without a live apiserver"
  affects:
    - "04-09 — release pipeline ships the now-complete cmd/tide binary; no more stub verbs in `tide --help`"
    - "04-11 — dashboard's pod-log SSE handler mirrors the same defaultTailStreamer + Pitfall-25 ctx-cancel pattern (proves the streaming approach before the dashboard consumes it)"
    - "04-14 — kind harness exercises the tide tail / approve / reject path against a real apiserver (no stub injection)"
tech_stack:
  added: []
  patterns:
    - "Annotation handshake — client.MergeFrom(obj.DeepCopy()) + client.Patch(ctx, obj, patch). One-shot semantics that mirror the reconciler-side reads in plan 04-04/04-05. No full Updates, no JSON-merge patches, no race against ConsumeApprove/ConsumeReject."
    - "Level discovery — `tide approve` walks Milestone → Phase → Plan → Task lists filtered by tideproject.k8s/project label; first child whose Status.Phase=AwaitingApproval receives the annotation. Mirrors how patchMilestoneAwaitingApproval / etc. set the AwaitingApproval state in plan 04-05."
    - "Foreground cascade delete — client.Delete(ctx, &project, client.PropagationPolicy(metav1.DeletePropagationForeground)). K8s GC cascades to children via owner refs; PVC cleanup runs via the existing CTRL-05 finalizer. --dry-run pattern enumerates owner-ref'd children without performing the delete."
    - "pods/log streaming — clientset.CoreV1().Pods(ns).GetLogs(name, &corev1.PodLogOptions{Follow:true, Container:..., TailLines:ptr.To(N), Timestamps:bool}).Stream(ctx). io.Copy until ctx.Done() or EOF."
    - "Pitfall 25 mitigation — goroutine watches ctx.Done() and closes the stream so io.Copy returns within ~1s of Ctrl-C. Defer stream.Close() handles the EOF path."
    - "Function-var test seams — tailPodPicker and tailStreamer as `var = defaultImpl`. Tests swap in deterministic resolvers without touching the cobra plumbing or requiring a live apiserver. Restored in defer."
    - "Container-resolution heuristic — pickContainer: explicit --container wins; otherwise first non-credproxy/non-init-* container (subagent main by Phase-1/2 convention)."
key_files:
  created:
    - cmd/tide/approve_test.go
    - cmd/tide/reject_test.go
    - cmd/tide/resume_test.go
    - cmd/tide/cancel_test.go
    - cmd/tide/tail_test.go
  modified:
    - cmd/tide/approve.go
    - cmd/tide/reject.go
    - cmd/tide/resume.go
    - cmd/tide/cancel.go
    - cmd/tide/tail.go
    - cmd/tide/subcommands.go
    - cmd/tide/cmd_test.go
decisions:
  - "approve discovers the AwaitingApproval level from CHILD CRDs (Milestone/Phase/Plan/Task lists, filtered by tideproject.k8s/project label), NOT from Project.Status.Conditions. Plan body suggested either approach; the child-list path is more accurate because plan 04-05's patchMilestoneAwaitingApproval sets Status.Phase=\"AwaitingApproval\" on the child itself — the Project's WaveOrLevelPaused condition is bubbled up but the canonical truth lives on the child. Iteration order Milestone → Phase → Plan → Task matches dependency-order (a Milestone awaiting approval gates everything below)."
  - "--wave format regex is ^[a-z0-9.-]+/\\d+$ — the leading section permits K8s DNS-label characters (lowercase ASCII + digits + . + -) per K8s name conventions. Per the plan's Test 2 wording (`--wave my-plan/3` valid, bad format rejected). The trailing /N uses \\d+ rather than [0-9]+ because Go's regexp package accepts both with identical semantics — \\d is the standard Perl-ish form."
  - "rejectRun accepts empty reason and applies the default `\"rejected by operator\"` internally (not just at the cobra flag default). Reasoning: gates.CheckRejected(obj) returns false for empty values (T-04-G4 mitigation — empty reject is treated as no-rejection so clear-via-empty kubectl annotate doesn't accidentally halt the run). The default MUST be non-empty, and the testable seam must enforce that — if a future caller bypassed the cobra flag and called rejectRun with \"\", we'd silently write a no-op annotation. Defense-in-depth: both layers default to \"rejected by operator\"."
  - "tide cancel --dry-run enumerates children by the tideproject.k8s/project LABEL, not by metav1.IsControlledBy(owner-ref). Reasoning: the canonical label vocabulary is uniformly stamped (per internal/controller/plan_controller.go:513-523 + the rest of the reconciler set), while owner refs require a per-Kind GetOwnerReferences walk. Label filtering is one line per Kind. The actual Delete uses PropagationPolicyForeground which IS owner-ref driven — those are independent concerns."
  - "tail uses two function vars (tailPodPicker + tailStreamer) for test injection instead of an interface-typed seam. Reasoning: only ONE production implementation exists per var; the test override is short-lived (deferred restore). An interface (type PodPicker; type LogStreamer) would add ceremony without buying decoupling. Function vars are the Go-idiomatic seam for this exact case (cf. http.DefaultTransport, time.Now in tests)."
  - "tailPodPicker matches Pods by the canonical tideproject.k8s/task-uid label (per task_controller.go:673), NOT by the deterministic Job name (podjob.JobName(task.UID, attempt)). Reasoning: the Job name is a string the reconciler computes; the label is set by the reconciler at Job-create time and propagates to Pods via the Job's PodTemplateSpec. The label is the documented contract (multiple controllers/tests already filter on it); the Job name is an implementation detail."
  - "Pending pods are allowed as tail targets (not just Running). Reasoning: Follow=true streams begin emitting once the container becomes ready — kubectl logs -f against a Pending pod waits for the container, then streams. Matching that UX means an operator can `tide tail my-task` as soon as the Task transitions to Running, without timing the call against pod-status updates."
  - "EOF from a pod-terminate mid-stream prints `(stream closed by pod termination)` to stderr and exits 0. Reasoning: matches `kubectl logs -f` UX — terminate-mid-stream is the normal end-of-stream, not an error. The stderr line gives the operator a signal that the EOF was pod-driven rather than apiserver-network-error. Scripts that grep stdout for log lines aren't disrupted by the stderr marker."
  - "TestStubVerbsReturnNotImplemented (from 04-07) renamed to TestStubVerbsRequireArgs. The five plan-04-08 verbs are no longer stubs; the test now verifies the cobra Args: ExactArgs(1) guard rejects a no-args invocation. Maintains the structural assertion (each verb errors on misuse) without lying about implementation state."
metrics:
  duration_minutes: 35
  completed_date: 2026-05-19
  tasks_completed: 3
  files_created: 5
  files_modified: 7
  commits: 6
requirements_completed: [GATE-03, CLI-04]
---

# Phase 4 Plan 08: tide CLI annotation-writer + tail verbs Summary

Land the five plan-04-08 write-back verbs that close the loop between
operator CLI annotation writes and the reconciler-side annotation reads
shipped in plan 04-04/04-05: `approve`, `reject`, `resume`, `cancel`, and
`tail`. Three of the five (`approve` / `reject` / `resume`) use
`client.MergeFrom + client.Patch` for one-shot annotation semantics that mirror
the reconciler's `gates.Consume*` purity contract. `cancel` requires `--force`
and uses `PropagationPolicyForeground` for K8s-native cascade delete. `tail`
streams pod logs via the canonical `pods/log` subresource with `Follow:true`
and ctx-aware cancellation (Pitfall 25 mitigation).

## Performance

- **Duration:** 35 min
- **Tasks:** 3/3
- **Commits:** 6 (3 RED / 3 GREEN — strict TDD cycle)
- **Files created:** 5 (test files)
- **Files modified:** 7 (5 verb files + subcommands.go + cmd_test.go)

## What landed

### `cmd/tide/approve.go` — annotation writer with two paths

```go
var approveWaveRE = regexp.MustCompile(`^[a-z0-9.-]+/\d+$`)

func approveRun(ctx context.Context, c client.Client, ns, projectName, waveFlag string, out io.Writer) error {
    if waveFlag != "" {
        return approveWave(ctx, c, ns, projectName, waveFlag)
    }
    return approveLevel(ctx, c, ns, projectName)
}

// patchApproveLevel writes approve-<level>=true via MergeFrom + Patch.
func patchApproveLevel(ctx context.Context, c client.Client, obj client.Object, level string) error {
    original := obj.DeepCopyObject().(client.Object)
    patch := client.MergeFrom(original)
    anno := obj.GetAnnotations()
    if anno == nil { anno = map[string]string{} }
    anno[gates.AnnotationApprovePrefix+level] = "true"
    obj.SetAnnotations(anno)
    return c.Patch(ctx, obj, patch)
}
```

| Path | Trigger | Target | Annotation written |
|------|---------|--------|--------------------|
| Level discovery | `tide approve my-project` | First child with `Status.Phase=AwaitingApproval` (Milestone → Phase → Plan → Task) | `tideproject.k8s/approve-<level>: "true"` |
| Wave | `tide approve my-project --wave my-plan/3` | Plan named `my-plan` | `tideproject.k8s/approve-wave-3: "true"` |

Friendly errors:
- `tide: project %q not found in namespace %q`
- `tide: no level awaiting approval on project %s`
- `--wave must be <plan-name>/<integer>; got %q`

### `cmd/tide/reject.go` — annotation writer with default reason

```go
func rejectRun(ctx context.Context, c client.Client, ns, projectName, reason string) error {
    if reason == "" { reason = "rejected by operator" }
    var proj tidev1alpha1.Project
    if err := c.Get(ctx, ...); err != nil { /* friendly NotFound */ }
    patch := client.MergeFrom(proj.DeepCopy())
    anno := proj.GetAnnotations()
    if anno == nil { anno = map[string]string{} }
    anno[gates.AnnotationReject] = reason
    proj.SetAnnotations(anno)
    return c.Patch(ctx, &proj, patch)
}
```

Defense-in-depth: empty reason defaults to `"rejected by operator"` both at the
cobra flag layer (`c.Flags().StringVar(..., "rejected by operator", ...)`) AND
inside `rejectRun`. The seam-level default exists because `gates.CheckRejected`
returns `false` for empty values (T-04-G4 mitigation), and a future caller
bypassing the cobra flag must not silently write a no-op annotation.

### `cmd/tide/resume.go` — annotation clear via ConsumeReject

```go
func resumeRun(ctx context.Context, c client.Client, ns, projectName string) error {
    var proj tidev1alpha1.Project
    if err := c.Get(ctx, ...); err != nil { /* friendly NotFound */ }
    patch := client.MergeFrom(proj.DeepCopy())
    proj.SetAnnotations(gates.ConsumeReject(&proj))
    return c.Patch(ctx, &proj, patch)
}
```

`gates.ConsumeReject` (from plan 04-04) returns a NEW map with the reject key
removed; the caller patches once. Same purity contract as
`budget.ConsumeBypass`. Idempotent: calling `tide resume` on an un-rejected
Project is a no-op (the patch is a no-op at the apiserver level).

### `cmd/tide/cancel.go` — destructive cascade with --force + --dry-run

```go
func cancelRun(ctx context.Context, c client.Client, ns, projectName string, force, dryRun bool, out, errOut io.Writer) error {
    if !force {
        return errors.New("tide: cancel is destructive — pass --force to confirm cascading delete of project, children, and PVC")
    }
    var proj tidev1alpha1.Project
    if err := c.Get(ctx, ...); err != nil { /* friendly NotFound */ }
    if dryRun {
        return cancelDryRun(ctx, c, ns, projectName, &proj, out)
    }
    fmt.Fprintf(errOut, "Deleting project %s (foreground cascade)…\n", projectName)
    if err := c.Delete(ctx, &proj, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
        return fmt.Errorf("delete project: %w", err)
    }
    fmt.Fprintf(errOut, "Project %s deleted. Children cascade via owner refs; PVC cleanup runs via finalizer.\n", projectName)
    return nil
}
```

`--dry-run` enumerates owner-ref'd / project-labelled children (Milestone +
Phase + Plan + Task lists filtered by the canonical
`tideproject.k8s/project` label) so the operator sees the deletion scope
before committing.

### `cmd/tide/tail.go` — pod-log streaming with ctx-aware cancellation

```go
func defaultTailStreamer(ctx context.Context, cs kubernetes.Interface, ns, pod, container string, opt tailOptions, out, errOut io.Writer) error {
    req := cs.CoreV1().Pods(ns).GetLogs(pod, &corev1.PodLogOptions{
        Follow:     true,
        Container:  container,
        TailLines:  ptr.To(opt.tailLines),
        Timestamps: opt.timestamps,
    })
    stream, err := req.Stream(ctx)
    if err != nil { return fmt.Errorf("open log stream for pod/%s: %w", pod, err) }
    defer stream.Close()

    // Pitfall 25 — ctx-cancel watcher closes the stream so io.Copy returns within ~1s.
    go func() { <-ctx.Done(); _ = stream.Close() }()

    if _, err := io.Copy(out, stream); err != nil && ctx.Err() == nil {
        return fmt.Errorf("read log stream: %w", err)
    }
    if ctx.Err() == nil {
        fmt.Fprintln(errOut, "(stream closed by pod termination)")
    }
    return nil
}
```

| Flag | Default | Description |
|------|---------|-------------|
| `--container` / `-c` | `""` (heuristic picks first non-credproxy/non-init-*) | Container name to stream from |
| `--tail` | `100` | Number of recent lines before streaming |
| `--timestamps` / `-t` | `true` | Include timestamps |

Container-resolution heuristic (`pickContainer`):
1. Explicit `--container` wins.
2. Otherwise first container whose Name is NOT `credproxy` AND does NOT start with `init-` — the subagent main container by Phase-1/2 convention.
3. Empty string return → friendly error `tide: task %q has pod %q with no resolvable container; pass --container`.

`tailPodPicker` matches Pods by the canonical `tideproject.k8s/task-uid`
label (per `task_controller.go:673`). Pending Pods are allowed targets
(matches kubectl logs -f UX — Follow=true waits for container ready).

### `cmd/tide/subcommands.go` — stub removal

Drop `plan0408Stubs()` helper + `firstWord()` helper. Wire the five real
verbs directly:

```go
func registerSubcommands(root *cobra.Command) {
    root.AddCommand(newApplyCmd())
    root.AddCommand(newWatchCmd())
    root.AddCommand(newInspectWaveCmd())
    root.AddCommand(newArtifactGetCmd())
    root.AddCommand(newDescribeBudgetCmd())
    // Plan 04-08 — real write-back verbs (approve / reject / cancel / resume / tail).
    root.AddCommand(newApproveCmd())
    root.AddCommand(newRejectCmd())
    root.AddCommand(newCancelCmd())
    root.AddCommand(newResumeCmd())
    root.AddCommand(newTailCmd())
}
```

`TestStubVerbsReturnNotImplemented` in `cmd_test.go` renamed to
`TestStubVerbsRequireArgs` — the verbs are no longer stubs, so the
assertion is now "each errors on missing positional arg" (cobra ExactArgs(1)).

## Test coverage

`cmd/tide/approve_test.go` — 6 tests:
- `TestApproveLevelDiscoversAwaitingMilestone`
- `TestApproveWaveFormatRejection` (5 bad-format inputs)
- `TestApproveWaveWritesAnnotationOnPlan`
- `TestApproveProjectNotFound`
- `TestApproveNoAwaitingLevel`
- `TestApproveUsesMergeFromPatch` (asserts MergeFrom preserves other annotations)

`cmd/tide/reject_test.go` — 4 tests:
- `TestRejectWritesAnnotationWithReason`
- `TestRejectDefaultsReasonWhenEmpty`
- `TestRejectProjectNotFound`
- `TestRejectPreservesOtherAnnotations`

`cmd/tide/resume_test.go` — 4 tests:
- `TestResumeClearsRejectAnnotation`
- `TestResumePreservesOtherAnnotations`
- `TestResumeProjectNotFound`
- `TestResumeNoOpWhenNoReject`

`cmd/tide/cancel_test.go` — 4 tests:
- `TestCancelRequiresForce`
- `TestCancelForceDeletes` (verifies stderr banner)
- `TestCancelMissingProjectFriendlyError`
- `TestCancelDryRunListsChildren` (verifies project + child names in output)

`cmd/tide/tail_test.go` — 5 tests:
- `TestTailDefaultsContainerSkipsCredproxy` (3 sub-cases in `pickContainer`)
- `TestTailTaskNotFoundError`
- `TestTailTaskNoRunningPodFriendlyError`
- `TestTailContextCancellationReturnsWithin1s` (ctx-cancel propagation via injected streamer + 500ms streamer-invocation guard)
- `TestTailDefaultFlagValues`

```
=== Test Summary ===
40/40 PASS with -race (17 from 04-07 + 23 from 04-08)
go test ./cmd/tide/... -race -count=1  →  ok  github.com/jsquirrelz/tide/cmd/tide  3.5s
```

## Plan verification block satisfied

| Check | Result |
|-------|--------|
| `go test ./cmd/tide/... -race -v` | 40/40 PASS |
| `bin/tide approve --help` documents `--wave <plan/N>` | YES (Long text + flag description) |
| `bin/tide cancel my-project` (no --force) exits non-zero with warning | YES (exit 1 + "destructive — pass --force") |
| `bin/tide tail --help` documents --container/--tail/--timestamps | YES (all three flags + defaults visible) |
| `grep -c "client.MergeFrom" cmd/tide/{approve,reject,resume}.go` | **5 + 2 + 1 = 8** (plan asks ≥ 3) |
| `grep -c "DeletePropagationForeground" cmd/tide/cancel.go` | **1** (plan asks ≥ 1) |
| `grep -c "GetLogs" cmd/tide/tail.go` | **2** (plan asks ≥ 1) |
| `make tide-lint` | clean (exit 0) |
| `go build ./...` | clean |
| `go vet ./...` | clean |

## TDD Gate Compliance

All three tasks followed strict RED → GREEN cycles. Commit ledger on branch
`worktree-agent-ae2d5382308d47813`:

| Task | Phase | Commit    | Type | Subject                                                       |
| ---- | ----- | --------- | ---- | ------------------------------------------------------------- |
| 1    | RED   | `da142ae` | test | approve/reject/resume annotation-writer tests + cancel/tail RED stubs |
| 1    | GREEN | `22a33dd` | feat | approve/reject/resume annotation writers via client.Patch    |
| 2    | RED   | `a708949` | test | `tide cancel --force` cascade-delete tests                    |
| 2    | GREEN | `290052c` | feat | `tide cancel --force` cascade-delete + --dry-run preview      |
| 3    | RED   | `3bc5aea` | test | `tide tail` pod-log streaming tests + helper stub             |
| 3    | GREEN | `35a30db` | feat | `tide tail` pod-log streaming via client-go pods/log          |

Six commits. Each RED was verified to fail (`"not yet implemented (RED stub)"`
on the testable seam OR `"RED-STUB"` on `pickContainer`) BEFORE the matching
GREEN landed. GREEN commits are the only places production code lands.

## What downstream plans now consume

| Downstream plan | Consumes |
|----------------|----------|
| **04-09** (release pipeline + Krew) | The now-complete cmd/tide binary — no more stub verbs in `tide --help` |
| **04-11** (dashboard pod-log SSE handler) | `defaultTailStreamer`'s ctx-aware streaming pattern (mirrors the Pitfall 25 mitigation in a server-side SSE wrapper) |
| **04-14** (kind harness E2E) | The full annotation-handshake loop — `tide approve` writes → reconciler reads → Status transitions; `tide tail` against a real Pod |

## Deviations from Plan

None. The plan executed exactly as written. Eight implementation-detail
choices documented as `decisions` in the frontmatter:

1. Level discovery walks child CRDs (not Project.Status.Conditions) — accuracy
2. --wave regex permits DNS-label characters
3. rejectRun applies default reason at the seam layer (defense-in-depth)
4. --dry-run enumerates by label (not owner ref)
5. Function vars (not interfaces) for tail test seams
6. tailPodPicker matches by `task-uid` label (not deterministic Job name)
7. Pending pods are valid tail targets (matches kubectl logs -f UX)
8. EOF on stream-terminate prints stderr marker + exits 0

All eight are plan-conformance refinements (no behavioral departures from the
plan's intent).

## Known Stubs

None. Every plan-04-08 verb has a real implementation. The cobra `--help`
surface is now honest:

```
Available Commands:
  apply           Apply a TIDE manifest (server-side apply)
  approve         Approve the current AwaitingApproval level or a specific wave
  artifact-get    Fetch a PVC artifact via an apiserver-proxied inspector pod
  cancel          Destructively cancel a Project (cascade delete)
  completion      Generate the autocompletion script for the specified shell
  describe-budget Show a Project's budget cap vs. running spend
  help            Help about any command
  inspect-wave    List Tasks in a Project's wave with status/age/attempt/wave columns
  reject          Halt a Project with an optional reason
  resume          Clear a tideproject.k8s/reject annotation on a Project
  tail            Stream pod logs for a Task
  watch           Watch a TIDE Project's live status
```

(`artifact-get` still ships as dry-run only — the real apiserver pod-exec
proxy lands in plan 04-14's kind harness; deferred per the 04-07 SUMMARY
decision, NOT a plan-04-08 issue.)

## Threat Flags

None new. The plan's `<threat_model>` (T-04-G2 over-approval, T-04-G4
delete-the-world, T-04-C4 stream leak) is fully mitigated:

- **T-04-G2** (over-approval): annotation is one-shot — consumed by the
  reconciler via `gates.ConsumeApprove` (plan 04-05). Re-triggering the gate
  requires a fresh annotation write. `approve` writes the canonical key the
  reconciler reads (no string drift between writer and reader).
- **T-04-G4** (delete-the-world): `cancel` requires explicit `--force` and
  surfaces a clear "destructive" error without it. `--dry-run` lets the
  operator preview the deletion scope before committing. Foreground cascade
  is the K8s-native, no-mode-other-than-fast contract — children GC via owner
  refs, PVC via the existing CTRL-05 finalizer.
- **T-04-C4** (stream leak): `defer stream.Close()` + the ctx.Done() watcher
  goroutine guarantees the stream closes on either EOF or Ctrl-C. Tested via
  `TestTailContextCancellationReturnsWithin1s` (streamer-invocation guard +
  1.5s timeout).

No new threat surface introduced.

## Self-Check: PASSED

**Files exist:**
- `cmd/tide/approve.go` (274 LOC — was a 30-LOC RED stub)
- `cmd/tide/reject.go` (97 LOC — was a 30-LOC RED stub)
- `cmd/tide/resume.go` (87 LOC — was a 30-LOC RED stub)
- `cmd/tide/cancel.go` (164 LOC — was a 30-LOC RED stub)
- `cmd/tide/tail.go` (224 LOC — was a 60-LOC RED stub)
- `cmd/tide/approve_test.go` (146 LOC)
- `cmd/tide/reject_test.go` (75 LOC)
- `cmd/tide/resume_test.go` (79 LOC)
- `cmd/tide/cancel_test.go` (109 LOC)
- `cmd/tide/tail_test.go` (188 LOC)
- `cmd/tide/subcommands.go` (modified — stub helpers removed)
- `cmd/tide/cmd_test.go` (modified — TestStubVerbsReturnNotImplemented → TestStubVerbsRequireArgs)

**Commits exist on worktree branch (`git log --oneline 6d81a9df..HEAD`):**
- `da142ae` test(04-08): RED — approve/reject/resume annotation-writer tests + cancel/tail RED stubs
- `22a33dd` feat(04-08): GREEN — approve/reject/resume annotation writers via client.Patch
- `a708949` test(04-08): RED — `tide cancel --force` cascade-delete tests
- `290052c` feat(04-08): GREEN — `tide cancel --force` cascade-delete + --dry-run preview
- `3bc5aea` test(04-08): RED — `tide tail` pod-log streaming tests + helper stub
- `35a30db` feat(04-08): GREEN — `tide tail` pod-log streaming via client-go pods/log

All 40/40 cmd/tide tests pass with `-race`. `make tide-lint` clean.
`go build ./...` clean. `go vet ./...` clean. Plan verification block fully
satisfied (8 grep matches against required ≥5; help surfaces document the
flag set; --force gate verified on the live binary).

STATE.md / ROADMAP.md NOT touched — orchestrator owns those writes after all
worktree agents in the wave complete.
