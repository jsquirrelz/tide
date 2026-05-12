#!/usr/bin/env bash
# Post-helmify augmentation for charts/tide-crds/.
#
# Helmify regenerates charts/tide-crds/ from kubebuilder's config/crd/ Kustomize
# output on every `make helm-crds` invocation. This script applies the
# project-specific Chart.yaml metadata (TIDE description, appVersion = 0.1.0-dev)
# that helmify cannot infer.
#
# Idempotent: running this script multiple times produces the same output.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
HACK_DIR="${REPO_ROOT}/hack/helm"
CHART_DIR="${REPO_ROOT}/charts/tide-crds"

cp "${HACK_DIR}/tide-crds-chart.yaml" "${CHART_DIR}/Chart.yaml"

echo "OK: charts/tide-crds/ augmented with TIDE Chart.yaml metadata"
