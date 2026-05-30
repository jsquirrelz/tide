---
phase: quick-260530-hrc
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - .planning/ROADMAP.md
  - .planning/STATE.md
  - .planning/phases/05-distribution-self-hosting-acceptance/deferred-items.md
  - .planning/phases/06-v1-image-publish-and-ship-readiness-revalidation/06-FINDINGS.md
autonomous: true
requirements:
  - QUICK-260530-hrc-A  # ROADMAP carries Phase 6 row + section heading + back-reference to FINDINGS doc
  - QUICK-260530-hrc-B  # STATE.md frontmatter + body reframed to reflect Phase 5 closure was premature (8/9 → in-progress milestone)
  - QUICK-260530-hrc-C  # 06-FINDINGS.md authored as scope-of-record for the SPEC/DISCUSS/PLAN sessions to follow
  - QUICK-260530-hrc-D  # Phase 5 deferred-items.md APPENDED with a new 2026-05-30 entry pointing forward to Phase 6
user_setup: []

must_haves:
  truths:
    - "`.planning/ROADMAP.md` carries a new Phase 6 row in the Progress table (`| 6. v1.0 Image-Publish Pipeline & Ship-Readiness Revalidation | 0/0 (planning) | Not started |     |`) AND a `### Phase 6:` section heading below Phase 5 with a placeholder pointer to `06-FINDINGS.md` for scope (no plan rows enumerated)"
    - "`.planning/ROADMAP.md` overview list at lines 19-23 carries a new Phase 6 bullet (preserves the established `[ ]` convention even though Phases 1-5 boxes are also `[ ]` per Plan 05-17's deviation note)"
    - "`.planning/phases/06-v1-image-publish-and-ship-readiness-revalidation/06-FINDINGS.md` exists as the scope-of-record doc covering (a) what happened today, (b) root cause, (c) what's already in main, (d) proposed Phase 6 scope as DRAFT requirement seeds, (e) explicit note that requirements get finalized by `/gsd-discuss-phase` + `/gsd-plan-phase` next session"
    - "`.planning/STATE.md` frontmatter reframes the milestone: `total_phases: 8 → 9`, `completed_phases: 8` (unchanged), `total_plans: 100` (unchanged), `completed_plans: 100` (unchanged), `percent: 100 → 88` (8/9), `status: completed → in_progress`, `stopped_at` reframed, `last_updated` bumped to today, `last_activity` reflects this Phase 6 opening with today's date"
    - "`.planning/STATE.md` body Current Position prose matches the frontmatter reframe (Phase 5 stays Complete in the narrative; Phase 6 is the new in-progress phase that needs SPEC/DISCUSS/PLAN/EXECUTE cycles); Progress bar updated to `[████████░░]` 88%"
    - "`.planning/phases/05-distribution-self-hosting-acceptance/deferred-items.md` APPENDED (NOT modified above the last entry) with a new heading `## Discovered 2026-05-30 (during BOOT-04 acceptance retry, second cascade) — **DEFERRED to Phase 6**` carrying brief description + forward-pointer to `06-FINDINGS.md`"
    - "Charts (`charts/`), `.goreleaser.yaml`, both acceptance scripts (`hack/scripts/{acceptance-v1,dry-run-v1}.sh`), `README.md`, `docs/INSTALL.md`, and `Makefile` are ALL untouched (chart contract preserved; these files belong to Phase 6 execution plans, NOT this opening task)"
    - "Atomic commits land on main — planner chose split: (a) `chore(06): open Phase 6 — v1.0 image-publish pipeline + acceptance revalidation` for ROADMAP + STATE, (b) `docs(06): author 06-FINDINGS.md + back-reference from Phase 5 deferred-items` for the docs"
    - "Planning artifacts that are NOT in executor scope (the PLAN.md you are reading; the eventual SUMMARY.md) are NOT committed by the executor — orchestrator step 8 handles those"
  artifacts:
    - path: ".planning/ROADMAP.md"
      provides: "Phase 6 row + section heading + overview bullet"
      contains: "v1.0 Image-Publish Pipeline"
      contains_token: "0/0 (planning)"
    - path: ".planning/phases/06-v1-image-publish-and-ship-readiness-revalidation/06-FINDINGS.md"
      provides: "Scope-of-record findings doc covering today's BOOT-04 second-cascade discovery, root cause, what's-in-main, proposed scope as DRAFT"
      contains: "ImagePullBackOff"
      contains_token: ".goreleaser.yaml"
      contains_token: "DRAFT"
      min_lines: 80
    - path: ".planning/STATE.md"
      provides: "Reframed milestone state — 8/9 phases, in_progress, Phase 6 is the new active phase"
      contains: "total_phases: 9"
      contains_token: "percent: 88"
      contains_token: "in_progress"
    - path: ".planning/phases/05-distribution-self-hosting-acceptance/deferred-items.md"
      provides: "Back-reference entry pointing forward to 06-FINDINGS.md"
      contains: "DEFERRED to Phase 6"
      contains_token: "06-FINDINGS.md"
  key_links:
    - from: ".planning/ROADMAP.md Phase 6 section heading"
      to: ".planning/phases/06-v1-image-publish-and-ship-readiness-revalidation/06-FINDINGS.md"
      via: "placeholder paragraph 'TBD — see ... for scope'"
      pattern: "06-FINDINGS\\.md"
    - from: ".planning/phases/05-distribution-self-hosting-acceptance/deferred-items.md new entry"
      to: ".planning/phases/06-v1-image-publish-and-ship-readiness-revalidation/06-FINDINGS.md"
      via: "forward-pointer in the entry body"
      pattern: "06-FINDINGS\\.md"
    - from: ".planning/STATE.md body Current Position"
      to: ".planning/phases/06-v1-image-publish-and-ship-readiness-revalidation/06-FINDINGS.md"
      via: "Current focus paragraph names the FINDINGS doc as the next read"
      pattern: "06-FINDINGS\\.md"
---

<objective>
Open Phase 6 — v1.0 image-publish pipeline + ship-readiness revalidation — by landing the planning bookkeeping (ROADMAP row + STATE reframe + Phase 5 back-reference) and authoring the findings doc that subsequent `/gsd-spec-phase` + `/gsd-discuss-phase` + `/gsd-plan-phase` sessions will consume as scope-of-record.

Purpose: Today's BOOT-04 acceptance ritual (second retry, BG task `bs3ntw3rt` at 2026-05-30T16:25:00Z) made it past the cert-manager prereq fix from this morning's quick task `260530-h2h` but then revealed a deeper gap: `kubectl wait --for=condition=Available deploy/tide-controller-manager` timed out at 5m, and `kubectl describe pod` showed the controller stuck Pending on `ghcr.io/jsquirrelz/tide-controller:v0.1.0-dev` AND the dashboard pod in `ImagePullBackOff` on `ghcr.io/jsquirrelz/tide-dashboard:1.0.0`. Root cause: `.goreleaser.yaml` builds ONLY the `tide` CLI binary — no `dockers:` section, no `docker_manifests:`, no workflow publishes the 6 component images `charts/tide/values.yaml` references. Phase 5 closed claiming v1.0 ship-ready, but BOOT-04 was never actually executed end-to-end before today's attempt; the operator-only D-A4 gate didn't catch the gap. Phase 6 picks up the slack.

Output: Four atomic changes across two commits — (1) ROADMAP + STATE reframed to reflect 8/9 milestone phases (Phase 6 now in_progress), (2) `06-FINDINGS.md` authored as the scope-of-record document for SPEC/DISCUSS/PLAN to consume next session, with proposed scope labeled DRAFT to preserve final-requirement authority for the discuss phase.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@./CLAUDE.md
@.planning/ROADMAP.md
@.planning/STATE.md
@.planning/phases/05-distribution-self-hosting-acceptance/deferred-items.md

<scope_discipline>
This quick task is the "open the phase" step in a multi-session ramp-up to Phase 6 execution. The actual fix work (image build/push pipeline, chart-tag SOT alignment, dry-run-v1.sh cert-manager fix, image-load fallback, BOOT-04 revalidation, README cleanup) is OUT OF SCOPE for this task — those changes will be authored by `/gsd-plan-phase` and executed by `/gsd-execute-phase` in subsequent sessions.

The four-file edit surface below is the ENTIRE executor scope:

1. `.planning/ROADMAP.md` — add Phase 6 row + heading + overview bullet
2. `.planning/STATE.md` — reframe frontmatter + body for 8/9 milestone
3. `.planning/phases/05-distribution-self-hosting-acceptance/deferred-items.md` — APPEND back-reference entry
4. `.planning/phases/06-v1-image-publish-and-ship-readiness-revalidation/06-FINDINGS.md` — author the scope-of-record doc

NOTHING ELSE. Any urge to fix the image-publish pipeline NOW must be resisted — Phase 6 execution will do that work with proper scope, plans, and verification gates.
</scope_discipline>

<failure_observed>
Today's second BOOT-04 attempt (BG task `bs3ntw3rt`, 2026-05-30T16:25:00Z onward) progressed further than this morning's `bess2gftr` cascade-1 attempt (which failed on cert-manager CRDs — fixed in commits `adb1053` + `7d3af9d` via quick task `260530-h2h`). The cluster bring-up + cert-manager install + BOTH helm install commands ALL succeeded:

```
helm install tide-crds → STATUS: deployed
helm install tide      → STATUS: deployed
```

But the next gate failed:

