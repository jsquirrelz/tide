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

# Phase 2 additions (Plan 12):
# 5. signing-secret.yaml — HMAC signing key auto-generated on first install via Helm
#    lookup + resource-policy: keep (D-C3 / Blocker #1 fix). Data key TIDE_SIGNING_KEY
#    matches the env var name so envFrom: [{secretRef: ...}] auto-populates it on the
#    Manager Deployment and credproxy sidecar (T-02-12-02/T-02-12-03 mitigations).
cp "${HACK_DIR}/signing-secret.yaml" "${CHART_DIR}/templates/signing-secret.yaml"

# 6. serviceaccount-subagent.yaml — NEW template for the tide-subagent ServiceAccount
#    (Warning #9 fix — separate file from Phase 1's serviceaccount.yaml which is NOT
#    modified). Zero RoleBindings on tide-subagent SA per D-A4 / T-02-12-04.
cp "${HACK_DIR}/serviceaccount-subagent.yaml" "${CHART_DIR}/templates/serviceaccount-subagent.yaml"

# 6a. push-rbac.yaml — NEW template for the tide-push ServiceAccount + Role +
#     RoleBinding (Phase 3 plan 03-09 / D-B1 / T-304 mitigation). Dedicated SA,
#     distinct from tide-subagent, scoped to `secrets get` only in the controller
#     namespace. Documents cross-namespace caveat in its own comment block.
cp "${HACK_DIR}/push-rbac.yaml" "${CHART_DIR}/templates/push-rbac.yaml"

# 7. projects-pvc.yaml — Single shared tide-projects ReadWriteMany PVC (Blocker #2/#3
#    fix — single-shared-PVC + subPath architecture, RESEARCH.md OQ#2 RESOLVED).
#    resource-policy: keep preserves in-flight workspace state across helm uninstall.
cp "${HACK_DIR}/projects-pvc.yaml" "${CHART_DIR}/templates/projects-pvc.yaml"

# 7a. per-namespace-rolebinding.yaml — Phase 5 D-X4 / AUTH-02 catch-up. Helm template
#     ranges .Values.projectNamespaces; emits one RoleBinding per listed namespace
#     binding the controller-manager SA (in .Release.Namespace) to the consolidated
#     manager-role ClusterRole shipped from Phase 1. Empty default → zero RoleBindings
#     emitted; opt-in for multi-Project installs.
cp "${HACK_DIR}/per-namespace-rolebinding.yaml" "${CHART_DIR}/templates/per-namespace-rolebinding.yaml"

# 8. Phase 2 Deployment augmentation: envFrom (TIDE_SIGNING_KEY secret), Phase 2 CLI
#    args (--subagent-image, --credproxy-image, --default-file-touch-mode,
#    --rate-limit-default-rpm, --rate-limit-default-burst), and the tide-projects PVC
#    volume + volumeMount at /workspaces (no subPath).
#    Idempotent: Python checks for the presence of the Phase 2 markers before inserting.
if [ -f "${DEPLOY}" ]; then
  python3 - "${DEPLOY}" <<'PYEOF'
import sys, re

path = sys.argv[1]
with open(path) as f:
    content = f.read()

# ── 8a: envFrom block ────────────────────────────────────────────────────────
# Insert after the last `env:` list item in the manager container, just before
# the `image:` line. Only if not already present.
ENVFROM_MARKER = "envFrom:"
ENVFROM_BLOCK = """        envFrom:
        - secretRef:
            name: {{ .Values.signingKey.secretName | default "tide-signing-key" }}
"""
if ENVFROM_MARKER not in content:
    # Insert envFrom just before the `image:` line (first occurrence after `env:`).
    content = re.sub(
        r'(\n        image: )',
        '\n' + ENVFROM_BLOCK.rstrip('\n') + r'\1',
        content,
        count=1,
    )

