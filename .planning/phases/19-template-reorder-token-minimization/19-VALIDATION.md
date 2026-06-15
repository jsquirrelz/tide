---
phase: 19
slug: template-reorder-token-minimization
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-15
---

# Phase 19 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go testing + goldie v2.8.0 (test-only) |
| **Config file** | None — uses `go test` directly |
| **Quick run command** | `go test ./internal/eval/` |
| **Full suite command** | `make test` (unit tier — includes fmt/vet/envtest prep; per Phase 18 D-02a the roadmap's `make test-unit` wording reads as `make test`) |
| **Estimated runtime** | ~1s quick (`internal/eval/`, zero-network); minutes for full `make test` |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/eval/` (fast, zero-network)
- **After every plan wave:** Run `make test`
- **Before `/gsd:verify-work`:** Full `make test` must be green
- **Max feedback latency:** ~1s (quick) — the deterministic eval gate fires per commit

---

## Per-Task Verification Map

> Populated during planning; task IDs assigned by gsd-planner. Requirement→test mapping below is locked from RESEARCH.md.

| Req ID | Behavior | Test Type | Automated Command |
|--------|----------|-----------|-------------------|
| PROMPT-01 | Five templates in canonical D-03 order (role → instructions → shared-context slot → volatile suffix → prompt) | golden render diff | `go test ./internal/eval/ -run TestGoldenRender_` |
| PROMPT-02 | `{{.TaskUID}}`/`{{.Provider.*}}` in volatile suffix only (exactly 1 TaskUID per template) | golden render + grep | `go test ./internal/eval/ -run TestGoldenRender_` + `grep -c TaskUID internal/subagent/common/templates/*.tmpl` |
| PROMPT-03 | "Why-this-line" annotation comments present before any trim | human review of annotated diff | Code review (no automated check — annotations are zero-token `{{/* */}}` comments) |
| PROMPT-04 | Each section trim preserves protocol compliance | protocol-compliance gate | `go test ./internal/eval/` (child-CRD parse / declared-output-path / DAG acyclicity) |
| PROMPT-05 | No map-typed data interpolated into stable prefix (confirmed no-op) | grep guard | `grep -rn "Params" internal/subagent/common/templates/*.tmpl` returns zero hits |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

*Existing infrastructure covers all phase requirements.* The Phase 18 eval harness (goldie golden renders + per-template byte ratchets + protocol-compliance tests in `internal/eval/`) already exists and is the gate. No new test files needed; `make eval` (online, `//go:build eval`) provides the live `count_tokens` confirmation per D-05.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Trim did not degrade prompt quality | PROMPT-04 | Gate is structural-only (no LLM-judge this milestone per Phase 18 D-05); semantic quality is not machine-checkable here | Review the annotated per-section diff; run `make eval` and confirm token count dropped and the ratchet did not grow (D-05) |
| Annotation rationale captures load-bearing proof | PROMPT-03 | "Why-this-line" justification is a judgment about prior production cascades | Reviewer confirms each surviving load-bearing line has an inline `{{/* WHY */ -}}` annotation; removal rationale present in the per-section commit message |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify (eval gate) or are manual-only with documented reason
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references (none — existing harness covers all)
- [ ] No watch-mode flags
- [ ] Feedback latency < ~1s (quick eval run)
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
