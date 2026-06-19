# Phase 29: Operator Tooling + E2E - Research

**Researched:** 2026-06-19
**Domain:** Go CLI tooling (cobra), PVC I/O pods, bundle format, kind integration testing
**Confidence:** HIGH — every claim below is grounded in live file reads, not training-data assumptions.

---

## Summary

Phase 29 wraps the already-built Phase 28 import core in two cobra verbs (`tide export-envelopes` / `tide import-envelopes --dry-run`) and proves them end-to-end with a kind integration test. The code surface is narrow: two new `cmd/tide/` files + one new kind test file. The implementation has well-blazed trails — `artifact_get_run.go` supplies the inspector-pod pattern and `cmd/tide-import/main.go` supplies the inverse (loader) pattern.

**Three DECISION-COLLIDING findings require planner attention before code is authored.** All three are below in the "Decisions vs Code Actuality" section. The most critical: every successful planner envelope in the salvage fixture lacks the `childCount` field, which makes `tide-import`'s `isEnvelopeComplete` guard reject all 18 of them. Export must patch this before staging.

**Primary recommendation:** Author `exportEnvelopesRun` and `importEnvelopesRun` as testable seam functions (exact pattern from `artifactGetRun`) registered in `subcommands.go`. Loader pod inverts the inspector pod: same `busybox:1.36`, same RBAC, but writes tgz in via stdin rather than reads bytes out via log stream.

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- D-01: Export reads PVC bytes via reused inspector-pod pattern (busybox:1.36, tar subtree, stream out).
- D-02: Bundle mirrors salvage-20260618 fixture shape: CR-tree YAMLs + seed manifest + pvc-envelopes/ + SEED-OUTLINE.md. Default output is a .tgz; --dir flag for unpacked directory.
- D-03: Export emits the seed manifest capturing live UIDs at export time. Import applies verbatim. FQ-name keying per Phase 28 D-07.
- D-04: Export writes per-envelope sha256 into seed manifest. Import/dry-run verify as an additional gate.
- D-05: import-envelopes is stage-only: loads envelopes to PVC + creates seed ConfigMap + surfaces project.yaml. Does NOT apply the Project. Operator runs `tide apply project.yaml` separately.
- D-06: Envelopes land on PVC via loader pod (inverse of inspector pod): short-lived busybox mounts PVC, CLI streams tgz in, pod unpacks to declared pvcSubPath at old-UID paths.
- D-07: Dry-run validates locally on unpacked bundle — no cluster writes, no pods, works offline. Reuses Phase 28 validation code: ValidateAPIVersionKind, completeness check, sha256 check, dag.ComputeWaves.
- D-08: Output is per-level adopt/re-plan table (level | name | verdict | reason) + summary count. --output json for machine consumption.
- D-09: Cycle hard-rejects entire import. Dry-run reports cycle edges and marks whole import would-fail.
- D-10: E2E test drives the real CLI (exec.Command("tide", ...)) for both verbs.
- D-11: Two-tier assertion: (a) small purpose-built fixture drains all-Milestones-Succeeded with stub subagents; (b) full salvage-20260618 asserts adoption + zero planner Jobs for imported levels + $0 re-paid planning cost.
- D-12: Heavy paths gated behind testing.Short() + long-test label.
- D-13: Wave CRs never exported or imported. Always re-derived.
- D-14: Budget rollup suppressed for imported envelopes (Phase 28 D-11).
- D-15: Seed covers down to Plan only (Phase 28 D-04). Tasks materialize from plan-level envelope children.

### Claude's Discretion
- Exact cobra flag names, seed-manifest on-disk schema, inspector/loader pod RBAC verbs, small E2E fixture's concrete shape.
- Whether export/import share a single pkg/-level bundle reader/writer vs per-command code.

