#!/usr/bin/env bash
# Post-helmify augmentation for charts/tide-crds/.
#
# Helmify regenerates charts/tide-crds/ from kubebuilder's config/crd/ Kustomize
# output on every `make helm-crds` invocation. This script applies the
# project-specific Chart.yaml metadata (TIDE description, appVersion = 0.1.0-dev)
# that helmify cannot infer, AND it injects the canonical Helm CRD-preservation
# annotation (`helm.sh/resource-policy: keep`) into every CRD template's
# metadata.annotations block.
#
# Why the resource-policy annotation matters (Phase 05 RESEARCH Topic 8 / Pitfall 2):
# Without `helm.sh/resource-policy: keep` on each CRD, `helm uninstall tide-crds`
# would cascade-delete the 6 CRDs, which in turn would cascade-delete every
# Project/Milestone/Phase/Plan/Task/Wave custom resource in the cluster —
# catastrophic data loss for any operator who uninstalls + reinstalls. The
# annotation is Helm's canonical opt-out for that cascade.
#
# Idempotent: running this script multiple times produces the same output. The
# annotation injection is gated by a `grep -q` pre-check so re-runs do not
# accumulate duplicate annotation lines.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
HACK_DIR="${REPO_ROOT}/hack/helm"
CHART_DIR="${REPO_ROOT}/charts/tide-crds"

cp "${HACK_DIR}/tide-crds-chart.yaml" "${CHART_DIR}/Chart.yaml"

# Inject `helm.sh/resource-policy: keep` into the metadata.annotations block of
# each CRD template. The annotation is placed immediately after the existing
# `controller-gen.kubebuilder.io/version:` line, preserving the 4-space indent
# helmify emits. Idempotent: the grep pre-check skips files that already carry
# the annotation, so re-running this script does not duplicate lines.
injected_count=0
for crd in "${CHART_DIR}"/templates/*-crd.yaml; do
  if grep -q 'helm.sh/resource-policy: keep' "${crd}"; then
    continue
  fi
  awk '
    /^    controller-gen\.kubebuilder\.io\/version:/ {
      print
      print "    helm.sh/resource-policy: keep"
      next
    }
    { print }
  ' "${crd}" > "${crd}.tmp"
  mv "${crd}.tmp" "${crd}"
  injected_count=$((injected_count + 1))
done

echo "OK: charts/tide-crds/ augmented with TIDE Chart.yaml metadata"
echo "OK: charts/tide-crds/ augmented with helm.sh/resource-policy: keep on ${injected_count} CRD(s) (already-present skipped)"
