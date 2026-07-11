---
phase: 38-small-independents-pricing-accuracy-promptfile-telemetry-nud
plan: 04
subsystem: helm-chart
tags: [telemetry, helm, notes-txt, prometheus, install-docs]
requires: []
provides:
  - "prometheus.enabled umbrella key (default false) in both values files"
  - "PROMETHEUS_ENABLED always-rendered env on the dashboard container (plan 38-05 consumes)"
  - "charts/tide/templates/NOTES.txt with conditional telemetry warning (TELEM-02)"
  - "assert-telemetry-render.sh permutations E/F/G"
  - "docs/INSTALL.md 'Enable telemetry (Prometheus)' walkthrough (TELEM-01)"
affects: [38-05]
tech-stack:
  added: []
  patterns:
    - "augment-tide-chart.sh heredoc ownership for net-new chart templates (Pitfall 3)"
key-files:
  created:
    - charts/tide/templates/NOTES.txt
  modified:
    - hack/helm/tide-values.yaml
    - charts/tide/values.yaml
    - hack/helm/augment-tide-chart.sh
    - charts/tide/templates/dashboard-deployment.yaml
    - hack/helm/assert-telemetry-render.sh
    - docs/INSTALL.md
decisions:
  - "PROMETHEUS_ENABLED is ALWAYS rendered (value = quoted prometheus.enabled); explicit \"false\" = disabled-by-config, unset var = legacy chart (binary falls back to PROM_ENDPOINT presence)"
  - "NOTES.txt [G] assertion codifies helm install --dry-run=client (helm v4.2.0's --show-only does not emit NOTES.txt)"
  - "INSTALL.md walkthrough orders the TIDE upgrade before the ServiceMonitor label fix — the ServiceMonitor only exists after serviceMonitor.enabled=true"
metrics:
  duration: "~9 min"
  completed: "2026-07-11"
status: complete
---

# Phase 38 Plan 04: Chart-surface telemetry nudge + INSTALL.md walkthrough Summary

prometheus.enabled umbrella key with always-rendered PROMETHEUS_ENABLED dashboard env, net-new NOTES.txt warning gated on the key, and a kube-prometheus-stack INSTALL.md walkthrough ending at the Targets page — all dual-sourced, no chart version bump (D-13).

## Contract for plan 38-05 (PROMETHEUS_ENABLED env)

- **Env name:** `PROMETHEUS_ENABLED` on the `dashboard` container (charts/tide/templates/dashboard-deployment.yaml, immediately after the PROM_ENDPOINT conditional block).
- **Value semantics:** always rendered, `{{ quote .Values.prometheus.enabled }}` — literal string `"false"` (chart default) or `"true"`. An explicit `"false"` means disabled-by-config (banner signal). A legacy chart lacking the key leaves the var **unset** — the binary must fall back to PROM_ENDPOINT presence in that case.
- `prometheus.endpoint` (PROM_ENDPOINT, conditional) and `prometheus.serviceMonitor.enabled` (scrape gate, default stays false) keep their existing roles; the umbrella key does not replace either.

## Task Commits

| Task | Name | Commit | Files |
| ---- | ---- | ------ | ----- |
| 1 | prometheus.enabled key, PROMETHEUS_ENABLED env, configmap default, pricing comment | 0a4a9ae | hack/helm/tide-values.yaml, charts/tide/values.yaml, charts/tide/templates/dashboard-deployment.yaml |
| 2 | NOTES.txt (dual-source) + render permutations E/F/G | c0a89f4 | hack/helm/augment-tide-chart.sh, charts/tide/templates/NOTES.txt, hack/helm/assert-telemetry-render.sh |
| 3 | INSTALL.md enable-telemetry walkthrough | 5b32224 | docs/INSTALL.md |

## What landed