### Deferred Ideas (OUT OF SCOPE)
- Automatic export-on-halt.
- Hybrid by-name write-side envelope paths.
- Partial/incremental re-planning.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| TOOL-01 | An operator CLI command exports a Project's planner envelopes to a portable bundle and imports a bundle into a new run, with a dry-run mode that reports what would be adopted vs re-planned. | Inspector-pod pattern (artifact_get_run.go) + loader-pod inverse + seed ConfigMap schema from import_controller.go seedEntry/seedManifest structs + ValidateAPIVersionKind + ComputeWaves all verified importable offline. |
| TOOL-02 | A kind integration test proves end-to-end resumption against the real salvage-20260618 fixture: import the salvaged plan, planners skipped, execution proceeds, planning cost not re-paid. | kind test harness (suite_test.go + exec.Command pattern) + JobList label filter (tideproject.k8s/role=planner) + Project.Status.Budget.CostSpentCents verified. Salvage fixture envelope completeness profile documented (see finding #3). |
</phase_requirements>

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Export: read PVC bytes | CLI (tide cmd) | K8s pod (busybox) | CLI orchestrates; pod has PVC access (subPath mount) |
| Export: generate seed manifest | CLI (tide cmd) | K8s API (read live CRs) | UIDs available only by querying live cluster |
| Import: write envelopes to PVC | CLI (tide cmd) | K8s pod (busybox) | CLI streams tgz; pod has write-mode PVC access |
| Import: create seed ConfigMap | CLI (tide cmd) | K8s API | ConfigMap is a K8s object; CLI creates it directly |
| Dry-run validation | CLI (offline) | — | Pure bundle inspection, no cluster required |
| Import: create Project CR | Operator (manual `tide apply`) | — | D-05: CLI stages only; operator applies project.yaml |
| Import: UID-rekey envelopes | In-pod tide-import Job | ImportController | Phase 28 responsibility — not Phase 29 |

---

## Inspector-Pod Surface (D-01 / D-06) — Concrete Code Facts

**Source files:** `cmd/tide/artifact_get.go` + `cmd/tide/artifact_get_run.go` [VERIFIED: live read]

### What the pattern already provides

`inspectorPodRunner` is a `var` func (line 57 in `artifact_get_run.go`) that:
- Creates a `busybox:1.36` pod with `RestartPolicy: Never` in the target namespace.
- Mounts the named PVC at `/workspace` with `SubPath = projectUID` (read-only).
- Delivers the artifact path via env var `ARTIFACT_PATH` (T-15-08: never interpolated into shell string).
- Pod internal shell waits for file stability (two consecutive `stat -c %s` samples 2s apart), then `cat`s the file — stdout becomes the log stream.
- Uses `cs.CoreV1().Pods(ns).GetLogs(podName, &corev1.PodLogOptions{Follow: true})` to stream bytes to `out io.Writer`.
- Defers pod delete with `context.Background()` on all exit paths (T-15-09).
- Waits for pod phase `Running` or `Succeeded` before opening log stream (`waitForPodRunning`, line 287).

**RBAC required (documented in cmd/tide/artifact_get.go:32-34):** `pods: create, get, delete` + `pods/log: get`.

### What export needs that artifact-get does NOT already provide

Export needs to tar a **directory subtree** (`envelopes/<projectUID>/workspace/`) and stream it out, not cat a single file. The pod command changes from `cat "$ARTIFACT_PATH"` to `tar czf - -C /workspace envelopes/ artifacts/` (or equivalent). The streaming mechanism (GetLogs) works identically — the pod's stdout carries the tgz bytes.

Additionally, export needs to **resolve the old project UID** (to address the correct PVC subpath). `artifactGetRun` already shows this pattern: line 139 does `k.Get(ctx, ...)` on the live Project CR to get `proj.UID`.

**No new function-var seam is needed.** The planner can author `exportInspectorPodRunner` as a separate func-var following the identical pattern, or extend `inspectorPodRunner` signature — either is in Claude's discretion.

### What the loader pod (D-06) needs — all new code

The loader pod is the **write-direction inverse**:
- Mount the PVC read-write (not read-only) at `/workspace` with `SubPath = <oldProjectUID>/workspace`.
- The pod command: pipe-read from stdin + `tar xzf - -C /workspace` (unpack to correct subpath prefix).
- The CLI streams the bundle tgz to the pod via `pods/attach` (not `pods/log`). This requires `cs.CoreV1().Pods(ns).GetSubResourceURLs("attach", ...)` or the remotecommand/SPDY exec API.

**Critical difference from export:** the log-stream approach used by artifact-get is a READ path (pod stdout → CLI). For the loader pod write direction, tgz bytes must flow CLI → pod stdin. This requires Kubernetes pod exec/attach, not log streaming. The `k8s.io/client-go/tools/remotecommand` package provides `NewSPDYExecutor` + `Stream()` — this IS already in go.mod (client-go is a controller-runtime transitive dep). [CITED: cmd/tide-import/main.go + k8s.io/client-go usage throughout codebase]

The loader pod approach in practice:
1. Create pod with `busybox:1.36`, RW PVC mount at `/workspace`, `stdin: true`, command `tar xzf - -C /workspace`.
2. Use `remotecommand.NewSPDYExecutor(restCfg, "POST", url)` where url is `cs.CoreV1().Pods(ns).GetSubResourceURLs("exec", podName, "inspector")` with stdin attached.
3. `executor.StreamWithContext(ctx, remotecommand.StreamOptions{Stdin: tgzReader})`.
4. Defer pod delete.

**RBAC for loader pod:** `pods: create, get, delete` + `pods/exec: create` (new verb — not needed by artifact-get). [ASSUMED: pods/exec is the correct additional verb; verify against RBAC docs before wiring the ClusterRole/Role.]

---

## PVC Envelope Layout and Bundle Format

**Source:** `internal/dispatch/podjob/backend.go:94-96` (FilesystemEnvelopeReader.ReadOut) [VERIFIED: live read]

### On-PVC path contract

```
<PVC root>/
  <projectUID>/workspace/
    envelopes/
      <taskUID>/
        in.json
        out.json
        children/
          <childName>.json
          ...
        events.jsonl
        MILESTONE.md (for milestone-level planners)
    artifacts/
      ...
```

The `FilesystemEnvelopeReader.ReadOut` reads: `WorkspaceRoot/{projectUID}/workspace/envelopes/{taskUID}/out.json`.

The inspector pod mounts `subPath=projectUID` so its `/workspace` = `<PVC>/<projectUID>/`. Export must tar: `envelopes/` + `artifacts/` (the workspace contents, not the projectUID directory itself).

The `ImportJobOptions.OldSubPath` = `project.Spec.ImportSource.SalvagedPVCSubPath` = `<oldProjectUID>/workspace` (set in `reconcileCopyingEnvelopes`, line 600 in `import_controller.go`). The loader pod must place the unpacked tgz at PVC prefix `<oldProjectUID>/workspace/`, so the tgz root is the workspace directory.

**Bundle tgz internal structure (what export must produce):**
```
pvc-envelopes.tgz root:
  envelopes/
    <oldTaskUID>/
      in.json, out.json, children/*.json, events.jsonl, *.md
  artifacts/
    ...
```
When the loader pod unpacks with `tar xzf - -C /workspace`, the result is `/workspace/envelopes/<oldTaskUID>/...` which maps to PVC path `<oldProjectUID>/workspace/envelopes/<oldTaskUID>/...` — exactly what `tide-import` reads from `/old-workspace/envelopes/<oldUID>/`.

### Salvage fixture pvc-envelopes structure (verified)

`examples/projects/dogfood/salvage-20260618/pvc-envelopes/` contains `envelopes/` (59 UID directories) + `artifacts/`. [VERIFIED: live ls]

**No workspace/ prefix** in the fixture — `pvc-envelopes/` IS the workspace content. This matches the required loader-pod target. The `pvc-envelopes.tgz` already has the correct structure for unpacking directly into `/workspace` on the loader pod.

---

## Seed ConfigMap Contract — Export Must Generate

**Source:** `internal/controller/import_controller.go:92-130` (seedEntry + seedManifest types) [VERIFIED: live read]

### Exact wire schema the ImportController consumes

The seed ConfigMap carries a single key `"manifest"` whose value is a JSON object matching `seedManifest`:

```go
type seedManifest struct {
    Milestones []seedEntry `json:"milestones"`
    Phases     []seedEntry `json:"phases"`
    Plans      []seedEntry `json:"plans"`
}

type seedEntry struct {
    Name         string   `json:"name"`         // CR .metadata.name
    FQName       string   `json:"fqName"`        // "milestone-01/phase-02/plan-foo"
    OldUID       string   `json:"oldUID"`        // old run .metadata.uid
    DependsOn    []string `json:"dependsOn,omitempty"` // mirrors Spec.DependsOn
    Status       string   `json:"status,omitempty"`    // initial Status.Phase to patch
    PhaseRef     string   `json:"phaseRef,omitempty"`  // for Plan entries
    MilestoneRef string   `json:"milestoneRef,omitempty"` // for Phase entries
    ProjectRef   string   `json:"projectRef,omitempty"`   // for Milestone entries
}
```

The import controller reads seed at line 303: `rawManifest, ok := seedCM.Data["manifest"]` — it strict-parses this as `seedManifest{}`. Unknown extra keys in `seedEntry` are tolerated by `json.Unmarshal` (unknown fields ignored by default).

**Where does the name→oldUID map come from at export time?** The live CRs carry their UIDs in `.metadata.uid`. Export queries the live cluster's Milestone/Phase/Plan objects via the K8s client and reads `.metadata.uid` for each. The FQName is computed from the CR's name + its owner-ref chain (milestoneRef / phaseRef).

**Per-envelope sha256 (D-04):** The `seedEntry` struct does NOT have a sha256 field currently. Export should add it as an additional JSON key that the ImportController's `json.Unmarshal` will silently ignore (since Go's `encoding/json` drops unknown fields). However, dry-run (D-07) will read the sha256 from the seed manifest — so the bundle reader must understand the sha256 field. This means:
- The seed manifest JSON can have extra fields beyond `seedEntry` — they will be ignored by the ImportController.
- Dry-run reads the bundle's seed manifest and performs sha256 validation before the ImportController ever sees it.
- The sha256 field lives at the seedEntry level (per envelope, keyed to oldUID or fqName).
- **Zero breakage to Phase 28 ImportController** — unknown fields silently dropped. [VERIFIED: Go json.Unmarshal behavior]

