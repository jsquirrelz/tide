#!/usr/bin/env python3
"""Assert OTEL_EXPORTER_OTLP_HEADERS env var presence/absence on the manager
AND dashboard containers.

Phase 47 PHX-02/D-08 render gate. Sibling of assert-phoenix-env.py with two
divergences: (1) it checks BOTH containers named `manager` and `dashboard` —
failing if either is missing the expectation — because Task 1 wires the
guarded env block on both the controller-manager Deployment and the
dashboard Deployment; (2) its present-mode flag is `--expect-secretref NAME
KEY`, asserting the env entry is Secret-sourced via `valueFrom.secretKeyRef`
with the given name/key and carries NO literal `value` field. This script
never asserts on a secret's actual value (ASVS V6) — only on the reference
shape.

Why this exists: Plan 47-02 adds a guarded `OTEL_EXPORTER_OTLP_HEADERS` env
var to the manager and dashboard containers — emitted IFF
`.Values.otel.exporter.headersSecretRef.name` is non-empty. When the value is
empty neither container renders the env at all. This script fails CI if the
template logic drifts: e.g. a PR that accidentally hard-codes a literal
value, drops the guard, renames a container, or only wires one of the two
containers would be caught before merging.

Invocation:
  python3 hack/helm/assert-otlp-headers-env.py <rendered-chart.yaml> \\
      (--expect-secretref <name> <key> | --expect-absent)
Driven by the `make helm-telemetry-assert` Makefile target (Phase 47 PHX-02/D-08).
"""

import sys
import yaml

CONTAINER_NAMES = ("manager", "dashboard")


def _find_containers(docs):
    """Return {container_name: env_list} for every target container found
    across all Deployments in the rendered manifest set."""
    found = {}
    for doc in docs:
        if not doc:
            continue
        if doc.get("kind") != "Deployment":
            continue
        containers = doc.get("spec", {}).get("template", {}).get("spec", {}).get("containers", [])
        for container in containers:
            name = container.get("name")
            if name in CONTAINER_NAMES and name not in found:
                found[name] = container.get("env") or []
    return found


def main() -> int:
    argv = sys.argv
    if len(argv) < 3:
        print(
            f"usage: {argv[0]} <rendered-chart.yaml> (--expect-secretref <name> <key> | --expect-absent)",
            file=sys.stderr,
        )
        return 2

    chart_path = argv[1]
    mode_flag = argv[2]

    if mode_flag == "--expect-absent":
        if len(argv) != 3:
            print(f"usage: {argv[0]} <rendered-chart.yaml> --expect-absent", file=sys.stderr)
            return 2
        mode = "absent"
        expected_name = None
        expected_key = None
    elif mode_flag == "--expect-secretref":
        if len(argv) != 5:
            print(f"usage: {argv[0]} <rendered-chart.yaml> --expect-secretref <name> <key>", file=sys.stderr)
            return 2
        mode = "secretref"
        expected_name = argv[3]
        expected_key = argv[4]
    else:
        print(
            f"usage: {argv[0]} <rendered-chart.yaml> (--expect-secretref <name> <key> | --expect-absent)",
            file=sys.stderr,
        )
        return 2

    with open(chart_path) as f:
        docs = list(yaml.safe_load_all(f))

    containers = _find_containers(docs)
    missing = [name for name in CONTAINER_NAMES if name not in containers]
    if missing:
        print(
            f"FAIL: no Deployment with a container named {missing!r} in rendered chart "
            f"(expected both {list(CONTAINER_NAMES)!r})",
            file=sys.stderr,
        )
        return 1

    overall_fail = False

    for name in CONTAINER_NAMES:
        env = containers[name]
        entries = [e for e in env if e.get("name") == "OTEL_EXPORTER_OTLP_HEADERS"]

        if mode == "absent":
            if len(entries) == 0:
                print(f"PASS: OTEL_EXPORTER_OTLP_HEADERS env var is absent from {name} container (expected)")
                continue
            print(
                f"FAIL: expected no OTEL_EXPORTER_OTLP_HEADERS env on {name} container, "
                f"but found {len(entries)} entry/entries {entries!r}",
                file=sys.stderr,
            )
            overall_fail = True
            continue

        # mode == "secretref"
        if len(entries) == 0:
            print(
                f"FAIL: expected OTEL_EXPORTER_OTLP_HEADERS secretKeyRef on {name} container, "
                f"but no OTEL_EXPORTER_OTLP_HEADERS env entry was found",
                file=sys.stderr,
            )
            overall_fail = True
            continue

        if len(entries) > 1:
            print(
                f"FAIL: expected exactly one OTEL_EXPORTER_OTLP_HEADERS env entry on {name} container, "
                f"but found {len(entries)} entries {entries!r}",
                file=sys.stderr,
            )
            overall_fail = True
            continue

        entry = entries[0]

        if "value" in entry:
            print(
                f"FAIL: OTEL_EXPORTER_OTLP_HEADERS on {name} container carries a literal 'value' field "
                f"({entry.get('value')!r}) — it must be Secret-sourced only (never a literal, ASVS V6)",
                file=sys.stderr,
            )
            overall_fail = True
            continue

        secret_ref = entry.get("valueFrom", {}).get("secretKeyRef")
        if not secret_ref:
            print(
                f"FAIL: OTEL_EXPORTER_OTLP_HEADERS on {name} container has no valueFrom.secretKeyRef "
                f"(entry: {entry!r})",
                file=sys.stderr,
            )
            overall_fail = True
            continue

        actual_name = secret_ref.get("name")
        actual_key = secret_ref.get("key")
        if actual_name != expected_name or actual_key != expected_key:
            print(
                f"FAIL: OTEL_EXPORTER_OTLP_HEADERS secretKeyRef mismatch on {name} container: "
                f"expected name={expected_name!r} key={expected_key!r}, "
                f"got name={actual_name!r} key={actual_key!r}",
                file=sys.stderr,
            )
            overall_fail = True
            continue

        print(
            f"PASS: OTEL_EXPORTER_OTLP_HEADERS on {name} container is Secret-sourced via "
            f"secretKeyRef(name={actual_name!r}, key={actual_key!r}) with no literal value (expected)"
        )

    return 1 if overall_fail else 0


if __name__ == "__main__":
    sys.exit(main())
