# GSD Debug Knowledge Base

Resolved debug sessions. Used by `gsd-debugger` to surface known-pattern hypotheses at the start of new investigations.

---

## rc1-dryrun-small-project-stall — v1.0.7-rc.1 dry-run gate: small-project never reaches Complete (envelope apiVersion skew from stale sample image pin)
- **Date:** 2026-07-15
- **Error patterns:** dry-run timeout, kubectl wait timed out waiting for the condition, projects/small-project, status.phase=Complete never reached, planner Job Failed, zero child CRs, stub-subagent exit 2, envelope load validate, unknown apiVersion, dispatch.tideproject.k8s/v1alpha1, expected tideproject.k8s/v1alpha1, stale image pin, version skew, spec.subagent.image, MAKE_EXIT=2
- **Root cause:** Plan 40-02 (D-08, commit dfb8d58) changed the envelope contract apiVersion group from 'tideproject.k8s/v1alpha1' to 'dispatch.tideproject.k8s/v1alpha1' with strict-equality validation and no dual-accept. examples/projects/small/project.yaml pins spec.subagent.image to the published ghcr.io/jsquirrelz/tide-stub-subagent:1.0.0 (never bumped since Phase 5), and spec.subagent.image is a live override — so the v1.0.7-rc.1 dry-run dispatched a stub compiled with the OLD group against a manager writing the NEW group. Every subagent dispatch exits 2 at envelope validation; the project-level planner Job fails through its backoff; no child CRs are ever created; the Project never reaches Complete and the 10m kubectl wait times out. Kind Layer B never sees this because the suite injects the tree-built stub via subagent.defaults.image=:test.
- **Fix:** Matched-pair restore + rot guard (commit 4712437): bumped stub/claude-subagent pins in examples/projects/{small,medium,large}/ from 1.0.0 to 1.0.7 (must-equal-appVersion invariant documented inline), updated small/README.md pull instructions, and added TestExamplesSubagentImagePinsMatchChartAppVersion (test/integration/kind/examples_image_pin_test.go) asserting every versioned tide-{stub,claude}-subagent tag under examples/projects/ equals charts/tide/Chart.yaml appVersion. Deliberately no dual-accept of the old envelope group (Phase 40 clean-break posture). Deferred /gsd:quick follow-ups: stale `chartVersions: 1.0.1` in dry-run-report.json; dry-run-v1.sh captures zero failure diagnostics at timeout.
- **Files changed:** examples/projects/small/project.yaml, examples/projects/medium/project.yaml, examples/projects/large/project.yaml, examples/projects/small/README.md, test/integration/kind/examples_image_pin_test.go
---
