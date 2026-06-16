---
phase: 22-dashboard-embed-freshness-fix
plan: "03"
subsystem: build-tooling
tags: [makefile, freshness-gate, fix, wr-01, wr-02]
dependency_graph:
  requires: ["22-01"]
  provides: ["verify-dashboard-freshness (hardened)"]
  affects: ["CI freshness gate", "release pipeline"]
tech_stack:
  added: []
  patterns: ["diff -rq for full recursive tree comparison (catches added/removed/changed files)"]
key_files:
  created: []
  modified:
    - Makefile
key_decisions:
  - "Use diff -rq (full recursive tree compare) instead of git diff --quiet — closes WR-01 (no mutation of tracked tree) and WR-02 (catches net-new untracked assets) in one move"
  - "Build SPA directly in gate recipe (cd dashboard/web && npm ci && npm run build && npm run test) without calling $(MAKE) dashboard-frontend — keeps dashboard-frontend as the developer regenerate-and-commit path, gate as pure audit"
metrics:
  duration: "~35 minutes (dominated by two full npm ci + vitest runs for behavioral verification)"
  completed: "2026-06-16"
  tasks_completed: 1
  files_modified: 1
---

# Phase 22 Plan 03: Dashboard Freshness Gate Hardening Summary

Rewrote `verify-dashboard-freshness` Makefile target to use `diff -rq` against a fresh SPA build instead of `git diff --quiet` after calling `dashboard-frontend`, closing WR-01 (gate no longer mutates the tracked embed tree on failure) and WR-02 (full recursive diff catches added/removed files that `git diff --quiet` was blind to).

## Tasks Completed

| # | Task | Commit | Files |
|---|------|--------|-------|
| 1 | Rewrite verify-dashboard-freshness to use diff -rq without mutating tracked tree | a8e094e | Makefile |

## What Was Built

The `verify-dashboard-freshness` target in `Makefile` (lines 283-297) was rewritten:

**Before:**
```make
verify-dashboard-freshness:
    $(MAKE) dashboard-frontend        # mutates cmd/dashboard/embed/dist/ in place
    @if ! git diff --quiet cmd/dashboard/embed/dist/; then \
        ...
    fi
```

**After:**
```make
verify-dashboard-freshness:
    cd dashboard/web && npm ci && npm run build && npm run test   # builds into dashboard/web/dist only
    @if ! diff -rq dashboard/web/dist cmd/dashboard/embed/dist; then \
        echo "FAIL: cmd/dashboard/embed/dist/ diverges from a fresh SPA build — run 'make dashboard-frontend' and commit the result before merging"; \
        exit 1; \
    fi
    @echo "PASS: cmd/dashboard/embed/dist/ matches a fresh SPA build (added/removed/changed files all checked)"
    # ... telemetry-marker assertion unchanged ...
```

## Behavioral Verification Results

All three verification scenarios passed before committing:

**1. Clean tree (WR-01 guard):**
- `make verify-dashboard-freshness` exited 0
- Printed both PASS lines ("cmd/dashboard/embed/dist/ matches a fresh SPA build" and telemetry marker)
- `git status --porcelain cmd/dashboard/embed/dist/` returned empty — no tree mutation

**2. WR-02 probe (added-file detection):**
- `touch cmd/dashboard/embed/dist/assets/_probe.js`
- Gate output: `Only in cmd/dashboard/embed/dist/assets: _probe.js` + FAIL line
- Gate exited non-zero
- Probe removed; `git status --porcelain cmd/dashboard/embed/dist/` returned empty (tracked tree left untouched by the gate)

**3. Tests still green:**
- 204/204 vitest tests passed in both runs

## Deviations from Plan

None — plan executed exactly as written. The single task matched the locked decisions precisely.

## Known Stubs

None.

## Threat Flags

None — this change is internal build tooling only (Makefile target), no new network endpoints, auth paths, file access patterns, or schema changes.

## Self-Check: PASSED

- [x] `Makefile` modified: contains `diff -rq dashboard/web/dist cmd/dashboard/embed/dist` (verified)
- [x] `Makefile` does NOT contain `$(MAKE) dashboard-frontend` in `verify-dashboard-freshness` (verified)
- [x] Commit a8e094e exists: `git log --oneline | grep a8e094e` confirms
- [x] Clean-tree run: exit 0, both PASS lines, `git status --porcelain cmd/dashboard/embed/dist/` empty
- [x] WR-02 probe: gate caught `_probe.js` as `Only in cmd/dashboard/embed/dist/assets`, exited non-zero
- [x] Telemetry marker assertion preserved and passing
