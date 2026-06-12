#!/usr/bin/env python3
"""Assert PROM_ENDPOINT env var presence/absence on the dashboard container.

Phase 04 helm-telemetry-config render gate (plan-04-helm-template-verification,
task-01). Walks a `helm template` stdout YAML dump for the Deployment that
contains a container named `dashboard` and asserts whether the `PROM_ENDPOINT`
env entry is present with a specific value (--expect-endpoint) or entirely
absent (--expect-absent).

Why this exists: Plan-02 adds a guarded `PROM_ENDPOINT` env var to the
dashboard container — emitted IFF `.Values.prometheus.endpoint` is non-empty.
When the value is empty the dashboard proxy falls back to a JSON 'unavailable'
status payload. This script fails CI if the template logic drifts: e.g. a PR
that accidentally hard-codes the env var, drops the guard, or renames the
container would be caught before merging.

Invocation:
  python3 hack/helm/assert-prometheus-env.py <rendered-chart.yaml> \\
      (--expect-endpoint <value> | --expect-absent)
Driven by the `make helm-telemetry-assert` Makefile target (Phase 16 TELEM-05 D-13).
"""

import sys
import yaml


def main() -> int:
    # --- argument parsing (manual, mirroring sibling assert-dashboard-rbac.py) ---
    argv = sys.argv
    if len(argv) < 3:
        print(f"usage: {argv[0]} <rendered-chart.yaml> (--expect-endpoint <value> | --expect-absent)", file=sys.stderr)
        return 2

    chart_path = argv[1]
    mode_flag = argv[2]

    if mode_flag == "--expect-absent":
        if len(argv) != 3:
            print(f"usage: {argv[0]} <rendered-chart.yaml> --expect-absent", file=sys.stderr)
            return 2
        mode = "absent"
        expected_value = None
    elif mode_flag == "--expect-endpoint":
        if len(argv) != 4:
            print(f"usage: {argv[0]} <rendered-chart.yaml> --expect-endpoint <value>", file=sys.stderr)
            return 2
        mode = "endpoint"
        expected_value = argv[3]
    else:
        print(f"usage: {argv[0]} <rendered-chart.yaml> (--expect-endpoint <value> | --expect-absent)", file=sys.stderr)
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

    # --- collect PROM_ENDPOINT entries ---
    prom_entries = [e for e in dashboard_env if e.get("name") == "PROM_ENDPOINT"]

    # --- evaluate assertion ---
    if mode == "absent":
        if len(prom_entries) == 0:
            print("PASS: PROM_ENDPOINT env var is absent from dashboard container (expected)")
            return 0
        found_values = [e.get("value") for e in prom_entries]
        print(
            f"FAIL: expected no PROM_ENDPOINT env on dashboard container, "
            f"but found {len(prom_entries)} entry/entries with value(s) {found_values!r}",
            file=sys.stderr,
        )
        return 1

    # mode == "endpoint"
    if len(prom_entries) == 0:
        print(
            f"FAIL: expected PROM_ENDPOINT={expected_value!r} on dashboard container, "
            f"but no PROM_ENDPOINT env entry was found",
            file=sys.stderr,
        )
        return 1

    if len(prom_entries) > 1:
        found_values = [e.get("value") for e in prom_entries]
        print(
            f"FAIL: expected exactly one PROM_ENDPOINT env entry with value {expected_value!r}, "
            f"but found {len(prom_entries)} entries with values {found_values!r}",
            file=sys.stderr,
        )
        return 1

    actual_value = prom_entries[0].get("value")
    if actual_value != expected_value:
        print(
            f"FAIL: PROM_ENDPOINT env value mismatch on dashboard container: "
            f"expected {expected_value!r}, got {actual_value!r}",
            file=sys.stderr,
        )
        return 1

    print(f"PASS: PROM_ENDPOINT={actual_value!r} on dashboard container (expected)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