**Is the seed ConfigMap schema additive-safe?** YES. The ImportController's `json.Unmarshal([]byte(rawManifest), &seed)` will ignore extra fields in the JSON that don't exist in `seedManifest` / `seedEntry`. Adding sha256 per entry is safe.

**Does anything strict-parse the seed and reject unknown fields?** NO. The import controller uses standard `json.Unmarshal` without `DisallowUnknownFields()`. [VERIFIED: import_controller.go:311]

### No seed ConfigMap exists in the salvage fixture

The salvage fixture has: `projects.yaml`, `milestones.yaml`, `phases.yaml`, `plans.yaml`, `pvc-envelopes/`, `SEED-OUTLINE.md`. **No seed ConfigMap YAML**. [VERIFIED: live ls]

Export must GENERATE the seed manifest from scratch by:
1. Querying live CRs (or from the already-exported CRD YAML files in the bundle's directory tree).
2. Building `FQName` from the CR name + parent chain.
3. Stamping `OldUID` from `.metadata.uid`.
4. Collecting `DependsOn` from `.spec.dependsOn`.
5. Recording `Status` from `.status.phase` if non-empty.
6. Computing sha256 of the envelope's out.json bytes.

### project.yaml in the bundle — the name-binding handshake

Export must emit a `project.yaml` that an operator can `tide apply`. This project.yaml carries `spec.importSource.seedManifestConfigMap = "<configmap-name>"` and `spec.importSource.salvagedPVCSubPath = "<oldProjectUID>/workspace"`. Import creates the seed ConfigMap with a deterministic name (e.g., `tide-import-seed-<bundleID>`), then the operator applies the project.yaml pointing at it. The seed ConfigMap name must appear in both the project.yaml and as the actual ConfigMap created by `import-envelopes`. This is the name-binding handshake.

**Claude's discretion:** The planner chooses the ConfigMap naming convention.

---

## Decisions Confirmed vs Decisions that Hit a Wall in the Code

### CONFIRMED — proceed as designed

| Decision | Status | Evidence |
|----------|--------|----------|
| D-01: inspector-pod reuse for export | CONFIRMED | `artifactGetRun` + `inspectorPodRunner` var seam directly reusable. Only command differs (tar vs cat). |
| D-03: export captures live UIDs | CONFIRMED | CR `.metadata.uid` is the correct source. ImportController reads FQName→oldUID from seed at `import_controller.go:415`. |
| D-07: dry-run imports offline | CONFIRMED | `pkg/dispatch/envelope.go` imports only `"time"` — zero K8s deps. `pkg/dag/kahn.go` imports only `"fmt"/"sort"`. Both are pure-stdlib and usable in any Go binary with no cluster connection. |
| D-09: cycle hard-rejects | CONFIRMED | `dag.CycleError.InvolvedNodes []string` carries all stuck nodes — planner can print these for the dry-run report. `ComputeWaves` returns `*CycleError` via `errors.As`. |
| D-13: no Wave exports | CONFIRMED | Salvage envelopes have zero Wave children. Wave CRs are never in `childCRDs`. |
| D-14: budget suppression | CONFIRMED | `project_controller.go:1253` checks `project.Spec.ImportSource != nil` and skips rollup. All four dispatch sites have the same check. |
| D-15: seed down to Plan level only | CONFIRMED | Salvage fixture: project/milestone/phase/plan planners all present; ZERO task-level planners in the salvage (Tasks materialize from plan-level children files). |

### COLLISION #1 — childCount absent in ALL salvage planner out.json

**Severity: BLOCKER for the salvage-based E2E test.**

`isEnvelopeComplete()` in `cmd/tide-import/main.go:299-307` requires `env.ExitCode == 0 AND len(env.ChildCRDs) == env.ChildCount`. [VERIFIED: live read]

**Actual salvage out.json:** `childCount` field is **completely absent** from all 18 successful planner envelopes in the salvage fixture (exitCode=0, childCRDs present). When `json.Unmarshal` decodes these into `pkgdispatch.EnvelopeOut`, `ChildCount` defaults to `0`. Since `len(childCRDs) > 0` (3–6 children per envelope), the guard fails and `tide-import` logs `"incomplete"` and SKIPS every successful planner envelope. [VERIFIED: Python analysis of all 59 envelopes]

**Root cause:** The salvage was produced by the dogfood run before `ChildCount` was added to `EnvelopeOut` (plan 09-08 Defect B fix). The `omitempty` JSON tag means old envelopes legitimately lack the field.

**Required fix (must happen in Phase 29):** Export must patch `childCount = len(childCRDs)` in each out.json when copying it into the bundle. Concretely: when export reads `out.json` off the PVC and finds `ChildCount == 0` but `len(ChildCRDs) > 0`, it writes the repaired bytes (with `childCount` set) into the bundle. The bundled out.json files are what the loader pod stages, so tide-import then sees a valid `childCount` field.

Alternatively: tide-import's `isEnvelopeComplete` could be relaxed to treat `ChildCount == 0 AND len(ChildCRDs) > 0` as complete (legacy mode). But this is a Phase 28 code change — the export-side repair is cleaner and stays within Phase 29 scope.

**Planner decision required:** choose export-side repair (preferred — Phase 29 scope) or import-side guard relaxation (requires touching Phase 28 code).

### COLLISION #2 — ALL plan-level planners failed in the salvage fixture

**Severity: Affects E2E test assertion design.**

Plan-level envelope completion distribution in the salvage: [VERIFIED: Python analysis]
- project planners: 1 success / 0 fail
- milestone planners: 3 success / 0 fail
- phase planners: 14 success / 1 fail
- **plan planners: 0 success / 38 fail** (all failed, budget-halted mid-planning)

**Impact on D-11 assertion:** "zero planner Jobs dispatched for imported levels + $0 re-paid planning cost" applies to the 18 successfully-imported envelopes (1 project + 3 milestone + 14 phase). Plan-level planners have NO complete envelopes, so after import tide-import skips all plan envelope copies, and the plan reconcilers will dispatch fresh planner Jobs for all 42 plans. The $90 claim is $90 of milestone/phase planning — that is what import saves. Plan planners will re-run and add NEW cost.

**The kind E2E test assertion must be:**
- `JobList` with label `tideproject.k8s/role=planner` AND `tideproject.k8s/level in {milestone,phase}` → 0 Jobs (adopted, skipped)
- `Project.Status.Budget.CostSpentCents == 0` after import completes and milestone/phase planners are skipped (before any plan planners fire)

The D-11 test fixture does NOT require all 42 plan planners to succeed. It proves the adopted levels were free. Planner must bound the assertion window to before plan dispatch fires.

### COLLISION #3 — the loader pod requires `pods/exec` not `pods/log`

The existing inspector-pod pattern uses `pods/log: get` to stream output (artifact bytes) from the pod to the CLI. The loader pod writes tgz bytes INTO the pod, which requires `pods/exec: create`. [CITED: Kubernetes API + remotecommand package conventions, ASSUMED for exact verb name — verify against ClusterRole docs]

The `k8s.io/client-go/tools/remotecommand` package (already in go.mod as a controller-runtime transitive dep) provides the SPDY executor for streaming stdin to a running pod. The pattern:
```go
url := cs.CoreV1().RESTClient().Post().
    Resource("pods").Name(podName).Namespace(ns).
    SubResource("exec").
    Param("stdin", "true").Param("command", "tar").Param("command", "xzf").
    Param("command", "-").Param("command", "-C").Param("command", "/workspace").URL()
exec, _ := remotecommand.NewSPDYExecutor(restCfg, "POST", url)
exec.StreamWithContext(ctx, remotecommand.StreamOptions{Stdin: tgzReader, Stderr: errOut})
```
[ASSUMED: exact API surface — verify against k8s.io/client-go/tools/remotecommand docs before implementing]

---

## Standard Stack

### Core (no new go.mod entries)

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/spf13/cobra` | already in go.mod | CLI verb construction | All existing tide verbs use this |
| `k8s.io/client-go/tools/remotecommand` | transitive via controller-runtime | Loader pod stdin streaming | Already available; only new usage site |
| `crypto/sha256` | Go stdlib | Per-envelope checksum (D-04) | Already used in internal/credproxy/token.go |
| `archive/tar` + `compress/gzip` | Go stdlib | tgz bundle creation/extraction | Pure stdlib, no new deps |
| `pkg/dispatch` (internal) | in-tree | ValidateAPIVersionKind + EnvelopeOut | Zero external deps |
| `pkg/dag` (internal) | in-tree | ComputeWaves + CycleError | Zero external deps |

**Installation:** No new `go get` required. [VERIFIED: STACK.md + go.mod analysis]

---

## Package Legitimacy Audit

No new external packages are introduced by Phase 29. All implementation uses the existing go.mod dependency tree + Go stdlib. [VERIFIED: STACK.md confirms "zero new go.mod entries"]

| Package | Status |
|---------|--------|
| All existing deps | Unchanged — already vetted |
| New external packages | None |

---

## Architecture Patterns

### System Architecture Diagram

```
export-envelopes:
  operator invokes tide export-envelopes <ns>/<project> --output bundle.tgz
       |
       ├─ K8s GET Project CR (read live UID, status)
       ├─ K8s LIST Milestone/Phase/Plan CRs (read UIDs, dependsOn, status)
       ├─ Create inspector pod (busybox:1.36, tar envelopes/ + artifacts/)
       │    subPath=<projectUID>, ReadOnly=true
       │    command: tar czf - -C /workspace envelopes/ artifacts/
       ├─ Stream pod stdout → tgz bytes → local bundle.tgz
       ├─ Delete inspector pod
       └─ Write bundle: {projects.yaml, milestones.yaml, phases.yaml, plans.yaml,
                          seed-manifest.json, SEED-OUTLINE.md, pvc-envelopes.tgz}
          (seed-manifest.json includes FQName→oldUID map + sha256 per envelope + D-03)

import-envelopes (live mode):
  operator invokes tide import-envelopes bundle.tgz --namespace <ns>
       |
       ├─ Unpack bundle (local temp dir or stream)
       ├─ Create seed ConfigMap in <ns> with seed-manifest.json as "manifest" key
       ├─ Create loader pod (busybox:1.36, RW PVC mount, stdin=true)
       │    subPath=<oldProjectUID>/workspace
       │    command: tar xzf - -C /workspace
       ├─ Stream pvc-envelopes.tgz → pod stdin via remotecommand/SPDY exec
       ├─ Delete loader pod
       └─ Write project.yaml to disk (spec.importSource populated)
          operator applies: tide apply project.yaml

import-envelopes (--dry-run):
  operator invokes tide import-envelopes bundle.tgz --dry-run
       |
       ├─ Unpack bundle locally (no K8s calls)
       ├─ Read seed-manifest.json
       ├─ For each envelope: ValidateAPIVersionKind + len(childCRDs)==childCount + sha256
       ├─ ComputeWaves(nodes from seed, edges from dependsOn) → cycle check
       └─ Print table: level | name | verdict (adopt/re-plan) | reason
```

### Recommended Project Structure

```
cmd/tide/
  export_envelopes.go        # newExportEnvelopesCmd() + exportEnvelopesRun() seam
  export_envelopes_run.go    # exportInspectorPodRunner func-var + defaultExportInspectorPodRunner
  import_envelopes.go        # newImportEnvelopesCmd() + importEnvelopesRun() seam
  import_envelopes_run.go    # loaderPodRunner func-var + defaultLoaderPodRunner
  subcommands.go             # ADD: root.AddCommand(newExportEnvelopesCmd()) + newImportEnvelopesCmd()

pkg/bundle/                  # (Claude's discretion: shared or per-cmd)
  bundle.go                  # BundleWriter/BundleReader for round-trip symmetry
  seed.go                    # seedManifest/seedEntry types + sha256 logic

test/integration/kind/
  import_resume_test.go      # new E2E test (both tiers)
  testdata/
    import-small-fixture/    # purpose-built small fixture for drain-to-Succeeded tier
      projects.yaml, milestones.yaml, phases.yaml, plans.yaml
      seed-manifest.json
      pvc-envelopes.tgz
```

### Pattern: cobra RunE + testable seam (mirrors artifact-get)

```go
// export_envelopes.go
func newExportEnvelopesCmd() *cobra.Command {
    var timeout time.Duration
    var pvcName string
    var outputPath string
    var outputDir bool
    c := &cobra.Command{
        Use:   "export-envelopes <namespace>/<project>",
        Short: "Export a Project's planner envelopes to a portable bundle",
        RunE: func(cmd *cobra.Command, args []string) error {
            return runExportEnvelopes(cmd, args, timeout, pvcName, outputPath, outputDir)
        },
    }
    // flags: --timeout, --pvc, --output, --dir
    return c
}

func runExportEnvelopes(cmd *cobra.Command, args []string, ...) error {
    k, _ := K8sClient()
    cfg, _ := RESTConfig()
    cs, _ := kubernetes.NewForConfig(cfg)
    ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
    defer cancel()
    return exportEnvelopesRun(ctx, k, cs, args[0], pvcName, outputPath, outputDir, cmd.OutOrStdout(), cmd.ErrOrStderr())
}
// Source: mirrors artifact_get.go:35-91 [VERIFIED: live read]
```

```go
// The func-var seam (mirrors inspectorPodRunner at artifact_get_run.go:57)
var exportInspectorPodRunner = defaultExportInspectorPodRunner
var loaderPodRunner = defaultLoaderPodRunner
```

### Pattern: seedManifest struct for bundle reader/writer

The `seedEntry` and `seedManifest` structs are already defined in `internal/controller/import_controller.go:92-130`. The planner must decide whether to:
1. Re-declare them in `pkg/bundle/` (avoids importing `internal/controller` from `cmd/`) — recommended per Go visibility conventions.
2. Move them to `api/v1alpha2/` or `pkg/` where both CLI and controller can import them.

**Recommended:** Re-declare in `pkg/bundle/seed.go` with an added `SHA256` field on `BundleEntry` (the CLI-facing superset of `seedEntry`). The ImportController's `seedEntry` stays unchanged. [ASSUMED: planner should confirm this matches Go package import rules]

### Anti-Patterns to Avoid

- **Streaming tgz via pod logs** for the loader pod: pod log streams are read-only (container stdout). Use `remotecommand.SPDY` for write-direction streaming.
- **Accepting wave CRs in the bundle**: D-13 — never export or import Wave objects.
- **Applying the Project from import-envelopes**: D-05 — stage only. The operator runs `tide apply`.
- **Using fieldSelector instead of label filter for Job assertions**: the existing pattern uses `client.MatchingLabels{"tideproject.k8s/role": "planner"}` — use this for E2E assertions.

---

## Validation Code — Offline Dry-Run Confirmed

`pkg/dispatch/envelope.go` imports only `"time"`. [VERIFIED: live grep]
`pkg/dag/kahn.go` imports only `"fmt"` and `"sort"`. [VERIFIED: live read]

Both are fully usable in `cmd/tide/` (a standalone binary) with no cluster/controller-runtime dependency. Dry-run validation can call them directly without any network connection.

**CycleError shape for dry-run reporting:**
```go
type CycleError struct {
    InvolvedNodes []NodeID  // sorted lexicographically — all stuck nodes
}
func (e *CycleError) Error() string // "cyclic DAG: nodes with unresolvable indegrees: [...]"
```
Source: `pkg/dag/errors.go:21-31` [VERIFIED: live read]

**Completeness check reuse:**
```go
// pkg/dispatch/envelope.go:407-415
func ValidateAPIVersionKind(apiVersion, kind, expectedKind string) error
// Returns *UnknownAPIVersionError or *UnknownKindError or nil
```

The dry-run reads `out.json` from the unpacked bundle, calls `ValidateAPIVersionKind`, checks `len(childCRDs) == childCount` (after the export-side repair), and verifies sha256. If any check fails, verdict = "re-plan" with the failure class in `reason`.

---

## E2E Kind Test Harness

**Source:** `test/integration/kind/suite_test.go` [VERIFIED: live read]

### How tests stand up the cluster

- `BeforeSuite`: `ensureKindCluster()` + `applyCRDs()` + `installCertManager()` + `applyController()` (helm install `--wait --timeout 5m`) + `ensureSigningKeySecret(kindNamespace)`.
- Tests use `applyFile("testdata/fixture.yaml")` via `exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "apply", "-f", path)` (line 1042-1050).
- AfterEach: `deleteNamespace(ns)`.
- `testing.Short()` skip is checked in `TestIntegrationKind` (line 119).

### How to invoke the real `tide` CLI binary (D-10)

The tests use `exec.CommandContext` for `kind`, `kubectl`, `helm`. The same pattern applies for `tide`:
```go
tideBin, err := exec.LookPath("tide")
Expect(err).NotTo(HaveOccurred(), "tide binary must be in PATH")
cmd := exec.CommandContext(ctx, tideBin,
    "--kubeconfig", kubeconfigPath,
    "export-envelopes", ns+"/"+projectName,
    "--output", bundlePath,
)
out, err := cmd.CombinedOutput()
Expect(err).NotTo(HaveOccurred(), "tide export-envelopes: %s", out)
```
The binary must be built and available in PATH before the test runs. The Makefile must build it as part of `make test-int-kind-prep`. [ASSUMED: test-int-kind-prep is where this build step goes — verify Makefile targets]

### "Zero planner Jobs dispatched" assertion

```go
// Filter by role=planner label, level label for specificity
jobs := &batchv1.JobList{}
Expect(k8sClient.List(ctx, jobs,
    client.InNamespace(ns),
    client.MatchingLabels{
        "tideproject.k8s/role":  "planner",
        "tideproject.k8s/level": "milestone",  // or phase — one per assertion
    },
)).To(Succeed())
Expect(jobs.Items).To(BeEmpty(), "no milestone planner Jobs should be dispatched for imported levels")
```
Source: `chaos_resume_test.go:223-226` shows `MatchingLabels{"tideproject.k8s/role": "executor"}` pattern. [VERIFIED: live read]

### "$0 re-paid planning cost" assertion

```go
var project tideprojectv1alpha2.Project
Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: projectName}, &project)).To(Succeed())
// CostSpentCents should be 0 after import completes and before plan planners fire
Expect(project.Status.Budget.CostSpentCents).To(Equal(int64(0)),
    "imported envelopes must not re-pay planning cost (D-14)")
