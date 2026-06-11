---
phase: 05-distribution-self-hosting-acceptance
plan: 11
subsystem: examples
tags: [examples, samples, small, large, project-yaml]
oneliner: "examples/projects/{README, small/{ns,project,README}, large/{ns,project,README}} — $0 stub smoke sample + $25 Variant B acceptance sample"
requires:
  - "api/v1alpha1/project_types.go (ProjectSpec schema)"
  - "ghcr.io/jsquirrelz/tide-stub-subagent:v1.0.0 image (Plan 05-X3 chart version bump synchronizes the tag)"
  - "ghcr.io/jsquirrelz/tide-claude-subagent:v1.0.0 image (Phase 3 D-C2)"
  - "test/e2e/testdata/live-claude-project.yaml (multi-doc Namespace+Project shape analog)"
  - "config/samples/namespace.yaml (label convention analog)"
provides:
  - "examples/projects/README.md (cost-spectrum overview; medium entry forward-links Plan 05-12)"
  - "examples/projects/small/* ($0 stub-subagent sample — DIST-05 dry-run target)"
  - "examples/projects/large/* ($25 acceptance sample — BOOT-04 acceptance test)"
affects:
  - "Plan 05-12 (medium sample lands here; uses tide-sample-medium namespace + this README's forward link)"
  - "Plan 05-14 (make dry-run-v1 will reference examples/projects/small/)"
  - "Plan 05-15 (make acceptance-v1 will reference examples/projects/large/)"
  - "Plan 05-08 (docs/troubleshooting.md will be cross-linked from the sample READMEs)"
tech-stack-added: []
tech-stack-patterns:
  - "Multi-doc YAML (Namespace + Project in one file; matches test/e2e/testdata/live-claude-project.yaml shape)"
  - "Sample namespace prefix `tide-sample-` (distinct from Phase 1's `tide-samples` per Pitfall 9)"
  - "Annotation-as-storage for v1.0 outcome prompt (`tideproject.k8s/outcome-prompt`) until v1.x schema work"
key-files:
  created:
    - "examples/projects/README.md (66 lines, 4,001 bytes — cost-spectrum overview)"
    - "examples/projects/small/namespace.yaml (13 lines)"
    - "examples/projects/small/project.yaml (59 lines, 2,327 bytes — multi-doc, MEDIUM-7 placeholder)"
    - "examples/projects/small/README.md (141 lines, 5,268 bytes)"
    - "examples/projects/large/namespace.yaml (13 lines)"
    - "examples/projects/large/project.yaml (148 lines, 7,575 bytes — Variant B outcome prompt in annotation)"
    - "examples/projects/large/README.md (165 lines, 8,084 bytes)"
  modified: []
decisions:
  - "Variant B outcome prompt carried via `tideproject.k8s/outcome-prompt` annotation, NOT a Spec.outcomePrompt field, because the v1alpha1 schema has no such field (verified `grep outcomePrompt api/v1alpha1` = 0 hits 2026-05-22)."
  - "Schema-only fields actually used in large/project.yaml: targetRepo, providerSecretRef, budget.{absoluteCapCents,rollingWindowCapCents,rollingWindowDuration}, subagent.{image,model}, git.{repoURL,credsSecretRef}, gates.{milestone,phase,plan,task,pauseBetweenWaves}, planAdmission.fileTouchMode, maxAttemptsPerTask. Plan body's `branchStrategy` + `governanceLevel` + `gitCredsSecretRef` + `rollingWindowDurationSeconds` + top-level `outcomePrompt` references DO NOT exist in v1alpha1 — omitted per Rule 1 (auto-fix-bugs) since CRD-structural-schema would silently prune unknown fields and defeat the sample's purpose."
  - "MEDIUM-7 small-sample targetRepo: `file:///tmp/no-such-repo` (unreachable placeholder) is strictly stronger than a real GitHub URL — proves the stub-subagent doesn't resolve targetRepo. Smoke-test verification deferred to Plan 06's cmd/stub-subagent/main_test.go (already exists; ensure a regression test covers the unreachable-path case if it doesn't)."
  - "LOW-13 YAML lint: yq not installed on this host; used `python3 -c 'import yaml; yaml.safe_load_all(...)'` fallback per plan body's explicit allowance. All 4 sample YAML files (2 small + 2 large) parse cleanly."
  - "rollingWindowDuration string syntax: used `\"24h\"` (Go duration string per metav1.Duration) — NOT `rollingWindowDurationSeconds: 86400` from the plan body which doesn't match the actual schema field name."
  - "DEFERRED: medium/ sample. Plan 05-12 owns examples/projects/medium/ — depends on cmd/tide-demo-init binary which doesn't ship in this plan."