```
kubectl wait --for=condition=Available deploy/tide-controller-manager -n tide-system --timeout=5m
error: timed out waiting for the condition on deployments/tide-controller-manager
```

`kubectl describe pod` showed:

- `tide-controller-manager-*`: Pending, 0/1 ready, ImagePullBackOff on `ghcr.io/jsquirrelz/tide-controller:v0.1.0-dev`
- `tide-dashboard-d675d58d4-s2cfc`: ImagePullBackOff on `ghcr.io/jsquirrelz/tide-dashboard:1.0.0`

Neither image exists on ghcr.io. The chart references 6 component images total; none of them have ever been published by any workflow.
</failure_observed>

<root_cause>
Two structural gaps surfaced today:

1. **No image-publish pipeline exists.** `.goreleaser.yaml` lines 30-51 have one `builds:` entry for the `tide` CLI binary only. No `dockers:` section; no `docker_manifests:`. `grep -lrnE 'docker push|docker buildx|ko build|kaniko' .github/workflows/` returns zero hits. After tagging `v1.0.0`, the published charts will reference 6 images that don't exist.

2. **Chart values.yaml component tags drifted relative to dashboard.** Plan 05-05 bumped the Helm chart version from `0.1.0-dev` → `1.0.0` (lockstep), but `charts/tide/values.yaml` still hardcodes 5 component tags at `v0.1.0-dev`:
   - `controllerManager.manager.image.tag: v0.1.0-dev` (line 39)
   - `images.stubSubagent.tag: v0.1.0-dev` (line 140)
   - `images.credProxy.tag: v0.1.0-dev` (line 144)
   - `images.tidePush.tag: v0.1.0-dev` (line 155)
   - `images.claudeSubagent.tag: v0.1.0-dev` (line 165)

   Only `dashboard.image.tag: ""` (line 244) defaults to `.Chart.AppVersion` and therefore resolves to `1.0.0` — which is why `kubectl describe pod` showed the controller pulling `v0.1.0-dev` and the dashboard pulling `1.0.0`. None of the 6 images exist either way.

The deeper structural lesson: Phase 5 closed claiming v1.0 ship-ready, but BOOT-04 was never actually executed end-to-end before today's two-cascade attempt. By D-A4 design, BOOT-04 is the operator's responsibility (no CI integration). So the gate that should have caught both gaps wasn't in CI — it was in the operator's hands, and the operator (the user) is exercising it for the first time on 2026-05-30. Phase 6 is the catch-up.

Quick task `260530-h2h` (this morning) plugged the cert-manager prereq gap surfaced by cascade-1; this quick task opens Phase 6 to plug the image-publish + chart-tag-alignment gap surfaced by cascade-2.
</root_cause>

