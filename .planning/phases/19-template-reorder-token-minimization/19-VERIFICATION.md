---
phase: 19-template-reorder-token-minimization
verified: 2026-06-15T00:00:00Z
status: passed
gate_decision: APPROVED
score: 6/6 must-haves verified
overrides_applied: 0
re_verification:
  previous_status: none
  previous_score: n/a
---

# Phase 19: Template Reorder + Token Minimization — Verification Report

**Phase Goal:** All five prompt templates restructured stable-prefix-first (role preamble → fixed instructions → shared-context slot → volatile metadata → per-task prompt) so wave-sibling dispatches share an identical cache-eligible prefix; non-essential boilerplate trimmed; each change gated green by the Phase 18 eval harness.
**Verified:** 2026-06-15
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths (ROADMAP Success Criteria + PROMPT-01..05)

| # | Truth | Status | Evidence |
| --- | ----- | ------ | -------- |
| 1 | PROMPT-01/02 — all 5 templates in canonical D-03 order; `{{.TaskUID}}`/`{{.Provider.*}}` only in volatile suffix | ✓ VERIFIED | `awk '/SharedContext slot/{f=1} !f && /\{\{\.TaskUID\}\}/{c++} END{exit c}'` exits 0 for all 5 templates (zero UID in stable prefix). Suffix UID counts: planners 2 (label + path-mapping per D-01), task_executor 5 (label + 4 filesystem-layout paths — legitimately volatile per D-01). `grep -rnE '\{\{\.?Provider'` / `Level` / `Role` → NONE. No printed `Level:`/`Role:` lines. Rendered `milestone_planner.golden` confirms order: role(L1)→fixed-instructions(L3-36)→[slot stripped]→`TaskUID:`(L38)→`Original prompt:`(L41). |
| 2 | PROMPT-03 — each template carries `{{/* WHY */ -}}` annotations on load-bearing lines | ✓ VERIFIED | WHY-comment counts: milestone 5, project 5, plan 5, phase 3, task_executor 2. Visual inspection confirms every load-bearing block (spec-read, HOW-TO-EMIT, JSON-shape, write-only, FILE-TOUCH RULE, 4-required-fields, JSON-escape, executor contract, EnvelopeOut) carries an inline `{{/* WHY ... */ -}}` annotation. Annotations committed before trims (git log: annotate→reorder→trim sequence per template). |
| 3 | PROMPT-05 — `TestNoMapInterpolation` guard exists; no map/range interpolation | ✓ VERIFIED | `render_test.go:168` `TestNoMapInterpolation` runs over all 5 templates, asserting absence of `.Params` and `{{range}}`. Test PASSES (`-count=1` fresh run). `grep -rnE '\{\{[-[:space:]]*range\|index'` → NONE. Only scalar fields interpolated. |
| 4 | PROMPT-04 / criterion 4 — eval gate green; protocol-compliance preserved | ✓ VERIFIED | Fresh `go test -count=1 ./internal/eval/` → `ok ... 0.421s`, all 15 tests PASS including `TestDAGAcyclicity_AcyclicFixture`, `TestDAGAcyclicity_CyclicFixture`, `TestDeclaredOutputPaths_Presence` (child-CRD/output-path/DAG-acyclicity protocol gate). Per-section commit discipline confirmed in git log (commit B reorder, C/D/E trims, each gated). `make test` = unit tier (Makefile:85); eval package is the relevant gate and is green. |
| 5 | Criterion 5 — goldens regenerated; ratchets LOWERED below v1.0.1 baselines | ✓ VERIFIED | Ratchet files: milestone 1862, project 2193, phase 1974, plan 3985, task 1566 — match the lowered targets exactly (down from 2214/2474/2271/4281/1961). Golden byte sizes match ratchets exactly (`wc -c`: 1862/1974/3985/2193/1566). `TestByteRatchet_*` (strict equality) all PASS. Goldens reflect reordered structure (SharedContext/WHY comments stripped at render, zero-token as designed). |
| 6 | Load-bearing directives preserved (conservative-trim contract) | ✓ VERIFIED | In rendered goldens: spec-read `README.md` directive in all 5. `plan_planner.golden`: FILE-TOUCH RULE (1), declaredOutputPaths (2), "REQUIRED and MUST be" non-empty 4-field block (1), JSON-escape "escape any newline" (1), "ONLY the JSON object" (1). `task_executor.golden`: DeclaredOutputPaths (2), "do not push" (1), "no git credentials" (1). All survive to rendered output. |

