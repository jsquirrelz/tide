#!/usr/bin/env bash
# Post-helmify augmentation for charts/tide/.
#
# Helmify regenerates charts/tide/ from kubebuilder's Kustomize output on every
# `make helm-controller` invocation, overwriting values.yaml, Chart.yaml, and
# deployment.yaml. This script applies the hand-maintained augmentations that
# helmify cannot infer:
#
#   1. Chart.yaml — TIDE-specific metadata (description, appVersion = 0.1.0-dev)
#   2. values.yaml — ghcr.io/jsquirrelz/* image refs + Phase 1 tunables
#      (plannerConcurrency, executorConcurrency, maxConcurrentReconciles,
#       leaderElection block)
#   3. templates/deployment.yaml — deduplicate the webhook container port
#      (helmify emits both `webhook` and `webhook-server` on 9443; keep the
#      latter since the helmified Service uses port 9443 and the standard
#      controller-runtime name is `webhook-server`)
#   4. templates/configmap.yaml — hand-authored ConfigMap that mounts at
#      /etc/tide/config.yaml (helmify cannot generate this because there is
#      no ConfigMap in the Kustomize base; the dev-loop deployment references
#      `tide-config` ConfigMap as optional)
#
# Idempotent: running this script multiple times produces the same output.
# Source-of-truth files live under hack/helm/, version-controlled separately
# from charts/tide/ so a clean `make helm` always produces the same result.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
HACK_DIR="${REPO_ROOT}/hack/helm"
CHART_DIR="${REPO_ROOT}/charts/tide"

# 1. Chart.yaml
cp "${HACK_DIR}/tide-chart.yaml" "${CHART_DIR}/Chart.yaml"

# 2. values.yaml
cp "${HACK_DIR}/tide-values.yaml" "${CHART_DIR}/values.yaml"

# 3. Deduplicate webhook port in deployment.yaml.
#    Pattern: helmify emits two consecutive port entries with containerPort: 9443
#    (one named `webhook`, one named `webhook-server`). Strip the `webhook` block.
DEPLOY="${CHART_DIR}/templates/deployment.yaml"
if [ -f "${DEPLOY}" ]; then
  # Use awk to remove the first 9443/webhook block while keeping webhook-server.
  awk '
    BEGIN { skip = 0 }
    # Match "- containerPort: 9443" lines and look ahead for the next line.
    /^[[:space:]]*- containerPort: 9443[[:space:]]*$/ && !skip {
      buf = $0
      getline next1
      getline next2
      # If next1 is "name: webhook" (NOT webhook-server), skip the 3-line block.
      if (next1 ~ /^[[:space:]]*name: webhook[[:space:]]*$/) {
        skip = 1
        next
      } else {
        # Emit the buffered + next lines unchanged.
        print buf
        print next1
        print next2
        next
      }
    }
    { print }
  ' "${DEPLOY}" > "${DEPLOY}.tmp"
  mv "${DEPLOY}.tmp" "${DEPLOY}"
fi

# 4. ConfigMap template (hand-authored — helmify cannot generate this).
cat > "${CHART_DIR}/templates/configmap.yaml" <<'EOF'
# Runtime config ConfigMap (Plan 11 / D-E1).
#
# Mounted at /etc/tide/config.yaml in the controller Deployment (see
# templates/deployment.yaml — volumes[].configMap.name: tide-config). The
# manager binary parses this file via internal/config.Load (CTRL-04 / Plan 04).
# Values are sourced from Helm values.yaml; the `optional: true` on the
# volume mount means the dev-loop deployment (kubectl apply -k config/default)
# falls back to internal/config built-in defaults when this ConfigMap is absent.
#
# The ConfigMap name matches the volume's configMap.name reference produced by
# helmify ("tide-config", not release-prefixed) so the helmify-emitted
# Deployment template and this hand-authored ConfigMap reference each other
# without an additional helmify rewrite step.
apiVersion: v1
kind: ConfigMap
metadata:
  name: tide-config
  labels:
    {{- include "tide.labels" . | nindent 4 }}
data:
  config.yaml: |
    plannerConcurrency: {{ .Values.plannerConcurrency | default 16 }}
    executorConcurrency: {{ .Values.executorConcurrency | default 4 }}
    maxConcurrentReconciles:
      project: {{ .Values.maxConcurrentReconciles.project | default 1 }}
      milestone: {{ .Values.maxConcurrentReconciles.milestone | default 1 }}
      phase: {{ .Values.maxConcurrentReconciles.phase | default 2 }}
      plan: {{ .Values.maxConcurrentReconciles.plan | default 4 }}
      wave: {{ .Values.maxConcurrentReconciles.wave | default 8 }}
      task: {{ .Values.maxConcurrentReconciles.task | default 16 }}
    leaderElection:
      enabled: {{ .Values.leaderElection.enabled | default true }}
EOF

echo "OK: charts/tide/ augmented with project-specific values + ConfigMap template"