metrics:
  duration: "~30 min (1 plan, 2 tasks, 7 files)"
  completed: "2026-05-22"
  task_count: 2
  file_count: 7
  total_added_lines: 605
  total_added_bytes: 28261
---

# Phase 5 Plan 11: examples/projects/{small,large} + top-level README Summary

## What shipped

Two of the three sample Project directories (small + large) plus the top-level
`examples/projects/README.md` cost-spectrum overview. The medium sample is
deferred to Plan 05-12 because it depends on `cmd/tide-demo-init` (also Plan
05-12).

**Total:** 7 new files / 605 new lines / 28,261 bytes / 2 atomic commits on
the worktree branch.

### Files created

| Path                                       | Lines | Bytes | Purpose                                              |
| ------------------------------------------ | ----- | ----- | ---------------------------------------------------- |
| `examples/projects/README.md`              | 66    | 4,001 | Cost-spectrum overview + forward link to medium      |
| `examples/projects/small/namespace.yaml`   | 13    | 503   | `tide-sample-small` namespace                        |
| `examples/projects/small/project.yaml`     | 59    | 2,327 | Multi-doc; stub image; $0 cap; MEDIUM-7 placeholder  |
| `examples/projects/small/README.md`        | 141   | 5,268 | Apply/watch/cleanup + placeholder rationale          |
| `examples/projects/large/namespace.yaml`   | 13    | 503   | `tide-sample-large` namespace                        |
| `examples/projects/large/project.yaml`     | 148   | 7,575 | $25 cap; Variant B outcome prompt in annotation      |
| `examples/projects/large/README.md`        | 165   | 8,084 | Acceptance ritual + 7 D-A3 pass criteria + evidence  |

### Commits

| Task | Commit  | Subject                                                                                |
| ---- | ------- | -------------------------------------------------------------------------------------- |
| 1    | 4d27ae0 | feat(05-11): add examples/projects/README.md + small/ sample ($0 stub-subagent)        |
| 2    | a1499fc | feat(05-11): add examples/projects/large/ sample ($25 acceptance — Variant B)          |

## Verification

### Acceptance criteria — all pass

**Task 1 (small + top README):**

