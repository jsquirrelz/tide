# Phase Brief: phase-05-chart-secret-wiring

Milestone: milestone-03-hetero-integration
Wave position: Milestone wave 2, depends on phase-04-per-level-vendor-switch

Objective

Wire the Codex image default, the Codex provider-Secret reference, and the
OPENAI_API_KEY env-injection into dispatch Job pods. Phase-04 delivers the
per-level vendor switch in the controller; this phase delivers the Helm chart
surface and controller-side secret injection that make the vendor selection
executable end-to-end. Set Levels.task.Vendor=openai plus
CodexProviderSecretRef=my-secret in a Project CR and TIDE dispatches Codex
Jobs with OPENAI_API_KEY populated -- no credproxy, no host config, no OAuth.

Scope

In scope:
- Add images.codexSubagent block to charts/tide/values.yaml.
- Add CODEX_SUBAGENT_IMAGE env to charts/tide/templates/deployment.yaml.
- Extend ProviderDefaults.CodexImage in dispatch_helpers.go; vendor-aware resolveImage.
- Populate CodexImage from CODEX_SUBAGENT_IMAGE in cmd/manager/env.go.
- Add CodexProviderSecretRef string to ProjectSpec; regenerate deepcopy.
- Inject OPENAI_API_KEY from CodexProviderSecretRef in all four Job-creating
  controller reconcilers when ResolveProvider returns Vendor:openai.
- Update dogfood example with codexProviderSecretRef: openai-secrets.

Out of scope: codex subagent package (phase-02), container image (phase-03),
vendor switch in ResolveProvider (phase-04), integration test (phase-06).

Verification Gates

1. make test exits 0.
2. make verify-import-firewall + make verify-dispatch-imports green.
3. helm template renders CODEX_SUBAGENT_IMAGE; tag override passes through.
4. Unit: resolveImage returns CodexImage when vendor=openai; Image when anthropic.
5. Unit: Job pod has envFrom secretRef when vendor=openai and secret ref set.
6. make generate exits 0; deepcopy regenerated.
7. No OpenAI API call in tests without OPENAI_API_KEY env guard.
8. No secret material inlined in any manifest or chart value.
