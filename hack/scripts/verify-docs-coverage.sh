#!/usr/bin/env bash
# Phase 5 DIST-04 — docs/README.md index completeness verifier (--non-strict default; --strict for Wave 2 closeout + CI).
#
# Two modes:
#   --non-strict (default): asserts docs/README.md exists + contains a Markdown
#     link to concepts.md (the entry #2 doc landed by Plan 05-04). This is the
#     Wave 1/2 gate when not all referenced docs exist yet — lets Plan 05-04
#     close while INSTALL.md / project-authoring.md are being authored in
#     parallel by Wave 2 plans 05-07/08.
#   --strict: asserts docs/README.md exists + contains Markdown links to all 12
#     file references AND every referenced file exists on disk + is non-empty.
#     This is the Wave 2 closeout / CI gate after all Wave 2 docs have landed.
#
# Wired into the CI release gate via the `verify-docs` Makefile target.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

# Parse mode flag. Default = --non-strict (Wave 1/2 partial state).
MODE="${1:---non-strict}"
case "${MODE}" in
  --strict|--non-strict)
    ;;
  *)
    echo "FAIL: unrecognized arg \"${MODE}\" (expected --strict or --non-strict)"
    exit 2
    ;;
esac

# The set of expected entries: 12 file references corresponding to the 11 D-C3
# entries (entry #4 is co-located dashboard + cli = 2 links on one numbered
# line in the index).
EXPECTED=(
  INSTALL.md
  concepts.md
  project-authoring.md
  dashboard.md
  cli.md
  gates.md
  observability.md
  git-hosts.md
  rwx-drivers.md
  live-e2e.md
  troubleshooting.md
  rbac.md
)

FAILS=0

# ---------------------------------------------------------------------------
# Check 1 (both modes): docs/README.md exists + non-empty.
# ---------------------------------------------------------------------------
if [ ! -s "${REPO_ROOT}/docs/README.md" ]; then
  echo "FAIL: docs/README.md missing or empty at ${REPO_ROOT}/docs/README.md"
  FAILS=$((FAILS+1))
fi

# ---------------------------------------------------------------------------
# Check 2 (both modes): docs/README.md contains a Markdown link to concepts.md
# (the Wave 1/2 minimal gate — concepts.md is the entry #2 doc landed by this
# plan).
# ---------------------------------------------------------------------------
if [ "${FAILS}" -eq 0 ]; then
  if ! grep -qE '\[.*\]\(concepts\.md\)' "${REPO_ROOT}/docs/README.md"; then
    echo "FAIL: docs/README.md does not link to concepts.md"
    FAILS=$((FAILS+1))
  fi
fi

# ---------------------------------------------------------------------------
# Check 3 (--strict only): every expected file is linked from docs/README.md.
# ---------------------------------------------------------------------------
if [ "${MODE}" = "--strict" ] && [ "${FAILS}" -eq 0 ]; then
  MISSING_LINKS=()
  for f in "${EXPECTED[@]}"; do
    # Escape the dot for the regex; match the bracketed-link form on any line.
    escaped="${f//./\\.}"
    if ! grep -qE "\[.*\]\(${escaped}\)" "${REPO_ROOT}/docs/README.md"; then
      MISSING_LINKS+=("${f}")
    fi
  done
  if [ "${#MISSING_LINKS[@]}" -gt 0 ]; then
    echo "FAIL: docs/README.md missing Markdown links to: ${MISSING_LINKS[*]}"
    FAILS=$((FAILS+1))
  fi
fi

# ---------------------------------------------------------------------------
# Check 4 (--strict only): every expected file exists on disk + non-empty.
# ---------------------------------------------------------------------------
if [ "${MODE}" = "--strict" ] && [ "${FAILS}" -eq 0 ]; then
  MISSING_FILES=()
  for f in "${EXPECTED[@]}"; do
    if [ ! -s "${REPO_ROOT}/docs/${f}" ]; then
      MISSING_FILES+=("${f}")
    fi
  done
  if [ "${#MISSING_FILES[@]}" -gt 0 ]; then
    echo "FAIL: docs/ missing files referenced by docs/README.md: ${MISSING_FILES[*]}"
    FAILS=$((FAILS+1))
  fi
fi

# ---------------------------------------------------------------------------
# Summary.
# ---------------------------------------------------------------------------
if [ "${FAILS}" -gt 0 ]; then
  echo ""
  echo "FAIL: ${FAILS} check(s) failed (mode=${MODE})"
  exit 1
fi

if [ "${MODE}" = "--strict" ]; then
  echo "PASS: docs/README.md index complete + all ${#EXPECTED[@]} doc files present (--strict mode)"
else
  echo "PASS: docs/README.md index present + concepts.md linked (--non-strict mode; full coverage check requires --strict)"
fi
exit 0
