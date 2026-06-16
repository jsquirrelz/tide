---
phase: 22-dashboard-embed-freshness-fix
reviewed: 2026-06-16T07:05:00Z
depth: standard
files_reviewed: 5
files_reviewed_list:
  - Dockerfile.dashboard
  - .dockerignore
  - Makefile
  - .github/workflows/ci.yaml
  - .github/workflows/release.yaml
findings:
  critical: 0
  warning: 2
  info: 5
  total: 7
status: issues_found
---

# Phase 22: Code Review Report

**Reviewed:** 2026-06-16T07:05:00Z
**Depth:** standard
**Files Reviewed:** 5
**Status:** issues_found

## Summary

Reviewed the FIX-01 build/CI infrastructure changeset: a multi-stage `Dockerfile.dashboard`
with a digest-pinned `node:22-alpine` `spa-builder` stage, `.dockerignore` re-includes for the
SPA source, a `make verify-dashboard-freshness` gate, and the gate wired into `ci.yaml` (PR) and
`release.yaml` (`helmify-verify`, gating `release`/`build-images` via `needs:`).

The core mechanism is sound and I verified it empirically:

- The Docker multi-stage ordering is correct — `rm -rf cmd/dashboard/embed/dist` (line 44) and
  `COPY --from=spa-builder` (line 45) both precede `go build` (line 48), so the image always
  compiles a freshly built bundle and never trusts committed `dist/`.
- The freshness gate **passes** on a clean tree (exit 0, both PASS lines) and **fails** (exit 1)
  when I edited a `dashboard/web/src` string without committing the rebuilt `dist/` — confirming
  it catches the exact staleness class FIX-01 targets. The minification-surviving `data-testid`
  marker (`panel-cache-efficiency`) assertion also fires correctly.
- All `npm run build` inputs (`package.json`, lockfile, `src/`, `index.html`, the three
  tsconfigs, `vite.config.ts`, `.nvmrc`) are present in the Docker build context; `vitest.config.ts`
  is correctly omitted (test-only). No `public/` dir or separate postcss/tailwind config exists.
- Both workflow files are valid YAML; `release`/`build-images` `needs: [helmify-verify, ...]`
  wiring is intact; Node 22 is consistent across `.nvmrc`, the Dockerfile stage, and both
  workflows; the npm cache key (`dashboard/web/package-lock.json`) is correct; no job permissions
  were changed (`contents: read`). No secret-leakage risk in the re-included SPA source.

No Critical defects. Two Warnings concern gate robustness gaps that can produce a misleading
local-developer experience and a narrow false-pass window; the Info items are fragility/scope notes.

## Warnings

### WR-01: Freshness gate leaves the working tree dirty (and untracked files behind) on failure

**File:** `Makefile:285-298`
**Confidence:** High (reproduced)
**Issue:** The gate runs `$(MAKE) dashboard-frontend`, which `rm -rf`s and rebuilds
`cmd/dashboard/embed/dist/` in place, then checks `git diff --quiet`. When the gate FAILS on a
stale tree, it `exit 1`s **without restoring** the regenerated `dist/`. I reproduced this: after a
failing run the working tree was left with a deleted tracked asset (`index-BEfeN1Kf.js`), a modified
`index.html`, AND a new **untracked** asset (`index-D645NizY.js`) that `git checkout` does not
remove — it required a `git clean -fd` to fully restore. In CI this is harmless (ephemeral runner),
but a local developer who runs the gate is left with a dirty, partially-rebuilt tree on every
failure, which is exactly when they are least able to reason about what changed. The PASS path is
clean (verified), but a gate that mutates the tree it is auditing and does not clean up after a
failure is a footgun.
**Fix:** Either (a) build into a temp dir and diff against it without mutating the tracked tree, or
(b) trap and restore on failure, e.g.:
```make
verify-dashboard-freshness:
	@$(MAKE) dashboard-frontend
	@if ! git diff --quiet cmd/dashboard/embed/dist/ || [ -n "$$(git ls-files --others --exclude-standard cmd/dashboard/embed/dist/)" ]; then \
		echo "FAIL: cmd/dashboard/embed/dist/ is stale — run 'make dashboard-frontend' and commit the result"; \
		git diff --stat cmd/dashboard/embed/dist/; \
		git checkout -- cmd/dashboard/embed/dist/ 2>/dev/null; git clean -fdq cmd/dashboard/embed/dist/; \
		exit 1; \
	fi
	...
```
The `git ls-files --others` clause also closes WR-02 below.

### WR-02: `git diff --quiet` does not detect a NET-NEW untracked asset (narrow false-pass window)

**File:** `Makefile:286`
**Confidence:** Medium (mechanism reproduced; real-world trigger narrow)
**Issue:** `git diff --quiet cmd/dashboard/embed/dist/` reports only changes to **tracked** files;
it is blind to brand-new untracked files (I verified: `touch dist/assets/index-FAKENEW.js` leaves
`git diff --quiet` at exit 0). In the normal Vite flow a content change rewrites the content-hashed
filename — which deletes the old tracked asset AND rewrites the tracked `index.html` — so the gate
catches staleness via the deletion/`index.html` modification (verified: the failing run showed
`index-BEfeN1Kf.js | 294 ----` plus `index.html | 2 +-`). The gap is the case where a build emits an
**additional** asset (e.g. a new code-split chunk, a newly added font/worker) whose introduction does
**not** modify any already-tracked file: that net-new artifact is untracked, `git diff --quiet`
passes, and the gate green-lights a tree that is missing a file the running image would need. Vite's
single-entry config makes this unlikely today, but the gate's correctness silently depends on "every
build change touches a tracked file," which is not guaranteed as the SPA grows.
**Fix:** Add an untracked-file check alongside the diff (the canonical "is the tree reproducible"
gate covers both), as shown in WR-01's fix:
```make
[ -z "$$(git ls-files --others --exclude-standard cmd/dashboard/embed/dist/)" ]
```
or use `git status --porcelain cmd/dashboard/embed/dist/` and assert empty output.

