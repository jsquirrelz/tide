---
phase: quick-260530-h2h
plan: 01
subsystem: maintainer-ritual + install-docs
tags: [cert-manager, acceptance-v1, INSTALL.md, prereq, phase-02.2-lessons]
dependency_graph:
  requires:
    - .planning/quick/260530-h2h-boot-04-acceptance-v1-cert-manager-prere/260530-h2h-PLAN.md
    - test/integration/kind/suite_test.go (pattern reference; not modified)
    - charts/tide/templates/{serving-cert,selfsigned-issuer,metrics-certs}.yaml (chart contract; not modified)
  provides:
    - hack/scripts/acceptance-v1.sh now bootstraps cert-manager before helm install tide
    - docs/INSTALL.md documents cert-manager v1.20.2 prerequisite for both Quickstart + cloned-repo install paths
  affects:
    - The orchestrator's pending acceptance-v1 re-run (BOOT-04 cascade) — script will no longer crash with "no matches for kind Certificate in version cert-manager.io/v1"
    - deferred-items.md (the entry the orchestrator added when BG task bess2gftr failed needs a RESOLVED flip in step 8)
tech-stack:
  added: []
  patterns:
    - "Mirror Layer B integration test cert-manager bootstrap in a bash recipe (same pinned version, same env override, same three-Deployment rollout-wait)"
    - "Pin a single shared env override (TIDE_CERT_MANAGER_VERSION) honored by both the Go integration test and the bash acceptance script — change once, both follow"
key-files:
  created:
    - .planning/quick/260530-h2h-boot-04-acceptance-v1-cert-manager-prere/260530-h2h-SUMMARY.md
  modified:
    - hack/scripts/acceptance-v1.sh (lines 61-74 — cert-manager install + rollout-wait block; commit adb1053)
    - docs/INSTALL.md (lines 101-122 — `### cert-manager prerequisite` subsection inside `## Install order`; commit 7d3af9d)
decisions:
  - "Pin cert-manager v1.20.2 as the shared default — same version the Layer B integration harness uses (test/integration/kind/suite_test.go:317) — so the two recipes never diverge"
  - "Land the prereq subsection inside `## Install order`, after the Pitfall-4 (CRDs first) paragraph and before `### Quickstart`, so it covers both the OCI and cloned-repo install paths without duplication"
  - "Honor the plan's `--timeout` carve-out: only `kubectl rollout status` carries `--timeout=120s`; `kubectl apply -f <URL>` does NOT (Go's exec.CommandContext context-timeout doesn't translate to a CLI flag for `apply`)"
metrics:
  duration_seconds: 203
  completed_date: 2026-05-30T16:25:35Z
  tasks_completed: 2
  files_modified: 2
---

# Quick Task 260530-h2h: BOOT-04 acceptance-v1 cert-manager prereq — Summary

**One-liner:** Wedge a cert-manager v1.20.2 install + three-Deployment rollout-wait into `hack/scripts/acceptance-v1.sh` between `kind create cluster` and `helm install tide`, and document the same prereq on the public `docs/INSTALL.md` install on-ramp — fixing today's BG-task-`bess2gftr` crash and closing the open documentation gap.

## What changed

### `hack/scripts/acceptance-v1.sh` (lines 61-74) — commit `adb1053`

Added a 14-line cert-manager bootstrap block immediately after the optional `ACCEPTANCE_LOAD_IMAGES` image-load step and immediately before the `# ── Helm install (both charts) ──` separator. The block:

- Sets `CERT_MANAGER_VERSION="${TIDE_CERT_MANAGER_VERSION:-v1.20.2}"` (same env var name as `test/integration/kind/suite_test.go:334`).
- Runs `kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.yaml` (NO `--timeout` flag — only valid on `rollout status` / `wait`, not `apply`).
- Loops `kubectl -n cert-manager rollout status deployment/${deploy} --timeout=120s` over all three Deployments (`cert-manager`, `cert-manager-cainjector`, `cert-manager-webhook`) before letting helm fire.

The block is unconditional — runs on every invocation, NOT guarded by `ACCEPTANCE_LOAD_IMAGES`.

### `docs/INSTALL.md` (lines 101-122) — commit `7d3af9d`

