---
phase: 47
slug: self-hosted-phoenix-install-end-to-end-proof
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-17
---

# Phase 47 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` (plain `Test*` funcs) for chart-render assertions; Ginkgo v2.28 + Gomega for the Layer B `test/integration/kind` suite (live cluster) |
| **Config file** | `test/integration/kind/suite_test.go` (Layer B cluster bootstrap) |
| **Quick run command** | `go test ./test/integration/kind/... -run TestHelm -v` (offline-safe render subset) |
| **Full suite command** | `make test-int` (Layer A envtest + Layer B kind; requires Docker + kind) |
| **Estimated runtime** | quick ~30s · full suite ~15-30 min (constrained-VM discipline applies) |

---

## Sampling Rate

- **After every task commit:** Run `go test ./test/integration/kind/... -run TestHelm -v`
- **After every plan wave:** Run `make test-int`
- **Before `/gsd:verify-work`:** Full suite must be green PLUS the live proof's manual evidence checklist (D-13) — this phase's acceptance bar is not test-green alone
- **Max feedback latency:** 60 seconds (quick command)

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| (filled by planner) | — | — | PHX-02 nudge-present-when-endpoint-empty | — | N/A | integration render-gate | `go test ./test/integration/kind/... -run TestHelmNotesTxt -v` (name TBD; strategy per Research Pitfall A — `helm template` does NOT render NOTES.txt) | ❌ W0 | ⬜ pending |
| (filled by planner) | — | — | PHX-02 nudge-absent-when-endpoint-set | — | N/A | integration render-gate | same test, second case | ❌ W0 | ⬜ pending |
| (filled by planner) | — | — | PHX-02 headers wiring (Pitfall B option 1) | V6 | `valueFrom: secretKeyRef` only — never a literal header value in chart or docs | unit + render | `go test ./internal/controller/... -run TestReporterOptions` + helm render check for the Secret-sourced env | ❌ W0 | ⬜ pending |
| (filled by planner) | — | — | PHX-01 chart pin, no `latest` | — | N/A | doc-review (manual grep) | grep doc examples for the pinned `--version` | n/a | ⬜ pending |
| (filled by planner) | — | — | PROOF-01 five-level tree + redacted messages, queryable | V2 (Phoenix auth ON, Secret-sourced) | auth credentials from K8s Secret, never literal | live/manual evidence capture | browser-driven screenshots + trace IDs (D-13) | n/a | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] NOTES.txt render-gate test (both cases) — location and strategy (raw-byte assertion vs live-cluster `helm install --dry-run`) is an explicit planner decision per Research Pitfall A (`helm template` never renders NOTES.txt; `--dry-run=client` still needs a reachable cluster on Helm 3.16.3)
- [ ] OTLP-headers wiring tests (Pitfall B option 1 chosen): new `ReporterOptions` field test + chart-render test confirming the Secret-sourced env entry appears only when the new values key is set
- [ ] No framework gaps otherwise — Layer A envtest and `test/integration/kind` are fully wired and green

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Five-level trace tree renders in Phoenix with redacted Task-level message arrays | PROOF-01 | Requires a live run with real API spend + browser inspection | Follow the documented recipe on a fresh kind cluster; drive `examples/projects/medium`-shaped run; capture trace-tree + LLM-span-detail screenshots, record trace IDs, run an OBS-03 tag-filter DSL query (D-13) |
| Phoenix DSL queryability | PROOF-01 | Phoenix UI interaction | Filter by `tag.tags`/metadata enrichment (e.g. "every span from Phase N"); screenshot the filtered result |
| Doc-recipe accuracy | PHX-01/PHX-02 | The recipe is proven by following it (D-11) | Fresh cluster stood up exclusively from the doc's own steps; any divergence is a doc bug to fix in-phase |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