**Score:** 6/6 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | -------- | ------ | ------- |
| `internal/subagent/common/templates/milestone_planner.tmpl` | D-03 reorder + WHY + trim | ✓ VERIFIED | UID-free prefix, 5 WHY annotations, SharedContext marker present |
| `internal/subagent/common/templates/project_planner.tmpl` | D-03 reorder + WHY + trim | ✓ VERIFIED | UID-free prefix, 5 WHY, both-must-be-produced contract retained |
| `internal/subagent/common/templates/phase_planner.tmpl` | D-03 reorder + WHY + trim | ✓ VERIFIED | UID-free prefix, 3 WHY, filesTouched directive retained |
| `internal/subagent/common/templates/plan_planner.tmpl` | D-03 + FILE-TOUCH + 4-fields + JSON-escape | ✓ VERIFIED | All load-bearing blocks present and annotated |
| `internal/subagent/common/templates/task_executor.tmpl` | D-03 + DeclaredOutputPaths + no-push; 6 UID→suffix | ✓ VERIFIED | UID-free prefix; 5 UID in volatile filesystem-layout suffix |
| `internal/eval/render_test.go` | TestNoMapInterpolation + goldens + strict ratchet | ✓ VERIFIED | Guard + golden + ratchet tests all present and green |
| `internal/eval/testdata/ratchets/*.txt` | Lowered byte counts | ✓ VERIFIED | 1862/2193/1974/3985/1566 — match lowered targets |
| `internal/eval/testdata/goldie/*.golden` | Regenerated reordered renders | ✓ VERIFIED | Byte sizes match ratchets; structure confirms D-03 order |

### Key Link Verification

| From | To | Via | Status | Details |
| ---- | -- | --- | ------ | ------- |
| templates | LoadPromptTemplate | embed + `tmpl.Execute` | WIRED | `ratchetAssert`/golden tests load via `common.LoadPromptTemplate(role, level)` and render — proves templates are the live compiled-in artifacts |
| render_test ratchet | ratchets/*.txt | `os.ReadFile` strict equality | WIRED | Byte-count compared to committed integer; growth OR shrink fails |
| goldens | templates | goldie render compare | WIRED | Goldens are the rendered output of the actual templates |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| Eval gate green | `go test -count=1 ./internal/eval/` | ok 0.421s, 15 tests PASS | ✓ PASS |
| Zero UID in stable prefix (all 5) | `awk '/SharedContext slot/{f=1} !f && /TaskUID/{c++} END{exit c}'` | exit 0 ×5 | ✓ PASS |
| No Provider/Level/Role interpolation | `grep -rnE '\{\{\.?(Provider\|Level\|Role)'` | NONE | ✓ PASS |
| Goldens match ratchets | `wc -c *.golden` vs ratchet files | exact match ×5 | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| ----------- | ----------- | ----------- | ------ | -------- |
| PROMPT-01 | 19-01/02/03 | Reorder stable-prefix-first | ✓ SATISFIED | Truth 1 — D-03 order in all 5 |
| PROMPT-02 | 19-01/02/03 | Volatile metadata in suffix | ✓ SATISFIED | Truth 1 — UID/Provider out of prefix |
| PROMPT-03 | 19-01/02/03 | Why-this-line annotations | ✓ SATISFIED | Truth 2 — WHY comments, committed before trim |
| PROMPT-04 | 19-01..04 | Per-section trim, gated green | ✓ SATISFIED | Truth 4 — per-section commits + green eval gate |
| PROMPT-05 | 19-04 | Deterministic serialization guard | ✓ SATISFIED | Truth 3 — TestNoMapInterpolation |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |
| (none) | — | No TBD/FIXME/XXX/PLACEHOLDER in templates or eval tests | — | Clean |

### Human Verification Required

None outstanding. The D-05 human-verify checkpoint (annotated-diff review + live `make eval` token confirmation) was approved by the maintainer (commit `cf84a33`, "D-05 human-verify approved"). Live `make eval` against the real Anthropic API confirmed per-template token drops (milestone 612→509, project 660→583, phase 648→558, plan 1168→1078, task 559→455) — informational, not re-run here.

### Gaps Summary

No gaps. All five templates are reordered to the canonical D-03 stable-prefix-first structure with zero volatile (`{{.TaskUID}}`/`{{.Provider.*}}`) tokens in the stable prefix, verified by the spec's own `awk` check exiting 0 across all five. All load-bearing directives (spec-read in all 5, plan_planner's FILE-TOUCH RULE + 4 required spec fields + JSON-escape/JSON-purity blocks, task_executor's DeclaredOutputPaths + no-push) survive to the rendered goldens. PROMPT-05's `TestNoMapInterpolation` regression guard exists and passes. The strict byte ratchets are lowered to exactly the trimmed targets (1862/2193/1974/3985/1566) and the goldens match them byte-for-byte. The full eval gate (`go test ./internal/eval/`) is green on a fresh `-count=1` run, with DAG-acyclicity, declared-output-paths, and child-CRD JSON-shape protocol compliance preserved.

---

_Verified: 2026-06-15_
_Verifier: Claude (gsd-verifier)_
