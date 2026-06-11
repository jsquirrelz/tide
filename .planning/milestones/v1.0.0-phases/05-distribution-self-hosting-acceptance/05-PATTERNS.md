# Phase 5: Distribution & Self-Hosting Acceptance — Pattern Map

**Mapped:** 2026-05-22
**Files analyzed:** 28 new/modified files (researcher's count, RESEARCH.md §"Recommended File Layout")
**Analogs found:** 23 / 28 (5 truly new patterns — LICENSE, NOTICE, OSS docs, DinD, Helm OCI publish)

## Orienting note for the planner

Phase 5 is the **v1.0 ship phase**. Almost nothing is "new logic" — it is:
- Doc authoring (4 new + 1 index + 1 README mod)
- Sample YAML authoring (3 sample directories + 1 fixture directory)
- Helm template additions (1 new template + 2 Chart.yaml version bumps + 1 resource-policy annotation)
- Release-pipeline extension (3 new jobs in `release.yaml`)
- Shell-script + Makefile additions (`make dry-run-v1` + `make acceptance-v1` + 3 hack scripts)
- One new tiny Go binary (`cmd/tide-demo-init/` + Dockerfile)
- One legal/OSS root file (`LICENSE` + `NOTICE`)

**The chart-is-fixed-contract rule (CLAUDE.md anti-pattern) is load-bearing.** Phase 5 ONLY touches `charts/tide/values.yaml` to add a single additive key (`projectNamespaces: []`, empty default). The hand-authored `hack/helm/tide-values.yaml` is the source-of-truth; plans must edit there and re-augment via `make helm-controller` (NOT edit `charts/tide/values.yaml` directly).

**Phase 04.1 P2.4 lock** — `workflow_dispatch:` only for live-LLM workflows, no `schedule:` cron. The new `v*-rc.*` dry-run gate workflow follows the same posture (tag-trigger, not cron-trigger).

**Phase 1 D-E1 invariant** — `charts/tide-crds/` is a **distinct chart**, not a subchart-as-folder under `charts/tide/`. Both bump in lockstep to `1.0.0`. Both publish to `oci://ghcr.io/jsquirrelz/tide-charts/{tide,tide-crds}` independently.

## File Classification

### P1 — OSS readiness (root legal/community files) — NO IN-REPO ANALOG

| New File | Role | Data Flow | Closest Analog | Match Quality |
|----------|------|-----------|----------------|---------------|
| `LICENSE` | legal | static | (none in-repo; pattern = `hack/boilerplate.go.txt` for license year/owner conventions) | no-analog |
| `NOTICE` | legal | static | (none; pattern = upstream Apache ASF NOTICE rules; researcher's `go-licenses report` audit feeds content) | no-analog |
| `CONTRIBUTING.md` | docs | static | (none in-repo; pattern = `docs/live-e2e.md` audience/scope/headings shape) | partial |
| `SECURITY.md` | docs | static | (none in-repo; pattern = `docs/gates.md` table-driven structure) | partial |

### P2 — README + docs (operator-journey) — DOC HEADING STYLE ANALOG

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `README.md` (prepend ~30-line Quickstart) | docs | static | `README.md` itself (existing line 1-43 is the title/acronym block; Quickstart goes ABOVE it) | self-reference |
| `docs/README.md` (new index) | docs | static | (none; pattern = `docs/live-e2e.md`'s "Scope of this doc" bulleted-link block) | partial |
| `docs/INSTALL.md` (new) | docs | static | `docs/git-hosts.md` (audience/scope/per-platform recipes structure) | exact |
| `docs/project-authoring.md` (new) | docs | static | `docs/cli.md` (verb-by-verb reference tables) | exact |
| `docs/troubleshooting.md` (new, ~12-row table) | docs | static | `docs/live-e2e.md` §"Troubleshooting" (lines 108-132 — `Symptom/Cause/Fix` block-paragraph format) AND `docs/rwx-drivers.md` (single-table-as-doc format) | exact |
| `docs/rbac.md` (new) | docs | static | `docs/rwx-drivers.md` (Markdown driver matrix table; lines 21-27) | exact |

### P3 — Helm chart additions

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `charts/tide/templates/per-namespace-rolebinding.yaml` (new) | chart-template | static | `charts/tide/templates/project-admin-rbac.yaml` (per-Kind RBAC) AND `charts/tide/templates/manager-rbac.yaml` (RoleBinding shape, lines 79-92) | exact |
| `charts/tide/Chart.yaml` (modified — version 0.1.0-dev → 1.0.0) | chart-meta | static | `charts/tide/Chart.yaml` itself (lines 11-12 are the bump target) AND `hack/helm/tide-chart.yaml` (source-of-truth that augment-script copies) | self-reference |
| `charts/tide-crds/Chart.yaml` (modified — version 0.1.0-dev → 1.0.0) | chart-meta | static | `charts/tide-crds/Chart.yaml` itself (lines 11-12) AND `hack/helm/tide-crds-chart.yaml` | self-reference |
| `charts/tide-crds/templates/*-crd.yaml` (modified — add `helm.sh/resource-policy: keep` annotation) | chart-template | static | `charts/tide-crds/templates/project-crd.yaml` (lines 1-8, `metadata.annotations` block) | self-reference |
| `charts/tide/values.yaml` (additive only — `projectNamespaces: []`) | chart-values | static | `hack/helm/tide-values.yaml` (canonical SOT) + Phase 4 dashboard-block additive pattern | exact |

### P4 — Examples (sample Projects + demo fixture)

| New File | Role | Data Flow | Closest Analog | Match Quality |
|----------|------|-----------|----------------|---------------|
| `examples/projects/README.md` (new) | docs | static | `docs/rwx-drivers.md` (overview-paragraph + matrix-table form) | partial |
| `examples/projects/small/project.yaml` (new) | sample-yaml | static | `config/samples/tide_v1alpha1_project.yaml` (lines 1-13) AND `test/e2e/testdata/live-claude-project.yaml` (multi-doc with namespace/secret/Project) | exact |
| `examples/projects/small/namespace.yaml` (new) | sample-yaml | static | `config/samples/namespace.yaml` (lines 1-11, labels convention) | exact |
| `examples/projects/small/README.md` (new) | docs | static | (none in-repo; pattern = `docs/live-e2e.md` audience/scope intro + apply-and-verify recipe) | partial |
| `examples/projects/medium/project.yaml` (new) | sample-yaml | static | `test/e2e/testdata/live-claude-project.yaml` (lines 47-87 — Project with budget + providerSecretRef + subagent + git) | exact |
| `examples/projects/medium/namespace.yaml` (new) | sample-yaml | static | `config/samples/namespace.yaml` | exact |
| `examples/projects/medium/demo-remote-pvc.yaml` (new) | sample-yaml | static | `charts/tide/templates/projects-pvc.yaml` (referenced; PVC shape) | partial |
| `examples/projects/medium/demo-remote-init-job.yaml` (new) | sample-yaml | event-driven | (none in-repo for in-cluster init-from-fixture Job; nearest is Phase 2 D-G2 init-Job pattern) | partial |
| `examples/projects/medium/README.md` (new) | docs | static | partial — same as small/ | partial |
| `examples/projects/large/project.yaml` (new) | sample-yaml | static | `test/e2e/testdata/live-claude-project.yaml` (lines 47-87 — closest budget/subagent/git shape; large/ uses `costCeilingCents: 2500` per D-A2) | exact |
| `examples/projects/large/namespace.yaml` (new) | sample-yaml | static | `config/samples/namespace.yaml` | exact |
| `examples/projects/large/README.md` (new) | docs | static | partial — same as small/ | partial |
| `examples/tide-demo-fixture/README.md` (new — MIT-licensed inline) | docs | static | (none; tiny scaffold convention — MIT inline header) | no-analog |
| `examples/tide-demo-fixture/go.mod` (new) | scaffold | static | `go.mod` (root) + any minimal Go module shape | exact |
| `examples/tide-demo-fixture/go.sum` (new) | scaffold | static | `go.sum` (root) | exact |
| `examples/tide-demo-fixture/main.go` (new) | scaffold | static | `cmd/stub-subagent/main.go` (minimal entry-point shape; or any single-file Go demo) | partial |
| `examples/tide-demo-fixture/main_test.go` (new) | scaffold-test | static | `cmd/stub-subagent/main_test.go` (minimal `_test.go` shape) | partial |

### P5 — `cmd/tide-demo-init/` (new Go binary — local-only git remote bootstrap)

| New File | Role | Data Flow | Closest Analog | Match Quality |
|----------|------|-----------|----------------|---------------|
| `cmd/tide-demo-init/main.go` (new) | binary | file-I/O + git-remote-bootstrap | `cmd/tide-push/main.go` (Go binary with package-doc header; cobra/flag-based CLI; pkg/git import for git operations — lines 1-100) | exact |
| `cmd/tide-demo-init/README.md` (new) | docs | static | (none; tiny CLI README — short, operator-facing, focused on `--bootstrap-dir` flag) | partial |
| `images/tide-demo-init/Dockerfile` (new, IF Topic 4 Option 2 chosen) | image | static | `images/tide-push/Dockerfile` (lines 1-43 — two-stage golang:1.26-alpine + distroless/static:nonroot) | exact |

### P6 — Release pipeline + Makefile additions

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `.github/workflows/release.yaml` (modified — add 3 jobs: helmify-verify, dry-run-gate, chart-publish) | workflow | event-driven | `.github/workflows/ci.yaml` `helm-lint` job (lines 108-159 — helmify regenerate + lint + drift-check) AND `.github/workflows/live-e2e.yml` (workflow_dispatch posture for live-LLM gating) AND `.github/workflows/release.yaml` itself (existing goreleaser job, lines 20-63) | exact |
| `Makefile` (modified — add `dry-run-v1`, `acceptance-v1` targets) | makefile | request-response | `Makefile` `test-e2e-live` target (lines 190-197 — env-var fail-fast + script invocation) AND `Makefile` `test-int-kind-prep` (lines 153-167 — multi-image build + kind load DinD-adjacent pattern) | exact |

### P7 — `hack/scripts/` shell scripts (new directory)

| New File | Role | Data Flow | Closest Analog | Match Quality |
|----------|------|-----------|----------------|---------------|
| `hack/scripts/dry-run-v1.sh` (new) | shell-script | event-driven | `hack/helm/augment-tide-chart.sh` (lines 1-30 — `set -euo pipefail`, `REPO_ROOT` derivation pattern) AND `Makefile` `test-int-kind-prep` (DinD `kind load` + `docker build` orchestration) AND RESEARCH §"dry-run-v1.sh skeleton" (lines 635-715, the canonical reference) | exact |
| `hack/scripts/acceptance-verify.sh` (new — 7-check D-A3 verifier) | shell-script | request-response | `hack/helm/augment-tide-chart.sh` (boilerplate + idempotency) AND `Makefile` `verify-no-blocking` (lines 473-482 — grep-driven pass/fail) — 7 sequential checks each emitting `OK:` or `FAIL:` per Makefile gate idiom | exact |
| `hack/scripts/render-dry-run-report.sh` (new — dry-run-report.json shaper) | shell-script | transform | RESEARCH §"dry-run-v1.sh skeleton" (lines 689-701 — heredoc-generated JSON) | exact |
| `hack/scripts/acceptance-v1.sh` (new) | shell-script | event-driven | `Makefile` `test-e2e-live` (env-gate + script invocation) AND `hack/helm/augment-tide-chart.sh` (preamble pattern) | exact |

### P8 — Phase closeout

| New File | Role | Data Flow | Closest Analog | Match Quality |
|----------|------|-----------|----------------|---------------|
| (Phase 5 closeout STATE/ROADMAP edits — not a new file; pattern = Phase 04.1 closeout SUMMARY) | meta | static | `.planning/phases/04.1-*/04.1-15-SUMMARY.md` (recent closeout shape) | exact |

---

## Pattern Assignments

### P1.1 — `LICENSE` (new — Apache-2.0 boilerplate)

**Analog:** None in-repo. Pattern source = upstream Apache 2.0 (https://www.apache.org/licenses/LICENSE-2.0.txt) — VERBATIM copy per Don't-Hand-Roll table (RESEARCH line 392).

**Boilerplate header convention** (from `hack/boilerplate.go.txt` lines 1-15 — every Go file uses this; LICENSE root file aligns):
```
Copyright YEAR TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
```

**Apply this:**
- `YEAR` = `2026` (matches `Makefile` line 4: `YEAR ?= $(shell date +%Y)` and existing Go file headers)
- `[name of copyright owner]` in the verbatim Apache 2.0 boilerplate APPENDIX = `The TIDE Authors`
- Place at `/Users/justinsearles/Projects/tide/LICENSE` (repo root)

### P1.2 — `NOTICE` (new — bundled-deps NOTICE propagation)

**Analog:** None in-repo. Pattern source = Apache ASF licensing-howto (https://infra.apache.org/licensing-howto.html). RESEARCH line 393 cites: "the relevant portions [must be] bubbled up into the top-level NOTICE file."

**Apply this:**
- Run `go-licenses report ./...` over the production binaries (`cmd/manager/`, `cmd/tide/`, `cmd/tide-push/`, `cmd/dashboard/`, `cmd/credproxy/`, `cmd/stub-subagent/`, `cmd/tide-lint/`, `cmd/claude-subagent/`).
- Filter to Apache-2.0-licensed transitive deps that ship their own `NOTICE` file (likely some `k8s.io/*` deps; verify).
- Concatenate those NOTICE files into `/Users/justinsearles/Projects/tide/NOTICE` with section headers per dep.
- If no transitive Apache-2.0 dep ships a NOTICE file, skip the `NOTICE` file (D-X1 fallback: "no NOTICE unless mandated").

### P1.3 — `CONTRIBUTING.md` (new)

**Analog (partial):** `docs/live-e2e.md` (lines 1-15 — `**Audience:** ... **Status:** ... **Scope of this doc:**` heading shape) for audience/scope intro.

**Content sections** (per CONTEXT.md D-C5):
- Prerequisites (Go 1.26, kubebuilder, kind, helm 3)
- Development workflow (`make test` / `make test-int` / `make test-e2e-kind`)
- Branch naming (`feat/`, `fix/`, `docs/`)
- Commit message conventions (Conventional Commits)
- DCO signoff (`git commit -s`)
- PR template (link to issue, test plan, breaking change?)

**Reference for Make-target verbs** (so contributor sees the canonical entry surface): `Makefile` lines 80-90 (`test`), 145-147 (`test-int`), 138-139 (`test-e2e-kind`), 199-202 (`lint`).

### P1.4 — `SECURITY.md` (new)

**Analog (partial):** `docs/gates.md` (table-driven structure with `| Value | Semantics |` columns).

**Content sections** (per CONTEXT.md D-C5):
- How to report a vulnerability (email + GH Security Advisory)
- Expected response time (48h ack)
- What's in scope (controller, dashboard, CRDs, RBAC)
- What's out of scope (third-party LLM provider key compromise, K8s cluster compromise)

**Boilerplate header convention** (matches `docs/live-e2e.md` lines 1-3):
```markdown
# TIDE Security Policy

**Audience:** Security researchers and operators reporting vulnerabilities in TIDE.
```

---

### P2.1 — `README.md` (modified — prepend ~30-line Quickstart)

**Analog (self-reference):** `README.md` lines 1-43 (existing title + acronym block; Quickstart goes ABOVE this content, NOT replacing it).

**Quickstart block** (per CONTEXT.md D-C1 + Pitfall 4 mitigation): MUST be the FIRST content above the title `# TIDE`.

**Apply this** (from RESEARCH Pitfall 4 lines 450-457):
```bash
kind create cluster --name tide-demo
helm install tide-crds oci://ghcr.io/jsquirrelz/tide-charts/tide-crds --version 1.0.0 -n tide-system --create-namespace
helm install tide oci://ghcr.io/jsquirrelz/tide-charts/tide --version 1.0.0 -n tide-system
kubectl apply -f examples/projects/small/project.yaml
```

**With "expected output" interleaved** (per CONTEXT.md specifics line 244 — "~10 of the 30 lines"):
- After each command, show 2-3 lines of actual output (e.g. `helm install` shows `STATUS: deployed`).

**Callout** (per Pitfall 8 mitigation): `> **First time? Skip to [docs/INSTALL.md](docs/INSTALL.md) for the 4-command install.**`

### P2.2 — `docs/README.md` (new — 10-entry index)

**Analog (partial):** `docs/live-e2e.md` lines 7-15 ("Scope of this doc:" bulleted block).

**10-entry index** (per CONTEXT.md D-C3, reader-journey order):
```markdown
# TIDE Documentation

1. [install](INSTALL.md) — prerequisites + Helm install + first sample
2. [project authoring](project-authoring.md) — Project.Spec reference + 3-sample walkthrough
3. [dashboard](dashboard.md) — port-forward + ingress + auth
4. [`tide` CLI](cli.md) — operator-facing CLI reference
5. [gates](gates.md) — per-level gate policy + approval handshake
6. [observability](observability.md) — OTel + Prometheus
7. [git hosts](git-hosts.md) — GitHub/GitLab/Gitea PAT setup
8. [storage drivers](rwx-drivers.md) — RWX PVC driver matrix
9. [live E2E](live-e2e.md) — nightly Claude-backed cron
10. [troubleshooting](troubleshooting.md) — 12-row symptom/cause/recipe table
11. [RBAC reference](rbac.md) — per-Kind verbs + per-ns RoleBinding template
```

**Place at:** `/Users/justinsearles/Projects/tide/docs/README.md`

### P2.3 — `docs/INSTALL.md` (new — single source of truth for install)

**Analog (exact):** `docs/git-hosts.md` (the per-platform-recipes structure). Lines 1-15 are the audience/scope opener; lines 23-50 are the per-host (GitHub/GitLab/Gitea) recipes — INSTALL.md mirrors with per-OS (macOS/Linux/Windows-WSL2) recipes.

**Heading shape** (from `docs/git-hosts.md` line 1-7):
```markdown
# TIDE Install Guide

**Audience:** TIDE operators installing the controller + CRDs into a Kubernetes cluster.

**Status:** v1.0 ships Helm chart pair (`tide` + `tide-crds`) via OCI registry (ghcr.io). Index.yaml distribution is also supported.

**Scope of this doc:**

- Prerequisites
- The CRD-subchart-first install order (Pitfall 4)
- Per-OS prerequisite install (macOS / Linux / Windows-WSL2)
- ANTHROPIC_API_KEY Secret creation
- Git creds Secret creation
- First-Project apply
- "Is TIDE for me?" 3-bullet section (Pitfall 8 mitigation)
```

**OCI install commands** (from CONTEXT.md D-X6):
```bash
helm install tide-crds oci://ghcr.io/jsquirrelz/tide-charts/tide-crds --version 1.0.0
helm install tide oci://ghcr.io/jsquirrelz/tide-charts/tide --version 1.0.0
```

Plus the alternative path: `helm install ./charts/tide-crds` for cloned-repo users.

### P2.4 — `docs/project-authoring.md` (new — 3-sample walkthrough)

**Analog (exact):** `docs/cli.md` (verb-by-verb table-of-contents shape; lines 1-12 are the intro paragraph). `docs/git-hosts.md` lines 23-50 (per-host structure) for the 3-sample walkthrough.

**Sections** (per CONTEXT.md D-C3 item 2):
- `Project.Spec` field reference (lift from `api/v1alpha1/project_types.go` field comments)
- 3-sample walkthrough (small → medium → large) with cost discriminator (per CONTEXT.md D-B1)
- Outcome-prompt authoring guidance (Variant B from RESEARCH §"Large Sample Project.yaml Outcome Prompt")

### P2.5 — `docs/troubleshooting.md` (new — ~12-row table)

**Analog (exact):**
- `docs/rwx-drivers.md` lines 21-27 (single-table-as-doc Markdown format).
- `docs/live-e2e.md` lines 108-132 (`**Symptom:** ... **Cause:** ... **Fix:** ...` paragraph block format).

**Table shape** (per CONTEXT.md D-C4 — table format wins over paragraph blocks for scannability):
```markdown
| Symptom | Cause | Recipe |
|---------|-------|--------|
| `kubectl patch project foo --type=merge -p '{"metadata":{"finalizers":[]}}'` is needed | finalizer stuck (Project hangs in Deleting) | `kubectl patch finalizers ...` per ROADMAP |
| Controller log: `401 unauthorized` | invalid `ANTHROPIC_API_KEY` | Recreate Secret with valid key; restart deploy/tide-controller-manager |
| `Project.Status.git.condition=PushLeaseFailed` | concurrent push race (Phase 3 D-B6) | `kubectl annotate project foo tideproject.k8s/bypass-push-lease=true` |
... (12 entries total per CONTEXT.md D-C4)
```

### P2.6 — `docs/rbac.md` (new)

**Analog (exact):** `docs/rwx-drivers.md` (table-driven matrix). Per-Kind ClusterRoles already shipped (Phase 1 plan 09) — `charts/tide/templates/project-admin-rbac.yaml`, `phase-admin-rbac.yaml`, etc.

**Sections** (per CONTEXT.md D-X4 + D-X7):
- Per-Kind verbs table (project-admin / phase-admin / plan-admin / task-admin / wave-admin / milestone-admin — each gets {`*` on body, `get` on /status})
- Per-namespace RoleBinding usage (links to `charts/tide/templates/per-namespace-rolebinding.yaml` + `projectNamespaces:` values key)
- Conversion-webhook no-op section (per D-X7 — webhook still needs RBAC plumbing even as no-op)

---

### P3.1 — `charts/tide/templates/per-namespace-rolebinding.yaml` (new)

**Analog (exact):**
- `charts/tide/templates/project-admin-rbac.yaml` (lines 1-19 — per-Kind ClusterRole pattern; the new RoleBinding REFERENCES one of these by name).
- `charts/tide/templates/manager-rbac.yaml` lines 79-92 (the RoleBinding shape itself — `apiVersion`, `kind: ClusterRoleBinding/RoleBinding`, `roleRef`, `subjects`).

**Copy this** (from RESEARCH §"Per-Namespace RoleBinding Template" lines 573-600):
```yaml
{{- range $ns := .Values.projectNamespaces }}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: tide-orchestrator-{{ $ns }}
  namespace: {{ $ns }}
  labels:
    {{- include "tide.labels" $ | nindent 4 }}
    app.kubernetes.io/component: per-namespace-rbac
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: '{{ include "tide.fullname" $ }}-project-admin-role'  # adjust to aggregate as needed
subjects:
- kind: ServiceAccount
  name: '{{ include "tide.fullname" $ }}-controller-manager'
  namespace: {{ $.Release.Namespace }}
{{- end }}
```

**Note on values pattern:**
- Empty default in `hack/helm/tide-values.yaml`: `projectNamespaces: []` (then re-augmented into `charts/tide/values.yaml` by `bash hack/helm/augment-tide-chart.sh`).
- Plan MUST edit `hack/helm/tide-values.yaml` (the SOT) — NOT `charts/tide/values.yaml` directly (CLAUDE.md chart-is-fixed-contract).

**SA name reference:** verify against `charts/tide/templates/serviceaccount.yaml` line 4 — `name: {{ include "tide.fullname" . }}-controller-manager`. The RoleBinding's `subjects[].name` must match.

**ClusterRole aggregation note (gap):** The Helm template above binds to `project-admin-role` only. A real implementation might bind to multiple per-Kind ClusterRoles (phase-admin, plan-admin, task-admin, wave-admin, milestone-admin). Planner decides: (a) emit one RoleBinding per per-Kind ClusterRole inside the `range`, (b) create an aggregate "tide-orchestrator-namespace" ClusterRole that fans out (see RESEARCH line 594 commented `name: tide-orchestrator-namespace`), OR (c) bind to the existing `manager-role` ClusterRole. Option (b) matches RESEARCH's example shape; planner verifies if such an aggregate role exists or needs creation.

### P3.2 — `charts/tide/Chart.yaml` (modified — version bump)

**Analog (self-reference):** `charts/tide/Chart.yaml` lines 11-12 (target lines of the bump).

**Edit pattern:**
```diff
-version: 0.1.0-dev
-appVersion: "0.1.0-dev"
+version: 1.0.0
+appVersion: "1.0.0"
```

**Critical:** The SOT is `/Users/justinsearles/Projects/tide/hack/helm/tide-chart.yaml` (lines 11-12 same shape). Edit BOTH the SOT (`hack/helm/tide-chart.yaml`) AND the generated chart (`charts/tide/Chart.yaml`) OR edit the SOT and run `bash hack/helm/augment-tide-chart.sh` to re-augment.

**Grep first** (per RESEARCH lines 411-418): `grep -rn "0\.1\.0-dev" --include="*.yaml" --include="*.go" --include="*.md" .` — confirm only `Chart.yaml` (x2) + `values.yaml` IMAGE TAGS (5 locations) carry the literal. Image tags are separately bumped by goreleaser ldflags at release time; the chart `appVersion: "1.0.0"` propagates through `_helpers.tpl` `app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}` (verify `charts/tide/templates/_helpers.tpl` lines 36-43).

### P3.3 — `charts/tide-crds/Chart.yaml` (modified — version bump LOCKSTEP)

**Analog (self-reference):** `charts/tide-crds/Chart.yaml` lines 11-12.

**Same edit pattern as P3.2** (LOCKSTEP per D-X3 — both Chart.yaml files bump to `1.0.0` in the SAME commit; never one without the other).

SOT: `/Users/justinsearles/Projects/tide/hack/helm/tide-crds-chart.yaml` (lines 11-12).

### P3.4 — `charts/tide-crds/templates/*-crd.yaml` (modified — add `helm.sh/resource-policy: keep` annotation)

**Analog (self-reference):** `charts/tide-crds/templates/project-crd.yaml` lines 1-8 (existing `metadata.annotations` block — `controller-gen.kubebuilder.io/version: v0.20.1` is already there).

**Edit pattern** (per RESEARCH Pitfall 2 / Topic 8 finding):
```diff
 metadata:
   name: projects.tideproject.k8s
   annotations:
     controller-gen.kubebuilder.io/version: v0.20.1
+    helm.sh/resource-policy: keep
```

**Apply to all 6 CRDs:** `project-crd.yaml`, `milestone-crd.yaml`, `phase-crd.yaml`, `plan-crd.yaml`, `task-crd.yaml`, `wave-crd.yaml` — all under `charts/tide-crds/templates/`.

**Source-of-truth:** the CRDs are generated by `make helm-crds` (Makefile lines 554-558) from `config/crd/bases/`. The annotation must be added EITHER:
- (a) in the helmify post-augment step `hack/helm/augment-tide-crds-chart.sh` (currently only does Chart.yaml — augment script needs a `sed -i` to inject the annotation into each generated CRD template), OR
- (b) in the kubebuilder source markers under `api/v1alpha1/*_types.go` via `// +kubebuilder:resource:` annotation propagation (verify if kubebuilder supports this for CRD-level annotations).

Planner recommendation: option (a) — extend the existing augment script. Pattern source = `hack/helm/augment-tide-chart.sh` lines 41-65 (awk-driven YAML edit) — but `sed -i` should suffice for adding a single annotation key.

### P3.5 — `charts/tide/values.yaml` (additive — `projectNamespaces: []`)

**Analog (exact):** `hack/helm/tide-values.yaml` is the SOT. Existing additive examples = the `dashboard:` block (Phase 4 D-X3) and the `images:` block (lines 137-166 — multiple image refs with `repository`, `tag`, `pullPolicy`).

**Edit pattern** (per CONTEXT.md Code Examples line 602-610):
```diff
+# Phase 5 D-X4 — per-namespace RoleBinding template (AUTH-02 catch-up).
+# Empty default: no per-namespace RoleBindings shipped unless operator opts in.
+# For multi-Project installs, list the Project namespaces here:
+#   projectNamespaces:
+#     - tide-customer-acme
+#     - tide-customer-globex
+projectNamespaces: []
```

**Place at end of `hack/helm/tide-values.yaml`** (then re-augment via `bash hack/helm/augment-tide-chart.sh` to propagate to `charts/tide/values.yaml`).

---

### P4.1 — `examples/projects/small/project.yaml` (new — $0 stub-subagent)

**Analog (exact):**
- `config/samples/tide_v1alpha1_project.yaml` (lines 1-13 — minimal `Project` shape).
- `test/e2e/testdata/live-claude-project.yaml` (lines 16-87 — multi-doc with Namespace + Secret + Project pattern).

**Apply this** (compose the two patterns):
```yaml
---
apiVersion: v1
kind: Namespace
metadata:
  name: tide-sample-small  # Pitfall 9 — tide-sample- prefix avoids collisions
  labels:
    tideproject.k8s/sample: small
---
apiVersion: tideproject.k8s/v1alpha1
kind: Project
metadata:
  name: small-project
  namespace: tide-sample-small
spec:
  targetRepo: "https://github.com/jsquirrelz/tide-demo-fixture.git"  # ?TBD per D-B3 local-only-git
  budget:
    absoluteCapCents: 0  # $0 stub — no LLM cost
  subagent:
    image: ghcr.io/jsquirrelz/tide-stub-subagent:v1.0.0  # stub, not claude
```

**Wait time + readiness assertion** for D-D2 timer-stop semantics: small-sample drives `Project.Status.phase=Complete` via stub subagent's canned envelopes (Phase 2 SUB-04). Verify the stub subagent is a v1.0.0 image (per P3 chart version bump).

### P4.2 — `examples/projects/medium/project.yaml` (new — ~$5 real Claude + local-only git remote)

**Analog (exact):** `test/e2e/testdata/live-claude-project.yaml` (lines 47-87 — has Project + budget + providerSecretRef + subagent + git pattern).

**Apply this:**
```yaml
spec:
  targetRepo: "file:///demo-remote.git"  # per RESEARCH Pitfall 12 option 2 — bare repo on PVC
  providerSecretRef: anthropic-creds
  budget:
    absoluteCapCents: 500  # $5 cap per D-B1 cost-spectrum
    rollingWindowDurationSeconds: 86400  # 24h per Phase 04.1 P4.1
  subagent:
    image: ghcr.io/jsquirrelz/tide-claude-subagent:v1.0.0
    model: claude-haiku-4-5  # cheaper model per docs/live-e2e.md cost discipline
  git:
    repoURL: "file:///demo-remote.git"
    credsSecretRef: medium-git-creds  # GIT_PAT="" for file:// (no auth)
```

**Plus init-Job mechanism** (per RESEARCH Pitfall 12 option 2): apply `demo-remote-init-job.yaml` BEFORE the Project. The init Job mounts the same PVC as TIDE workspaces (`tide-projects` PVC) and runs `cmd/tide-demo-init/` (Containerized as `ghcr.io/jsquirrelz/tide-demo-init:v1.0.0`) to `git init --bare /workspace/demo-remote.git` and populate it from the embedded fixture content.

### P4.3 — `examples/projects/large/project.yaml` (new — $25 acceptance)

**Analog (exact):** `test/e2e/testdata/live-claude-project.yaml` (lines 47-87) + RESEARCH §"Large Sample Project.yaml Outcome Prompt" (lines 717-785 — Variant B recommended).

**Apply this** (Variant B from RESEARCH):
```yaml
spec:
  targetRepo: "https://github.com/jsquirrelz/tide.git"  # THIS repo (per D-A1)
  providerSecretRef: anthropic-creds
  budget:
    absoluteCapCents: 2500  # $25 hard cap, D-A2 no bypass
    rollingWindowDurationSeconds: 86400  # explicit per Pitfall 7
  subagent:
    image: ghcr.io/jsquirrelz/tide-claude-subagent:v1.0.0
    model: claude-opus-4-7  # acceptance test deserves the real model
  git:
    repoURL: "https://github.com/jsquirrelz/tide.git"
    credsSecretRef: large-git-creds
  outcomePrompt: |
    Author the scaffold for internal/subagent/openai/ mirroring internal/subagent/anthropic/.
    ONE Phase. ONE Plan. Target 5-7 Tasks. Stub Subagent.Run() to return canned envelope.
    Ship the Dockerfile. Wire one e2e test. Do NOT integrate against the real OpenAI API.
  gates:
    phase: auto  # Single Phase scope per D-A1 — no human-gate inside the acceptance run
    plan: auto
    task: auto
```

(Variant B is recommended; planner may verify Variants A/C from RESEARCH Topic 5 if Variant B feels too prescriptive.)

### P4.4 — `examples/tide-demo-fixture/` (new — scaffold content)

**Analog (partial):** None directly. Pattern = "tiny Go module with single cmd + test". Use `go.mod`/`go.sum` shape from repo root; `main.go` shape from `cmd/stub-subagent/main.go` (single-file CLI).

**Layout** (per RESEARCH recommended file layout):
```
examples/tide-demo-fixture/
├── README.md       (MIT license header inline; describes "this is the seed content for the medium sample")
├── go.mod          (module github.com/jsquirrelz/tide-demo-fixture; go 1.26)
├── go.sum          (empty or stdlib-only)
├── main.go         (tiny "hello world" + a function TIDE will modify)
└── main_test.go    (one passing test for TIDE to extend)
```

**README header convention** (MIT — distinct from TIDE's Apache-2.0 distribution per D-B3):
```markdown
# tide-demo-fixture

Tiny Go scaffold for the TIDE medium sample.

Licensed under the MIT License (separate from TIDE's Apache-2.0 distribution per Phase 5 D-B3).
```

---

### P5.1 — `cmd/tide-demo-init/main.go` (new — local-only git remote bootstrap)

**Analog (exact):** `cmd/tide-push/main.go` (lines 1-100 — package-doc header, flag-based CLI, pkg/git import, structured exit codes).

**Package-doc header pattern** (mirror `cmd/tide-push/main.go` lines 1-37 structure):
```go
// Command tide-demo-init is the local-only git remote bootstrap binary for
// the TIDE medium sample (Phase 5 D-B3). It initializes a bare git repository
// at the given --bootstrap-dir path and populates it with the embedded
// examples/tide-demo-fixture/ content as an initial commit.
//
// Operating modes:
//
//	--bootstrap-dir <path>  — bare repo destination. For in-cluster init Job
//	                          usage, this points at a PVC mount (e.g.
//	                          /workspace/demo-remote.git) so TIDE's controller
//	                          can resolve the file:// URL.
//
// Exit codes:
//
//	0  — bootstrap succeeded
//	1  — generic failure (git init, content extract, initial-commit failure)
//	2  — invariant violation (--bootstrap-dir empty, target dir exists with non-bare repo)
package main
```

**Imports** (mirror `cmd/tide-push/main.go` lines 39-61):
```go
import (
    "context"
    "embed"
    "flag"
    "fmt"
    "os"
    "path/filepath"

    gogit "github.com/go-git/go-git/v5"
    "github.com/go-git/go-git/v5/plumbing/object"

    pkggit "github.com/jsquirrelz/tide/pkg/git"
)

//go:embed all:fixture
var fixtureFS embed.FS  // embed examples/tide-demo-fixture/ content at build time
```

**Apache-2.0 file header** (from `hack/boilerplate.go.txt` — every Go file under `cmd/` has this prepended; `make generate` adds for `api/`, but `cmd/` files need it MANUALLY in the planner authoring).

**Flag parsing + main shape** (mirror `cmd/stub-subagent/main.go` lines 61-80):
```go
func main() {
    fs := flag.NewFlagSet("tide-demo-init", flag.ExitOnError)
    bootstrapDir := fs.String("bootstrap-dir", "", "destination path for the bare git repo")
    if err := fs.Parse(os.Args[1:]); err != nil { ... }
    if *bootstrapDir == "" { fmt.Fprintln(os.Stderr, "ERROR: --bootstrap-dir required"); os.Exit(2) }
    ctx := context.Background()
    if err := bootstrap(ctx, *bootstrapDir); err != nil { ... os.Exit(1) }
}
```

### P5.2 — `images/tide-demo-init/Dockerfile` (new, conditional on Topic 4 Option 2)

**Analog (exact):** `images/tide-push/Dockerfile` (lines 1-43 — two-stage golang:1.26-alpine + distroless/static:nonroot).

**Copy this verbatim** (with paths swapped for `cmd/tide-demo-init/`):
```dockerfile
FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY pkg/git/ pkg/git/
COPY cmd/tide-demo-init/ cmd/tide-demo-init/
COPY examples/tide-demo-fixture/ examples/tide-demo-fixture/   # for go:embed
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/tide-demo-init ./cmd/tide-demo-init

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /out/tide-demo-init /usr/local/bin/tide-demo-init
USER 1000
ENTRYPOINT ["/usr/local/bin/tide-demo-init"]
```

**Goreleaser registration** (`.goreleaser.yaml` lines 30-50): add a second `builds:` entry IF tide-demo-init ships as a binary asset, OR add an `images:` block IF it ships only as a container image (RESEARCH Topic 4 leans toward container-only).

---

### P6.1 — `.github/workflows/release.yaml` (modified — add 3 jobs)

**Analog (exact):**
- Existing `release` job in `.github/workflows/release.yaml` (lines 20-63) — keep as-is.
- `.github/workflows/ci.yaml` `helm-lint` job (lines 108-159) — model for the new `helmify-verify` job (reuses `make helm` + `git diff --exit-code charts/`).
- `.github/workflows/live-e2e.yml` lines 1-12 — model for `workflow_dispatch` posture (NOT used here — `release.yaml` is tag-triggered, not workflow-dispatch; but the LIMIT is "no `schedule:` cron" per P2.4 — which release.yaml already honors).

**Three new jobs to add** (per RESEARCH lines 530-571 + Topics 1, 2, 9):

1. **`helmify-verify`** (depends on: nothing; runs before `release`):
   - Triggered on the same `v*` tag push.
   - Steps: checkout → setup-go → install helm → `make helm` → `git diff --exit-code charts/` (fail if drift).
   - Pattern: copy `helm-lint` job from `ci.yaml` lines 108-159 verbatim, then add `needs: helmify-verify` to the existing `release` job.

2. **`dry-run-gate`** (triggered ONLY on `v*-rc.*` tag — separate workflow file OR conditional in release.yaml):
   - Per CONTEXT.md D-D3: "blocks the goreleaser release job if the dry-run fails or wall-clock exceeds 30 min."
   - Triggered ONLY on `v*-rc.*` tag (not `v*`). Per Pitfall 11: upload `dry-run-report.json` as `actions/upload-artifact@v4`; the `release` job downloads via `actions/download-artifact@v4` before invoking goreleaser.
   - Tag-pattern filtering: per RESEARCH "Don't Hand-Roll" line 398: `on: push: tags: ['v*-rc.*']` workflow-level filter (NOT step-level `if:` checks).
   - Steps: checkout → setup-go → install kind+helm → `make dry-run-v1` → upload artifact.

3. **`chart-publish`** (depends on: `release`; runs AFTER goreleaser):
   - Copy from RESEARCH §"Helm OCI Publish" (lines 530-571) — this is the canonical reference.
   - Permissions critical: `packages: write` (Pitfall 3 — default `secrets.GITHUB_TOKEN` does NOT have this by default).
   - Steps: checkout → install helm → `helm registry login ghcr.io` → `helm package` x2 → `helm push` x2 → `helm registry logout`.

**Permissions block** (top-of-file, per RESEARCH Pitfall 3):
```yaml
permissions:
  contents: write   # existing — for goreleaser
  packages: write   # NEW — for ghcr.io OCI push
```

### P6.2 — `Makefile` (modified — add `dry-run-v1`, `acceptance-v1` targets)

**Analog (exact):**
- `Makefile` `test-e2e-live` target (lines 190-197 — env-var fail-fast + script invocation).
- `Makefile` `test-int-kind-prep` (lines 153-167 — multi-image build + kind load).

**Copy this** (from RESEARCH §"make dry-run-v1" lines 615-633):
```makefile
##@ Phase 5 v1.0 ship gates

DRY_RUN_IMAGE ?= ubuntu:24.04
DRY_RUN_TIMEOUT_SECONDS ?= 1800  # 30 min — D-D3

.PHONY: dry-run-v1
dry-run-v1: ## Phase 5 D-D1 — Docker-in-Docker external-operator dry-run.
	@hack/scripts/dry-run-v1.sh

.PHONY: acceptance-v1
acceptance-v1: ## Phase 5 D-A4 — maintainer ritual ($25 hard cap; requires ANTHROPIC_API_KEY).
	@if [ -z "$$ANTHROPIC_API_KEY" ]; then \
	  echo "ERROR: ANTHROPIC_API_KEY env not set — refusing to run acceptance-v1"; \
	  echo "       See docs/INSTALL.md for Secret setup."; \
	  exit 1; \
	fi
	@hack/scripts/acceptance-v1.sh
```

**Placement** (per existing `Makefile` structure):
- After `##@ Live nightly E2E (TEST-03 — Phase 3 plan 03-11)` section (line 169 onwards).
- BEFORE `##@ Dependencies` section (line 302).

**Convention to honor** (from `Makefile` line 191-197 `test-e2e-live`):
- Env-var check via inline shell `@if [ -z "$$ANTHROPIC_API_KEY" ]; then ... exit 1; fi` (NOT a Make `ifdef` — explicit per Makefile idiom).
- Help text via `## ...` comment (parsed by `make help` awk at line 47).

---

### P7.1 — `hack/scripts/dry-run-v1.sh` (new — Docker-in-Docker scripted)

**Analog (exact):**
- `hack/helm/augment-tide-chart.sh` lines 1-30 (`set -euo pipefail`, `REPO_ROOT` derivation).
- `Makefile` `test-int-kind-prep` lines 153-167 (kind cluster + kind load orchestration).
- RESEARCH §"dry-run-v1.sh skeleton" lines 635-715 — canonical reference, copy verbatim.

**File header preamble pattern** (mirror `hack/helm/augment-tide-chart.sh` lines 1-25):
```bash
#!/usr/bin/env bash
# Phase 5 D-D1 — Docker-in-Docker external-operator dry-run.
# Maps the README Quickstart against a clean ubuntu:24.04 image and times each phase.
# Outputs dry-run-report.json + transcript. Exits non-zero if elapsed > DRY_RUN_TIMEOUT_SECONDS
# or if any Quickstart step fails.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
```

**Docker-in-Docker invocation** (per RESEARCH Pitfall 6 — MUST mount `/var/run/docker.sock`, MUST NOT run nested `dockerd`):
```bash
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$DRY_RUN_DIR":/workspace \
  --network host \
  ubuntu:24.04 bash -c '
    set -euo pipefail
    apt-get update -qq && apt-get install -qq -y curl ca-certificates git
    # install kind / helm / kubectl (pinned versions matching ci.yaml)
    ...
    # Quickstart commands (must mirror README exactly)
    cd /workspace/tide
    kind create cluster --name tide-dry-run
    helm install tide-crds ./charts/tide-crds -n tide-system --create-namespace
    helm install tide ./charts/tide -n tide-system
    kubectl wait --for=condition=Available deploy/tide-controller-manager -n tide-system --timeout=5m
    kubectl apply -f examples/projects/small/project.yaml
    kubectl wait --for=jsonpath="{.status.phase}"=Complete project/small-project -n tide-sample-small --timeout=10m
  ' 2>&1 | tee "$TRANSCRIPT_PATH"
```

**Pinned tool versions** (match `ci.yaml` lines 129 + 184-189):
- kind: `v0.31.0`
- helm: `v3.16.3`
- kubectl: `v1.31.0` (or match `ENVTEST_K8S_VERSION` derivation in `Makefile` line 330)

**Timeout enforcement** (per RESEARCH Pitfall 5):
```bash
if [ "$ELAPSED" -gt "$TIMEOUT_SECONDS" ]; then
  echo "FAIL: dry-run exceeded timeout (${ELAPSED}s > ${TIMEOUT_SECONDS}s)"
  exit 1
fi
```

### P7.2 — `hack/scripts/acceptance-verify.sh` (new — 7-check D-A3 verifier)

**Analog (exact):**
- `Makefile` `verify-no-blocking` (lines 473-482 — `grep`-driven pass/fail with `OK:` and exit codes).
- `Makefile` `verify-no-rbac-wildcards` (lines 486-495 — multi-condition verifier pattern).
- `hack/helm/augment-tide-chart.sh` lines 1-30 (preamble).

**7 checks** (per CONTEXT.md D-A3 — copy as 7 sequential blocks, each emitting `OK:` or `FAIL:`):

```bash
#!/usr/bin/env bash
# Phase 5 D-A3 — acceptance-test verifier.
# Reads kubectl + cluster state. Returns 0 only if all 7 D-A3 checks pass.
set -euo pipefail

PROJECT_NAME="${1:?ERROR: project name required}"
NAMESPACE="${2:?ERROR: namespace required}"
RUN_START="${3:?ERROR: run-start timestamp required for log filter}"

FAILS=0

# Check 1: per-run branch exists on configured remote (Phase 3 D-B6)
echo "Check 1: per-run branch 'tide/run-${PROJECT_NAME}-<unix-ts>' exists on origin..."
if git ls-remote --heads origin "tide/run-${PROJECT_NAME}-*" | grep -q .; then
  echo "  OK"
else
  echo "  FAIL: no per-run branch found"
  FAILS=$((FAILS+1))
fi

# Check 2: per-run branch has commits matching all 4 D-B2 shapes
echo "Check 2: per-run branch carries 4 D-B2 commit shapes..."
# (regex per shape: 'tide: plan .+ authored \+ executed', 'tide: phase .+ authored', etc.)
...

# Check 3: Project.Status.Phase == Complete
echo "Check 3: Project.Status.phase == Complete..."
STATUS=$(kubectl get project "${PROJECT_NAME}" -n "${NAMESPACE}" -o jsonpath='{.status.phase}')
[ "${STATUS}" = "Complete" ] && echo "  OK" || { echo "  FAIL: status=${STATUS}"; FAILS=$((FAILS+1)); }

# Check 4-7: ERROR log scan, orphan-job check, gitleaks metric, budget under cap
... (each 5-15 lines)

if [ "${FAILS}" -gt 0 ]; then
  echo "FAIL: ${FAILS} of 7 D-A3 checks failed"
  exit 1
fi
echo "PASS: all 7 D-A3 checks passed"
```

### P7.3 — `hack/scripts/render-dry-run-report.sh` (new — dry-run-report.json shaper)

**Analog (exact):** RESEARCH §"dry-run-v1.sh skeleton" lines 689-701 (heredoc-generated JSON).

**Schema** (per CONTEXT.md D-D4 + RESEARCH specifics line 236 — `schemaVersion: 1` for forward-compat):
```json
{
  "schemaVersion": 1,
  "runId": "<rc-tag>",
  "totalSeconds": N,
  "phases": [
    {"name": "clone", "elapsedSeconds": N, "exitCode": 0},
    {"name": "chart-install", "elapsedSeconds": N, "exitCode": 0},
    ...
  ],
  "kindVersion": "v0.31.0",
  "helmVersion": "v3.16.3",
  "kubeVersion": "v1.31.0",
  "baseImage": "ubuntu:24.04",
  "timestamp": "..."
}
```

**Pattern source:** `Makefile` `helm-rbac-assert` (lines 519-528) uses `python3 hack/helm/assert-dashboard-rbac.py` for non-trivial JSON/YAML processing. The dry-run-report.sh COULD use either:
- (a) Pure-bash heredoc (RESEARCH skeleton lines 689-701) — simplest, no python dep
- (b) Python helper script (mirrors `hack/helm/assert-dashboard-rbac.py` precedent) — better for the per-phase array

Planner recommendation: (a) for v1.0 since the schema is flat; revisit if the per-phase array grows.

### P7.4 — `hack/scripts/acceptance-v1.sh` (new — orchestrates kind+helm+apply+verify)

**Analog (exact):**
- `Makefile` `test-e2e-live` (lines 190-197 — env-gate + script invocation).
- `hack/helm/augment-tide-chart.sh` lines 1-30 (preamble).

**Orchestration steps** (per CONTEXT.md D-A4):
```bash
#!/usr/bin/env bash
# Phase 5 D-A4 — maintainer-only acceptance ritual.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
TIMESTAMP=$(date +%s)
ACCEPTANCE_DIR="${REPO_ROOT}/.acceptance-runs/${TIMESTAMP}"
mkdir -p "${ACCEPTANCE_DIR}"

# 1. Fresh kind cluster
kind create cluster --name tide-acceptance

# 2. helm install
helm install tide-crds "${REPO_ROOT}/charts/tide-crds" -n tide-system --create-namespace
helm install tide "${REPO_ROOT}/charts/tide" -n tide-system

# 3. Apply ANTHROPIC_API_KEY Secret
kubectl create secret generic anthropic-creds \
  --from-literal=ANTHROPIC_API_KEY="${ANTHROPIC_API_KEY}" \
  -n tide-sample-large --dry-run=client -o yaml | kubectl apply -f -

# 4. Apply large sample
kubectl apply -f "${REPO_ROOT}/examples/projects/large/project.yaml"

# 5. Wait for completion (or budget halt) — bound by 4h ceiling
RUN_START=$(date -u +%Y-%m-%dT%H:%M:%SZ)
kubectl wait --for=jsonpath='{.status.phase}'=Complete project/large-project -n tide-sample-large --timeout=4h

# 6. Capture evidence under .acceptance-runs/<ts>/
kubectl logs -n tide-system deploy/tide-controller-manager --since="${RUN_START}" > "${ACCEPTANCE_DIR}/controller.log"
kubectl get project,phase,plan,task,wave -A -o yaml > "${ACCEPTANCE_DIR}/crds.yaml"
# Per-run-branch git log
cd "${REPO_ROOT}" && git log "tide/run-large-project-*" --oneline > "${ACCEPTANCE_DIR}/run-branch.log"

# 7. Run 7-check verifier
bash "${REPO_ROOT}/hack/scripts/acceptance-verify.sh" large-project tide-sample-large "${RUN_START}"
```

**Evidence directory convention** (per CONTEXT.md `code_context` line 215 — "Evidence capture under `.acceptance-runs/<timestamp>/`"):
- Mirrors Phase 02.2 + 04.1 evidence-capture pattern.
- Per CONTEXT.md D-A4: "controller logs, Project + child CRDs (yaml -o), per-run-branch git log, dashboard screenshot (Chrome DevTools MCP)."

---

## Shared Patterns

### Apache-2.0 file header (every Go file under `cmd/`, `api/`, `internal/`, `pkg/`, `test/`, `tools/`)

**Source:** `hack/boilerplate.go.txt` lines 1-15.
**Apply to:** `cmd/tide-demo-init/main.go` (new Go binary file).

```go
/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
```

**`make generate`** (Makefile line 70) auto-injects this header for `api/` types (via `controller-gen object:headerFile=...,year=$(YEAR)`). For `cmd/` files, the planner authors it manually at file creation time.

### Helm template label boilerplate (every new chart template)

**Source:** `charts/tide/templates/manager-rbac.yaml` lines 4-6 (and 81-83).
**Apply to:** `charts/tide/templates/per-namespace-rolebinding.yaml`.

```yaml
metadata:
  name: ...
  namespace: ...
  labels:
  {{- include "tide.labels" $ | nindent 4 }}  # use $ inside range, . outside
```

The `tide.labels` helper (`_helpers.tpl` lines 36-43) emits `helm.sh/chart`, `app.kubernetes.io/name`, `app.kubernetes.io/instance`, `app.kubernetes.io/version`, `app.kubernetes.io/managed-by` — standard Helm + Kubernetes recommended labels.

### Markdown doc audience/scope opener (every new doc)

**Source:** `docs/live-e2e.md` lines 1-15.
**Apply to:** `docs/INSTALL.md`, `docs/project-authoring.md`, `docs/troubleshooting.md`, `docs/rbac.md`, `CONTRIBUTING.md`, `SECURITY.md`.

```markdown
# <Doc title>

**Audience:** <one-line target reader>

**Status:** <v1.0 lock or stub level>

**Scope of this doc:**

- <bulleted topic 1>
- <bulleted topic 2>
- ...
```

### Shell-script preamble (every new `hack/scripts/*.sh`)

**Source:** `hack/helm/augment-tide-chart.sh` lines 1-30.
**Apply to:** `hack/scripts/dry-run-v1.sh`, `hack/scripts/acceptance-verify.sh`, `hack/scripts/render-dry-run-report.sh`, `hack/scripts/acceptance-v1.sh`.

```bash
#!/usr/bin/env bash
# <Plan ID + one-line purpose>
# <2-3 line description of what it does + Phase 5 D-X reference>
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
```

`set -euo pipefail` is non-negotiable (fail-fast on errors + unset vars + pipeline failures).

### Sample namespace name prefix (every `examples/projects/*/namespace.yaml`)

**Source:** RESEARCH Pitfall 9 (lines 498-504) + `config/samples/namespace.yaml` lines 1-11 (existing `tide-samples` convention).
**Apply to:** `examples/projects/small/namespace.yaml`, `examples/projects/medium/namespace.yaml`, `examples/projects/large/namespace.yaml`.

**Convention:** `tide-sample-{small,medium,large}` (NOT `tide-samples` — that's the existing Phase 1 fixture namespace; the new examples use a different prefix to avoid collision per D-B2 "no symlinks/mirrors between the two paths").

### GitHub Actions workflow `permissions:` minimization

**Source:** `.github/workflows/release.yaml` line 18 (`permissions: {}`) + lines 25-28 (job-level `contents: write`).
**Apply to:** New `chart-publish` job in modified `release.yaml`.

```yaml
permissions: {}   # workflow-level: deny by default
jobs:
  chart-publish:
    permissions:
      contents: read
      packages: write   # Pitfall 3 — REQUIRED for ghcr.io OCI push
```

---

## No Analog Found (truly new patterns)

| File | Role | Reason | Pattern Source |
|------|------|--------|----------------|
| `LICENSE` (repo-root) | legal | No prior repo-root legal file | Verbatim from https://www.apache.org/licenses/LICENSE-2.0.txt (D-X1) |
| `NOTICE` (repo-root) | legal | No prior repo-root NOTICE file | `go-licenses report` audit + Apache ASF licensing-howto rules (D-X1 fallback: skip if no transitive dep mandates) |
| `CONTRIBUTING.md` | docs | No prior CONTRIBUTING file | DCO + Conventional Commits + branch-naming conventions (D-C5) |
| `SECURITY.md` | docs | No prior SECURITY file | Standard OSS shape; in/out-of-scope from D-C5 |
| `examples/projects/medium/demo-remote-init-job.yaml` | sample-yaml | No in-cluster init-Job-from-embedded-fixture pattern exists | Phase 2 D-G2 init-Job pattern (closest) + RESEARCH Pitfall 12 option 2 |
| `examples/tide-demo-fixture/README.md` | docs | No prior MIT-licensed-inline doc convention | RESEARCH D-B3 (MIT license inline for the fixture content) |
| `make dry-run-v1` Docker-in-Docker target | makefile | No prior DinD usage in Makefile | RESEARCH §"dry-run-v1.sh skeleton" canonical reference (skeleton ships in PATTERNS.md) |

---

## File-Naming Conventions Where New Patterns Emerge

| Convention | Examples | Justification |
|------------|----------|---------------|
| `examples/projects/{cost-tier}/` | `small/`, `medium/`, `large/` | Cost-spectrum discriminator (D-B1) — wins over feature-spectrum naming |
| `examples/tide-demo-fixture/` | (one location) | Singular fixture name; NOT `examples/fixtures/` plural because there's exactly one |
| `tide-sample-{cost-tier}` namespace | `tide-sample-small` | Pitfall 9 collision avoidance; distinct from Phase 1's `tide-samples` (plural) |
| `cmd/tide-demo-init/` | (binary) | `tide-` prefix matches existing `cmd/tide-push/`, `cmd/tide-lint/`, `cmd/tide/` convention |
| `hack/scripts/*.sh` | `dry-run-v1.sh`, `acceptance-verify.sh`, etc. | NEW subdirectory; existing `hack/helm/` is for helm augmentation only |
| `hack/scripts/*-v1.sh` | `dry-run-v1.sh`, `acceptance-v1.sh` | `-v1` suffix marks Phase 5 ship-gate scripts (forward-compat: `-v2` for future major-release gates) |
| Tag-pattern `v*-rc.*` | `v1.0.0-rc.1`, `v1.0.0-rc.2` | Release-candidate gate; matches existing `v*` for stable releases (`.goreleaser.yaml` line 92: `prerelease: auto` already supports the `-rc.*` suffix semantics) |

---

## Metadata

**Analog search scope:**
- `charts/tide/templates/` (40 templates)
- `charts/tide-crds/templates/` (6 CRDs + helpers)
- `cmd/` (8 binaries — focused on tide-push, stub-subagent for shape; closest matches for cmd/tide-demo-init/)
- `docs/` (7 existing docs — heading/scope conventions)
- `.github/workflows/` (6 workflows — ci.yaml helm-lint job and release.yaml are the load-bearing analogs)
- `hack/helm/` (2 augment scripts + 7 source-of-truth YAMLs — chart-augmentation pattern)
- `config/samples/` (9 sample YAMLs — minimal Project/Task/Namespace shape)
- `test/e2e/testdata/` (1 multi-doc Project fixture — closest budget+subagent+git shape)
- `Makefile` (558 lines — env-gate + script-invocation pattern, multi-image build pattern, helmify pattern)
- `images/` (4 Dockerfiles — two-stage distroless build pattern)

**Files scanned:** ~120 across analog directories. Strong matches identified at 3-5 per file role (per CRITICAL_RULES early-stop guidance — stopped at first 3 strong matches per file).

**Pattern extraction date:** 2026-05-22

**Source-of-truth references for the planner:**
- CLAUDE.md anti-pattern: "Edits to `charts/tide/values.yaml`" → ALWAYS edit `hack/helm/tide-values.yaml` SOT, re-augment via `bash hack/helm/augment-tide-chart.sh`.
- Phase 1 D-E1 lock: `charts/tide-crds/` is a separate chart, not subchart-as-folder.
- Phase 04.1 P2.4 lock: `workflow_dispatch:`-only for live-LLM workflows. The new `v*-rc.*` dry-run gate is tag-triggered, NOT cron — same posture.