# ── 8b: Phase 2 CLI args ─────────────────────────────────────────────────────
# Replace the helmify-generated `args: {{- toYaml ... }}` one-liner with a fully
# templated args block that appends Phase 2 flags after the Phase 1 args list.
# This avoids YAML structure issues from appending list items after a toYaml block.
ARGS_MARKER = "# phase2-args-injected"
PHASE2_ARGS_REPLACEMENT = """args:
          {{- toYaml .Values.controllerManager.manager.args | nindent 10 }}
          - --subagent-image={{ .Values.images.stubSubagent.repository }}:{{ .Values.images.stubSubagent.tag | default .Chart.AppVersion }}
          - --credproxy-image={{ .Values.images.credProxy.repository }}:{{ .Values.images.credProxy.tag | default .Chart.AppVersion }}
          - --default-file-touch-mode={{ .Values.planAdmission.fileTouchMode | default "warn" }}
          - --rate-limit-default-rpm={{ .Values.rateLimits.defaults.requestsPerMinute | default 60 }}
          - --rate-limit-default-burst={{ .Values.rateLimits.defaults.burst | default 10 }}
          # phase2-args-injected"""
if ARGS_MARKER not in content:
    content = re.sub(
        r'args: \{\{- toYaml \.Values\.controllerManager\.manager\.args \| nindent 8 \}\}',
        PHASE2_ARGS_REPLACEMENT,
        content,
        count=1,
    )

# ── 8c: tide-projects volumeMount on manager container ───────────────────────
# Insert inside the existing volumeMounts block, after the last listed mount.
VMOUNT_MARKER = "# phase2-vmount-injected"
VMOUNT_BLOCK = """        - mountPath: /workspaces
          name: tide-projects
          # phase2-vmount-injected
"""
if VMOUNT_MARKER not in content:
    # Insert after the last volumeMounts entry (after the webhook-certs mount line).
    content = re.sub(
        r'(        - mountPath: /tmp/k8s-webhook-server/serving-certs\n          name: webhook-certs\n          readOnly: true\n)',
        r'\1' + VMOUNT_BLOCK,
        content,
        count=1,
    )

# ── 8d: tide-projects volume sourced from PVC ────────────────────────────────
VOLUME_MARKER = "# phase2-volume-injected"
VOLUME_BLOCK = """      - name: tide-projects
        persistentVolumeClaim:
          claimName: {{ .Values.workspaces.pvc.name | default "tide-projects" }}
      # phase2-volume-injected
"""
if VOLUME_MARKER not in content:
    # Append after the last existing volume entry (after webhook-certs secret volume).
    content = re.sub(
        r'(\n      - name: webhook-certs\n        secret:\n          secretName: webhook-server-cert\n)',
        r'\1' + VOLUME_BLOCK,
        content,
        count=1,
    )

# ── 8e: Phase 3 plan 03-09 env-var injection ────────────────────────────────
# Adds TIDE_PUSH_IMAGE, CLAUDE_SUBAGENT_IMAGE, TIDE_DEFAULT_MODEL_*, and
# TIDE_LEADER_*_SECONDS env vars on the manager container. Read by
# cmd/manager/main.go via env.go helpers (D-C4 per-level models, D-D1
# leader-election tuning, D-B1 push image wiring).
ENV3_MARKER = "# phase3-env-injected"
ENV3_BLOCK = """        - name: TIDE_PUSH_IMAGE
          value: "{{ .Values.images.tidePush.repository }}:{{ .Values.images.tidePush.tag | default .Chart.AppVersion }}"
        - name: CLAUDE_SUBAGENT_IMAGE
          value: "{{ .Values.images.claudeSubagent.repository }}:{{ .Values.images.claudeSubagent.tag | default .Chart.AppVersion }}"
        - name: TIDE_DEFAULT_MODEL_MILESTONE
          value: "{{ .Values.subagent.levels.milestone.model | default \"claude-opus-4-7\" }}"
        - name: TIDE_DEFAULT_MODEL_PHASE
          value: "{{ .Values.subagent.levels.phase.model | default \"claude-sonnet-4-6\" }}"
        - name: TIDE_DEFAULT_MODEL_PLAN
          value: "{{ .Values.subagent.levels.plan.model | default \"claude-sonnet-4-6\" }}"
        - name: TIDE_DEFAULT_MODEL_TASK
          value: "{{ .Values.subagent.levels.task.model | default \"claude-haiku-4-5\" }}"
        - name: TIDE_LEADER_LEASE_SECONDS
          value: "{{ .Values.leaderElection.leaseDurationSeconds | default 15 }}"
        - name: TIDE_LEADER_RENEW_SECONDS
          value: "{{ .Values.leaderElection.renewDeadlineSeconds | default 10 }}"
        - name: TIDE_LEADER_RETRY_SECONDS
          value: "{{ .Values.leaderElection.retryPeriodSeconds | default 2 }}"
        # phase3-env-injected
"""
if ENV3_MARKER not in content:
    # Insert just before `envFrom:` on the manager container (the same anchor
    # used by the Phase 2 envFrom block). Pattern: lines containing exactly
    # 8-space-indented `envFrom:`.
    content = re.sub(
        r'(\n        envFrom:\n)',
        '\n' + ENV3_BLOCK + r'\1',
        content,
        count=1,
    )

