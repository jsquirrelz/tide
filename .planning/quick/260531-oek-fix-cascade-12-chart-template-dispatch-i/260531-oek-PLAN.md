---
quick_id: 260531-oek
type: quick-plan
created: 2026-05-31
description: "Fix cascade-12 — chart-template dispatch image tags default to \"latest\" instead of .Chart.AppVersion"
---

# Quick Task 260531-oek: Fix cascade-12 chart-template dispatch image tags

## Problem

`charts/tide/templates/deployment.yaml` resolves the four **dispatched-pod** image refs with `| default "latest"`. With CHART-01's empty (`tag: ""`) values, these render as `:latest` (not `.Chart.AppVersion`), so dispatched planner/executor pods request unpublished `ghcr.io/jsquirrelz/*:latest` — and because the tag is `:latest`, Kubernetes forces `imagePullPolicy: Always` → 403 from ghcr.io → `ImagePullBackOff` → the `$0` acceptance (`make acceptance-v1-smoke`) stalls at `Project=Running` and times out. The controller's own image (line 77) and the dashboard (line 39) already correctly use `| default .Chart.AppVersion`; these four lines are the inconsistency.

## Task 1: Align dispatch image tags with chart appVersion

- **files:** `charts/tide/templates/deployment.yaml`
- **action:** On exactly these four lines, change `| default "latest"` → `| default .Chart.AppVersion` (one-token change each, matching lines 39 and 77):
  - line 27 `--subagent-image` (`images.stubSubagent.tag`)
  - line 28 `--credproxy-image` (`images.credProxy.tag`)
  - line 41 `TIDE_PUSH_IMAGE` (`images.tidePush.tag`)
  - line 43 `CLAUDE_SUBAGENT_IMAGE` (`images.claudeSubagent.tag`)
  Do NOT touch any other line, `values.yaml`, the controller binary, or `hack/scripts/`.
- **verify:**
  - `helm template charts/tide | grep -E 'subagent-image|credproxy-image|TIDE_PUSH_IMAGE|CLAUDE_SUBAGENT_IMAGE'` → each renders with `:1.0.0` (the appVersion), NOT `:latest`.
  - `grep -c 'default "latest"' charts/tide/templates/deployment.yaml` → `0`.
  - Run chart/helm-template Go unit tests (e.g. `go test ./... -run HelmDeployment` or the package containing the helm-template tests) → no NEW failures vs the unmodified tree. NOTE: `TestHelmDeploymentTemplateRendersManagerPodAnnotations` is a KNOWN PRE-EXISTING failure unrelated to image tags — do not fix it here; only confirm no additional failures are introduced.
- **done:** All 4 dispatch image refs render at appVersion `1.0.0`; zero `default "latest"` remain in the template; no new test failures.

## must_haves

- truths:
  - The four dispatch image refs in deployment.yaml resolve to `.Chart.AppVersion`, not `"latest"`.
- artifacts:
  - `charts/tide/templates/deployment.yaml` (4 lines changed)
- key_links:
  - charts/tide/templates/deployment.yaml:27
  - charts/tide/templates/deployment.yaml:28
  - charts/tide/templates/deployment.yaml:41
  - charts/tide/templates/deployment.yaml:43