<whats_in_main>
Commits that landed today (referenced by `06-FINDINGS.md`'s "what's-in-main" section so SPEC/DISCUSS doesn't accidentally re-author these fixes):

- `489dd71` — `style(quick-260526-w11): gofmt cmd/dashboard/api/{plans,tasks}.go` (Phase 5 closeout polish, Plan 05-17 deferred-item resolution)
- `1769a60` — `docs(quick-260526-w11): reconcile ROADMAP Phase 5 Progress row to 17/17`
- `3f48032` — `docs(quick-260526-w11): Phase 5 closeout polish — SUMMARY + STATE + deferred-items RESOLVED`
- `adb1053` — `fix(quick-260530-h2h): install cert-manager v1.20.2 before helm install tide in acceptance-v1.sh` (closes cascade-1)
- `7d3af9d` — `docs(quick-260530-h2h): document cert-manager v1.20.2 prerequisite in INSTALL.md` (closes cascade-1)
- `a2193cf` — `docs(quick-260530-h2h): cert-manager prereq fix — SUMMARY + STATE + deferred-items RESOLVED`

The Phase 5 closure remains valid for everything Phase 5 actually delivered (LICENSE, NOTICE, docs, samples, chart additions, release.yaml chain, BOOT-02/BOOT-04 plumbing). Phase 6 is the missing-piece phase, not a Phase 5 reopen.
</whats_in_main>

<proposed_phase_6_scope_DRAFT>
The findings doc captures these as DRAFT proposed requirement seeds. `/gsd-discuss-phase` will lock the final REQ-IDs + scope; `/gsd-plan-phase` will decompose into plans. Each item below is one possible plan's worth of work.

- **IMG-01..05 — Docker image build + push pipeline** for all 6 components (`tide-controller`, `tide-dashboard`, `tide-stub-subagent`, `tide-credproxy`, `tide-push`, `tide-claude-subagent`). Multi-arch (linux/amd64 + linux/arm64). Most likely shape: extend `.goreleaser.yaml` `dockers:` + `docker_manifests:` sections, OR add a new `docker-build-push` job to `release.yaml` gated on `v*` tag push. Tag derived from `.Version` (e.g. `ghcr.io/jsquirrelz/tide-controller:v1.0.0`).

- **CHART-01 — Chart-side image-tag alignment.** SOT update at `hack/helm/tide-values.yaml`: 5 hardcoded `v0.1.0-dev` tags flipped to `""` so they default to `.Chart.AppVersion` (matching the dashboard pattern already in place). Augment-script re-run; `helm template` to verify the rendered image tags match the chart appVersion.

- **DRY-01 — `dry-run-v1.sh` cert-manager install** (same fix already in `acceptance-v1.sh` per commit `adb1053`). Without this, the rc-tag dry-run path (Door 3) is also deadlocked.

- **DRY-02 — Image-load fallback for local clusters.** Both `acceptance-v1.sh` and `dry-run-v1.sh` need a code path that builds the 6 component images locally + `kind load docker-image`s them into the cluster, for the case where the operator is testing pre-tag (no published images yet) or testing a tag that doesn't match HEAD. Plausibly a new Makefile target `acceptance-images-load` (deferred to Phase 6 — do not pre-create here).

- **ACC-01 — BOOT-04 end-to-end revalidation** with published images (or local image-load fallback). Demonstrates TIDE-on-TIDE actually works. Likely the closeout gate of Phase 6.

- **ACC-02 — README + INSTALL.md updates** to reflect actual ship state. Remove any premature "v1.0 ship-ready" claims; document `make container-images` if a local-build path is added.

- **Other plausible items** (deferred to discuss-phase prioritization): `test-int-kind-prep` cluster-name parameterization (currently hardcoded to `tide-test`); `.acceptance-runs/` gitignore (today's run littered the worktree with output files); operator-facing Troubleshooting entry for `ImagePullBackOff` mid-install.

All items above are labeled DRAFT in `06-FINDINGS.md`. The final scope is the `/gsd-discuss-phase` output, NOT this list.
</proposed_phase_6_scope_DRAFT>

<interfaces>
<!-- Key references the executor needs. Read these to find exact insertion points + counts. -->

From `.planning/ROADMAP.md` (current shape):

- **Overview list (lines 19-23):** Five `- [ ] **Phase N: ...**` bullets, one per current milestone phase. Phase 6 bullet must be appended at line 24 (after Phase 5's bullet at line 23), preserving the `- [ ] **Phase 6: <title>** — <one-liner>` shape. Use the established `[ ]` even though Phases 1-5 are `[ ]` too (Plan 05-17's deviation note documented why — Progress table is the SOT, overview is decorative).

- **Phase sections (after line 25 `## Phase Details`):** Each `### Phase N:` block has Goal / Depends on / Requirements / Success Criteria / Plans subheadings. Phase 6 section is a STUB only — `### Phase 6: v1.0 Image-Publish Pipeline & Ship-Readiness Revalidation`, `**Goal**: TBD — see `.planning/phases/06-v1-image-publish-and-ship-readiness-revalidation/06-FINDINGS.md` for scope.`, `**Depends on**: Phase 5`, `**Requirements**: TBD`, `**Plans:** 0/0 (planning)`. Insert it AFTER Phase 5's section (which ends with the Plans table at line 290 — the "Wave 6" section). Do NOT enumerate any plans (no `Plans:` bulleted list — the orchestrator's plan-phase command writes those next).

- **Progress table (lines 298-306):** Add a Phase 6 row to the bottom of the table BEFORE the closing footer `All 8 phases complete — TIDE v1.0 ship-ready.` line. The new row reads exactly:
  ```
  | 6. v1.0 Image-Publish Pipeline & Ship-Readiness Revalidation | 0/0 (planning) | Not started |     |
  ```
- **Closing footer (line 308):** UPDATE the closing footer from `All 8 phases complete — TIDE v1.0 ship-ready.` to something like `8 of 9 phases complete — Phase 6 in planning (v1.0 image-publish pipeline + ship-readiness revalidation).`

From `.planning/STATE.md` (current shape):

- **Frontmatter (lines 1-15):** Five field changes:
  - `status: completed` → `status: in_progress`
  - `stopped_at: Phase 5 closed — v1.0 ship-ready` → `stopped_at: Phase 6 opened — v1.0 image-publish pipeline + acceptance revalidation in planning`
  - `last_updated: "2026-05-30T16:25:00.000Z"` → `last_updated: "<bumped to current run timestamp; use 'date -u +%Y-%m-%dT%H:%M:%S.000Z' equivalent>"`
  - `last_activity:` (line 8) → replace the existing prose with a new 2026-05-30 entry naming this quick task (`Quick task 260530-hrc opened Phase 6 — v1.0 image-publish pipeline + ship-readiness revalidation`). Keep it terse but include: cascade-2 of today's BOOT-04 retry surfaced ImagePullBackOff on tide-controller + tide-dashboard; root cause = `.goreleaser.yaml` builds only CLI binary, no `dockers:` section, no workflow publishes the 6 component images; chart values.yaml component tags hardcoded `v0.1.0-dev` vs dashboard `""`-defaults to chart appVersion `1.0.0` — neither exists. Forward pointer to `.planning/phases/06-v1-image-publish-and-ship-readiness-revalidation/06-FINDINGS.md`.
  - `progress.total_phases: 8` → `total_phases: 9`
  - `progress.percent: 100` → `percent: 88`
  - `progress.completed_phases: 8` (unchanged)
  - `progress.completed_plans: 100` (unchanged)
  - `progress.total_plans: 100` (unchanged — bumps later when Phase 6 plans are authored)

- **Body Current Position section (lines 26-34):**
  - The narrative needs to flip from "all 8 phases complete" to "8 of 9 phases complete; Phase 6 opened by today's BOOT-04 second cascade."
  - Update `Phase:` line from `5 (distribution-self-hosting-acceptance) — COMPLETE` to `6 (v1-image-publish-and-ship-readiness-revalidation) — PLANNING`.
  - Update `Plan:` line from `17 of 17 — Phase 5 closeout commit landed; ...` to `0 of TBD — Phase 6 opened by quick task 260530-hrc; SPEC/DISCUSS/PLAN/EXECUTE cycles to follow`.
  - Update `Status:` line to a 1-2 sentence narrative: Phase 5 stays Complete (its deliverables shipped — LICENSE, docs, samples, chart additions, release.yaml chain, BOOT-02/BOOT-04 plumbing); Phase 6 is the catch-up for the image-publish pipeline gap surfaced by today's BOOT-04 cascade-2.
  - Update `Last activity:` line to a 1-sentence quick-task summary naming the four edited files + scope discipline note.

- **Progress bar (line 33):** Change `[██████████] 100%` to `[████████░░] 88%` (8 of 9 phases). Update the parenthetical from `(all 8 milestone phases complete — TIDE v1.0 ship-ready)` to `(8 of 9 milestone phases complete — Phase 6 in planning)`.

- **Performance Metrics / Quick Tasks Completed table (around line 145):** Append today's quick task row:
  ```
  | 260530-hrc | Open Phase 6 — v1.0 image-publish pipeline + ship-readiness revalidation (ROADMAP row + STATE reframe + 06-FINDINGS.md + Phase 5 deferred-items back-reference) | 2026-05-30 | TBD | [260530-hrc-open-phase-6-v1-0-image-publish-pipeline](./quick/260530-hrc-open-phase-6-v1-0-image-publish-pipeline/) |
  ```
  Leave commit SHA as `TBD` — Task 1's commit hasn't landed yet at write time. Better: stage with the placeholder, then before the actual commit, replace TBD with the post-commit SHA via `git rev-parse HEAD`. But because we want both edits in ONE commit per the planner's split, the order is: edit Quick Tasks row → commit Task 1 → don't go back. Acceptable to leave `TBD` and document in the SUMMARY as a known minor that orchestrator can flip in step 8 if it cares. (Plan body recommends: leave TBD; orchestrator can refresh in step 8.)

- **Session Continuity (lines 156-160):** Update `Last session:` to today's UTC timestamp; `Stopped at:` to `Phase 6 opened (260530-hrc); ready for /gsd-spec-phase 06 in next session`; `Resume file:` to `.planning/phases/06-v1-image-publish-and-ship-readiness-revalidation/06-FINDINGS.md`.

From `.planning/phases/05-distribution-self-hosting-acceptance/deferred-items.md` (current shape):

- Last entry ends at line 111 with the closing details of the cert-manager prereq fix entry. The new entry MUST be APPENDED at line 112 onward — no edits above. New entry shape:

  ```markdown

  ## Discovered 2026-05-30 (during BOOT-04 acceptance retry, second cascade) — **DEFERRED to Phase 6**

  **Image-publish pipeline gap + chart-tag drift.** BG task `bs3ntw3rt` (2026-05-30T16:25:00Z) made it past the cert-manager prereq fix from cascade-1 (quick task 260530-h2h, commits `adb1053` + `7d3af9d`) but then hit `kubectl wait --for=condition=Available deploy/tide-controller-manager` timeout at 5m. `kubectl describe pod` showed `tide-controller-manager` Pending with ImagePullBackOff on `ghcr.io/jsquirrelz/tide-controller:v0.1.0-dev` and `tide-dashboard` Pending with ImagePullBackOff on `ghcr.io/jsquirrelz/tide-dashboard:1.0.0`. Root cause: `.goreleaser.yaml` builds only the `tide` CLI binary (no `dockers:` section, no `docker_manifests:`); no `.github/workflows/*.yaml` publishes the 6 component Docker images that `charts/tide/values.yaml` references; additionally, chart values.yaml hardcodes 5 component tags at `v0.1.0-dev` while dashboard defaults to `.Chart.AppVersion` (`1.0.0`) — neither tag exists on ghcr.io.

  **DEFERRED to Phase 6** — `.planning/phases/06-v1-image-publish-and-ship-readiness-revalidation/06-FINDINGS.md` carries the scope-of-record. Phase 6 will be opened via `/gsd-spec-phase 06` + `/gsd-discuss-phase 06` + `/gsd-plan-phase 06` + `/gsd-execute-phase 06` in subsequent sessions; this quick task (`260530-hrc`) handled the planning bookkeeping only (ROADMAP row + STATE reframe + FINDINGS doc + this back-reference). Phase 5's claim of "v1.0 ship-ready" was premature — the deliverables that Phase 5 actually shipped (LICENSE, NOTICE, docs, samples, chart-additions, release.yaml chain, BOOT-02/BOOT-04 plumbing) all stand; the image-publish pipeline they assumed exists is what's missing, and Phase 6 is the catch-up.
  ```

  No modifications above line 111. APPEND-ONLY at the bottom.
</interfaces>

</context>

<tasks>

<task type="auto">
  <name>Task 1: Reframe ROADMAP + STATE for 8/9 milestone (commit: chore(06): open Phase 6)</name>
  <files>.planning/ROADMAP.md, .planning/STATE.md</files>
  <action>
Reframe the project's two top-level planning artifacts to reflect that Phase 5's closure was premature and Phase 6 is now in planning.

**`.planning/ROADMAP.md` edits (4 surgical changes; do NOT rewrite the file):**

1. Append a Phase 6 bullet to the overview list. Insert AFTER line 23 (the Phase 5 bullet):
   ```
   - [ ] **Phase 6: v1.0 Image-Publish Pipeline & Ship-Readiness Revalidation** — Multi-arch Docker image build + push for all 6 chart-referenced components, chart values.yaml tag-alignment SOT fix, dry-run-v1 cert-manager prereq fix, image-load fallback for local cluster acceptance, BOOT-04 end-to-end revalidation, README + INSTALL.md ship-state corrections
   ```
   Preserve the `[ ]` even though Phases 1-5 boxes are also `[ ]` (established Plan 05-17 deviation pattern — Progress table is the SOT, overview is decorative).

2. Append a Phase 6 STUB section AFTER Phase 5's section (Phase 5's section ends at line 290 with the closing Wave 6 entry `[x] 05-17-PLAN.md — Phase 5 closeout (ROADMAP + STATE update + 05-SUMMARY.md)`). Insert at line 291 (one blank line above the `## Progress` heading at line 293). The new section reads:
   ```markdown

   ### Phase 6: v1.0 Image-Publish Pipeline & Ship-Readiness Revalidation
   **Goal**: TBD — see `.planning/phases/06-v1-image-publish-and-ship-readiness-revalidation/06-FINDINGS.md` for scope. Phase opens with the bookkeeping landed by quick task `260530-hrc` (this commit). Final goal + requirements + plans are authored by `/gsd-spec-phase 06` + `/gsd-discuss-phase 06` + `/gsd-plan-phase 06` in subsequent sessions.
   **Depends on**: Phase 5
   **Requirements**: TBD
   **Plans:** 0/0 (planning)
   ```
   Do NOT enumerate any `Plans:` bullets (the plan-phase orchestrator writes those when plans actually exist). Do NOT author Success Criteria — that's discuss-phase output.

3. Append a Phase 6 row to the Progress table BEFORE the closing footer. The table is at lines 298-306; the new row goes immediately after the Phase 5 row (`| 5. Distribution & Self-Hosting Acceptance | 17/17 | Complete | 2026-05-23 |`) and BEFORE the `All 8 phases complete — TIDE v1.0 ship-ready.` line. The exact row:
   ```
   | 6. v1.0 Image-Publish Pipeline & Ship-Readiness Revalidation | 0/0 (planning) | Not started |     |
   ```
   The trailing `|     |` empty Completed cell uses 5 spaces (mimics the established `| - |` pattern visible on other "Not started" rows — but examined visually those rows use `- ` not 5 spaces; either form is acceptable; pick the form that matches the table's most-recent "Not started" row at line 300 if present, otherwise use `| - |`).

4. UPDATE the closing footer (currently line 308: `All 8 phases complete — TIDE v1.0 ship-ready.`) to:
   ```
   8 of 9 milestone phases complete — Phase 6 in planning (v1.0 image-publish pipeline + ship-readiness revalidation). See `.planning/phases/06-v1-image-publish-and-ship-readiness-revalidation/06-FINDINGS.md` for Phase 6 scope-of-record.
   ```

**`.planning/STATE.md` edits (5 surgical changes; do NOT rewrite the file):**

1. Frontmatter (lines 1-15) — apply these 6 field changes:
   - `status: completed` → `status: in_progress`
   - `stopped_at: Phase 5 closed — v1.0 ship-ready` → `stopped_at: Phase 6 opened — v1.0 image-publish pipeline + acceptance revalidation in planning`
   - `last_updated: "2026-05-30T16:25:00.000Z"` → `last_updated: "<current ISO-8601 UTC; use date -u +%Y-%m-%dT%H:%M:%S.000Z>"`
   - `last_activity: <existing 2026-05-30 cert-manager prereq prose>` → replace with a new 2026-05-30 entry. Suggested shape (you may tighten):
     ```
     last_activity: 2026-05-30 -- Quick task 260530-hrc (Open Phase 6 — v1.0 image-publish pipeline + ship-readiness revalidation): today's BOOT-04 retry second cascade (BG task bs3ntw3rt at 2026-05-30T16:25:00Z) made it past the morning cert-manager prereq fix (260530-h2h, commits adb1053 + 7d3af9d) but timed out at `kubectl wait deploy/tide-controller-manager`. `kubectl describe pod` showed tide-controller-manager Pending + tide-dashboard ImagePullBackOff. Root cause: `.goreleaser.yaml` builds only the tide CLI binary (no dockers: section); no workflow publishes the 6 component images charts/tide/values.yaml references. Plus chart values.yaml hardcodes 5 component tags at `v0.1.0-dev` while dashboard defaults to chart appVersion `1.0.0` — neither tag exists. Phase 5's claim of "v1.0 ship-ready" was premature. Phase 6 opened to plug the gap; this quick task landed the planning bookkeeping (ROADMAP row + STATE reframe + 06-FINDINGS.md + Phase 5 deferred-items back-reference). SPEC/DISCUSS/PLAN/EXECUTE cycles to follow in subsequent sessions. See `.planning/phases/06-v1-image-publish-and-ship-readiness-revalidation/06-FINDINGS.md` for scope-of-record.
     ```
   - `progress.total_phases: 8` → `total_phases: 9`
   - `progress.percent: 100` → `percent: 88`
   (Leave `completed_phases: 8`, `total_plans: 100`, `completed_plans: 100` unchanged — Phase 5's plan counts stay valid; Phase 6's plan inventory bumps later when plan-phase authors them.)

2. Body Current Position section (lines 26-31) — three line replacements:
   - `Phase: 5 (distribution-self-hosting-acceptance) — COMPLETE` → `Phase: 6 (v1-image-publish-and-ship-readiness-revalidation) — PLANNING`
   - `Plan: 17 of 17 — Phase 5 closeout commit landed; ROADMAP Progress row = Complete; all 8 milestone phases complete.` → `Plan: 0 of TBD — Phase 6 opened by quick task 260530-hrc; SPEC/DISCUSS/PLAN/EXECUTE cycles to follow.`
   - `Status: Phase 5 closed 2026-05-23. All 16 execution plans landed across 6 waves ...` (the existing long Phase 5 narrative) → replace the long Phase 5 narrative with a Phase 6 narrative of similar tightness. Suggested replacement (you may revise):
     ```
     Status: Phase 5 stays Complete (closed 2026-05-23 — LICENSE + NOTICE + 5 new docs + concepts.md + 3-sample cost spectrum + chart version 1.0.0 lockstep + per-namespace-rolebinding + resource-policy:keep + dry-run.yaml + release.yaml chain all shipped). Phase 6 opened 2026-05-30 to plug the image-publish-pipeline gap surfaced by today's BOOT-04 second cascade (BG task bs3ntw3rt): `.goreleaser.yaml` builds only the tide CLI, no workflow publishes the 6 component images charts/tide/values.yaml references, and chart values.yaml hardcodes 5 component tags at `v0.1.0-dev` while dashboard defaults to chart appVersion `1.0.0` — none exist on ghcr.io. Phase 6 is the catch-up phase, NOT a Phase 5 reopen. This quick task (260530-hrc) handled the planning bookkeeping (ROADMAP row + STATE reframe + 06-FINDINGS.md + Phase 5 deferred-items back-reference). Next steps: `/gsd-spec-phase 06` → `/gsd-discuss-phase 06` → `/gsd-plan-phase 06` → `/gsd-execute-phase 06` in subsequent sessions.
     ```
   - `Last activity: 2026-05-23 -- Plan 05-17 (Phase 5 closeout): ...` (the existing Plan 05-17 closeout prose) → `Last activity: 2026-05-30 -- Quick task 260530-hrc opened Phase 6 — v1.0 image-publish pipeline + ship-readiness revalidation. ROADMAP carries new Phase 6 row + STUB section (Goal: TBD pointing forward to 06-FINDINGS.md); STATE.md frontmatter reframed (8/9, percent 88, in_progress); 06-FINDINGS.md authored as scope-of-record for SPEC/DISCUSS/PLAN to consume next session; Phase 5 deferred-items.md appended with cascade-2 back-reference.`

3. Progress bar (line 33) — replace `Progress: [██████████] 100% (all 8 milestone phases complete — TIDE v1.0 ship-ready)` with `Progress: [████████░░] 88% (8 of 9 milestone phases complete — Phase 6 in planning)`.

4. Quick Tasks Completed table — APPEND a new row to the table at the bottom of the existing rows (the table is at lines 145-155 area; last row is `260530-h2h`). The new row reads:
   ```
   | 260530-hrc | Open Phase 6 — v1.0 image-publish pipeline + ship-readiness revalidation (ROADMAP row + STATE reframe + 06-FINDINGS.md + Phase 5 deferred-items back-reference) | 2026-05-30 | TBD | [260530-hrc-open-phase-6-v1-0-image-publish-pipeline](./quick/260530-hrc-open-phase-6-v1-0-image-publish-pipeline/) |
   ```
   Leave Commit column as `TBD` — orchestrator step 8 can refresh post-commit if desired; not blocking.

5. Session Continuity (lines 156-160) — three line replacements:
   - `Last session: 2026-05-22T11:34:35.384Z` → `Last session: <today's UTC timestamp>`
   - `Stopped at: Phase 5 context gathered` → `Stopped at: Phase 6 opened (quick task 260530-hrc); ready for /gsd-spec-phase 06 in next session`
   - `Resume file: .planning/phases/05-distribution-self-hosting-acceptance/05-CONTEXT.md` → `Resume file: .planning/phases/06-v1-image-publish-and-ship-readiness-revalidation/06-FINDINGS.md`

**Critical do-nots for this task:**
- Do NOT touch any of: `.goreleaser.yaml`, `charts/`, `hack/scripts/acceptance-v1.sh`, `hack/scripts/dry-run-v1.sh`, `README.md`, `docs/INSTALL.md`, `Makefile`, `.github/workflows/`. Those edits are Phase 6 execution scope.
- Do NOT modify the closing footer's intent — the project is still on track for v1.0; we just have a 9th phase to plug a gap.
- Do NOT touch `06-FINDINGS.md` in this task (Task 2 authors it).
- Do NOT touch `.planning/phases/05-distribution-self-hosting-acceptance/deferred-items.md` in this task (Task 2 appends to it).
- Do NOT alter any Phase 5 row / Phase 5 narrative beyond what's specified (Phase 5 stays Complete; its claims of v1.0 ship-ready are scoped by the new Status narrative, not retracted).
- Do NOT touch the .planning/quick/ PLAN.md / SUMMARY.md / CONTEXT.md you're reading or any other quick-task artifacts.

Stage ONLY `.planning/ROADMAP.md` and `.planning/STATE.md`. Then commit with shape:

```
chore(06): open Phase 6 — v1.0 image-publish pipeline + acceptance revalidation

Today's BOOT-04 acceptance ritual surfaced that Phase 5 closure was
premature. cascade-1 (BG `bess2gftr` at 16:13:45Z) failed on cert-manager
CRDs — fixed via quick task `260530-h2h` (commits adb1053, 7d3af9d).
cascade-2 (BG `bs3ntw3rt` at 16:25:00Z) progressed past cert-manager,
both `helm install` commands reported `STATUS: deployed`, but `kubectl
wait deploy/tide-controller-manager` timed out at 5m. `kubectl describe
pod` showed `tide-controller-manager` Pending with ImagePullBackOff on
`ghcr.io/jsquirrelz/tide-controller:v0.1.0-dev` and `tide-dashboard`
Pending with ImagePullBackOff on `ghcr.io/jsquirrelz/tide-dashboard:1.0.0`.

Root cause: `.goreleaser.yaml` builds only the `tide` CLI binary (no
`dockers:` section, no `docker_manifests:`). No workflow under
`.github/workflows/` publishes the 6 component Docker images that
`charts/tide/values.yaml` references. Plus chart values.yaml hardcodes
5 component tags at `v0.1.0-dev` while dashboard defaults to
`.Chart.AppVersion` (1.0.0). Neither tag exists on ghcr.io.

Phase 6 plugs the gap. This commit lands the planning bookkeeping
only — ROADMAP gets a new Phase 6 row + STUB section, STATE.md
frontmatter reframes from 8/8 / 100% to 8/9 / 88%, body Current
Position narrates the catch-up. Phase 5 stays Complete (its actual
deliverables — LICENSE, NOTICE, docs, samples, chart additions,
release.yaml chain, BOOT-02/BOOT-04 plumbing — all shipped). Phase 6
is the catch-up phase, NOT a Phase 5 reopen.

Scope-of-record + proposed Phase 6 requirements DRAFT lands in Task 2's
companion commit (`docs(06): author 06-FINDINGS.md + back-reference
from Phase 5 deferred-items`). Final Phase 6 requirements + plans are
authored by `/gsd-spec-phase 06` + `/gsd-discuss-phase 06` +
`/gsd-plan-phase 06` in subsequent sessions.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

Do NOT stage `06-FINDINGS.md` (Task 2), `deferred-items.md` (Task 2), or any planning artifacts under `.planning/quick/260530-hrc-*/`.
  </action>
  <verify>
    <automated>
      bash -c '
        set -e
        # Gate (a): ROADMAP Progress table carries the Phase 6 row exactly.
        if [ "$(grep -cE "^\| 6\. v1\.0 Image-Publish Pipeline & Ship-Readiness Revalidation \| 0/0 \(planning\) \| Not started" .planning/ROADMAP.md)" -ne 1 ]; then
          echo "FAIL: expected exactly 1 Phase 6 Progress-table row"; exit 1
        fi
        # Gate (b): ROADMAP Phase 6 section heading exists, with forward pointer to 06-FINDINGS.md.
        grep -qE "^### Phase 6: v1\.0 Image-Publish Pipeline & Ship-Readiness Revalidation$" .planning/ROADMAP.md
        grep -qE "06-FINDINGS\.md" .planning/ROADMAP.md
        # Gate (c): ROADMAP overview list carries the Phase 6 bullet.
        grep -qE "^- \[ \] \*\*Phase 6: v1\.0 Image-Publish Pipeline" .planning/ROADMAP.md
        # Gate (d): STATE frontmatter reframed.
        grep -qE "^  total_phases: 9$" .planning/STATE.md
        grep -qE "^  percent: 88$" .planning/STATE.md
        grep -qE "^status: in_progress$" .planning/STATE.md
        # Gate (e): STATE body Current Position references Phase 6.
        grep -qE "Phase: 6 \(v1-image-publish-and-ship-readiness-revalidation\)" .planning/STATE.md
        # Gate (f): STATE progress bar reflects 88%.
        grep -qE "Progress: \[████████░░\] 88%" .planning/STATE.md
        # Gate (g): STATE Quick Tasks Completed table has 260530-hrc row.
        grep -qE "\| 260530-hrc \| Open Phase 6" .planning/STATE.md
        # Gate (h): forbidden files untouched in staged tree.
        if git diff --cached --name-only | grep -qE "^(charts/|\.goreleaser\.yaml$|hack/scripts/(acceptance-v1|dry-run-v1)\.sh$|README\.md$|docs/INSTALL\.md$|Makefile$|\.github/workflows/)"; then
          echo "FAIL: forbidden file staged"; git diff --cached --name-only; exit 1
        fi
        # Gate (i): exactly two files staged for this task — ROADMAP + STATE.
        STAGED=$(git diff --cached --name-only)
        EXPECTED_COUNT=2
        ACTUAL_COUNT=$(echo "${STAGED}" | grep -cE "^\.planning/(ROADMAP|STATE)\.md$" || true)
        if [ "${ACTUAL_COUNT}" -ne "${EXPECTED_COUNT}" ]; then
          echo "FAIL: expected exactly 2 staged files (ROADMAP + STATE), got ${ACTUAL_COUNT}"; echo "${STAGED}"; exit 1
        fi
        # Gate (j): planning quick-task artifacts NOT staged.
        if git diff --cached --name-only | grep -qE "^\.planning/quick/260530-hrc-"; then
          echo "FAIL: quick-task planning artifacts must not be staged here (orchestrator step 8 handles those)"; exit 1
        fi
        echo "OK: ROADMAP + STATE reframed for 8/9 milestone; Phase 6 row/section/overview-bullet present; STATE frontmatter + body + progress bar + quick-task row updated; forbidden files untouched."
      '
    </automated>
  </verify>
  <done>
`.planning/ROADMAP.md` carries the new Phase 6 row in the Progress table, the Phase 6 STUB section (Goal/Depends-on/Requirements/Plans = TBD pointing forward to `06-FINDINGS.md`), the Phase 6 bullet in the overview list at the top, and the updated closing footer reading "8 of 9 milestone phases complete". `.planning/STATE.md` frontmatter shows `status: in_progress`, `total_phases: 9`, `percent: 88`, `completed_phases: 8`, with `last_updated`/`stopped_at`/`last_activity` reflecting today's Phase 6 opening; body Current Position narrates Phase 5 staying Complete + Phase 6 catch-up; progress bar reads `[████████░░] 88%`; Quick Tasks Completed table has a 260530-hrc row; Session Continuity points at `06-FINDINGS.md` as the resume file. Single atomic commit `chore(06): open Phase 6 — v1.0 image-publish pipeline + acceptance revalidation` lands on `main` with exactly two staged files. No forbidden files touched.
  </done>
</task>

<task type="auto">
  <name>Task 2: Author 06-FINDINGS.md + back-reference Phase 5 deferred-items (commit: docs(06): author 06-FINDINGS.md)</name>
  <files>.planning/phases/06-v1-image-publish-and-ship-readiness-revalidation/06-FINDINGS.md, .planning/phases/05-distribution-self-hosting-acceptance/deferred-items.md</files>
  <action>
Author `.planning/phases/06-v1-image-publish-and-ship-readiness-revalidation/06-FINDINGS.md` as the scope-of-record for Phase 6, and APPEND a forward-pointing back-reference entry to `.planning/phases/05-distribution-self-hosting-acceptance/deferred-items.md`.

**`06-FINDINGS.md` structure (required sections, in order):**

1. **Frontmatter** — Minimal YAML:
   ```yaml
   ---
   phase: 06-v1-image-publish-and-ship-readiness-revalidation
   type: findings
   status: draft
   opened: 2026-05-30
   opened_by: quick task 260530-hrc
   tags: [phase-open, image-publish, boot-04-revalidation, v1-ship-readiness, findings]
   supersedes_premature_closure: phase 5 (closed 2026-05-23 — deliverables shipped, gap not surfaced until 2026-05-30 BOOT-04 retry)
   ---
   ```

2. **`# Phase 6 — v1.0 Image-Publish Pipeline & Ship-Readiness Revalidation`** — H1 title.

3. **`## Scope of record (DRAFT)`** — Opening paragraph (2-3 sentences) that:
   - States this doc is the scope-of-record for SPEC/DISCUSS/PLAN sessions to follow.
   - Notes that all proposed requirements below are labeled DRAFT — final requirement IDs and final scope come from `/gsd-discuss-phase 06`.
   - Phase 6 exists because Phase 5's "v1.0 ship-ready" claim was premature; the deliverables Phase 5 shipped are intact, but BOOT-04 (the gate that should have caught the image-publish gap) is operator-only by D-A4 design and didn't run end-to-end until 2026-05-30.

4. **`## What happened today (2026-05-30)`** — Narrative of the day's BOOT-04 cascade:
   - Two BG attempts: cascade-1 (BG `bess2gftr` at 2026-05-30T16:09:57Z / failure logged at T16:13:45Z) failed on cert-manager CRDs; fixed via quick task `260530-h2h` (commits `adb1053` + `7d3af9d`).
   - cascade-2 (BG `bs3ntw3rt` at 2026-05-30T16:25:00Z onward) progressed past cert-manager. Both helm installs reported `STATUS: deployed`.
   - `kubectl wait --for=condition=Available deploy/tide-controller-manager -n tide-system --timeout=5m` timed out.
   - `kubectl describe pod` showed:
     - `tide-controller-manager-*`: Pending, 0/1 ready, ImagePullBackOff on `ghcr.io/jsquirrelz/tide-controller:v0.1.0-dev`
     - `tide-dashboard-d675d58d4-s2cfc`: Pending, ImagePullBackOff on `ghcr.io/jsquirrelz/tide-dashboard:1.0.0`
   - Use direct quotes from the failure where they're informative.

5. **`## Root cause`** — Three concrete findings, each with file/line references:
   - **Finding 1: No image-publish workflow exists.**
     - `.goreleaser.yaml` lines 30-51 build ONLY the `tide` CLI binary; no `dockers:` section, no `docker_manifests:`. Confirmable via `grep -cE '^dockers:|^docker_manifests:' .goreleaser.yaml` → 0.
     - `grep -lrnE 'docker push|docker buildx|ko build|kaniko' .github/workflows/` returns zero hits.
     - After tagging `v1.0.0`, the published charts will reference 6 images that don't exist on ghcr.io.
   - **Finding 2: Chart values.yaml component tags drifted relative to dashboard.**
     - Plan 05-05 bumped chart version `0.1.0-dev` → `1.0.0` lockstep, but `charts/tide/values.yaml` (a hand-maintained surface per the file's own header) still hardcodes 5 component tags at `v0.1.0-dev`:
       - `controllerManager.manager.image.tag: v0.1.0-dev` (line 39)
       - `images.stubSubagent.tag: v0.1.0-dev` (line 140)
       - `images.credProxy.tag: v0.1.0-dev` (line 144)
       - `images.tidePush.tag: v0.1.0-dev` (line 155)
       - `images.claudeSubagent.tag: v0.1.0-dev` (line 165)
     - Only `dashboard.image.tag: ""` (line 244) defaults to `.Chart.AppVersion` and resolves to `1.0.0`.
     - This explains why `kubectl describe pod` showed controller pulling `v0.1.0-dev` and dashboard pulling `1.0.0` — both nonexistent.
     - The SOT lives at `hack/helm/tide-values.yaml`; per Phase 02.2's chart-vs-binary anti-pattern, the fix lands in SOT then propagates via `make helm` (binary catches up to chart contract, never reverse).
   - **Finding 3: `dry-run-v1.sh` also lacks cert-manager — Door 3 doesn't actually work either.**
     - `hack/scripts/dry-run-v1.sh` lines 80-82 do `helm install tide ./charts/tide -n tide-system` with no cert-manager bring-up before it. Same `ImagePullBackOff` deadlock will hit the rc-tag dry-run path (Door 3 from the v1.0 ship decision tree) once an rc tag is cut.
     - Quick task `260530-h2h` only fixed `acceptance-v1.sh` because that was the script the operator was invoking; the dry-run-v1.sh gap is structurally identical and was not in scope for `260530-h2h`. Phase 6 picks it up.

6. **`## Deeper lesson — Phase 5 closure was premature`** — 1-2 paragraphs:
   - Phase 5 closed 2026-05-23 claiming v1.0 ship-ready. The plumbing for BOOT-02 / BOOT-04 shipped (Plan 05-15: `make dry-run-v1` + `make acceptance-v1` + 4 hack/scripts). What Phase 5 did NOT ship: a workflow that actually publishes the 6 component Docker images those scripts assume exist. Plan 05-16 wired chart-publish + helmify-verify + release pipeline extensions, but the assumption that the controller / dashboard / 4 sidecar images "will be there" was never validated.
   - The structural failure is D-A4 by design — BOOT-04 is operator-only, no CI integration. The gate that should have caught both gaps (cert-manager + image-publish) was in the operator's hands; the operator (user) is exercising it for the first time on 2026-05-30. This is not an indictment of D-A4 — `acceptance-v1` is a real-money $25/run gate; CI-ifying it has its own tradeoffs. It IS an indictment of not having run the operator ritual once internally before declaring ship-readiness.
   - Phase 6 is the catch-up. It does NOT reopen Phase 5 — Phase 5's actually-shipped deliverables (LICENSE, NOTICE, docs, samples, chart additions, release.yaml chain, BOOT-02/BOOT-04 plumbing) all stand. Phase 6 just plugs the missing image-publish piece + the dry-run-v1.sh cert-manager mirror.

7. **`## What's already in main`** — Commit table covering today's landed work:
   | Commit | Subject | Scope |
   |--------|---------|-------|
   | `489dd71` | `style(quick-260526-w11): gofmt cmd/dashboard/api/{plans,tasks}.go struct alignment` | Phase 5 closeout polish (Plan 05-17 deferred-item resolution) |
   | `1769a60` | `docs(quick-260526-w11): reconcile ROADMAP Phase 5 Progress row to 17/17` | Phase 5 closeout polish |
   | `3f48032` | `docs(quick-260526-w11): Phase 5 closeout polish — SUMMARY + STATE + deferred-items RESOLVED` | Phase 5 closeout polish |
   | `adb1053` | `fix(quick-260530-h2h): install cert-manager v1.20.2 before helm install tide in acceptance-v1.sh` | Cascade-1 fix (cert-manager prereq in acceptance-v1.sh only) |
   | `7d3af9d` | `docs(quick-260530-h2h): document cert-manager v1.20.2 prerequisite in INSTALL.md` | Cascade-1 fix (INSTALL.md prereq subsection) |
   | `a2193cf` | `docs(quick-260530-h2h): cert-manager prereq fix — SUMMARY + STATE + deferred-items RESOLVED` | Cascade-1 closeout |

   Add a closing sentence: SPEC/DISCUSS/PLAN should NOT re-author any of these — they're already done. Phase 6 plans build on this baseline.

8. **`## Proposed scope (DRAFT — final scope set by /gsd-discuss-phase)`** — Numbered list of plausible requirement seeds, each a paragraph. ALL labeled DRAFT. Cover at minimum these items (the planner reserves the right to add/remove during discuss):
   - **DRAFT IMG-01..05 — Docker image build + push pipeline for all 6 components** (`tide-controller`, `tide-dashboard`, `tide-stub-subagent`, `tide-credproxy`, `tide-push`, `tide-claude-subagent`). Multi-arch (linux/amd64 + linux/arm64). Plausible shape: extend `.goreleaser.yaml` `dockers:` + `docker_manifests:`, OR new `docker-build-push` job in `release.yaml` gated on `v*` tag push. Tag derived from `.Version`. The choice between goreleaser-native vs a separate workflow is a discuss-phase decision.
   - **DRAFT CHART-01 — Chart-side image-tag alignment SOT fix.** Update `hack/helm/tide-values.yaml`: 5 hardcoded `v0.1.0-dev` tags → `""` so they default to `.Chart.AppVersion` (matching the dashboard pattern). Augment-script re-run; `helm template` verify the rendered image tags match the chart's appVersion. Per Phase 02.2 chart-vs-binary anti-pattern: SOT edit first, then `make helm` propagates — never the reverse.
   - **DRAFT DRY-01 — `dry-run-v1.sh` cert-manager install + rollout-wait.** Mirror the fix already landed in `acceptance-v1.sh` (commit `adb1053`). Without this, the rc-tag dry-run path also deadlocks on `ImagePullBackOff` + (separately) on cert-manager CRDs.
   - **DRAFT DRY-02 — Image-load fallback for local clusters.** Both `acceptance-v1.sh` and `dry-run-v1.sh` need a code path that builds the 6 component images locally + `kind load docker-image`s them, for the case where the operator is testing pre-tag (no published images yet) or testing a tag that doesn't match HEAD. Plausibly a new Makefile target `acceptance-images-load`. Whether to make this default-on for `make acceptance-v1` vs gated by env var is a discuss-phase decision.
   - **DRAFT ACC-01 — BOOT-04 end-to-end revalidation** with published images (or local image-load fallback). The closeout gate of Phase 6. Demonstrates TIDE-on-TIDE actually works.
   - **DRAFT ACC-02 — README + INSTALL.md ship-state corrections.** Remove any premature "v1.0 ship-ready" claims; document `make container-images` if a local-build path is added; document the image-publish workflow in maintainer docs.
   - **DRAFT plausible add-ons** (discuss-phase prioritization): `test-int-kind-prep` cluster-name parameterization (currently hardcoded to `tide-test` at the Makefile target); `.acceptance-runs/` gitignore (today's failed runs littered the worktree); operator-facing Troubleshooting entry for `ImagePullBackOff` mid-install; potential `make doctor` preflight that checks for cert-manager + image existence before running `helm install tide`.

9. **`## Out-of-scope (explicit non-goals for Phase 6)`** — Short list:
   - Phase 5 reopen — Phase 5's actually-shipped deliverables all stand.
   - New chart features beyond image-tag alignment.
   - Multi-version chart distribution (carry-forward from Phase 5 deferred-items).
   - Cosign / SLSA / supply-chain signing (carry-forward from Phase 5 deferred-items; v1.x scope per `.goreleaser.yaml` footer).
   - OperatorHub / OLM bundle submission (v1.x).
   - Anything CI-only that doesn't unblock BOOT-04 revalidation.

10. **`## Next-session playbook`** — Numbered steps the user runs next:
    1. `/gsd-spec-phase 06` — author `06-REQUIREMENTS.md` (formal REQ-IDs from the DRAFT seeds above).
    2. `/gsd-discuss-phase 06` — lock decisions (likely D-X scope: goreleaser-vs-separate-workflow choice, image-load-fallback default, dry-run-v1 cert-manager pinned version).
    3. `/gsd-plan-phase 06` — decompose into wave-structured PLAN.md files. Likely 4-6 plans depending on parallelization (image-publish pipeline + chart-tag SOT + dry-run-v1 cert-manager + image-load fallback are all parallel-eligible after wave 1 sets up shared structure).
    4. `/gsd-execute-phase 06` — run the plans.
    5. After Phase 6 closeout: re-run `make acceptance-v1` end-to-end as the v1.0 ship gate. On green, tag `v1.0.0`.

11. **`## Cross-references`** — Bullet list of relevant artifacts the next session should read:
    - `.planning/ROADMAP.md` Phase 6 row + section (added by this quick task, Task 1)
    - `.planning/STATE.md` Current Position (reframed by this quick task, Task 1)
    - `.planning/phases/05-distribution-self-hosting-acceptance/05-SUMMARY.md` (Phase 5 closeout — context for what's intact)
    - `.planning/phases/05-distribution-self-hosting-acceptance/deferred-items.md` (the new 2026-05-30 entry at the bottom — back-reference to this doc)
    - `.planning/quick/260530-h2h-boot-04-acceptance-v1-cert-manager-prere/260530-h2h-SUMMARY.md` (cascade-1 closure context)
    - `.goreleaser.yaml` (the file whose missing `dockers:` section is the root of Finding 1)
    - `charts/tide/values.yaml` (the file whose hardcoded `v0.1.0-dev` tags are the root of Finding 2; SOT is `hack/helm/tide-values.yaml`)
    - `hack/scripts/dry-run-v1.sh` (the file with the cert-manager gap — Finding 3)
    - `test/integration/kind/suite_test.go:329-369` (the proven Layer B cert-manager install pattern)
    - `./CLAUDE.md` (chart-vs-binary anti-pattern from Phase 02.2; Observe First / Execute Don't Ask / Verify Before Claiming)

**Format / voice requirements:**
- Tight, declarative, em-dash-heavy per the project doc-voice (CLAUDE.md "Structural conventions in the spec document").
- File:line references inline (e.g. `charts/tide/values.yaml:39`).
- Code blocks fenced as ```bash or ```yaml as appropriate; do NOT use unfenced indented blocks.
- Minimum length: 80 lines (the must_haves carries this floor). Quality matters more than length — write tight, but the substance above naturally exceeds 80 lines.

**`deferred-items.md` append (one new entry, APPEND-ONLY):**

Append to `.planning/phases/05-distribution-self-hosting-acceptance/deferred-items.md` AFTER the last line of the existing file (line 111 area; do not modify any line above). Exact entry shape:

```markdown

## Discovered 2026-05-30 (during BOOT-04 acceptance retry, second cascade) — **DEFERRED to Phase 6**

**Image-publish pipeline gap + chart-tag drift.** BG task `bs3ntw3rt` (2026-05-30T16:25:00Z) made it past the cert-manager prereq fix from cascade-1 (quick task `260530-h2h`, commits `adb1053` + `7d3af9d`) but then hit `kubectl wait --for=condition=Available deploy/tide-controller-manager` timeout at 5m. `kubectl describe pod` showed `tide-controller-manager` Pending with ImagePullBackOff on `ghcr.io/jsquirrelz/tide-controller:v0.1.0-dev` and `tide-dashboard` Pending with ImagePullBackOff on `ghcr.io/jsquirrelz/tide-dashboard:1.0.0`.

**Root cause:** `.goreleaser.yaml` builds only the `tide` CLI binary (no `dockers:` section, no `docker_manifests:`); no `.github/workflows/*.yaml` publishes the 6 component Docker images that `charts/tide/values.yaml` references; additionally, chart values.yaml hardcodes 5 component tags at `v0.1.0-dev` while dashboard defaults to `.Chart.AppVersion` (`1.0.0`) — neither tag exists on ghcr.io. `hack/scripts/dry-run-v1.sh` also lacks the cert-manager prereq (cascade-1's fix only landed in `acceptance-v1.sh`), so Door 3 of the v1.0 ship decision tree is also deadlocked.

**DEFERRED to Phase 6** — `.planning/phases/06-v1-image-publish-and-ship-readiness-revalidation/06-FINDINGS.md` carries the scope-of-record (root cause + commits-already-in-main + DRAFT proposed requirements + out-of-scope + next-session playbook). Phase 6 will be opened via `/gsd-spec-phase 06` + `/gsd-discuss-phase 06` + `/gsd-plan-phase 06` + `/gsd-execute-phase 06` in subsequent sessions; this quick task (`260530-hrc`) handled the planning bookkeeping only (ROADMAP row + STATE reframe + `06-FINDINGS.md` + this back-reference). Phase 5's claim of "v1.0 ship-ready" was premature — the deliverables that Phase 5 actually shipped (LICENSE, NOTICE, docs, samples, chart-additions, release.yaml chain, BOOT-02/BOOT-04 plumbing) all stand; the image-publish pipeline they assumed exists is what's missing, and Phase 6 is the catch-up.
```

**Critical do-nots for this task:**
- Do NOT touch any of: `.goreleaser.yaml`, `charts/`, `hack/scripts/acceptance-v1.sh`, `hack/scripts/dry-run-v1.sh`, `README.md`, `docs/INSTALL.md`, `Makefile`, `.github/workflows/`. Those edits are Phase 6 execution scope.
- Do NOT modify any line of `deferred-items.md` above line 111 — APPEND-ONLY.
- Do NOT touch `.planning/ROADMAP.md` or `.planning/STATE.md` (Task 1 handled those; this task's commit must NOT contain those files).
- Do NOT enumerate plan files in `06-FINDINGS.md` (no `06-01-PLAN.md` references — plans don't exist yet; plan-phase writes them).
- Do NOT formalize REQ-IDs in `06-FINDINGS.md` — every requirement seed is labeled DRAFT. The `IMG-01..05` / `CHART-01` / `DRY-01..02` / `ACC-01..02` IDs are proposed seeds, NOT final REQ-IDs.
- Do NOT touch any planning artifacts under `.planning/quick/260530-hrc-*/` (orchestrator step 8 handles those).
- Do NOT speculate on goreleaser vs separate-workflow shape — surface BOTH as options in the DRAFT scope, let discuss-phase decide.

Stage ONLY:
- `.planning/phases/06-v1-image-publish-and-ship-readiness-revalidation/06-FINDINGS.md` (new file)
- `.planning/phases/05-distribution-self-hosting-acceptance/deferred-items.md` (append-only modification)

Commit with shape:

```
docs(06): author 06-FINDINGS.md + back-reference from Phase 5 deferred-items

`06-FINDINGS.md` captures today's BOOT-04 second-cascade discovery as
Phase 6 scope-of-record: what happened (cascade-1 cert-manager fix
landed via 260530-h2h; cascade-2 surfaced ImagePullBackOff on
tide-controller + tide-dashboard), root cause (`.goreleaser.yaml` builds
only the tide CLI binary, no workflow publishes the 6 component images
charts/tide/values.yaml references, plus chart values.yaml component
tags drifted), deeper lesson (Phase 5's "v1.0 ship-ready" claim was
premature; D-A4 operator-only BOOT-04 didn't run end-to-end until
today), what's-already-in-main (6 commits from quick tasks
260526-w11 + 260530-h2h, NOT to be re-authored), DRAFT proposed scope
seeds (IMG-01..05 image build/push + CHART-01 tag-alignment SOT fix +
DRY-01 dry-run-v1 cert-manager mirror + DRY-02 image-load fallback +
ACC-01 BOOT-04 revalidation + ACC-02 README/INSTALL ship-state
corrections + plausible add-ons), explicit out-of-scope (no Phase 5
reopen; no new chart features; cosign/SLSA/OLM stay v1.x), and next-
session playbook (/gsd-spec-phase 06 → discuss → plan → execute).

All proposed requirements labeled DRAFT — final scope is the
`/gsd-discuss-phase 06` output, NOT this doc.

`deferred-items.md` (Phase 5) appended with a 2026-05-30 entry under
heading "## Discovered 2026-05-30 (during BOOT-04 acceptance retry,
second cascade) — **DEFERRED to Phase 6**" pointing forward to
06-FINDINGS.md.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

Do NOT stage `.planning/ROADMAP.md`, `.planning/STATE.md`, or any quick-task planning artifacts.
  </action>
  <verify>
    <automated>
      bash -c '
        set -e
        # Gate (a): 06-FINDINGS.md exists with the required frontmatter + sections.
        test -f .planning/phases/06-v1-image-publish-and-ship-readiness-revalidation/06-FINDINGS.md
        FINDINGS=.planning/phases/06-v1-image-publish-and-ship-readiness-revalidation/06-FINDINGS.md
        # Frontmatter sanity.
        grep -qE "^phase: 06-v1-image-publish-and-ship-readiness-revalidation$" "${FINDINGS}"
        grep -qE "^type: findings$" "${FINDINGS}"
        grep -qE "^status: draft$" "${FINDINGS}"
        # Required section headings.
        grep -qE "^## Scope of record \(DRAFT\)$" "${FINDINGS}"
        grep -qE "^## What happened today \(2026-05-30\)$" "${FINDINGS}"
        grep -qE "^## Root cause$" "${FINDINGS}"
        grep -qE "^## Deeper lesson — Phase 5 closure was premature$" "${FINDINGS}"
        grep -qE "^## What.s already in main$" "${FINDINGS}"
        grep -qE "^## Proposed scope \(DRAFT" "${FINDINGS}"
        grep -qE "^## Out-of-scope \(explicit non-goals for Phase 6\)$" "${FINDINGS}"
        grep -qE "^## Next-session playbook$" "${FINDINGS}"
        grep -qE "^## Cross-references$" "${FINDINGS}"
        # Key substantive content tokens.
        grep -qE "ImagePullBackOff" "${FINDINGS}"
        grep -qE "\.goreleaser\.yaml" "${FINDINGS}"
        grep -qE "DRAFT" "${FINDINGS}"
        grep -qE "bs3ntw3rt" "${FINDINGS}"
        grep -qE "bess2gftr" "${FINDINGS}"
        grep -qE "adb1053" "${FINDINGS}"
        grep -qE "v0\.1\.0-dev" "${FINDINGS}"
        grep -qE "tide-controller" "${FINDINGS}"
        grep -qE "tide-dashboard" "${FINDINGS}"
        # Minimum length floor (must_haves declares min_lines: 80).
        LINES=$(wc -l < "${FINDINGS}" | tr -d " ")
        if [ "${LINES}" -lt 80 ]; then
          echo "FAIL: 06-FINDINGS.md must be >=80 lines, got ${LINES}"; exit 1
        fi
        # Gate (b): Phase 5 deferred-items.md APPENDED with the new entry.
        DEFER=.planning/phases/05-distribution-self-hosting-acceptance/deferred-items.md
        grep -qE "^## Discovered 2026-05-30 \(during BOOT-04 acceptance retry, second cascade\) — \*\*DEFERRED to Phase 6\*\*$" "${DEFER}"
        grep -qE "06-FINDINGS\.md" "${DEFER}"
        grep -qE "bs3ntw3rt" "${DEFER}"
        # Gate (c): the new heading is the LAST `^## Discovered ` heading in the file (proves APPEND-ONLY).
        LAST_DISCOVERED_HEADING=$(grep -nE "^## Discovered 2026" "${DEFER}" | tail -1 | cut -d: -f2-)
        if ! echo "${LAST_DISCOVERED_HEADING}" | grep -qE "BOOT-04 acceptance retry, second cascade"; then
          echo "FAIL: the new entry is not the LAST `## Discovered` heading — was something modified above it?"
          echo "Got: ${LAST_DISCOVERED_HEADING}"
          exit 1
        fi
        # Gate (d): forbidden files untouched.
        if git diff --cached --name-only | grep -qE "^(charts/|\.goreleaser\.yaml$|hack/scripts/(acceptance-v1|dry-run-v1)\.sh$|README\.md$|docs/INSTALL\.md$|Makefile$|\.github/workflows/)"; then
          echo "FAIL: forbidden file staged"; git diff --cached --name-only; exit 1
        fi
        # Gate (e): exactly two files staged.
        STAGED=$(git diff --cached --name-only)
        EXPECTED=$(echo -e ".planning/phases/05-distribution-self-hosting-acceptance/deferred-items.md\n.planning/phases/06-v1-image-publish-and-ship-readiness-revalidation/06-FINDINGS.md" | sort)
        ACTUAL=$(echo "${STAGED}" | sort)
        if [ "${ACTUAL}" != "${EXPECTED}" ]; then
          echo "FAIL: expected staged files to be exactly:"
          echo "${EXPECTED}"
          echo "got:"
          echo "${ACTUAL}"
          exit 1
        fi
        # Gate (f): NO ROADMAP / STATE / quick-task planning files staged.
        if git diff --cached --name-only | grep -qE "^\.planning/(ROADMAP|STATE)\.md$"; then
          echo "FAIL: ROADMAP/STATE staged in Task 2 — those belong to Task 1"; exit 1
        fi
        if git diff --cached --name-only | grep -qE "^\.planning/quick/260530-hrc-"; then
          echo "FAIL: quick-task planning artifacts must not be staged here"; exit 1
        fi
        echo "OK: 06-FINDINGS.md authored (>=80 lines, all required sections + tokens); deferred-items.md APPENDED with new 2026-05-30 entry as the last heading; forbidden files untouched; exactly 2 files staged."
      '
    </automated>
  </verify>
  <done>
`.planning/phases/06-v1-image-publish-and-ship-readiness-revalidation/06-FINDINGS.md` exists with all 11 required sections (Scope of record, What happened today, Root cause, Deeper lesson, What's already in main, Proposed scope DRAFT, Out-of-scope, Next-session playbook, Cross-references, plus H1 title and YAML frontmatter), at least 80 lines, all proposed requirements labeled DRAFT, BG task IDs (`bs3ntw3rt`, `bess2gftr`) + key commit SHAs (`adb1053`, `7d3af9d`) referenced verbatim. `.planning/phases/05-distribution-self-hosting-acceptance/deferred-items.md` carries an APPENDED 2026-05-30 entry with the `**DEFERRED to Phase 6**` heading and forward-pointer to `06-FINDINGS.md`; no line above the new entry was modified. Single atomic commit `docs(06): author 06-FINDINGS.md + back-reference from Phase 5 deferred-items` lands on `main` with exactly the two staged files. No forbidden files touched.
  </done>
</task>

</tasks>

<verification>
After both tasks land on `main`, the orchestrator (NOT the executor) will run:

1. `grep -E '^\| 6\. v1\.0 Image-Publish' .planning/ROADMAP.md | wc -l` → expect `1` (one Progress-table row).
2. `test -f .planning/phases/06-v1-image-publish-and-ship-readiness-revalidation/06-FINDINGS.md` → exit 0.
3. `grep -E '^  total_phases: 9$' .planning/STATE.md | wc -l` → expect `1`.
4. `grep -E '^  percent: 88$' .planning/STATE.md | wc -l` → expect `1`.
5. `grep -E '^status: in_progress$' .planning/STATE.md | wc -l` → expect `1`.
6. `git diff --name-only HEAD~2..HEAD -- charts/` → expect empty (chart contract preserved).
7. `git diff --name-only HEAD~2..HEAD -- .goreleaser.yaml hack/scripts/acceptance-v1.sh hack/scripts/dry-run-v1.sh README.md docs/INSTALL.md Makefile` → expect empty (none of the forbidden files touched).
8. `git log --oneline -2` → expect exactly two new commits on `main`:
   - `docs(06): author 06-FINDINGS.md + back-reference from Phase 5 deferred-items` (Task 2, HEAD)
   - `chore(06): open Phase 6 — v1.0 image-publish pipeline + acceptance revalidation` (Task 1, HEAD~1)
9. `wc -l < .planning/phases/06-v1-image-publish-and-ship-readiness-revalidation/06-FINDINGS.md` → expect ≥ 80.
10. `grep -cE '^## Discovered 2026' .planning/phases/05-distribution-self-hosting-acceptance/deferred-items.md` → expect ≥ 8 (the original 7 + today's new one — APPEND-ONLY contract).

The orchestrator does NOT need to re-run BOOT-04 or any tide functional gate from this quick task — the actual fixes land in Phase 6 execution. This quick task is bookkeeping-only.
</verification>

<success_criteria>
- Two atomic commits land on `main`, in order:
  1. `chore(06): open Phase 6 — v1.0 image-publish pipeline + acceptance revalidation` (Task 1: ROADMAP + STATE)
  2. `docs(06): author 06-FINDINGS.md + back-reference from Phase 5 deferred-items` (Task 2: FINDINGS + deferred-items)
- `.planning/ROADMAP.md` carries the new Phase 6 Progress-table row, Phase 6 STUB section (Goal/Depends-on/Requirements/Plans = TBD pointing forward to `06-FINDINGS.md`), Phase 6 overview-list bullet, updated closing footer reading "8 of 9 milestone phases complete".
- `.planning/STATE.md` frontmatter shows `status: in_progress`, `total_phases: 9`, `percent: 88`, `completed_phases: 8`, `last_updated` bumped to today, `last_activity` + `stopped_at` reframed; body Current Position narrates Phase 5 staying Complete + Phase 6 catch-up; progress bar reads `[████████░░] 88%`; Quick Tasks Completed table appended with 260530-hrc row; Session Continuity points at `06-FINDINGS.md` as the resume file.
- `.planning/phases/06-v1-image-publish-and-ship-readiness-revalidation/06-FINDINGS.md` exists, at least 80 lines, carries all 11 required sections (H1 + YAML frontmatter + 9 H2 sections + content), all proposed requirements labeled DRAFT, BG task IDs + commit SHAs referenced verbatim, scope-discipline note explicitly stating final REQ-IDs come from `/gsd-discuss-phase 06`.
- `.planning/phases/05-distribution-self-hosting-acceptance/deferred-items.md` APPENDED with the new 2026-05-30 entry as the LAST `## Discovered` heading in the file (proves APPEND-ONLY contract); no line above was modified.
- `charts/`, `.goreleaser.yaml`, `hack/scripts/acceptance-v1.sh`, `hack/scripts/dry-run-v1.sh`, `README.md`, `docs/INSTALL.md`, `Makefile`, `.github/workflows/` are ALL untouched.
- No planning artifacts under `.planning/quick/260530-hrc-*/` are committed by the executor (orchestrator step 8 handles `260530-hrc-SUMMARY.md` and the PLAN.md/CONTEXT.md that already exist).
- Executor did NOT run `make acceptance-v1`, `make dry-run-v1`, `helm install`, `kind create cluster`, `goreleaser`, or any cluster-mutating command. Verification was syntactic / file-system only.
</success_criteria>

<output>
After both tasks complete, write a one-page summary to `.planning/quick/260530-hrc-open-phase-6-v1-0-image-publish-pipeline/260530-hrc-SUMMARY.md` covering:

- What changed in `.planning/ROADMAP.md` (cite: new Progress row, new STUB section, new overview bullet, updated closing footer — name them by their line-shape, not line numbers since those shifted).
- What changed in `.planning/STATE.md` (cite: frontmatter field flips — `status`, `stopped_at`, `last_updated`, `last_activity`, `progress.total_phases`, `progress.percent`; body Current Position narrative reframe; progress bar; Quick Tasks Completed table append; Session Continuity reframe).
- What was authored in `06-FINDINGS.md` (cite: 11 sections covered, line count, DRAFT-labeling discipline preserved).
- What was appended to Phase 5's `deferred-items.md` (cite: new heading, forward-pointer to `06-FINDINGS.md`, append-only contract honored).
- Both commit SHAs (`chore(06): ...` from Task 1 + `docs(06): ...` from Task 2).
- Confirmation that `charts/`, `.goreleaser.yaml`, both acceptance scripts, `README.md`, `docs/INSTALL.md`, `Makefile`, and `.github/workflows/` were untouched.
- Confirmation that the executor did NOT run `make acceptance-v1` / `make dry-run-v1` / `helm install` / `kind create cluster` / `goreleaser` / any cluster-mutating command.
- Explicit pointer to the next-session playbook in `06-FINDINGS.md` § "Next-session playbook" — `/gsd-spec-phase 06` is the next step.

Do NOT commit the SUMMARY.md — the orchestrator handles planning-artifact commits in step 8.
</output>
</task>

</tasks>
</content>
</invoke>