# ── 8f: Phase 4 plan 04-14 — OTel env-var injection ─────────────────────────
# Adds OTEL_EXPORTER_OTLP_ENDPOINT, OTEL_TRACES_SAMPLER, OTEL_TRACES_SAMPLER_ARG,
# OTEL_SERVICE_NAME on the manager container. Read by internal/otelinit at boot
# (plan 04-03). Empty endpoint → no-op TracerProvider (zero overhead, default
# posture). Sampler is env-driven (Pitfall 24 mitigation; no WithSampler in code).
ENV4_MARKER = "# phase4-env-injected"
ENV4_BLOCK = """        # Phase 4 plan 04-14 (D-O3): OTel env vars read by internal/otelinit.
        # Empty OTEL_EXPORTER_OTLP_ENDPOINT → no-op TracerProvider (zero
        # overhead, default posture for plain clusters). Sampler is env-driven
        # to honor Pitfall 24 mitigation (no WithSampler in code).
        - name: OTEL_EXPORTER_OTLP_ENDPOINT
          value: {{ quote .Values.otel.exporter.endpoint }}
        - name: OTEL_TRACES_SAMPLER
          value: {{ quote .Values.otel.tracesSampler }}
        - name: OTEL_TRACES_SAMPLER_ARG
          value: {{ quote .Values.otel.tracesSamplerArg }}
        - name: OTEL_SERVICE_NAME
          value: {{ quote .Values.otel.serviceName }}
        # phase4-env-injected
"""
if ENV4_MARKER not in content:
    # Insert immediately after the Phase 3 marker line so OTel vars sit
    # alongside the existing TIDE_* env block. Anchor: the literal phase3
    # marker line (8-space indent + `# phase3-env-injected`).
    content = re.sub(
        r'(        # phase3-env-injected\n)',
        r'\1' + ENV4_BLOCK,
        content,
        count=1,
    )

# ── 8g: podAnnotations passthrough on the manager pod template ───────────────
# helmify emits only the static `kubectl.kubernetes.io/default-container: manager`
# annotation on the pod template and drops any operator-supplied podAnnotations.
# Inject a `{{- with .Values.controllerManager.manager.podAnnotations }}` block
# immediately after that line so Helm renders controllerManager.manager.podAnnotations
# (e.g. the controller-args hash used to force a manager rollout on chart changes).
# Idempotent: keyed on the podAnnotations Values path already being present.
PODANN_MARKER = "with .Values.controllerManager.manager.podAnnotations"
PODANN_BLOCK = """        {{- with .Values.controllerManager.manager.podAnnotations }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
"""
if PODANN_MARKER not in content:
    # Anchor: the static default-container annotation line (8-space indent).
    content = re.sub(
        r'(        kubectl\.kubernetes\.io/default-container: manager\n)',
        r'\1' + PODANN_BLOCK,
        content,
        count=1,
    )

with open(path, 'w') as f:
    f.write(content)

print("OK: deployment.yaml Phase 2 + Phase 3 + Phase 4 fields injected (idempotent)")
PYEOF
fi

echo "OK: charts/tide/ augmented with project-specific values + ConfigMap template"