Added a new `### cert-manager prerequisite` h3 subsection inside the existing `## Install order (Pitfall 4 — CRDs first)` section. The subsection lands after the Pitfall-4 rationale paragraph (line 99) and before `### Quickstart (OCI registry — primary path)` (line 123), so it applies to **both** the OCI Quickstart and the cloned-repo install paths without duplication. Contents (in order):

1. Rationale paragraph naming the three chart templates (`serving-cert.yaml`, `selfsigned-issuer.yaml`, `metrics-certs.yaml`) and explaining that `tide-crds` does NOT depend on cert-manager (only the main `tide` chart does), so cert-manager and `tide-crds` can install in either order as long as cert-manager lands before `tide`.
2. Pinned-version install command — `kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.20.2/cert-manager.yaml` (fenced as `bash`).
3. Three `kubectl ... rollout status deployment/... --timeout=120s` commands covering all three cert-manager Deployments (fenced as `bash`).
4. Version-pinning guidance — v1.20.2 is the shared default, K8s 1.33-compatible, also pinned in the Layer B integration test + acceptance ritual, overridable via `TIDE_CERT_MANAGER_VERSION`.
5. Cross-references to [`docs/observability.md`](./observability.md) (metrics-server cert wiring) and [`docs/rbac.md`](./rbac.md) (cert-manager RBAC) — both linked by relative path, neither modified.

## Verification

Plan §`<verification>` block executed locally:

| Check | Result |
| --- | --- |
| `grep -cE 'cert-manager' hack/scripts/acceptance-v1.sh` | **9** (expected ≥ 3) |
| `grep -cE 'cert-manager' docs/INSTALL.md` | **9** (expected ≥ 1) |
| `bash -n hack/scripts/acceptance-v1.sh` | **exit 0** — clean parse |
| `git diff --name-only HEAD~2..HEAD -- charts/` | **empty** — chart contract preserved |
| `git log --oneline -2` | `7d3af9d docs(quick-260530-h2h): ...` + `adb1053 fix(quick-260530-h2h): ...` |

Additional sanity gates the executor ran beyond the plan's required list:

