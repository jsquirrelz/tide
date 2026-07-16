#!/usr/bin/env bash
# eval: redaction-scenario, version 1
# Owner: security team — weakening or replacing this eval requires their approval (PROJECT.md).
# Pass: exit 0. Fail: non-zero, with the surviving credential printed to stderr.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
FIXTURE="$HERE/../fixtures/credentials/planted.log"
OUT="$(mktemp)"
trap 'rm -f "$OUT"' EXIT

redactlog < "$FIXTURE" > "$OUT"

# The fixture plants AWS's documented example credentials and a fake bearer
# token. The scenario checks the exact planted literals: none may survive.
PLANTED=(
  'AKIAIOSFODNN7EXAMPLE'
  'wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY'
  'tok-3f9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c'
)
for p in "${PLANTED[@]}"; do
  if grep -Fq "$p" "$OUT"; then
    echo "FAIL: planted credential survived redaction: $p" >&2
    exit 1
  fi
done

# Guard against a silently empty or wrong input: redactions must have happened.
grep -q 'REDACTED' "$OUT" || { echo "FAIL: no redactions applied — wrong fixture or no-op redactor" >&2; exit 1; }

echo "PASS: redaction-scenario v1"