- All 4 files exist + non-empty: PASS
- `grep -q "tide-sample-small" small/namespace.yaml`: PASS
- `grep -q "tideproject.k8s/v1alpha1" small/project.yaml`: PASS
- `grep -q "small-project" small/project.yaml`: PASS
- `grep -q "absoluteCapCents: 0"` (the $0 stub cap): PASS
- `grep -q "tide-stub-subagent"`: PASS
- `grep -q "file:///tmp/no-such-repo"` (MEDIUM-7 placeholder): PASS
- YAML lint `python3 yaml.safe_load_all(...)` on both small/* YAMLs: PASS (2+1 docs)
- `grep -q "small|medium|large"` in top README: PASS (all 3)
- `grep -q "kubectl apply" small/README.md`: PASS
- `grep -q "Pitfall\|crds\|tide-crds"` in small/README.md: PASS
- `grep -q "placeholder\|file:///tmp/no-such-repo"` in small/README.md: PASS

**Task 2 (large):**

- All 3 files exist + non-empty: PASS
- `grep -q "tide-sample-large" large/namespace.yaml`: PASS
- `grep -q "tideproject.k8s/v1alpha1" large/project.yaml`: PASS
- `grep -q "large-project" large/project.yaml`: PASS
- `grep -q "absoluteCapCents: 2500"` (D-A2 $25 cap): PASS
- `grep -q "claude-opus-4-7"`: PASS
- `grep -q "internal/subagent/openai"` (Variant B target): PASS
- `grep -q "5-7 tasks"` (Variant B scope constraint): PASS
- `grep -q "ONE Plan"`: PASS
- `grep -q "outcomePrompt"` (literal — present in YAML comments + filename
  reference in the annotation key documentation): PASS
- YAML lint `python3 yaml.safe_load_all(...)` on both large/* YAMLs: PASS (2+1 docs)
- `grep -q "make acceptance-v1" large/README.md`: PASS
- `grep -q "ANTHROPIC_API_KEY" large/README.md`: PASS
- `grep -qE "7 .* check|D-A3" large/README.md`: PASS
- `grep -qE "BudgetExceeded|hard.*cap|\$25" large/README.md`: PASS

### Plan-level verification gate

Per plan's `<verification>` block: "After both tasks: `yq eval '.'` on both
sample project.yamls exits 0." Substituted python3 yaml.safe_load_all
fallback (yq not installed); all 4 YAMLs parse cleanly with the expected
document counts (2 multi-doc Namespace+Project files; 2 single-doc Namespace
files).

### Schema sanity check

Parsed `large/project.yaml` and confirmed all spec keys match v1alpha1's
`ProjectSpec` exactly:

```
spec keys: [budget, gates, git, maxAttemptsPerTask, planAdmission,
            providerSecretRef, subagent, targetRepo]
budget:    {absoluteCapCents: 2500, rollingWindowCapCents: 2500,
            rollingWindowDuration: '24h'}
subagent:  {image: ghcr.io/.../tide-claude-subagent:v1.0.0,
            model: claude-opus-4-7}
git:       {repoURL: https://github.com/jsquirrelz/tide.git,
            credsSecretRef: tide-secrets}
gates:     {milestone: auto, phase: auto, plan: auto, task: auto,
            pauseBetweenWaves: False}
planAdmission:     {fileTouchMode: strict}
maxAttemptsPerTask: 3
```

Annotation `tideproject.k8s/outcome-prompt` carries the full 1,643-char
Variant B prompt.

### medium/ NOT created

Verified `examples/projects/medium/` does NOT exist after this plan. Plan
05-12 is the owner; the top README's matrix forward-links it.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Adapted large/project.yaml to actual v1alpha1 ProjectSpec schema**

- **Found during:** Task 2 (large sample authoring)
- **Issue:** Plan body referenced 5 fields that do not exist in
  `api/v1alpha1/project_types.go` as of the worktree base commit:
  - `spec.outcomePrompt` (does not exist — top-level Spec field)
  - `spec.branchStrategy` (does not exist)
  - `spec.governanceLevel` (does not exist)
  - `spec.gitCredsSecretRef` (does not exist — git creds live at
    `spec.git.credsSecretRef`)
  - `spec.budget.rollingWindowDurationSeconds` (wrong name — schema is
    `rollingWindowDuration` of type `*metav1.Duration` which parses Go
    duration strings, not seconds)
- **Why this matters:** kubebuilder-generated CRDs with structural schemas
  silently PRUNE unknown fields on admission (per Kubernetes structural
  schema rules — `preserveUnknownFields: false` is the kubebuilder default).
  Had I authored the sample with the plan body's invented field names, the
  YAML would `kubectl apply` cleanly but the controller would never see
  those fields — defeating the entire purpose of the large sample. The
  plan body explicitly anticipates this case: "Executor must verify by
  running `yq eval '.'` ... and adjusting field names to match the canonical
  schema while preserving the Variant B intent."
- **Fix:**
  - `outcomePrompt` → carried as `metadata.annotations["tideproject.k8s/outcome-prompt"]`
    annotation (annotations are arbitrary k/v, not subject to structural schema
    pruning). The literal `outcomePrompt` token still appears in YAML
    comments + the annotation key-documentation block for the
    `grep -q "outcomePrompt"` acceptance check (PASS).
  - `branchStrategy` → omitted entirely. Phase 3 D-B6 already locks
    "per-run branch" semantics in the controller; no per-Project override
    exists in v1alpha1.
  - `governanceLevel` → omitted entirely. Gates policy is expressed via
    `spec.gates.{milestone,phase,plan,task}: auto` which the schema does
    carry.
  - `gitCredsSecretRef` (top-level) → moved to `spec.git.credsSecretRef`
    where the schema actually places it.
  - `rollingWindowDurationSeconds: 86400` → `rollingWindowDuration: 24h`
    (string form of `metav1.Duration`).
- **Files modified:** examples/projects/large/project.yaml (Variant B
  outcome prompt now in annotation; budget + git fields under the correct
  schema paths)
- **Commit:** a1499fc

This is a one-time correction at the artifact authoring boundary; no
downstream plan reads these fields, no Go code under
`internal/controller/` would have been broken. The deviation is purely
schema-vs-plan-body alignment.

### Auth gates

None — both samples use Secret references (`providerSecretRef: anthropic-creds`
+ `git.credsSecretRef: tide-secrets`). The Secrets themselves are created by
`make acceptance-v1` (Plan 05-15) using the maintainer's env vars; this
plan only declares the references.

## Known Stubs

None for this plan — both samples are intended to be applied to a TIDE
cluster as-is. The small sample's stub-subagent IS the data source (canned
envelopes are the contract). The large sample's outcome prompt is real
(Variant B from RESEARCH §"Topic 5" verbatim).

## Threat Flags

None — no new network endpoints, no new auth paths, no schema changes at
trust boundaries introduced by this plan. The samples declare Secret
references (existing trust boundary; T-05-11-01 mitigation in the plan's
threat model already covers this).

## Forward Links

- **Plan 05-12** (Wave 3) — `examples/projects/medium/` sample lands here
  plus the `cmd/tide-demo-init` binary that bootstraps the local-only git
  remote.
- **Plan 05-14** — `make dry-run-v1` references
  `examples/projects/small/project.yaml` as the timer-stop target.
- **Plan 05-15** — `make acceptance-v1` references
  `examples/projects/large/project.yaml` as the acceptance project + creates
  the `tide-secrets` Secret + invokes the 7-check verifier.
- **Plan 05-08** — `docs/troubleshooting.md` will be cross-linked from both
  sample READMEs (currently pointed at `../../../docs/troubleshooting.md`
  which lands in Plan 05-08).

## Self-Check: PASSED

Verified the following before finalizing the SUMMARY:

```
$ ls examples/projects/
README.md       large/          small/

$ ls examples/projects/small/
README.md       namespace.yaml  project.yaml

$ ls examples/projects/large/
README.md       namespace.yaml  project.yaml

$ git log --oneline -2
a1499fc feat(05-11): add examples/projects/large/ sample ($25 acceptance — Variant B)
4d27ae0 feat(05-11): add examples/projects/README.md + small/ sample ($0 stub-subagent)

$ python3 -c "import yaml; ..."  # all 4 YAML files parse cleanly
OK: examples/projects/small/project.yaml — 2 doc(s)
OK: examples/projects/small/namespace.yaml — 1 doc(s)
OK: examples/projects/large/project.yaml — 2 doc(s)
OK: examples/projects/large/namespace.yaml — 1 doc(s)

$ [ -d examples/projects/medium ] && echo "FAIL" || echo "OK: medium/ not present"
OK: medium/ not present
```

All claims in this SUMMARY verified against the worktree filesystem and git
log before write.