- `TIDE_CERT_MANAGER_VERSION` env override + `v1.20.2` default + `rollout status deployment/` all literally present in `acceptance-v1.sh`.
- The `kubectl apply -f <URL>` line on script line 70 does NOT carry `--timeout` (deliberate — Go's `exec.CommandContext` timeout does not translate to a CLI flag for `apply`).
- The cert-manager install block (line 69 `==> installing cert-manager`) precedes the first `helm install tide` command (line 79).
- The `### cert-manager prerequisite` heading (INSTALL.md line 101) precedes both `helm install tide` command lines (129 OCI, 149 cloned-repo).
- `docs/observability.md` and `docs/rbac.md` are NOT in the diff — cross-references are read-only links only.

## Deviations from Plan

### Auto-fixed (Rule 3 — blocking)

**1. [Rule 3 — Blocking] Reworded comment + doc prose to avoid false-positive in plan's order-check regex**

- **Found during:** Task 1 + Task 2 verification gates.
- **Issue:** The plan's `<verify>` block uses `grep -nE "helm install tide " | head -1 | cut -d: -f1` to find the first `helm install tide`-shaped line, then asserts the cert-manager block / heading line is less than it. That regex matches not just literal `helm install tide ...` command lines but also **prose** that quotes the command inline (with a trailing space before `...`).
  - In `hack/scripts/acceptance-v1.sh`, my initial comment said `# installed BEFORE 'helm install tide ...' below.` — caught by the regex, false-failed the gate.
  - In `docs/INSTALL.md`, the **pre-existing** Pitfall-4 paragraph on line 97 says `... \`helm install tide ...\` hangs at ...` — also caught by the regex; this line predates my edit.
- **Fix:**
  - `acceptance-v1.sh`: changed comment to "installed BEFORE the helm-install steps below." (no quoted command form).
  - `docs/INSTALL.md`: changed my rationale paragraph to "must be installed before the `tide` chart's helm-install step" instead of "before `helm install tide ...`". The pre-existing line-97 Pitfall-4 paragraph was NOT modified — only the new subsection prose was.
- **Files modified:** `hack/scripts/acceptance-v1.sh`, `docs/INSTALL.md` (both inside the same task commits, no separate commit).
- **Why the line-97 issue is benign:** The functional ordering is correct — the new `### cert-manager prerequisite` heading at INSTALL.md line 101 precedes both actual `helm install tide` command lines (129 in the Quickstart code block, 149 in the cloned-repo code block). The plan author's intent ("subsection lands AFTER the line-97 'CRDs first' paragraph and BEFORE the line-101 `### Quickstart` heading") is honored exactly. The over-broad regex catches the doc's own pre-existing inline command-quote, which would only be fixable by editing the Pitfall-4 paragraph — explicitly out-of-scope per the plan ("Do NOT modify any other section of `docs/INSTALL.md`"). The plan-level `<verification>` block (orchestrator step 6) uses a different set of gates (5 checks, none of which use this specific order regex), and all 5 of those pass cleanly above.
- **Suggested future fix (for the next plan author, not this executor):** When the gate is "subsection must precede the first command", anchor the regex to start-of-line (`^helm install tide `) so prose quoting doesn't false-match.

### Other

None. Both tasks executed exactly as specified, with the prescribed commit message shapes, exact files touched, and `charts/` left untouched.

## Authentication gates

None encountered. Executor did NOT invoke cert-manager, did NOT run `make acceptance-v1`, did NOT apply anything to a cluster — verification was syntactic only, as the plan instructed.

## Untouched (as required)

- `charts/` — entire tree, including `charts/tide/templates/{serving-cert,selfsigned-issuer,metrics-certs}.yaml`. Chart contract preserved per Phase 02.2 anti-pattern (CLAUDE.md §"chart is FIXED contract").
- `docs/observability.md` — referenced read-only via relative link.
- `docs/rbac.md` — referenced read-only via relative link.
- `test/integration/kind/suite_test.go` — pattern reference only, not modified.
- `hack/scripts/acceptance-v1.sh` lines 1-59 (cluster bring-up + env gate) and 76+ (helm + Secret + Project apply + evidence capture + verifier invocation) — only lines 61-74 are net-new.
- `docs/INSTALL.md` lines 1-99 (everything before the Pitfall-4 paragraph close) and 123+ (Quickstart through end-of-file) — only lines 101-122 are net-new.

## Note for the orchestrator

This executor did **not** invoke `make acceptance-v1`, `cert-manager`, `kubectl apply` against any cluster, or any other side-effecting command. The script + doc fixes are landed and syntactically verified; functional verification (re-running `make acceptance-v1`) is the orchestrator's responsibility in the post-execute step.

The orchestrator should also handle (per the plan's `<output>` block and `<verification>` step 8):

1. Final metadata commit covering `.planning/quick/260530-h2h-boot-04-acceptance-v1-cert-manager-prere/260530-h2h-SUMMARY.md`, `.planning/STATE.md`, and (if used by this quick-task surface) `.planning/ROADMAP.md`.
2. The `## Discovered 2026-05-30 ... **RESOLVED 2026-05-30**` entry in `deferred-items.md` with the two actual commit SHAs: `adb1053` (script fix) + `7d3af9d` (docs fix).
3. Re-running `make acceptance-v1` against a fresh kind cluster (now that the cert-manager prereq is in the script) to confirm the BOOT-04 cascade is closed.

## Self-Check: PASSED

- `hack/scripts/acceptance-v1.sh` exists with cert-manager block at lines 61-74 — FOUND.
- `docs/INSTALL.md` exists with `### cert-manager prerequisite` heading at line 101 — FOUND.
- Commit `adb1053` exists on `worktree-agent-a1585fd6189b8bed3` — FOUND (`fix(quick-260530-h2h): install cert-manager v1.20.2 before helm install tide in acceptance-v1.sh`).
- Commit `7d3af9d` exists on `worktree-agent-a1585fd6189b8bed3` — FOUND (`docs(quick-260530-h2h): document cert-manager v1.20.2 prerequisite in INSTALL.md`).
- `charts/` untouched across both commits — VERIFIED (`git diff --name-only HEAD~2..HEAD -- charts/` returns empty).
- `docs/observability.md` + `docs/rbac.md` untouched — VERIFIED (not in `git diff --name-only HEAD~2..HEAD`).
- `bash -n hack/scripts/acceptance-v1.sh` exits 0 — VERIFIED.
