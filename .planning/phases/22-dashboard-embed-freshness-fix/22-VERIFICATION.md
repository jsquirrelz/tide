---
phase: 22-dashboard-embed-freshness-fix
verified: 2026-06-16T11:10:00Z
status: passed
score: 6/6 must-have truths verified; criterion #3 runtime render confirmed via live deploy + screenshot (22-HUMAN-UAT.md, 22-telemetry-render-proof.png)
overrides_applied: 0
human_verification:
  - test: "Build the dashboard image from a clean checkout (docker build -f Dockerfile.dashboard -t tide-dashboard:verify .), run it against a live cluster, open the dashboard, and click the Telemetry tab."
    expected: "The Telemetry tab renders (cache-efficiency panel visible), proving the embedded bundle is the current post-telemetry SPA — not the frozen pre-telemetry bundle from commit 6d7a28f."
    result: "PASSED (2026-06-16). Built tide-dashboard:verify from a clean checkout, kind-loaded into tide-dogfood, rolled out deployment/tide-dashboard in tide-system, port-forwarded, drove the browser: served bundle index-BEfeN1Kf.js carries the panel-cache-efficiency marker, the header shows a Telemetry tab (absent from the 6d7a28f bundle), and clicking it renders all panels (BUDGET, COST OVER TIME, DISPATCH COUNTS, FAILURE RATE, TOKEN BREAKDOWN, CACHE EFFICIENCY). Proof: 22-telemetry-render-proof.png. See 22-HUMAN-UAT.md."
follow_up_hardening:
  - "22-REVIEW.md WR-01 (dirty-tree-on-failure) and WR-02 (untracked-asset false-pass) closed by gap plan 22-03: verify-dashboard-freshness now uses diff -rq against a fresh build with no tracked-tree mutation; verified clean-tree pass + probe-file detection on merged main."
---

# Phase 22: Dashboard Embed Freshness Fix Verification Report

**Phase Goal:** Every published TIDE image embeds the current dashboard SPA, so a release can never ship a bundle older than its source — closing the dogfood run #2 finding that v1.0.0/v1.0.1 images froze the embedded bundle at pre-telemetry commit 6d7a28f.
**Verified:** 2026-06-16T07:10:00Z
**Status:** human_needed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | Building Dockerfile.dashboard from a clean checkout regenerates `cmd/dashboard/embed/dist` from current `dashboard/web` source — the image never trusts the committed dist/. | ✓ VERIFIED | `Dockerfile.dashboard:18-29` spa-builder stage (digest-pinned `node:22-alpine`, `npm ci` + `npm run build`); `:44` `RUN rm -rf cmd/dashboard/embed/dist`; `:45` `COPY --from=spa-builder /spa/dist/ cmd/dashboard/embed/dist/`; `:48` `go build` — ordering confirmed (44 < 45 < 48), so the committed dist is wiped and overwritten with the freshly built SPA before compile. SUMMARY records `--target spa-builder` smoke build produced non-empty `/spa/dist/assets/*.js`. |
| 2 | `make verify-dashboard-freshness` exits 0 when committed dist/ matches a fresh `make dashboard-frontend`, and exits non-zero when `dashboard/web/src` changed without regenerating dist/. | ✓ VERIFIED | Ran the gate live: clean tree → EXIT=0, both PASS lines printed (npm ci + vite build + 204 vitest tests all green). Reproduced the true staleness case: added a *rendered* `data-probe` attribute to `TelemetryView.tsx:764` without regenerating dist → EXIT=2 with `FAIL: ... is stale` and diff `index-BEfeN1Kf.js | 294 ----` + `index.html | 2 +-`. Tree restored clean. `Makefile:283-298`. |
| 3 | `cmd/dashboard/embed/dist/` stays tracked in git (Option A) so `go vet ./...` and `make test` compile from a clean clone. | ✓ VERIFIED | `git ls-files cmd/dashboard/embed/dist/` returns 3 tracked files (index-BEfeN1Kf.js, index-BJNoTuKK.css, index.html). Gate is NOT a prerequisite of `vet`/`test`/`test-only`/`lint` (`Makefile:81,85,89,240` — none reference verify-dashboard-freshness). `//go:embed all:dist` at `cmd/dashboard/embed/embed.go:39` is satisfied by the tracked files. |
| 4 | The freshness gate asserts the built bundle contains the telemetry marker `panel-cache-efficiency` — a pre-telemetry bundle fails the gate. | ✓ VERIFIED | `Makefile:292-298` greps `cmd/dashboard/embed/dist/assets/*.js` for `panel-cache-efficiency`, `exit 1` if absent. Live run printed `PASS: embedded bundle contains telemetry marker (panel-cache-efficiency)`. Marker present in committed `index-BEfeN1Kf.js`. Source: `dashboard/web/src/components/TelemetryView.tsx:764` (`data-testid="panel-cache-efficiency"`). |
| 5 | A PR that changes dashboard/web/src without regenerating dist fails ci.yaml (pre-merge gate). | ✓ VERIFIED | `.github/workflows/ci.yaml:92-99` adds `actions/setup-node@v4` (node 22, npm cache on `dashboard/web/package-lock.json`) then `run: make verify-dashboard-freshness` inside the existing `test` job (job keys unchanged: `test`, `helm-lint`). Step precedes the timed `make test-only:137`. YAML parses clean. |
| 6 | A release tag with a stale embedded dist fails release.yaml at helmify-verify before any image publishes. | ✓ VERIFIED | `.github/workflows/release.yaml:100-107` appends setup-node@v4 + `make verify-dashboard-freshness` inside `helmify-verify` (after the chart-reproducibility gate `:85`, before next job `pre-flight:125`); `timeout-minutes` bumped 5→10 (`:52`). `release` `needs: [helmify-verify, pre-flight]` (`:198`) and `build-images` `needs: [helmify-verify]` (`:277`) wiring intact — a failing gate blocks publish. YAML parses clean. |