## Info

### IN-01: Shared `.dockerignore` now ships SPA source into every unrelated image build context

**File:** `.dockerignore:37-45`
**Confidence:** High
**Issue:** `.dockerignore` is shared across all image builds (manager `Dockerfile`,
`claude-subagent`, `tide-git-http-server`, etc.), not just `Dockerfile.dashboard`. The new
re-includes pull ~544K of `dashboard/web/src` into the build context of every image, including ones
that do not build `cmd/dashboard`. This is harmless to the output images (`COPY . .` + a targeted
`go build` won't embed it) but inflates every build context and upload. Acceptable, but worth a note.
**Fix:** None required for correctness. If context size matters, consider a dashboard-specific
ignore file (`Dockerfile.dashboard.dockerignore` via buildx) so the re-includes are scoped.

### IN-02: Docker build correctness depends on `vitest.config.ts` being in tsconfig `include` (not `files`)

**File:** `Dockerfile.dashboard:23-29`
**Confidence:** High (verified)
**Issue:** `npm run build` runs `tsc -b && vite build`. `tsconfig.node.json` declares
`"include": ["vite.config.ts", "vitest.config.ts"]`, and the Dockerfile copies `vite.config.ts` but
**not** `vitest.config.ts`. I verified a cold `tsc -b` with `vitest.config.ts` absent still exits 0,
because TypeScript treats `include` entries as globs and silently skips missing files. So the build
is fine today — but the plan's stated rationale ("vitest.config.ts is test-only; not required for
build") is only accidentally correct: if anyone moves `vitest.config.ts` into a tsconfig `files`
array (which errors on missing files), the Docker build breaks with a non-obvious "file not found,"
while local `make dashboard-frontend` keeps passing.
**Fix:** Either copy `vitest.config.ts` into the spa-builder stage too (cheap, removes the fragility)
or add a comment at `tsconfig.node.json` noting that `vitest.config.ts` must stay in `include`, never
`files`, so the Dockerfile context can omit it.

### IN-03: `dashboard-frontend` (and thus the gate) runs full vitest, making the gate heavier than needed

**File:** `Makefile:279,285`
**Confidence:** High
**Issue:** The gate calls `$(MAKE) dashboard-frontend`, whose recipe is
`npm ci && npm run build && npm run test`. So `verify-dashboard-freshness` runs the entire 204-test
vitest suite (~11s locally) on top of the build, even though its job is only freshness + marker. The
plan explicitly chose to omit vitest from the Dockerfile for this reason, but the gate reintroduces
it via `dashboard-frontend`. Not a defect (tests passing is a fine side effect), but the gate is
slower and couples freshness signal to test signal — a flaky frontend test would now also red the
freshness gate.
**Fix:** Optional. If freshness should be independent of tests, factor a `dashboard-frontend-build`
(no `npm run test`) target and have the gate call that; keep the full target for the dev workflow.

### IN-04: `grep -r` over an explicit `*.js` glob is redundant; relies on shell glob fail-safe

**File:** `Makefile:293`
**Confidence:** Medium
**Issue:** `grep -qr "$$MARKER" cmd/dashboard/embed/dist/assets/*.js 2>/dev/null` combines `-r`
(recursive) with an explicit file glob. If `assets/` ever contains a subdirectory, `-r` would recurse
into it; today it won't. If the glob matches zero files, the shell passes the literal `*.js`, grep
errors (swallowed by `2>/dev/null`), and the else-branch correctly FAILs — so the fail-safe is sound,
but it leans on shell non-`nullglob` behavior rather than being explicit. Minor robustness nit.
**Fix:** Drop `-r` (the glob already enumerates the files): `grep -ql "$$MARKER" .../assets/*.js`,
or guard with an explicit "no assets found → FAIL" check.

### IN-05: `helmify-verify` timeout bump is reasonable but the gate adds a hard dependency on npm registry availability to the release path

**File:** `.github/workflows/release.yaml:52,99-107`
**Confidence:** Low (design note, not a defect)
**Issue:** Appending the freshness gate (which runs `npm ci` → network fetch from the npm registry)
into `helmify-verify` means a registry outage or a transient `npm ci` failure now blocks `release`
and `build-images` (both `needs: helmify-verify`). Previously `helmify-verify` was a hermetic,
network-light chart-reproducibility check. The 5→10 min timeout bump is appropriately justified in
the comment. This is an accepted defense-in-depth tradeoff, not a bug, but it does widen the release
path's external-dependency surface; the `npm ci` lockfile install keeps it deterministic, and the
cache mitigates flakiness.
**Fix:** None required. If release robustness becomes a concern, consider `npm ci --prefer-offline`
or a retry wrapper on the gate step.

---

_Reviewed: 2026-06-16T07:05:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
