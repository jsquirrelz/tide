---
phase: 22
slug: dashboard-embed-freshness-fix
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-16
---

# Phase 22 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | vitest 1.6.1 (frontend unit) · go test / Ginkgo v2.28 (Go units) · shell assertions (CI gates) |
| **Config file** | `dashboard/web/vitest.config.ts`; Makefile targets for CI gates |
| **Quick run command** | `make verify-dashboard-freshness` |
| **Full suite command** | `make test` + `make verify-dashboard-freshness` |
| **Estimated runtime** | ~120 seconds (frontend rebuild + git diff) |

---

## Sampling Rate

- **After every task commit:** Run `make verify-dashboard-freshness` (runs `make dashboard-frontend` + `git diff --quiet cmd/dashboard/embed/dist/`)
- **After every plan wave:** Run `make test` (Go units) + `make verify-dashboard-freshness`
- **Before `/gsd:verify-work`:** Full suite + freshness gate must be green; a fresh `Dockerfile.dashboard` build must succeed
- **Max feedback latency:** ~120 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 22-01-xx | 01 | 1 | FIX-01a | — | `npm ci` deterministic lockfile install (no `npm install`) | integration (Docker smoke) | `docker build -f Dockerfile.dashboard --target spa-builder .` | ❌ W0 (new Dockerfile stage) | ⬜ pending |
| 22-01-xx | 01 | 1 | FIX-01b | — | N/A | shell gate | `make verify-dashboard-freshness` | ❌ W0 (new Makefile target) | ⬜ pending |
| 22-01-xx | 01 | 1 | FIX-01c | — | N/A | shell assertion | `make verify-dashboard-freshness` (telemetry marker `panel-cache-efficiency`) | ❌ W0 | ⬜ pending |
| 22-01-xx | 01 | 1 | FIX-01d | — | N/A | unit (vitest) | `cd dashboard/web && npm run test` | ✅ `src/__tests__/bundle-size.test.ts` | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

*Task IDs are placeholders — the planner assigns final `{phase}-{plan}-{task}` IDs.*

---

## Wave 0 Requirements

- [ ] `Makefile` — add `verify-dashboard-freshness` target (`make dashboard-frontend` + `git diff --quiet cmd/dashboard/embed/dist/` + telemetry marker grep)
- [ ] `Dockerfile.dashboard` — add `--platform=$BUILDPLATFORM node:22-alpine` spa-builder stage that runs `npm ci && npm run build` and emits `cmd/dashboard/embed/dist`
- [ ] `.dockerignore` — re-include `dashboard/web/src`, `package*.json`, vite/ts configs
- [ ] `.github/workflows/ci.yaml` — add `setup-node` + `make verify-dashboard-freshness` step
- [ ] `.github/workflows/release.yaml` — add `verify-dashboard-freshness` to the `helmify-verify` (reproducibility-gates) job

*Bundle-size gate (FIX-01d) already covered by existing `src/__tests__/bundle-size.test.ts`.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Freshly built dashboard image renders the Telemetry tab in a live cluster | FIX-01 (success criterion #3) | Requires a running cluster + browser; not deterministically automatable in CI | Build `Dockerfile.dashboard`, deploy to `kind-tide-dogfood`, open dashboard, confirm Telemetry tab renders the post-telemetry SPA (Cache Efficiency panel present) |

*The automated `panel-cache-efficiency`-marker grep against the built bundle (FIX-01c) is the deterministic CI proxy for this manual check.*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 120s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
