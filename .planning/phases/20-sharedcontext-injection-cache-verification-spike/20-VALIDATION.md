---
phase: 20
slug: sharedcontext-injection-cache-verification-spike
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-15
---

# Phase 20 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (stdlib) + goldie/v2 golden renders + Ginkgo (integration) |
| **Config file** | none — repo `Makefile` targets |
| **Quick run command** | `go test ./pkg/dispatch/... ./internal/subagent/... ./internal/controller/... ./internal/eval/...` |
| **Full suite command** | `make test` (deterministic eval gate + goldens + unit) |
| **Spike (live, env-gated)** | `make eval` (`//go:build eval`, credproxy `count_tokens` + real-API dispatch on `kind-tide-dogfood`) |
| **Estimated runtime** | ~30–90s unit; spike is manual/live |

---

## Sampling Rate

- **After every task commit:** Run the quick run command for the touched package(s)
- **After every plan wave:** Run `make test` (keeps goldens + token ratchet green)
- **Before `/gsd:verify-work`:** `make test` must be green; spike evidence recorded
- **Max feedback latency:** ~90 seconds (unit); spike is out-of-band live evidence

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 20-01-01 | 01 | 1 | CACHE-02 | — | EnvelopeIn/Out round-trips `sharedContext`; omitempty keeps old fixtures valid | unit | `go test ./pkg/dispatch/...` | ❌ W0 | ⬜ pending |
| 20-02-01 | 02 | 2 | CACHE-03 | — | `{{.SharedContext}}` renders into reserved slot; executor template never renders it | golden | `go test ./internal/subagent/... ./internal/eval/...` | ❌ W0 | ⬜ pending |
| 20-03-01 | 03 | 2 | CACHE-02/04 | — | Controller stamps one curated blob byte-identically onto every child CRD; planner EnvelopeIn populated uniformly, executor path empty | unit | `go test ./internal/controller/...` | ❌ W0 | ⬜ pending |
| 20-04-01 | 04 | 1 | CACHE-01 | — | Spike: sibling #2 `cache_read_input_tokens > 0` (PASS) OR request-body diff names the per-pod divergence (FAIL) | live | `make eval` (spike variant) | ❌ W0 | ⬜ pending |
| 20-05-01 | 05 | 3 | CACHE-01/05 | — | Spike decision recorded in PROJECT.md; design verified provider-neutral (no markers/branches) | manual | grep PROJECT.md for the decision record | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky. Task IDs are indicative — the planner sets the authoritative map.*

---

## Wave 0 Requirements

- [ ] Envelope round-trip test stubs for `sharedContext` (EnvelopeIn populated, EnvelopeOut carry, executor empty) — `pkg/dispatch`
- [ ] Golden-render fixtures regenerated for the four planner templates + token ratchet re-baselined — `internal/eval` / `testdata/`
- [ ] Spike harness scaffold (throwaway ≥1,024-token identical-prefix probe + 2-dispatch driver + credproxy body-tee for the FAIL path) under the `//go:build eval` tag
- [ ] Confirm `{{- if .SharedContext}}...{{end -}}` renders zero extra bytes against the empty-SharedContext eval fixture BEFORE committing (ratchet integrity)

*Existing infrastructure (Phase 18 eval harness, goldie goldens, `make eval` credproxy plumbing) covers most requirements; the spike harness is the main new scaffold.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Cross-pod cache hit / miss verdict | CACHE-01 | Requires live Anthropic API on the dogfood cluster; non-deterministic, network-bound | Run the spike: dispatch the ≥1,024-token identical-prefix probe twice within 5-min TTL on `kind-tide-dogfood`; read `cache_read_input_tokens` on sibling #2; on miss, diff both pods' outbound request bodies at the credproxy |
| Spike decision recorded | CACHE-01 | Decision-record prose in PROJECT.md | Confirm PROJECT.md contains either "cross-pod prefix-cache hits confirmed" or "does not fire; reframed to token-minimization-only" + root-cause divergence |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies (spike is live/manual by nature)
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 90s (unit layer)
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
