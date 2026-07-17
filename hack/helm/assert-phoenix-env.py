#!/usr/bin/env python3
"""Assert PHOENIX_BASE_URL env var presence/absence on the dashboard container.

Phase 46 OBS-01/OBS-04 render gate. Sibling of assert-prometheus-env.py with
PROM_ENDPOINT swapped for PHOENIX_BASE_URL throughout. Walks a `helm template`
stdout YAML dump for the Deployment that contains a container named
`dashboard` and asserts whether the `PHOENIX_BASE_URL` env entry is present
with a specific value (--expect-value) or entirely absent (--expect-absent).

Why this exists: Plan 46-02 adds a guarded `PHOENIX_BASE_URL` env var to the
dashboard container — emitted IFF `.Values.phoenix.baseURL` is non-empty.
When the value is empty the dashboard renders no deep-link affordance at all
(no dead buttons). This script fails CI if the template logic drifts: e.g. a
PR that accidentally hard-codes the env var, drops the guard, or renames the
container would be caught before merging.

Invocation:
  python3 hack/helm/assert-phoenix-env.py <rendered-chart.yaml> \\
      (--expect-value <url> | --expect-absent)
Driven by the `make helm-telemetry-assert` Makefile target (Phase 46 OBS-01/OBS-04).
"""

import sys
import yaml


def main() -> int:
    # --- argument parsing (manual, mirroring sibling assert-prometheus-env.py) ---
    argv = sys.argv
    if len(argv) < 3:
        print(f"usage: {argv[0]} <rendered-chart.yaml> (--expect-value <value> | --expect-absent)", file=sys.stderr)
        return 2

    chart_path = argv[1]
    mode_flag = argv[2]

    if mode_flag == "--expect-absent":
        if len(argv) != 3:
            print(f"usage: {argv[0]} <rendered-chart.yaml> --expect-absent", file=sys.stderr)
            return 2
        mode = "absent"
        expected_value = None
    elif mode_flag == "--expect-value":
        if len(argv) != 4:
            print(f"usage: {argv[0]} <rendered-chart.yaml> --expect-value <value>", file=sys.stderr)
            return 2
        mode = "value"
        expected_value = argv[3]
    else:
        print(f"usage: {argv[0]} <rendered-chart.yaml> (--expect-value <value> | --expect-absent)", file=sys.stderr)
        return 2

    # --- load rendered YAML ---
    with open(chart_path) as f:
        docs = list(yaml.safe_load_all(f))

    # --- locate dashboard container ---
    found_dashboard_container = False
    dashboard_env = []

    for doc in docs:
        if not doc:
            continue
        if doc.get("kind") != "Deployment":
            continue
        containers = doc.get("spec", {}).get("template", {}).get("spec", {}).get("containers", [])
        for container in containers:
            if container.get("name") == "dashboard":
                found_dashboard_container = True
                dashboard_env = container.get("env") or []
                break
        if found_dashboard_container:
            break

    if not found_dashboard_container:
        print("FAIL: no Deployment with a container named 'dashboard' in rendered chart", file=sys.stderr)
        return 1

    # --- collect PHOENIX_BASE_URL entries ---
    phoenix_entries = [e for e in dashboard_env if e.get("name") == "PHOENIX_BASE_URL"]

    # --- evaluate assertion ---
    if mode == "absent":
        if len(phoenix_entries) == 0:
            print("PASS: PHOENIX_BASE_URL env var is absent from dashboard container (expected)")
            return 0
        found_values = [e.get("value") for e in phoenix_entries]
        print(
            f"FAIL: expected no PHOENIX_BASE_URL env on dashboard container, "
            f"but found {len(phoenix_entries)} entry/entries with value(s) {found_values!r}",
            file=sys.stderr,
        )
        return 1

    # mode == "value"
    if len(phoenix_entries) == 0:
        print(
            f"FAIL: expected PHOENIX_BASE_URL={expected_value!r} on dashboard container, "
            f"but no PHOENIX_BASE_URL env entry was found",
            file=sys.stderr,
        )
        return 1

    if len(phoenix_entries) > 1:
        found_values = [e.get("value") for e in phoenix_entries]
        print(
            f"FAIL: expected exactly one PHOENIX_BASE_URL env entry with value {expected_value!r}, "
            f"but found {len(phoenix_entries)} entries with values {found_values!r}",
            file=sys.stderr,
        )
        return 1

    actual_value = phoenix_entries[0].get("value")
    if actual_value != expected_value:
        print(
            f"FAIL: PHOENIX_BASE_URL env value mismatch on dashboard container: "
            f"expected {expected_value!r}, got {actual_value!r}",
            file=sys.stderr,
        )
        return 1

    print(f"PASS: PHOENIX_BASE_URL={actual_value!r} on dashboard container (expected)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