- **prometheus.enabled: false** added as the first key of the `prometheus:` block in `hack/helm/tide-values.yaml`, mirrored byte-identically into `charts/tide/values.yaml` via `augment-tide-chart.sh` (verified: `diff` empty). Comment spells out the three-key story (enabled / serviceMonitor.enabled / endpoint).
- **pricing.overrides comment refresh (D-03):** names the primary use case — new Claude model rows without an image rebuild; overrides merge at manager startup and reach subagents via `TIDE_PRICING_OVERRIDES_JSON` (plumbing landed Phase 14).
- **NOTES.txt (TELEM-02/D-12):** install summary + dashboard port-forward (verified against the real dashboard Service: `{{ include "tide.fullname" . }}-dashboard`, port 80) + docs pointer + warning ("run telemetry beyond the budget tally is unavailable") gated on `{{- if not .Values.prometheus.enabled }}`. Owned by an `augment-tide-chart.sh` step-4b heredoc (hash-verified idempotent) so future augment runs regenerate rather than delete it.
- **Render gate extended to 7 permutations:** E (default `PROMETHEUS_ENABLED` value `"false"`), F (`--set prometheus.enabled=true` → `"true"`), G (NOTES warning present by default / absent when enabled, via `helm install --dry-run=client`). EC-7 permutations A–D intact; `helm lint` green.
- **INSTALL.md `### Enable telemetry (Prometheus)`** between "Verifying the install" and "Provider Secret": kps install, three-flag `helm upgrade` (using the doc's real OCI path `ghcr.io/jsquirrelz/tide-charts/tide`), the `release:` label fix (`kubectl -n tide-system label servicemonitor -l control-plane=controller-manager release=kps`) with the `serviceMonitorSelectorNilUsesHelmValues=false` one-line alternative, Targets-page UP as the explicit done signal, and the existing-Prometheus variant note.

## Deviations from Plan

### Auto-fixed / adjusted

**1. [Rule 1 - already-fixed] DEBT-02 configmap `default 16` remnant was already fixed**
- **Found during:** Task 1 read_first
- **Issue:** Plan step 3 directed changing `augment-tide-chart.sh:90` from `default 16` to `default 4`, but commit `5da4df6` (Phase 34 PREFLIGHT-01) already landed exactly that fix in both the script and `charts/tide/templates/configmap.yaml`.
- **Fix:** No change needed; verified all acceptance criteria hold (grep == 1 in both files, rendered `plannerConcurrency: 4` == 1).
- **Files modified:** none

**2. [Rule 3 - Blocking] Restored exec bit on assert-telemetry-render.sh**
- **Found during:** Task 1 verify
- **Issue:** The script was tracked as mode 100644 — the plan's `./hack/helm/assert-telemetry-render.sh` verify invocation failed with permission denied.
- **Fix:** `chmod +x`; mode change (100644 → 100755) included in the Task 2 commit that also edits the script.
- **Commit:** c0a89f4

**3. [Rule 1 - Bug] INSTALL.md step order: upgrade before label**
- **Found during:** Task 3 authoring
- **Issue:** Plan's content order put the ServiceMonitor label fix before enabling `serviceMonitor.enabled=true`, but the ServiceMonitor doesn't exist until that flag is set — the label command would fail for an operator following top-to-bottom.
- **Fix:** Walkthrough orders: kps install → TIDE three-flag upgrade → label fix → Targets verification. All plan-mandated content items present.
- **Commit:** 5b32224

### Notes

- **RESEARCH A1 caveat:** the kube-prometheus-stack `release:` label selection claim was not re-verified against the live chart README (no network fetch performed); per the plan's contingency, BOTH fixes (label + `serviceMonitorSelectorNilUsesHelmValues=false`) are documented so either selector behavior is covered.
- Helm v4.2.0 on the host: all render assertions pass; no Helm-3 divergence observed in template output.

## Known Stubs

The dashboard binary does not yet read `PROMETHEUS_ENABLED` — that is plan 38-05's scope (D-14 binary side). This plan deliberately ships only the chart side per the phase's wave split; the env contract above is the handoff.

## TDD Gate Compliance

Not a TDD plan (`type: execute`); contract coverage is the extended render gate (permutations E/F/G).

## Self-Check: PASSED

- charts/tide/templates/NOTES.txt: FOUND
- docs/INSTALL.md section: FOUND (heading line 175, between 158 and 217)
- Commits 0a4a9ae, c0a89f4, 5b32224: FOUND in git log
- `./hack/helm/assert-telemetry-render.sh` exit 0 (7/7 permutations)
- `diff hack/helm/tide-values.yaml charts/tide/values.yaml` empty
- Chart.yaml / tide-chart.yaml untouched across all commits (D-13)
