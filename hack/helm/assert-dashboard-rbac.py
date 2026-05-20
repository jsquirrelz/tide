#!/usr/bin/env python3
"""Assert the rendered dashboard ClusterRole is read-only.

Phase 4 plan 04-14 / T-04-D2 mitigation. Walks the helm-rendered chart for
any ClusterRole whose `metadata.name` contains the substring "dashboard"
and verifies every `rules[].verbs[]` entry is in the allow-list
{get, list, watch}. Any deviation exits non-zero with a clear failure
message naming the offending role + verb.

Why this exists: a future PR that widens the dashboard ClusterRole (e.g.
adds `patch` so the dashboard could write back annotations directly,
bypassing the CLI-only mutation rule from DASH-05) would silently ship
read-write RBAC. This script fails CI before that PR can merge.

Invocation: `python3 hack/helm/assert-dashboard-rbac.py <rendered-yaml>`
Driven by the `make helm-rbac-assert` Makefile target.
"""

import sys
import yaml

ALLOWED_VERBS = {"get", "list", "watch"}


def main() -> int:
    if len(sys.argv) != 2:
        print(f"usage: {sys.argv[0]} <rendered-chart.yaml>", file=sys.stderr)
        return 2

    with open(sys.argv[1]) as f:
        docs = list(yaml.safe_load_all(f))

    violations = []
    found_dashboard_role = False
    for doc in docs:
        if not doc:
            continue
        if doc.get("kind") != "ClusterRole":
            continue
        name = doc.get("metadata", {}).get("name", "")
        if "dashboard" not in name:
            continue
        found_dashboard_role = True
        for rule in doc.get("rules", []):
            for verb in rule.get("verbs", []):
                if verb not in ALLOWED_VERBS:
                    violations.append((name, verb))

    if not found_dashboard_role:
        print("FAIL: no dashboard ClusterRole found in rendered chart", file=sys.stderr)
        return 1

    if violations:
        print("FAIL: dashboard ClusterRole has non-read-only verbs (T-04-D2 violation):", file=sys.stderr)
        for name, verb in violations:
            print(f"  {name}: verb={verb!r} (allowed: {sorted(ALLOWED_VERBS)})", file=sys.stderr)
        return 1

    print("PASS: dashboard RBAC is read-only")
    return 0


if __name__ == "__main__":
    sys.exit(main())
