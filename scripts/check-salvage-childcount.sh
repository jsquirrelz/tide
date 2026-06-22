#!/usr/bin/env bash
# check-salvage-childcount.sh — assert every complete salvage envelope carries
# childCount == len(childCRDs) (D-16b). "Complete" means exitCode==0 AND
# childCRDs is a non-empty array. Failed envelopes (exitCode!=0) are skipped.
#
# Usage:
#   bash scripts/check-salvage-childcount.sh
#
# Exit 0  — all complete envelopes carry the correct childCount.
# Exit 1  — at least one complete envelope has childCount != len(childCRDs).
#           The offending path is printed to stderr.

set -euo pipefail

ENVELOPES_DIR="${1:-examples/projects/dogfood/salvage-20260618/pvc-envelopes/envelopes}"

if [[ ! -d "${ENVELOPES_DIR}" ]]; then
  echo "ERROR: envelopes directory not found: ${ENVELOPES_DIR}" >&2
  exit 1
fi

fail=0

for out_json in "${ENVELOPES_DIR}"/*/out.json; do
  # Skip if out.json does not exist (directory has no out.json)
  [[ -f "${out_json}" ]] || continue

  exit_code=$(python3 -c "
import json, sys
with open('${out_json}') as f:
    d = json.load(f)
print(d.get('exitCode', -1))
")

  # Only check exitCode==0 envelopes
  [[ "${exit_code}" == "0" ]] || continue

  result=$(python3 -c "
import json, sys
with open('${out_json}') as f:
    d = json.load(f)
child_crds = d.get('childCRDs', [])
child_count = d.get('childCount', 0)
num_children = len(child_crds)
if num_children == 0:
    # executor-level envelope: 0==0 is correct
    print('ok')
elif child_count == num_children:
    print('ok')
else:
    print(f'FAIL:childCount={child_count} len(childCRDs)={num_children}')
")

  if [[ "${result}" == FAIL* ]]; then
    echo "FAIL: ${out_json}: ${result}" >&2
    fail=1
  fi
done

if [[ "${fail}" -eq 0 ]]; then
  echo "check-salvage-childcount: all complete envelopes carry correct childCount"
  exit 0
else
  exit 1
fi
