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
PRICING_URL="https://platform.claude.com/docs/en/about-claude/pricing.md"
PRICING_TMP="$(mktemp /tmp/anthropic-pricing-XXXXXX)"
# BSD/macOS mktemp only randomizes TRAILING X's — a suffix after the X's makes
# the path literal and predictable, and a second run after an interrupted one
# fails with "File exists" (misread as drift under set -e). Clean up on any
# exit, including interrupts, so a leftover file never breaks the next run.
trap 'rm -f "${PRICING_TMP}"' EXIT

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

# For each compiled model ID, attempt to find its pricing row on the live page.
# The Anthropic pricing page lists models in a Markdown pipe table whose HEADER
# names the price columns, e.g.:
#   | Model | Base Input Tokens | 5m Cache Writes | 1h Cache Writes | Cache Hits & Refreshes | Output Tokens |
#   | Claude Fable 5 | $10 / MTok | $12.50 / MTok | $20 / MTok | $1 / MTok | $50 / MTok |
#
# Extraction is anchored on the header row: positional "first two $ amounts"
# parsing silently mis-reads a cache-write column as the output price whenever
# the column order is input / cache-write / ... / output. The model cell may
# carry a display name ("Claude Opus 4.8") or a raw ID (claude-opus-4-8) — the
# match pattern tolerates hyphen/space/dot separators in either form.
while IFS= read -r MODEL; do
    # Hyphens in the compiled ID may appear as hyphen, space, or dot on the
    # page ("claude-opus-4-8" vs "Claude Opus 4.8"); matching is case-insensitive.
    MODEL_PAT=$(printf '%s' "${MODEL}" | sed 's/-/[ .-]/g')

    # Header-anchored row extraction: remember the input/output/cache column
    # indexes from the most recent table header row, then print those cells
    # from the first priced row whose model cell (field 2) matches. Output is
    # one TAB-separated line: input, output, cache-write (5m), cache-read.
    ROW_CELLS=$(awk -v model="${MODEL_PAT}" '
        BEGIN { FS = "|"; in_col = 0; out_col = 0; cw_col = 0; cr_col = 0 }
        /^\|/ {
            low = tolower($0)
            if (low !~ /\$/) {
                # No prices on the line — candidate header row. Require both an
                # input and an output column label before trusting the indexes.
                if (low ~ /input/ && low ~ /output/) {
                    in_col = 0; out_col = 0; cw_col = 0; cr_col = 0
                    for (i = 2; i <= NF; i++) {
                        cell = tolower($i)
                        if (cell ~ /input/ && in_col == 0) in_col = i
                        else if (cell ~ /output/ && out_col == 0) out_col = i
                        else if (cell ~ /cache/ && cell ~ /write/ && cell !~ /1[ -]?h/ && cw_col == 0) cw_col = i
                        else if (cell ~ /cache/ && cell ~ /(hit|read)/ && cr_col == 0) cr_col = i
                    }
                }
                next
            }
            if (in_col > 0 && out_col > 0 && tolower($2) ~ model) {
                printf "%s\t%s\t%s\t%s\n", $(in_col), $(out_col), \
                    (cw_col > 0 ? $(cw_col) : ""), (cr_col > 0 ? $(cr_col) : "")
                exit
            }
        }
    ' "${PRICING_TMP}")

    if [ -z "${ROW_CELLS}" ]; then
        if grep -qiE "${MODEL_PAT}" "${PRICING_TMP}"; then
            # Mentioned on the page but no header-anchored pricing row parsed —
            # the table layout may have changed; surface as a parse error.
            PARSE_ERRORS="${PARSE_ERRORS}UNPARSEABLE: ${MODEL} — model mentioned on page but no header-anchored pricing row found; page format may have changed\n"
        fi
        # Otherwise: model absent from the live page — informational only (D-03
        # spec: "table models absent from the page are reported as informational only").
        continue
    fi

    # Extract one dollar amount per column cell ("$10", "$12.50", "$0.30").
    INPUT_DOLLARS=$(printf '%s' "${ROW_CELLS}" | cut -f1 | grep -oE '\$[0-9]+(\.[0-9]+)?' | head -1 | tr -d '$' || true)
    OUTPUT_DOLLARS=$(printf '%s' "${ROW_CELLS}" | cut -f2 | grep -oE '\$[0-9]+(\.[0-9]+)?' | head -1 | tr -d '$' || true)
    CACHE_WRITE_DOLLARS=$(printf '%s' "${ROW_CELLS}" | cut -f3 | grep -oE '\$[0-9]+(\.[0-9]+)?' | head -1 | tr -d '$' || true)
    CACHE_READ_DOLLARS=$(printf '%s' "${ROW_CELLS}" | cut -f4 | grep -oE '\$[0-9]+(\.[0-9]+)?' | head -1 | tr -d '$' || true)

    if [ -z "${INPUT_DOLLARS}" ] || [ -z "${OUTPUT_DOLLARS}" ]; then
        # Row found but prices not parseable — report as parse error.
        PARSE_ERRORS="${PARSE_ERRORS}UNPARSEABLE: ${MODEL} — pricing row found but price values not parseable; page format may have changed\n  Row: ${ROW_CELLS}\n"
        continue
    fi

    # Convert dollar amounts to cents (multiply by 100). %.0f rounds to the
    # nearest cent; %d would truncate binary-inexact products ($0.30 * 100 → 29).
    INPUT_LIVE_CENTS=$(awk "BEGIN { printf \"%.0f\", ${INPUT_DOLLARS} * 100 }")
    OUTPUT_LIVE_CENTS=$(awk "BEGIN { printf \"%.0f\", ${OUTPUT_DOLLARS} * 100 }")

    # Extract compiled values for this model from pricing.go.
    # The table entries look like:
    #   "claude-fable-5": {
    #       inputCentsPerMTok:  1000,
    #       outputCentsPerMTok: 5000,
    # We extract the block for this model by finding lines between the model key
    # and the closing brace.
    MODEL_BLOCK=$(awk "
        /\"${MODEL}\"[[:space:]]*:/ { found=1; next }
        found && /}/ { exit }
        found { print }
    " "${PRICING_GO}")

    INPUT_COMPILED=$(printf '%s' "${MODEL_BLOCK}" | grep 'inputCentsPerMTok' | grep -oE '[0-9]+' | head -1 || echo "")
    OUTPUT_COMPILED=$(printf '%s' "${MODEL_BLOCK}" | grep 'outputCentsPerMTok' | grep -oE '[0-9]+' | head -1 || echo "")
    CACHE_WRITE_COMPILED=$(printf '%s' "${MODEL_BLOCK}" | grep 'cacheWriteCentsPerMTok' | grep -oE '[0-9]+' | head -1 || echo "")
    CACHE_READ_COMPILED=$(printf '%s' "${MODEL_BLOCK}" | grep 'cacheReadCentsPerMTok' | grep -oE '[0-9]+' | head -1 || echo "")

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

    # Compare cache prices when both the live column and the compiled value
    # exist. The 5m cache write maps to cacheWriteCentsPerMTok (1.25x input);
    # cache hits/reads map to cacheReadCentsPerMTok (0.10x input). An absent
    # cache column is skipped rather than failed — input/output remain the
    # load-bearing comparison.
    if [ -n "${CACHE_WRITE_DOLLARS}" ] && [ -n "${CACHE_WRITE_COMPILED}" ]; then
        CACHE_WRITE_LIVE_CENTS=$(awk "BEGIN { printf \"%.0f\", ${CACHE_WRITE_DOLLARS} * 100 }")
        if [ "${CACHE_WRITE_COMPILED}" != "${CACHE_WRITE_LIVE_CENTS}" ]; then
            DRIFT_LINES="${DRIFT_LINES}DRIFT: ${MODEL} cacheWrite ${CACHE_WRITE_COMPILED} cents/MTok compiled vs ${CACHE_WRITE_LIVE_CENTS} cents/MTok live\n"
        fi
    fi
    if [ -n "${CACHE_READ_DOLLARS}" ] && [ -n "${CACHE_READ_COMPILED}" ]; then
        CACHE_READ_LIVE_CENTS=$(awk "BEGIN { printf \"%.0f\", ${CACHE_READ_DOLLARS} * 100 }")
        if [ "${CACHE_READ_COMPILED}" != "${CACHE_READ_LIVE_CENTS}" ]; then
            DRIFT_LINES="${DRIFT_LINES}DRIFT: ${MODEL} cacheRead ${CACHE_READ_COMPILED} cents/MTok compiled vs ${CACHE_READ_LIVE_CENTS} cents/MTok live\n"
        fi
    fi
done <<< "${COMPILED_MODELS}"

# ---------------------------------------------------------------------------
# Step 4: Check for models on the live page that are missing from the table.
# ---------------------------------------------------------------------------
# Extract model IDs from PRICED TABLE ROWS only (pipe-table rows carrying a $
# amount), skipping rows marked deprecated/retired. Scanning the whole page
# flags every claude-* token in prose, URLs, code samples, and legacy-model
# sections — permanent false-drift noise that keeps the deduped issue open
# forever and trains operators to ignore it.
LIVE_MODELS=$(grep -E '^\|' "${PRICING_TMP}" | grep '\$' | grep -viE 'deprecated|retired' | grep -oE 'claude-[a-z0-9-]+' | sort -u || true)

while IFS= read -r LIVE_MODEL; do
    # Only check models that look like real versioned model IDs (must contain a digit).
    if ! printf '%s' "${LIVE_MODEL}" | grep -qE '[0-9]'; then
        continue
    fi
    # Skip page presentation rows — "claude-<model>-introductory-pricing" is a
    # slugified section label on the live page, not a billable model ID.
    if printf '%s' "${LIVE_MODEL}" | grep -qE -- '-introductory-pricing$'; then
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
