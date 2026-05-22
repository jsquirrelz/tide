#!/usr/bin/env bash
# Phase 5 D-X1 / DIST-03 — LICENSE+NOTICE+Go-header verifier.
#
# Three sequential checks, each emitting `OK: ...` on pass or `FAIL: ...` on
# failure. Exits 0 only if all three checks pass. Wired into the CI release
# gate via the `verify-license` Makefile target (Phase 5 plan 05-16 inserts
# this gate into release.yaml).
#
# Check 1: LICENSE exists at repo root, is non-empty, contains the canonical
#          Apache-2.0 boilerplate ("Apache License") AND the project-specific
#          copyright line ("Copyright 2026 The TIDE Authors").
# Check 2: NOTICE exists at repo root, is non-empty, and carries the
#          "Copyright 2026 The TIDE Authors" header per Apache ASF
#          licensing-howto §"Required Third-Party Notices".
# Check 3: Every *.go file under the repo (excluding vendor/, testdata/,
#          .git/, .claude/) carries the verbatim "Apache License, Version 2.0"
#          string from hack/boilerplate.go.txt. Per-file header is the
#          canonical evidence that source-distribution Apache-2.0 compliance
#          is preserved (Apache License §4(c) — "retain ... all copyright ...
#          and attribution notices from the Source form of the Work").
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

FAILS=0

# ---------------------------------------------------------------------------
# Check 1: LICENSE
# ---------------------------------------------------------------------------
if [ ! -s "${REPO_ROOT}/LICENSE" ]; then
  echo "FAIL: LICENSE missing or empty at ${REPO_ROOT}/LICENSE"
  FAILS=$((FAILS+1))
elif ! grep -q "Apache License" "${REPO_ROOT}/LICENSE"; then
  echo "FAIL: LICENSE does not carry the canonical 'Apache License' string"
  FAILS=$((FAILS+1))
elif ! grep -q "Copyright 2026 The TIDE Authors" "${REPO_ROOT}/LICENSE"; then
  echo "FAIL: LICENSE missing 'Copyright 2026 The TIDE Authors' (D-X1)"
  FAILS=$((FAILS+1))
else
  echo "OK: LICENSE present + Apache-2.0 boilerplate + D-X1 copyright"
fi

# ---------------------------------------------------------------------------
# Check 2: NOTICE
# ---------------------------------------------------------------------------
if [ ! -s "${REPO_ROOT}/NOTICE" ]; then
  echo "FAIL: NOTICE missing or empty at ${REPO_ROOT}/NOTICE"
  FAILS=$((FAILS+1))
elif ! grep -q "Copyright 2026 The TIDE Authors" "${REPO_ROOT}/NOTICE"; then
  echo "FAIL: NOTICE missing 'Copyright 2026 The TIDE Authors' (D-X1)"
  FAILS=$((FAILS+1))
else
  echo "OK: NOTICE present + D-X1 copyright"
fi

# ---------------------------------------------------------------------------
# Check 3: Go-header coverage
#
# Walk every *.go under the repo, excluding:
#   - vendor/        (third-party deps; their own license headers apply)
#   - testdata/      (test fixtures; ignored by go tooling)
#   - .git/          (git internals)
#   - .claude/       (Claude Code working-tree state, including worktrees/)
#   - examples/      (operator-facing demo content; MIT-licensed per D-B3,
#                    distinct from TIDE's Apache-2.0 distribution)
# Any file lacking the verbatim "Apache License, Version 2.0" header string
# is a coverage gap; we list them all so the operator can see the full
# delta in one shot rather than fixing them one at a time.
# ---------------------------------------------------------------------------
MISSING=$(
  find "${REPO_ROOT}" -name '*.go' \
    -not -path '*/vendor/*' \
    -not -path '*/testdata/*' \
    -not -path '*/.git/*' \
    -not -path '*/.claude/*' \
    -not -path '*/examples/*' \
    -print0 \
  | xargs -0 grep -L "Apache License, Version 2.0" \
  || true
)

if [ -n "${MISSING}" ]; then
  echo "FAIL: Go files missing Apache-2.0 header:"
  echo "${MISSING}" | sed 's/^/  /'
  FAILS=$((FAILS+1))
else
  echo "OK: every *.go file under api/, cmd/, internal/, pkg/, test/, tools/ carries the Apache-2.0 header"
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
if [ "${FAILS}" -gt 0 ]; then
  echo ""
  echo "FAIL: ${FAILS} checks failed"
  exit 1
fi

echo ""
echo "PASS: LICENSE + NOTICE present + all Go files carry Apache-2.0 header"
