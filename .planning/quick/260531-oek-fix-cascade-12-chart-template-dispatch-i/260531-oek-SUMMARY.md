---
quick_id: 260531-oek
type: quick-summary
completed: 2026-05-31
phase: 07
plan: 12
subsystem: helm-chart
tags: [cascade-12, helm, image-tags, dispatch, acceptance]
key-files:
  modified:
    - charts/tide/templates/deployment.yaml
decisions:
  - "Dispatch image refs default to .Chart.AppVersion, matching the controller (line 77) and dashboard (line 39) — eliminates the :latest inconsistency that forced imagePullPolicy: Always."
metrics:
  tasks: 1
  files: 1
---

# Quick Task 260531-oek: Fix cascade-12 chart-template dispatch image tags Summary

Aligned the four dispatched-pod image refs in `charts/tide/templates/deployment.yaml` to default to `.Chart.AppVersion` instead of `"latest"`, so dispatched planner/executor pods request `:1.0.0` images (matching the controller/dashboard refs) rather than the unpublished `:latest` tag that forced `imagePullPolicy: Always` → ghcr 403 → `ImagePullBackOff` and stalled `make acceptance-v1-smoke`.

## Change

`charts/tide/templates/deployment.yaml`, exactly four lines, one token each (`| default "latest"` → `| default .Chart.AppVersion`):

- line 27 `--subagent-image` (`images.stubSubagent.tag`)
- line 28 `--credproxy-image` (`images.credProxy.tag`)
- line 41 `TIDE_PUSH_IMAGE` (`images.tidePush.tag`)
- line 43 `CLAUDE_SUBAGENT_IMAGE` (`images.claudeSubagent.tag`)

No other line touched. `values.yaml`, controller binary, and `hack/scripts/` untouched. Commit: `3edceb7` (1 file, 4 insertions / 4 deletions).

## Verification (exact output)

**1. `grep -c 'default "latest"' charts/tide/templates/deployment.yaml`**
```
0
```

**2. `helm template charts/tide 2>/dev/null | grep -E 'subagent-image=|credproxy-image=|TIDE_PUSH_IMAGE|CLAUDE_SUBAGENT_IMAGE'`** (env `value:` lines fetched with `-A1`)
```
          - --subagent-image=ghcr.io/jsquirrelz/tide-stub-subagent:1.0.0
          - --credproxy-image=ghcr.io/jsquirrelz/tide-credproxy:1.0.0
        - name: TIDE_PUSH_IMAGE
          value: "ghcr.io/jsquirrelz/tide-push:1.0.0"
        - name: CLAUDE_SUBAGENT_IMAGE
          value: "ghcr.io/jsquirrelz/tide-claude-subagent:1.0.0"
```
All four render at `:1.0.0` (chart appVersion), none at `:latest`.

**3. Helm-template Go unit tests** — `go test ./test/integration/kind/ -run 'TestHelm|TestThreeTaskWaveFixtureIncludesProjectsPVC|TestProjectsPVCYAML|TestSigningKeySecretYAML' -v`
```
--- PASS: TestThreeTaskWaveFixtureIncludesProjectsPVC (0.00s)
--- PASS: TestProjectsPVCYAMLBuildsNamespaceLocalRWOClaim (0.00s)
--- PASS: TestSigningKeySecretYAMLBuildsNamespaceLocalSecret (0.00s)
--- PASS: TestHelmControllerArgsUpgradeInstallReusesExistingRelease (0.00s)
--- PASS: TestHelmControllerArgsForcesManagerRollout (0.00s)
--- FAIL: TestHelmDeploymentTemplateRendersManagerPodAnnotations (0.00s)
```
The single failure is the KNOWN PRE-EXISTING `TestHelmDeploymentTemplateRendersManagerPodAnnotations`, whose failure message is:
```
projects_pvc_test.go:149: deployment template must render controllerManager.manager.podAnnotations
```
This is about `podAnnotations`, unrelated to image tags. **No NEW failures introduced** — all 5 other helm-template tests pass.

## Deviations from Plan

None — plan executed exactly as written.

## Self-Check: PASSED

- FOUND: charts/tide/templates/deployment.yaml (4 lines changed)
- FOUND: commit 3edceb7 on main
- `grep -c 'default "latest"'` = 0 (confirmed)
- All 4 dispatch refs render at `:1.0.0` (confirmed)
- No new test failures (only the documented pre-existing podAnnotations failure)
