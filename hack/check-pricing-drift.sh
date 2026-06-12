#!/usr/bin/env bash
# check-pricing-drift.sh — fetch Anthropic pricing docs and diff against the
# compiled priceTable in internal/subagent/anthropic/pricing.go.
#
# D-03: run weekly via .github/workflows/pricing-drift.yaml or locally:
#   ./hack/check-pricing-drift.sh
#
# Exit codes:
#   0 — no drift detected; compiled table matches published pricing page
#   1 — drift detected; one line per drifted entry: "MODEL DIMENSION TABLE_CENTS LIVE_CENTS"
#   2 — fetch failed; do NOT file a drift issue for network failures
#
# Usage notes:
#   - POSIX tools only (grep, awk, sed); no jq or python dependency.
#   - Prices on the published page are in USD per million tokens; this script
#     converts to cents per million (multiply by 100) for comparison against
#     the compiled table's integer cents/MTok values.
#   - Models absent from the compiled table but present on the live page are
#     reported as "missing from table" drift.
#   - Models in the compiled table but absent from the live page are informational
#     only (page may rename sections) — no drift exit.
#   - Unparseable price entries (model heading found but price line not parseable)
#     are reported as parse errors and exit 1 so the operator can investigate.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PRICING_URL="https://platform.claude.com/docs/en/pricing.md"
PRICING_TMP="$(mktemp /tmp/anthropic-pricing-XXXXXX.md)"

# ---------------------------------------------------------------------------
# Step 1: Fetch the live pricing page
# ---------------------------------------------------------------------------
if ! curl -fsSL \
        --retry 5 --retry-delay 3 --retry-all-errors --retry-connrefused \
        --connect-timeout 30 \
        -o "${PRICING_TMP}" \
        "${PRICING_URL}"; then
    echo "ERROR: failed to fetch pricing page from ${PRICING_URL}" >&2
    rm -f "${PRICING_TMP}"
    exit 2
fi

if [ ! -s "${PRICING_TMP}" ]; then
    echo "ERROR: pricing page fetch returned empty body from ${PRICING_URL}" >&2
    rm -f "${PRICING_TMP}"
    exit 2
fi

# ---------------------------------------------------------------------------
# Step 2: Extract compiled model IDs and prices from pricing.go
# ---------------------------------------------------------------------------
PRICING_GO="${REPO_ROOT}/internal/subagent/anthropic/pricing.go"
if [ ! -f "${PRICING_GO}" ]; then
    echo "ERROR: ${PRICING_GO} not found — run from repo root or adjust REPO_ROOT" >&2
    rm -f "${PRICING_TMP}"
    exit 2
fi

# Extract model IDs from the compiled table: lines matching "claude-..." key entries.
# grep -oP extracts only the quoted model ID from lines like: "claude-fable-5": {
COMPILED_MODELS=$(grep -oE '"claude-[a-z0-9-]+"' "${PRICING_GO}" | tr -d '"' | sort -u)

if [ -z "${COMPILED_MODELS}" ]; then
    echo "ERROR: no model IDs found in ${PRICING_GO} — file format may have changed" >&2
    rm -f "${PRICING_TMP}"
    exit 2
fi

# ---------------------------------------------------------------------------
# Step 3: Parse live page prices and compare against compiled table
# ---------------------------------------------------------------------------
DRIFT_LINES=""
PARSE_ERRORS=""

# For each compiled model ID, attempt to find its entry on the live page.
# The Anthropic pricing page uses Markdown table format with rows like:
#   | claude-fable-5 | $10 / MTok | $50 / MTok | ... |
# or section headers like:
#   ## claude-fable-5
# followed by a pricing table.
#
# We look for a table row containing the model ID, then extract the first
# two numeric dollar amounts (input and output prices per MTok).
while IFS= read -r MODEL; do
    # Escape dots in model ID for use in grep pattern.
    MODEL_PAT=$(printf '%s' "${MODEL}" | sed 's/\./\\./g')

    # Search the fetched page for a line containing this model ID.
    # The Anthropic pricing docs use pipe-delimited tables; extract the line.
    PAGE_LINE=$(grep -i "${MODEL_PAT}" "${PRICING_TMP}" | head -1 || true)

    if [ -z "${PAGE_LINE}" ]; then
        # Model not found on the live page — informational only (D-03 spec:
        # "table models absent from the page are reported as informational only").
        continue
    fi

    # Extract dollar amounts from the page line.
    # Match patterns like "$10" or "$10.00" or "$3" in the table row.
    # We want the FIRST two numeric dollar values (input price, output price).
    INPUT_DOLLARS=$(printf '%s' "${PAGE_LINE}" | grep -oE '\$[0-9]+(\.[0-9]+)?' | head -1 | tr -d '$' || true)
    OUTPUT_DOLLARS=$(printf '%s' "${PAGE_LINE}" | grep -oE '\$[0-9]+(\.[0-9]+)?' | sed -n '2p' | tr -d '$' || true)

    if [ -z "${INPUT_DOLLARS}" ] || [ -z "${OUTPUT_DOLLARS}" ]; then
        # Page line found but prices not parseable — report as parse error.
        PARSE_ERRORS="${PARSE_ERRORS}UNPARSEABLE: ${MODEL} — page line found but price values not parseable; page format may have changed\n  Line: ${PAGE_LINE}\n"
        continue
    fi

    # Convert dollar amounts to cents (multiply by 100).
    # Use awk for the arithmetic to handle potential decimals (e.g. $0.80 → 80 cents).
    INPUT_LIVE_CENTS=$(awk "BEGIN { printf \"%d\", ${INPUT_DOLLARS} * 100 }")
    OUTPUT_LIVE_CENTS=$(awk "BEGIN { printf \"%d\", ${OUTPUT_DOLLARS} * 100 }")

    # Extract compiled values for this model from pricing.go.
    # The table entries look like:
    #   "claude-fable-5": {
    #       inputCentsPerMTok:  1000,
    #       outputCentsPerMTok: 5000,
    # We extract the block for this model by finding lines between the model key
    # and the closing brace.
    MODEL_BLOCK=$(awk "
        /\"${MODEL_PAT}\"[[:space:]]*:/ { found=1; next }
        found && /}/ { exit }
        found { print }
    " "${PRICING_GO}")

    INPUT_COMPILED=$(printf '%s' "${MODEL_BLOCK}" | grep 'inputCentsPerMTok' | grep -oE '[0-9]+' | head -1 || echo "")
    OUTPUT_COMPILED=$(printf '%s' "${MODEL_BLOCK}" | grep 'outputCentsPerMTok' | grep -oE '[0-9]+' | head -1 || echo "")

    if [ -z "${INPUT_COMPILED}" ] || [ -z "${OUTPUT_COMPILED}" ]; then
        PARSE_ERRORS="${PARSE_ERRORS}UNPARSEABLE: ${MODEL} compiled values — pricing.go parse error; file format may have changed\n"
        continue
    fi

    # Compare input price.
    if [ "${INPUT_COMPILED}" != "${INPUT_LIVE_CENTS}" ]; then
        DRIFT_LINES="${DRIFT_LINES}DRIFT: ${MODEL} input ${INPUT_COMPILED} cents/MTok compiled vs ${INPUT_LIVE_CENTS} cents/MTok live\n"
    fi

    # Compare output price.
    if [ "${OUTPUT_COMPILED}" != "${OUTPUT_LIVE_CENTS}" ]; then
        DRIFT_LINES="${DRIFT_LINES}DRIFT: ${MODEL} output ${OUTPUT_COMPILED} cents/MTok compiled vs ${OUTPUT_LIVE_CENTS} cents/MTok live\n"
    fi
