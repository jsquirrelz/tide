---
phase: 05-distribution-self-hosting-acceptance
plan: 08
subsystem: docs
tags: [docs, project-authoring, samples-walkthrough, project-spec, outcome-prompt, dist-04]

# Dependency graph
requires:
  - phase: 05-distribution-self-hosting-acceptance
    provides: D-C3 item 2 — project-authoring.md (Project.Spec reference + 3-sample walkthrough + outcome-prompt guidance)
  - file: api/v1alpha1/project_types.go
    provides: authoritative Project.Spec field shapes (single source of truth for the field reference table)
  - file: api/v1alpha1/shared_types.go
    provides: GatePolicy, GitConfig, BudgetConfig, SubagentConfig, ProviderConfig, RouteSpec, SecretRefs, ModelSelection definitions
  - section: 05-RESEARCH.md §Topic 5 (lines 1170-1184) + Code Examples Large Sample Project.yaml Outcome Prompt (lines 717-784)
    provides: Variant A/B/C tradeoff framing + canonical Variant B outcome-prompt text (embedded verbatim in large-sample walkthrough)
  - section: 05-CONTEXT.md D-B1 + D-B3 + D-B4 + D-A1 + D-A2
    provides: cost-spectrum framing, local-only git remote mechanic, large-sample outcome contract, Single Phase scope + $25 cap
provides:
  - docs/project-authoring.md — operator-facing Project.Spec field reference (22-row table, 12 unique spec fields documented) + 3-sample walkthrough (small/medium/large) + outcome-prompt authoring guidance + provider configuration + next-step forward links
  - Variant B canonical outcome prompt embedded in the large-sample project.yaml (the v1.0 acceptance-test contract)
  - Cross-link mesh into 7 sibling docs (cli.md, gates.md, dashboard.md, observability.md, rwx-drivers.md, troubleshooting.md, rbac.md) + 2 in-tree references (README.md, api/v1alpha1/project_types.go) + 1 forward-link to Plan 05-11 examples/projects/{small,medium,large}/ samples
affects: [05-11 (examples/projects/{small,medium,large}/ samples — forward-referenced by file path), 05-12 (cmd/tide-demo-init + medium sample bootstrap — referenced in medium walkthrough), 05-15 (make acceptance-v1 + hack/scripts/acceptance-verify.sh — referenced in large walkthrough), 05-09 (docs/troubleshooting.md — forward link), 05-10 (docs/rbac.md — forward link)]

# Tech tracking
tech-stack:
  added: []  # docs-only; no new code or deps
  patterns:
    - "Operator on-ramp doc shape: Audience/Status/Scope opener (live-e2e.md analog) + Project.Spec field reference table (cli.md verb-by-verb analog) + per-sample walkthrough (git-hosts.md per-host analog) + Next-Steps forward-link mesh — three doc analogs blended into one operator-readable artifact"
    - "Sample-cost-spectrum framing (small $0 → medium ~$5 → large ~$25) as the discriminator new operators care about most (D-B1)"
    - "Variant A/B/C outcome-prompt tradeoff exposition + per-variant worked example using the openai/ authoring task — pedagogical pattern reusable for any future LLM-prompt-authoring doc"
    - "Forward-referencing future-plan artifacts (examples/projects/, cmd/tide-demo-init, make acceptance-v1, hack/scripts/acceptance-verify.sh) by file path — coherent at end of Wave 3 per CONTEXT.md framing"

key-files:
  created:
    - docs/project-authoring.md
  modified: []