```
`Project.Status.Budget.CostSpentCents` is the authoritative cost field. [VERIFIED: api/v1alpha2/project_types.go:270]

### Small purpose-built fixture (D-11 tier 1 — drain to all-Milestones-Succeeded)

Minimal viable fixture for a complete drain test:
- 1 Project → 1 Milestone → 1 Phase → 2 Plans → 2 Tasks (one Wave, independent tasks)
- Stub subagent handles planner + executor roles
- All envelopes authored by stub subagent (no salvage complexity)
- Export → import → tide apply → watch all Milestones reach Succeeded

The existing `bare-project.yaml` testdata (58 lines) demonstrates the minimum shape. [VERIFIED: live read]. The small E2E fixture needs a Project with importSource → seed ConfigMap → envelopes at old-UID paths.

**Critical design for small fixture:** The fixture's envelopes must already have `childCount` set correctly (the stub subagent writes them). Export of a stub-generated run should NOT hit the childCount absence issue — only the salvage fixture (from pre-09-08 code) has this problem.

### Testing.Short() + long-test gating (D-12)

```go
var _ = Describe("Import resume E2E", Label("kind", "long"), func() {
    BeforeEach(func() {
        skipIfCRDsOnlyMode()
        if testing.Short() {
            Skip("Skipping long import-resume test in short mode")
        }
    })
    // ...
})
```
The `testing.Short()` check inside `BeforeSuite` gates the entire kind suite. Individual heavy specs can add an additional guard per the convention shown in `wave_test.go`.

---

## Common Pitfalls

### Pitfall 1: childCount absent in legacy salvage envelopes
**What goes wrong:** `isEnvelopeComplete` rejects all 18 successful salvage envelopes (ChildCount=0, len(ChildCRDs)=3-6). Every envelope treated as incomplete. Milestone/phase planners re-run. $90 of already-paid planning cost is paid again.
**Why it happens:** The salvage was produced before plan 09-08 added `childCount`. The field has `omitempty`, so old envelopes lack it. Decoded `ChildCount` defaults to 0.
**How to avoid:** Export repairs `childCount = len(childCRDs)` in each out.json when `ChildCount == 0 AND len(ChildCRDs) > 0`. Add a warning log: "repaired legacy childCount for envelope <uid>".
**Warning signs:** tide-import's stderr shows "incomplete" for envelopes that have non-empty `childCRDs` and `exitCode=0`.

### Pitfall 2: Loader pod needs remotecommand exec, not log stream
**What goes wrong:** Developer tries to use `GetLogs` pattern from artifact-get for loader pod stdin — doesn't work. Log streaming is read-only (container stdout → CLI).
**Why it happens:** The export inspector pod is a natural template for the loader pod, but the data direction is reversed.
**How to avoid:** Use `k8s.io/client-go/tools/remotecommand.NewSPDYExecutor` with `StreamOptions{Stdin: tgzReader}`. RBAC needs `pods/exec: create` not `pods/log: get`.

### Pitfall 3: Loader pod tgz must unpack at workspace level, not PVC root
**What goes wrong:** Loader pod unpacks tgz to `/workspace/` but the PVC subPath mounts `<oldProjectUID>/workspace/` — so resulting paths are `<PVC>/<oldUID>/workspace/envelopes/...` which is correct. If the tgz root contains the `workspace/` directory prefix itself, paths double-nest: `envelopes/workspace/envelopes/...`.
**Why it happens:** The salvage `pvc-envelopes.tgz` has `envelopes/` at its root (correct). If export changes the tgz structure, paths break.
**How to avoid:** The tgz root must be workspace content directly: `envelopes/`, `artifacts/`. The loader pod mounts PVC at `/workspace` with `subPath=<oldUID>/workspace` and unpacks with `tar xzf - -C /workspace`.

### Pitfall 4: FQName collisions when subtrees reuse short plan names
**What goes wrong:** Plans named `plan-01-foo` appear in phase-01, phase-02, phase-03. Bare-name keying aliases them. The rekey table maps the wrong envelope.
**Why it happens:** The dogfood salvage reuses `plan-01-*` / `phase-01-*` across milestones.
**How to avoid:** FQName = `<milestone-name>/<phase-name>/<plan-name>` for plans, `<milestone-name>/<phase-name>` for phases. Phase 28 D-07 specifies this; export must generate it correctly. The ImportController already uses `msSeed.FQName` for the rekey table (line 415 in `import_controller.go`).

### Pitfall 5: spec.raw schema stripping for Plan children is EXPECTED behavior
**What goes wrong:** Developer notices that Plan children in salvage envelopes carry `objective`, `wave`, `filesTouched` fields that are NOT in `PlanSpec`. `convertSpecRaw` strips them silently. Developer treats this as a bug.
**Why it happens:** `PlanSpec` in v1alpha2 has only `phaseRef`, `dependsOn`, `sharedContext`. Extra fields in the salvage's Plan child specs (from the planning planner) are schema debris.
**How to avoid:** This is correct behavior — PlanSpec doesn't need these fields; the orchestrator creates CRs from the seed, not from the plan-level envelope children. The objective lives in the `children/<name>.json` prompt files on the PVC, not in the Plan CRD spec. No bug.

### Pitfall 6: Dry-run must handle the tgz OR unpacked directory
**What goes wrong:** Dry-run unpack logic only handles one format. D-02 specifies `.tgz` (default) and `--dir` (unpacked). Dry-run must work with both.
**How to avoid:** If input is `.tgz`, extract to a temp dir first, then run validation on the extracted tree. If input is a directory, validate directly. Clean up temp dir on exit.

### Pitfall 7: Kind E2E test must build the tide binary first
**What goes wrong:** E2E test calls `exec.LookPath("tide")` and fails because the binary isn't in PATH.
**How to avoid:** Add a `make build-tide` step to `make test-int-kind-prep` (the existing prep target). Or check `os.Getenv("TIDE_BINARY")` for an override path.

---

## Runtime State Inventory

Phase 29 is a new-code phase (CLI verbs + kind test). It does not rename anything.

| Category | Items Found | Action Required |
|----------|-------------|-----------------|
| Stored data | None — no new persistent state | — |
| Live service config | None | — |
| OS-registered state | None | — |
| Secrets/env vars | None new | — |
| Build artifacts | tide binary must be in PATH for kind E2E | Add build step to make test-int-kind-prep |

---

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| kind | kind E2E test | check at test runtime | v0.31+ | Skip if not found (suite_test.go:134) |
| docker | kind image loading | check at test runtime | any | Skip if not found (suite_test.go:139) |
| busybox:1.36 | inspector + loader pods | loaded in kind cluster | 1.36 | No fallback — must be in kind image cache |
| kubectl | applyFile helper | check at test runtime | any | — |
| helm | applyController | check at test runtime | 3.x | — |
| tide binary | D-10 real CLI test | must build before test | dev | Build via make first |

**Missing dependencies with no fallback:**
- `tide` binary in PATH for the E2E test — must be built first. Not currently in `make test-int-kind-prep`.

---

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Ginkgo v2.28 + Gomega |
| Config file | `test/integration/kind/suite_test.go` (no separate config file) |
| Quick run command | `go test ./cmd/tide/... -run TestExportImport` (unit tests for cobra commands) |
| Full suite command | `make test-int` (Layer B kind suite, includes new import_resume_test.go) |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| TOOL-01-unit | export-envelopes cobra command parses flags correctly | unit | `go test ./cmd/tide/... -run TestExportEnvelopes` | No — Wave 0 |
| TOOL-01-unit | import-envelopes cobra command parses flags correctly | unit | `go test ./cmd/tide/... -run TestImportEnvelopes` | No — Wave 0 |
| TOOL-01-unit | dry-run validates bundle offline (no cluster) | unit | `go test ./cmd/tide/... -run TestDryRun` | No — Wave 0 |
| TOOL-01-unit | cycle detection reports CycleError.InvolvedNodes | unit | `go test ./cmd/tide/... -run TestDryRunCycle` | No — Wave 0 |
| TOOL-02-kind | small fixture: import → drain → all-Milestones-Succeeded | kind integration | `make test-int` (Label("kind","long")) | No — Wave 0 |
| TOOL-02-kind | salvage-20260618: adoption + zero planner Jobs + $0 cost | kind integration | `make test-int` (Label("kind","long")) | No — Wave 0 |

### Sampling Rate

- Per task commit: `go test ./cmd/tide/... -run TestExportEnvelopes -run TestImportEnvelopes -run TestDryRun -v -count=1`
- Per wave merge: `go test ./cmd/tide/... ./pkg/bundle/... -count=1`
- Phase gate: `make test-int` green before `/gsd:verify-work`

### Wave 0 Gaps

- `cmd/tide/export_envelopes_test.go` — unit tests for exportEnvelopesRun seam (covers flag parsing, inspector pod command shape, seed manifest generation)
- `cmd/tide/import_envelopes_test.go` — unit tests for importEnvelopesRun seam (covers dry-run table output, cycle detection, sha256 check, loader pod command shape)
- `pkg/bundle/bundle_test.go` (if shared pkg) — round-trip: write bundle → read bundle → contents match
- `test/integration/kind/import_resume_test.go` — the E2E test (both tiers)
- `test/integration/kind/testdata/import-small-fixture/` — small fixture for drain tier

---

## Security Domain

Phase 29 is an operator CLI tool (not a server-side surface). Security considerations are operational:

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | No — relies on kubeconfig chain | K8s RBAC via kubeconfig |
| V3 Session Management | No | Stateless CLI |
| V4 Access Control | Yes (partial) | Namespace-scoped loader pod — only mounts own namespace's PVC |
| V5 Input Validation | Yes | Path traversal defense (inherits from artifact_get_run.go validateArtifactPath); tgz extraction must guard against path traversal (zip slip) |
| V6 Cryptography | No (sha256 is integrity, not secrecy) | crypto/sha256 stdlib |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Path traversal in tgz extraction | Tampering | Reject entries with `../` in path; use `filepath.Clean` + prefix check before `os.Create` |
| Loader pod writing outside project subPath | Elevation | PVC subPath enforces mount-level containment; no subPath escape possible from inside the pod |
| Shell injection via archive entry names | Tampering | The tar command is not executed via shell (`sh -c`) — no injection surface |
| UID poisoning in seed manifest (wrong oldUID) | Tampering | export reads UIDs from live CRs; seed manifest is output of export, not external input |

---

## Open Questions

1. **Loader pod write-direction streaming API**
   - What we know: `remotecommand.NewSPDYExecutor` is the standard pattern for pod exec streaming. `StreamOptions.Stdin` accepts any `io.Reader`.
   - What's unclear: The exact URL construction for the exec subresource when the container command takes stdin input (not a long-running shell). The busybox `tar` command reads from stdin and exits — verify whether `podRunning` wait is still needed before the exec call, or whether exec can fire as soon as the pod is created.
   - Recommendation: Prototype the loader pod with a small spike binary; test against a real kind cluster before authoring the plan.

2. **Makefile target for tide binary in E2E path**
   - What we know: `make test-int-kind-prep` exists for image loading. The tide binary is currently not built as part of it.
   - What's unclear: Whether there's a `make build-cli` target or whether it's `go build ./cmd/tide/`.
   - Recommendation: Add `go build -o bin/tide ./cmd/tide/` to `test-int-kind-prep`, or export `TIDE_BINARY=$(pwd)/bin/tide` before running kind tests.

3. **Export-side childCount repair vs import-side guard relaxation**
   - What we know: Collision #1 — all 18 salvage planner envelopes lack `childCount`. Both solutions work.
   - What's unclear: User preference on which layer owns the repair.
   - Recommendation: Export-side repair is cleanest — export is the Phase 29 surface, Phase 28 code stays untouched. The planner should default to this unless the user objects.

---

## Sources

### Primary (HIGH confidence)
- `cmd/tide/artifact_get_run.go` — inspectorPodRunner func-var seam, busybox pod spec, RBAC, log stream pattern (live read)
- `cmd/tide/artifact_get.go` — cobra command constructor pattern (live read)
- `cmd/tide/subcommands.go` — registerSubcommands pattern (live read)
- `cmd/tide/root_flags.go` — K8sClient(), RESTConfig(), registerPersistentFlags() (live read)
- `internal/controller/import_controller.go` — seedEntry/seedManifest structs, reconcileCreatingCRs, OldSubPath construction (live read)
- `internal/controller/import_jobspec.go` — BuildImportJob, ImportJobOptions, OldSubPath/NewSubPath mount semantics (live read)
- `cmd/tide-import/main.go` — isEnvelopeComplete, childKindAllowlist, convertSpecRaw, rekeyEntry (live read)
- `api/v1alpha2/import_types.go` — ImportSourceRef schema (live read)
- `pkg/dispatch/envelope.go` — ValidateAPIVersionKind, EnvelopeOut struct, ChildCount field (live read)
- `pkg/dispatch/childcrd.go` — ChildCRDSpec struct with Spec runtime.RawExtension (live read)
- `pkg/dag/kahn.go` + `pkg/dag/errors.go` — ComputeWaves, CycleError (live read)
- `internal/dispatch/podjob/backend.go` — FilesystemEnvelopeReader.ReadOut path contract (live read)
- `test/integration/kind/suite_test.go` — BeforeSuite, applyFile, exec.CommandContext pattern (live read)
- `examples/projects/dogfood/salvage-20260618/` — pvc-envelopes/, projects.yaml, SEED-OUTLINE.md (live ls + Python analysis)

### Secondary (MEDIUM confidence)
- `.planning/research/STACK.md` — zero new go.mod entries confirmed
- `.planning/research/ARCHITECTURE.md` — partial salvage behavior (incomplete envelopes → fresh planner)
- `.planning/research/PITFALLS.md` — R-06 (Wave schema), R-13 (budget double-count)

### Tertiary (LOW confidence)
- [ASSUMED] `pods/exec: create` is the required RBAC verb for loader pod stdin streaming — verify against Kubernetes RBAC reference
- [ASSUMED] `pkg/bundle/` as shared package for bundle reader/writer — verify Go import conventions don't conflict with `internal/` visibility

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `pods/exec: create` is the correct RBAC verb for the loader pod | Loader Pod section | Wrong verb → pod exec permission denied at runtime |
| A2 | `k8s.io/client-go/tools/remotecommand.NewSPDYExecutor` is the correct API for stdin streaming to a running pod | Loader Pod section | Wrong API → cannot write tgz into pod stdin |
| A3 | `make test-int-kind-prep` is the correct place to add the `tide` binary build step | Validation Architecture section | Wrong target → binary not built before kind tests run |
| A4 | Re-declaring `seedEntry`-equivalent in `pkg/bundle/` avoids `internal/controller` import conflicts | Architecture Patterns section | Import conflict → build failure |

---

## Metadata

**Confidence breakdown:**
- Inspector-pod reuse surface: HIGH — code read directly
- PVC path contract: HIGH — FilesystemEnvelopeReader.ReadOut read directly
- Seed ConfigMap schema: HIGH — seedEntry/seedManifest read directly from import_controller.go
- childCount absence in salvage: HIGH — verified by Python analysis of all 59 envelopes
- Plan envelope completeness profile: HIGH — Python enumerated all exit codes by level
- Dry-run offline capability: HIGH — import chain verified, stdlib-only deps confirmed
- Loader pod write-direction API: MEDIUM — remotecommand package is correct mechanism but exact call sequence not prototyped
- E2E test binary availability: MEDIUM — exec.LookPath pattern confirmed; build step location assumed

**Research date:** 2026-06-19
**Valid until:** 2026-07-19 (stable Go/K8s ecosystem; k8s.io/client-go API very stable)