done <<< "${COMPILED_MODELS}"

# ---------------------------------------------------------------------------
# Step 4: Check for models on the live page that are missing from the table.
# ---------------------------------------------------------------------------
# Extract model IDs the live page mentions (claude-* strings).
LIVE_MODELS=$(grep -oE 'claude-[a-z0-9-]+' "${PRICING_TMP}" | sort -u || true)

while IFS= read -r LIVE_MODEL; do
    # Only check models that look like real versioned model IDs (must contain a digit).
    if ! printf '%s' "${LIVE_MODEL}" | grep -qE '[0-9]'; then
        continue
    fi
    # Skip if already in compiled models.
    if printf '%s' "${COMPILED_MODELS}" | grep -qx "${LIVE_MODEL}"; then
        continue
    fi
    DRIFT_LINES="${DRIFT_LINES}DRIFT: ${LIVE_MODEL} input — model present on live page but missing from compiled table\n"
    DRIFT_LINES="${DRIFT_LINES}DRIFT: ${LIVE_MODEL} output — model present on live page but missing from compiled table\n"
done <<< "${LIVE_MODELS}"

# ---------------------------------------------------------------------------
# Cleanup temp file
# ---------------------------------------------------------------------------
rm -f "${PRICING_TMP}"

# ---------------------------------------------------------------------------
# Step 5: Report results
# ---------------------------------------------------------------------------
if [ -n "${PARSE_ERRORS}" ]; then
    printf "PARSE ERRORS (exit 1 — page or file format may have changed):\n"
    printf '%b' "${PARSE_ERRORS}"
    exit 1
fi

if [ -n "${DRIFT_LINES}" ]; then
    printf "Pricing drift detected between compiled table and live page:\n"
    printf "  %-40s  %-12s  %-12s\n" "MODEL + DIMENSION" "TABLE" "LIVE"
    printf "  %-40s  %-12s  %-12s\n" "---" "---" "---"
    printf '%b' "${DRIFT_LINES}" | while IFS= read -r LINE; do
        [ -z "${LINE}" ] && continue
        # Parse "DRIFT: MODEL DIMENSION TABLE_CENTS cents/MTok compiled vs LIVE_CENTS cents/MTok live"
        # or "DRIFT: MODEL DIMENSION — model present on live page but missing from compiled table"
        MODEL_DIM=$(printf '%s' "${LINE}" | sed 's/^DRIFT: //' | sed 's/ [0-9].*//' | sed 's/ —.*//')
        if printf '%s' "${LINE}" | grep -q 'missing from compiled table'; then
            printf "  %-40s  %-12s  %-12s\n" "${MODEL_DIM}" "(missing)" "present"
        else
            TABLE_VAL=$(printf '%s' "${LINE}" | grep -oE '[0-9]+ cents/MTok compiled' | grep -oE '^[0-9]+')
            LIVE_VAL=$(printf '%s' "${LINE}" | grep -oE '[0-9]+ cents/MTok live' | grep -oE '^[0-9]+')
            printf "  %-40s  %-12s  %-12s\n" "${MODEL_DIM}" "${TABLE_VAL}" "${LIVE_VAL}"
        fi
    done
    printf "\nTo fix: update internal/subagent/anthropic/pricing.go with the values above.\n"
    printf "See: ./hack/check-pricing-drift.sh (D-03) and .github/workflows/pricing-drift.yaml\n"
    exit 1
fi

printf "no pricing drift detected — compiled table matches %s\n" "${PRICING_URL}"
exit 0