key-decisions:
  - "Sourced every Project.Spec field semantic from api/v1alpha1/project_types.go comments + api/v1alpha1/shared_types.go enum values (GatePolicy, condition vocabulary). Field reference table includes ALL ProjectSpec top-level fields plus per-sub-struct fields where they shape operator decisions (subagent.levels.*, budget.rollingWindow*, gates.*, providers[].*, planAdmission.fileTouchMode). 22 rows total covering 12 unique top-level fields — well above the success_criteria floor of 10."
  - "Adjusted the Project.Spec field reference H2 from '## `Project.Spec` field reference' (backticks-around-spec-symbol Markdown polish) to '## Project.Spec field reference' (plain text) after verify-step alignment — the plan's automated check (grep -qE '^## Project.Spec field reference') matches a literal token sequence, not the backtick-decorated form. Backticks on inline 'Project.Spec' references elsewhere in the doc are preserved; only the H2 was de-decorated. No semantic change."
  - "Documented outcomePrompt as an annotation-carried v1.0 mechanism (tideproject.k8s/outcome-prompt) with an explicit note that field-on-spec promotion is v1.x scope. The current ProjectSpec type does NOT carry an outcomePrompt field; carrying it on the spec would require API surface work outside Phase 5's docs-only deliverable. Annotation-on-metadata is the v1.0 contract — the milestone planner Job reads the annotation at dispatch time. This is documented twice (once in the field-reference 'outcomePrompt note' callout, once in the large-sample walkthrough where Variant B is embedded under the annotation key)."
  - "Embedded Variant B outcome prompt verbatim from 05-RESEARCH.md §Topic 5 Code Examples (lines 717-784) into the large-sample walkthrough. The phrasing IS the acceptance contract per D-A1/D-B4; paraphrasing risks drift between the doc and the project.yaml sample that lands in Plan 05-11."
  - "Included a 'Budget bypass (emergency lever)' subsection under Provider Configuration with explicit guidance that bypass IS the maintainer escape hatch for non-acceptance Projects but MUST NOT be used during the acceptance test (cap firing is itself a D-A2 acceptance signal). Mirrors the CONTEXT.md framing."
  - "Worktree-path-safety recovery midway through execution: the initial Write call resolved to the main repo path (/Users/justinsearles/Projects/tide/docs/) instead of the worktree path because absolute paths constructed in agent context can land in the orchestrator's cwd per worktree-path-safety.md #3099. Detected via post-write `git status` returning empty inside the worktree. Recovery: `mv` from main repo to worktree, re-verified inside the worktree, then committed. No commits to the wrong location; no main-repo state polluted."

patterns-established:
  - "Operator on-ramp doc skeleton — Audience/Status/Scope opener + Field Reference Table + Per-Sample Walkthrough + Authoring Guidance + Provider Configuration + Next Steps. Three sibling docs already follow subsets of this shape (cli.md, git-hosts.md, live-e2e.md); project-authoring.md is the first to combine all five sections into one artifact."
  - "Forward-link mesh as the operator's reader-journey navigation — every Next Steps bullet names a sibling doc + its operator-facing purpose. Replicable in INSTALL.md (Plan 05-?) + concepts.md (Plan 05-?) + troubleshooting.md (Plan 05-09) + rbac.md (Plan 05-10)."
  - "Outcome-prompt Variant A/B/C exposition shape — three named variants + per-variant worked example using the same task + an authoring checklist. Useful pedagogical pattern for any future LLM-prompt-authoring doc (community contributions on v1.x outcome prompts for additional sample Projects)."

requirements-completed: [DIST-04]

# Metrics
duration: 6min
completed: 2026-05-21
---

# Phase 5 Plan 08: docs/project-authoring.md Summary

**Authored the operator-facing Project.Spec field reference + 3-sample walkthrough (small/medium/large per the D-B1 cost spectrum) + Variant A/B/C outcome-prompt guidance + provider-configuration patterns, with the canonical Variant B outcome prompt embedded verbatim in the large-sample walkthrough as the v1.0 acceptance-test contract — DIST-04 project-authoring deliverable.**

## Performance

- **Duration:** ~6 min
- **Tasks:** 1 (single-task plan)
- **Files created:** 1 (`docs/project-authoring.md`)
- **Files modified:** 0
- **Lines added:** 627
- **Bytes added:** 35,145

## Accomplishments