**Score:** 6/6 supporting truths verified (4 PLAN must-have truths for Wave 1 + 2 for Wave 2; all VERIFIED). Roadmap success criterion #3 runtime render is routed to human verification per the phase instruction.

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `Dockerfile.dashboard` | spa-builder node stage feeding fresh dist into go builder via COPY --from | ✓ VERIFIED | Digest-pinned `node:22-alpine` stage `:18`; `COPY --from=spa-builder` `:45` after `rm -rf` `:44`, before `go build` `:48`. `npm ci` only (no bare install), no `npm run test` in stage. |
| `.dockerignore` | re-includes dashboard/web source (minus node_modules) | ✓ VERIFIED | `:37-45` re-includes 9 SPA source entries (`!dashboard/web/src/**`, index.html, package(-lock).json, 3 tsconfigs, vite.config.ts, .nvmrc); node_modules intentionally NOT re-included. |
| `Makefile` | verify-dashboard-freshness target (rebuild + git-diff gate + telemetry marker) | ✓ VERIFIED | `:283-298`. Calls `$(MAKE) dashboard-frontend`, `git diff --quiet cmd/dashboard/embed/dist/`, asserts `panel-cache-efficiency`. Not in fast critical path. |
| `.github/workflows/ci.yaml` | PR-time freshness gate in test job | ✓ VERIFIED | `:92-99` setup-node + gate step inside `test` job. |
| `.github/workflows/release.yaml` | release-time freshness gate in helmify-verify | ✓ VERIFIED | `:100-107` setup-node + gate step inside `helmify-verify`; gates release/build-images. |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|----|--------|---------|
| Dockerfile go builder stage | cmd/dashboard/embed/dist | `COPY --from=spa-builder /spa/dist/ ...` after `rm -rf` | ✓ WIRED | Lines 44-45 precede go build at 48. |
| ci.yaml test job | make verify-dashboard-freshness | run step after actions/setup-node@v4 | ✓ WIRED | ci.yaml:92-99. |
| release.yaml helmify-verify | make verify-dashboard-freshness | run step after setup-node, after chart-reproducibility | ✓ WIRED | release.yaml:100-107; release/build-images needs helmify-verify intact. |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Gate passes on clean fresh tree | `make verify-dashboard-freshness` | EXIT=0; "PASS: ... matches a fresh make dashboard-frontend" + "PASS: ... contains telemetry marker" | ✓ PASS |
| Gate catches real staleness (source changed, dist not regenerated) | rendered attr added to TelemetryView.tsx:764, then `make verify-dashboard-freshness` | EXIT=2; "FAIL: ... is stale" + diff `index-BEfeN1Kf.js \| 294 ----` | ✓ PASS |
| Telemetry marker present in fresh build | grep `panel-cache-efficiency` in rebuilt dist/assets/*.js | found in index-BEfeN1Kf.js | ✓ PASS |
| ci.yaml valid YAML | `yaml.safe_load(ci.yaml)` | VALID | ✓ PASS |
| release.yaml valid YAML | `yaml.safe_load(release.yaml)` | VALID | ✓ PASS |
| dist tracked in git | `git ls-files cmd/dashboard/embed/dist/` | 3 files | ✓ PASS |
| Tree restored clean after staleness test | `git status --porcelain cmd/dashboard/embed/dist/` | empty | ✓ PASS |

Note: tree-shaking caveat observed — an *unused* `export const` source change is tree-shaken out of the production bundle, so it does not change the shipped output and the gate correctly passes (no real staleness). Only changes that alter the *rendered/emitted* bundle trip the gate. This is correct behavior, not a defect.

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|-------------|-------------|--------|----------|
| FIX-01 | 22-01, 22-02 | Dashboard image build embeds the current SPA; staleness gated in CI; verified against Telemetry tab. | ✓ SATISFIED (static + behavioral); runtime Telemetry render → human | REQUIREMENTS.md:61 maps FIX-01 → Phase 22 (line 80). Mechanism (Dockerfile regen), CI gate (ci.yaml + release.yaml), and telemetry-marker proxy all verified. Live Telemetry-tab render is the human-verification item. |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| (none) | — | No TBD/FIXME/XXX/TODO/HACK/PLACEHOLDER in any of the 5 changed files | — | Clean. No debt markers. |

### Code Review Warnings (from 22-REVIEW.md — assessed against goal)

| ID | Issue | Goal Impact | Disposition |
|----|-------|-------------|-------------|
| WR-01 | Freshness gate leaves working tree dirty/untracked on failure (local DX footgun; harmless in ephemeral CI). | Does NOT block any success criterion — gate still correctly catches staleness (reproduced). Local ergonomics only. | ⚠️ WARNING — non-blocking. Recommend the trap/clean fix in a follow-up; not required to close FIX-01. |
| WR-02 | `git diff --quiet` blind to a net-new untracked asset (narrow false-pass; Vite single-entry flow always touches a tracked file today). | Does NOT block the goal at current SPA shape — normal content change rewrites the content-hashed filename (tracked deletion) + index.html. Latent risk as SPA grows. | ⚠️ WARNING — non-blocking. Recommend adding `git ls-files --others` check; not required to close FIX-01. |

### Human Verification Required

#### 1. Telemetry tab renders from a freshly built image (ROADMAP success criterion #3)

**Test:** Build the dashboard image from a clean checkout (`docker build -f Dockerfile.dashboard -t tide-dashboard:verify .`), deploy/run it against a live cluster, open the dashboard, and click the Telemetry tab.
**Expected:** The Telemetry tab renders (cache-efficiency panel visible), proving the embedded bundle is the current post-telemetry SPA — not the frozen pre-telemetry bundle from commit 6d7a28f.
**Why human:** Requires a freshly built image run against a cluster with visual confirmation of the rendered tab — a runtime/visual outcome no grep can prove. The code path that GUARANTEES it (rm -rf committed dist + COPY --from=spa-builder before go build; verified `panel-cache-efficiency` marker in the freshly built bundle) is verified statically and behaviorally; only the live render remains.

### Gaps Summary

No gaps. All six supporting truths, all five artifacts, and all three key links are VERIFIED. The freshness mechanism (Dockerfile regenerates dist from source before compile), the staleness gate (Makefile target, empirically fails on a real source-without-regen change), and the CI/release wiring (both workflows, publish-gating preserved) are all in place and behaviorally confirmed. FIX-01 is accounted for in REQUIREMENTS.md → Phase 22.

The only item not provable statically is ROADMAP success criterion #3's live Telemetry-tab render against a running cluster — routed to human verification exactly as the phase instruction directs (the guaranteeing code path is verified; the visual runtime confirmation is not). The two REVIEW Warnings (WR-01 tree-dirtiness on failure, WR-02 untracked-asset false-pass window) are non-blocking local-DX/latent-robustness notes that do not affect goal achievement; recommended as follow-up hardening.

---

_Verified: 2026-06-16T07:10:00Z_
_Verifier: Claude (gsd-verifier)_