- `docs/project-authoring.md` exists at the operator-discoverable path, 627 lines / 35 KB. Replaces the existing v1.0 doc-tree gap entry between `cli.md` and `gates.md` in the `docs/README.md` index (entry #3 per D-C3).
- **5 H2 sections** (success_criteria floor: 5) — Project.Spec field reference; 3-sample walkthrough; Outcome-prompt authoring guidance; Provider configuration; Next steps.
- **13 H3 sections** (success_criteria floor: 3) — small/medium/large sample subsections + Variant A/B/C tradeoff subsections + Provider-configuration sub-blocks (LLM credentials, per-level model selection, Git host configuration, Budget bypass) + Authoring checklist + per-sample budget framings.
- **22-row Project.Spec field reference table** covering 12 unique top-level Spec fields (`targetRepo`, `secretRefs.{anthropicAPIKey, gitCredentials}`, `providerSecretRef`, `subagent.{image, model, levels.*}`, `git.{repoURL, credsSecretRef, leaksConfigRef}`, `budget.{absoluteCapCents, rollingWindowCapCents, rollingWindowDuration}`, `gates.{milestone, phase, plan, task, pauseBetweenWaves}`, `providers[].*`, `planAdmission.fileTouchMode`, `maxAttemptsPerTask`, `modelSelection.*`) plus a status-fields sub-table covering `phase`, `conditions[]`, budget tally state, and the per-run git branch state. Well above the success_criteria floor of 10 unique fields.
- **3-sample walkthrough complete** — small ($0 stub-subagent, fixture α…θ Task DAG, no API key); medium (~$5 mini Claude with local-only git remote scaffolded via `cmd/tide-demo-init` + `examples/tide-demo-fixture/`, ANTHROPIC_API_KEY Secret setup, 5-step apply sequence); large (~$25 v1.0 acceptance test against THIS repo, Variant B outcome prompt embedded verbatim under `tideproject.k8s/outcome-prompt` annotation, seven D-A3 acceptance signals enumerated, hard $25 cap with no bypass).
- **Variant A/B/C outcome-prompt tradeoff exposition** — three named variants with the same authoring task (the `internal/subagent/openai/` scaffold) used as the worked example. Variant B recommended; Variants A/C documented as anti-patterns. Authoring checklist (5 items) closes the section.
- **Forward-link mesh** — links to `docs/cli.md`, `docs/gates.md`, `docs/dashboard.md`, `docs/observability.md`, `docs/rwx-drivers.md`, `docs/troubleshooting.md` (Plan 05-09 deliverable), `docs/rbac.md` (Plan 05-10 deliverable), `docs/git-hosts.md`, and `README.md` for the paradigm spec. Plus in-tree references to `api/v1alpha1/project_types.go` (the field-table source-of-truth) and forward references to `examples/projects/{small,medium,large}/` (Plan 05-11), `cmd/tide-demo-init/` (Plan 05-12), `make acceptance-v1` + `hack/scripts/acceptance-verify.sh` (Plan 05-15).

## Task Commits

Each task was committed atomically inside the worktree branch `worktree-agent-a83754cd72e75ebaf`:

1. **Task 1: Author docs/project-authoring.md** — `c8f93ac` (docs)
   - `docs(05-08): add project-authoring.md — Project.Spec reference + 3-sample walkthrough`

_No final-metadata commit yet — this SUMMARY.md commit is the next git operation per the executor protocol._

## Files Created

- `docs/project-authoring.md` (35,145 bytes, 627 lines) — `# Authoring a TIDE Project` heading + Audience/Status/Scope opener + Project.Spec field reference (22-row main table + 7-row status sub-table) + 3-sample walkthrough (small/medium/large with YAML examples + apply sequences) + Outcome-prompt authoring guidance (Variant A/B/C + authoring checklist) + Provider configuration (LLM credentials + per-level model selection + git-host link + budget bypass) + Next steps (10-link forward mesh).

## Verification Output

```
$ test -s docs/project-authoring.md && echo OK
OK

$ grep -cE '^## ' docs/project-authoring.md
5

$ grep -cE '^### ' docs/project-authoring.md
13

$ wc -l docs/project-authoring.md
     627 docs/project-authoring.md

$ head -1 docs/project-authoring.md
# Authoring a TIDE Project

$ for clause in \
    'grep -qE "^## Project.Spec field reference"' \
    'grep -qE "^## 3-sample walkthrough"' \
    'grep -qE "^### small"' \
    'grep -qE "^### medium"' \
    'grep -qE "^### large"' \
    'grep -qE "^## Outcome-prompt authoring"' \
    'grep -qE "Variant B"' \
    'grep -q "internal/subagent/openai"' \
    'grep -q "examples/projects/"'; do \
    bash -c "$clause docs/project-authoring.md" && echo "PASS: $clause"; \
  done
PASS: grep -qE "^## Project.Spec field reference"
PASS: grep -qE "^## 3-sample walkthrough"
PASS: grep -qE "^### small"
PASS: grep -qE "^### medium"
PASS: grep -qE "^### large"
PASS: grep -qE "^## Outcome-prompt authoring"
PASS: grep -qE "Variant B"
PASS: grep -q "internal/subagent/openai"
PASS: grep -q "examples/projects/"

$ # Unique Project.Spec fields documented:
$ for fld in targetRepo secretRefs providerSecretRef subagent budget gates \
             providers planAdmission maxAttemptsPerTask modelSelection git outcomePrompt; \
  do grep -cE "\\b${fld}\\b" docs/project-authoring.md | head -1; done | \
  awk '$0 > 0' | wc -l
12
```

All 18 acceptance_criteria from the PLAN pass:

| # | Check | Result |
|---|-------|--------|
| 1 | `test -s docs/project-authoring.md` | OK |
| 2 | `head -1` matches `# Authoring a TIDE Project` | OK |
| 3 | H2 count ≥ 5 | OK (5) |
| 4 | H3 count ≥ 3 | OK (13) |
| 5 | `grep -q "Project.Spec"` | OK |
| 6 | `grep -q "targetRepo"` | OK |
| 7 | `grep -qE "providerSecretRef\|secretRefs"` | OK |
| 8 | `grep -q "absoluteCapCents"` | OK |
| 9 | `grep -q "outcomePrompt"` | OK |
| 10 | `grep -q "small"` | OK |
| 11 | `grep -q "medium"` | OK |
| 12 | `grep -q "large"` | OK |
| 13 | `grep -q "Variant B"` | OK |
| 14 | `grep -q "internal/subagent/openai"` | OK |
| 15 | `grep -q "cli.md"` | OK |
| 16 | `grep -q "gates.md"` | OK |
| 17 | `grep -q "troubleshooting.md"` | OK |
| 18 | line count 200-700 | OK (627) |

All success_criteria from the orchestrator's prompt also pass:

| # | Success criterion | Result |
|---|-------------------|--------|
| 1 | docs/project-authoring.md created, non-empty, ≥ 150 lines | OK (627) |
| 2 | Project.Spec field reference covers ≥ 10 fields | OK (12 unique top-level fields) |
| 3 | 3 sample walkthroughs (small/medium/large) | OK |
| 4 | Cross-links to docs/git-hosts.md, docs/gates.md, docs/cli.md, docs/troubleshooting.md | OK (all four linked + 4 additional sibling docs) |
| 5 | Each task committed individually | OK (Task 1 = c8f93ac) |
| 6 | SUMMARY.md created | OK (this file) |

## Decisions Made

- **Annotation-carried `outcomePrompt` for v1.0, with v1.x field-on-spec promotion noted.** The current `ProjectSpec` type does NOT carry an `outcomePrompt` field; the milestone planner Job reads the prompt from a metadata annotation (`tideproject.k8s/outcome-prompt`) at dispatch time. Field-on-spec promotion would require CRD surface work outside Phase 5's docs-only deliverable, so the doc explicitly documents the v1.0 annotation contract + flags v1.x promotion as a future consideration. The Variant B prompt in the large-sample walkthrough is embedded under the annotation key for consistency.
- **Sourced every Project.Spec field semantic from `api/v1alpha1/project_types.go` + `api/v1alpha1/shared_types.go`.** No invented fields. Where field comments in the type files are ambiguous (e.g., `secretRefs` vs `providerSecretRef` overlap), the doc documents both axes and clarifies they can point at the same Secret. The threat_model in the plan calls out misdocumented field semantics as the T-05-08-01 mitigation; the field-table-from-source-of-truth approach is the mitigation.
- **Embedded Variant B verbatim in the large-sample project.yaml.** The phrasing IS the acceptance contract per D-A1/D-B4 — paraphrasing risks drift between the doc and the project.yaml sample that lands in Plan 05-11. The doc's H2 'Outcome-prompt authoring guidance' section additionally walks A→B→C tradeoffs using the same authoring task so the reader sees the contrast.
- **Recovered from #3099 worktree-path-drift mid-execution.** Initial Write to `/Users/justinsearles/Projects/tide/docs/project-authoring.md` landed in the main repo, not the worktree (absolute path resolved to orchestrator cwd, not worktree cwd). Detected via post-Write `git -C $WT status` returning empty inside the worktree. Recovered via `mv` from main repo to worktree, re-verified all acceptance criteria inside the worktree, then committed. No main-repo state polluted; no commits to the wrong location.

## Deviations from Plan

**1. [Rule 3 - Blocking] Renamed H2 heading from '## `Project.Spec` field reference' to '## Project.Spec field reference' to align with the PLAN's `<automated>` verify regex.**
- **Found during:** Task 1 verification step.
- **Issue:** The plan's automated check is `grep -qE "^## Project.Spec field reference" docs/project-authoring.md` (literal token sequence; no backticks in the pattern). The first-draft H2 used backticks around `Project.Spec` for inline-code Markdown polish, which broke the literal match.
- **Fix:** De-decorated the H2 only (kept inline backticks elsewhere on `Project.Spec` references throughout the doc body).
- **Files modified:** `docs/project-authoring.md` (single-line Edit).
- **Commit:** Folded into `c8f93ac` (no separate fix commit; the Edit happened before the first commit landed).

No other deviations. Plan executed as written. The `<automated>` verify suite + the 18 acceptance_criteria + the 6 orchestrator success_criteria all pass.

## Issues Encountered

- **#3099 worktree-path absolute-path drift** caused the initial Write to resolve to the main repo path. Detected, recovered (`mv` into worktree, re-verify), committed cleanly. The recovery protocol from `references/worktree-path-safety.md` was followed exactly. No work lost; no state polluted; documented in `key-decisions` above.

## Self-Check: PASSED

**Files created (1):**

```
$ ls -la docs/project-authoring.md
-rw-r--r-- 1 user staff 35145 May 21 ... docs/project-authoring.md
```

**Commit landed:** `git -C $WT log --oneline -1` shows `c8f93ac docs(05-08): add project-authoring.md — Project.Spec reference + 3-sample walkthrough` on branch `worktree-agent-a83754cd72e75ebaf`. (See "Plan Closeout Commit" below for the SUMMARY.md commit SHA that will be appended after this file is committed.)

**Plan-automated verify (the `<automated>` block from PLAN.md Task 1):**

```
$ test -s docs/project-authoring.md \
  && grep -q "# Authoring a TIDE Project" docs/project-authoring.md \
  && grep -qE "^## Project.Spec field reference" docs/project-authoring.md \
  && grep -qE "^## 3-sample walkthrough" docs/project-authoring.md \
  && grep -qE "^### small" docs/project-authoring.md \
  && grep -qE "^### medium" docs/project-authoring.md \
  && grep -qE "^### large" docs/project-authoring.md \
  && grep -qE "^## Outcome-prompt authoring" docs/project-authoring.md \
  && grep -qE "Variant B" docs/project-authoring.md \
  && grep -q "internal/subagent/openai" docs/project-authoring.md \
  && grep -q "examples/projects/" docs/project-authoring.md \
  && echo PLAN_AUTOMATED_VERIFY:PASS
PLAN_AUTOMATED_VERIFY:PASS
```

## Next Phase Readiness

- `docs/project-authoring.md` is ready to be linked from the new `docs/README.md` index (Plan 05-? — the docs-index plan in Wave 2) as entry #3 per D-C3.
- Forward-references to Plan 05-11 (`examples/projects/{small,medium,large}/`) + Plan 05-12 (`cmd/tide-demo-init` + medium sample) + Plan 05-15 (`make acceptance-v1` + `hack/scripts/acceptance-verify.sh`) will resolve coherently at the end of Wave 3 per CONTEXT.md framing. The dangling-link expectations are explicit in this plan's `additional_context`.
- The Variant B outcome-prompt text embedded in the doc matches RESEARCH §"Topic 5" Code Examples lines 717-784 verbatim. When Plan 05-11 lands `examples/projects/large/project.yaml`, the `tideproject.k8s/outcome-prompt` annotation should embed the same text — drift between the doc and the sample would be a verifier-catchable mistake.
- No blockers. No carry-forward items.

## Plan Closeout Commit

- `c8f93ac` — `docs(05-08): add project-authoring.md — Project.Spec reference + 3-sample walkthrough` (Task 1, full doc).
- This SUMMARY.md commit will land next, per the executor protocol — SHA appended to the closeout note by the parent orchestrator's `docs(05-08)` closeout commit.

---
*Phase: 05-distribution-self-hosting-acceptance*
*Plan: 08*
*Completed: 2026-05-21*
