# TIDE Live Nightly E2E (TEST-03)

**Audience:** TIDE operators wiring a nightly CI job that exercises the full Phase 3 integration against the real Anthropic API.

**Status:** v1.0 ships the live spec under `test/e2e/live_claude_test.go` behind a `live_e2e` Go build tag (`REQUIREMENTS.md` TEST-03 deliverable from Phase 3 plan 03-11). The spec is skipped by default — `make test` and `make test-int` never invoke it. Operators wire it into a nightly cron via `make test-e2e-live` with `ANTHROPIC_API_KEY` injected from a CI secret store.

**Scope of this doc:**

- What the live E2E test does and why it's gated.
- The double-gate pattern (build tag + env var) that prevents accidental cost.
- A template GitHub Actions workflow for nightly CI (operators adapt to whatever CI they run).
- The fixture-repo pinning protocol.
- The $1.00 budget cap rationale.
- The expected cost baseline per run.
- Troubleshooting common failure modes.

## Overview

`TEST-03` (see `.planning/REQUIREMENTS.md`) requires a live nightly E2E that exercises TIDE's full Phase 3 integration with the real `tide-claude-subagent` image (Phase 3 plan 03-07) against the real Anthropic API. The test is the ONLY Phase 3 code surface that calls a paid LLM endpoint; every other test (`test`, `test-int`, `test-e2e`) uses the stub-subagent or envtest-only fakes and is cost-free.

The design rationale is captured in `.planning/phases/03-up-stack-reconcilers-git-integration-real-subagent-resumptio/03-CONTEXT.md` `<specifics>` #5:

> Live E2E nightly test against real Claude on real fixture repo — Phase 3 REQ-TEST-03. The plan-phase researcher should treat TEST-03 as a separate plan from chaos-resume; live cost is bounded by Phase 2 budget cap infrastructure but the test fixture (which fixture repo? Snapshotted at which SHA?) is a separable design surface.

Plan 03-11 is that separable design surface — fixture repo + spec + Makefile target + this doc.

The spec applies a single `Project` CRD pointing at a small fixture repo, watches the orchestrator dispatch a milestone planner Job, waits for the Claude subagent to emit a `MILESTONE.md` artifact and a structured `Milestone` child CRD, verifies the push Job lands a `tide: milestone <name> authored` commit on the per-run branch (Phase 3 D-B2 #3), and asserts the cost-bounded behavior via Phase 2's `Project.Status.budget` rollup.

Success looks like: `Project.Status.phase=Running`, `Project.Status.git.lastPushedSHA` non-empty, `Project.Status.budget.costSpentCents` in the open interval `(0, 100)` cents (real Claude call happened AND the budget gate held).

## Build Tag + Skip-on-missing-creds

Two gates protect against accidental live API calls during normal development:

1. **Go build tag `//go:build live_e2e`.** The first line of `test/e2e/live_claude_test.go` carries this build constraint. Without `-tags=live_e2e` the file is excluded from compilation; `go test`, `make test`, and `make test-int` never trigger it. (The tag uses an underscore rather than a hyphen because Go's build-constraint grammar requires identifier-shaped tags. The Makefile target keeps the operator-friendly hyphenated form `test-e2e-live`.)

2. **`ANTHROPIC_API_KEY` env-var check in `BeforeSuite`.** Even when the build tag is set, the Ginkgo BeforeSuite calls `Skip("ANTHROPIC_API_KEY not set — skipping live E2E")` if the env var is empty. The whole suite reports `Ran 0 of N Specs … 1 Skipped`.

3. **Makefile `test-e2e-live` fail-fast.** The Makefile target exits with code 1 BEFORE invoking `go test` if `ANTHROPIC_API_KEY` is empty — defense in depth against a CI that accidentally configured the build tag but forgot to wire the secret.

These three gates compose: a developer or operator must explicitly opt into all three to make a real API call. There is no path from `make test` to a paid request.

## Nightly CI Recipe

Template GitHub Actions workflow (`.github/workflows/live-e2e.yml`) — operators copy and adapt for whatever CI they use:

```yaml
name: Live Claude E2E (nightly)
on:
  schedule:
    # 06:00 UTC every day — adjust to your team's cost-monitoring window.
    - cron: '0 6 * * *'
  workflow_dispatch: {}  # allow manual triggering for debug runs

jobs:
  live-e2e:
    runs-on: ubuntu-latest
    timeout-minutes: 25  # 15m test + 5m kind setup + 5m slack
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
      - uses: helm/kind-action@v1
        with:
          version: v0.31.0
          cluster_name: tide-live-e2e
      - name: Build + load TIDE images
        run: make test-int-kind-prep
      - name: Install TIDE controller
        run: |
          helm install tide ./charts/tide --create-namespace -n tide-system \
            --wait --timeout 5m
      - name: Run live E2E spec
        env:
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
        run: make test-e2e-live
      - name: Capture controller logs on failure
        if: failure()
        run: kubectl logs -n tide-system deployment/tide-controller-manager --tail=500
```

**Alerting:** wire the workflow's failure notification to fire on two consecutive nightly failures (not the first one — a transient API blip should not page the on-call). GitHub Actions doesn't expose this natively; operators typically pipe failures into a tracker (Slack, PagerDuty) that aggregates by job name + dedupes on the same-day successful run. See `T-314` mitigation in the plan's `<threat_model>`.

**Cost capture:** Anthropic billing tags requests by API key; allocate a dedicated `tide-nightly-e2e` API key (not the dev or production key) so daily spend on this workflow is visible in the Anthropic console. The spec also asserts `Project.Status.budget.costSpentCents` and fails if it leaves the `(0, 100)` window — runaway spend is caught in-band.

## Fixture Repo Pinning

The fixture YAML (`test/e2e/testdata/live-claude-project.yaml`) declares a `Project.Spec.targetRepo` URL pointing at a small fixture repo. Two approaches:

1. **External GitHub repo pinned by tag/SHA (current default).** The fixture YAML references `https://github.com/jsquirrelz/tide-live-e2e-fixture.git`. The repo is a public clone of a minimal Go template (~200 LOC, single `cmd/hello/main.go` + `go.mod` + `README.md`), tagged at a fixed commit SHA so the milestone planner's input is byte-stable across runs. Tag the SHA on the fixture repo side; document the SHA here and in the fixture YAML's comment header.

2. **In-cluster tarball-extracted repo (alternative).** Ship a `test/e2e/testdata/fixture-repo.tar` in this repo, extract it into an in-cluster volume at test setup time, and serve it via a tiny in-cluster git daemon (e.g., `gitea` running as a Pod on the same kind cluster). This makes the test deterministic across network outages and eliminates the external-dependency surface. Trade-off: more cluster setup, larger test image, and the fixture content lives in this repo's git history (cleaner, but every fixture rotation is a TIDE PR).

**Default decision:** approach #1 for v1.0 (operational simplicity). The fixture repo is small enough that GitHub rate limits are not a concern. If the external repo becomes unavailable in nightly CI (rare — GitHub uptime is high enough that two consecutive failures from this cause would be visible), operators can fall back to approach #2 by checking in the tarball and updating the fixture YAML's `targetRepo` to a `file://` URL.

**Rotation policy:** refresh the fixture (bump the pinned SHA, regenerate the tarball) every 3-6 months OR when a fixture-repo SHA bug surfaces in nightly runs (whichever first). When the fixture rotates, also bump the cost baseline in `## Cost Baseline` below — different file contents produce different token counts.

**Per CONTEXT.md `<specifics>` #5,** the fixture is a "separable design surface" — the plan-phase researcher intentionally avoided locking it in, leaving the fixture choice to this plan. v1.x can revisit the choice without breaking the test contract.

## Budget Rationale

The fixture sets `Project.Spec.budget.absoluteCapCents=100` (= $1.00). This is the Phase 2 D-D2 budget infrastructure: per-Project absolute cap measured in USD cents. A runaway Claude dispatch — say, the milestone planner enters an infinite tool-call loop or the model output triples expected token consumption — exceeds the cap, the budget gate fires, and the Project halts with `Status.phase=BudgetExceeded`. The live spec then fails its post-run assertion (`costSpentCents < 100`), surfacing the over-spend in CI.

The cap is the **third safety net** after the build tag and the env-var gate. Each gate alone is sufficient to prevent accidental cost during normal development; together they make a paid request require explicit, multi-step intent.

Why $1.00 and not $10 or $0.10?

- **$10** is too loose — a misconfigured nightly run could drift to $300/month before anyone notices.
- **$0.10** is too tight — a normal milestone planner run on a 200 LOC fixture consumes ~$0.20-$0.80 (see baseline below). $0.10 would fail every run.
- **$1.00** is the smallest cap that comfortably accommodates the v1.0 baseline AND surfaces a 2-3x cost regression as a test failure (not just a budget metric).

## Cost Baseline

**v1.0 expected per-run cost:** $0.20–$0.80 against a ~200 LOC Go-template fixture, using `claude-haiku-4-5` for the milestone planner (overriding the Helm-chart default `claude-opus-4-7` — haiku is cheaper by ~10x and the fixture is small enough that planning quality is not gated on opus).

**Breakdown (approximate):**

- Input tokens: ~3,000 (fixture file contents + planner prompt + system prompt) ≈ $0.0024 at haiku rates.
- Output tokens: ~5,000-10,000 (the `MILESTONE.md` artifact + structured child CRDs) ≈ $0.04-$0.08.
- Tool-call overhead: planner typically issues 2-5 file reads + 1 file write; each tool call costs additional input tokens for the tool definitions ≈ $0.02-$0.04.
- Total per run: $0.06-$0.13 typical, $0.20-$0.80 envelope including occasional retries.

**At nightly cadence:** $2-$25/month at the typical end; $6-$24/month at the upper envelope. The $1.00 cap per run ensures the absolute worst case (every nightly run hits the cap) costs $30/month — manageable budget for a v1.0 OSS project, but a clear signal of a regression worth investigating.

**CI dashboard wiring:** operators should surface the histogram of `Project.Status.budget.costSpentCents` values across recent runs in their CI dashboard. A drift from the $0.06-$0.13 typical cost to consistent $0.30+ is a regression indicator (model behavior change, fixture grew, or planner prompts inflated). Dashboard wiring is outside Phase 3 scope; see v1.x dashboard work (Phase 5).

## Troubleshooting

**Symptom:** `BeforeSuite` Skip recorded immediately, suite exits 0 with 1 Skipped.
**Cause:** `ANTHROPIC_API_KEY` not set in the test environment. The skip-on-missing-creds gate fired.
**Fix:** verify the CI secret store injected the env var into the job. Locally, `export ANTHROPIC_API_KEY=sk-ant-…` before `make test-e2e-live`.

**Symptom:** `kubectl apply failed: error validating "live-claude-project.yaml": …`
**Cause:** the fixture YAML failed CRD admission. Most often: `Project.Spec.git.repoURL` doesn't match the `^https?://.+` pattern (e.g., the operator changed it to a `file://` URL that the CEL validator rejects).
**Fix:** restore the `https://…` URL OR temporarily disable the CEL validation on the Project CRD (not recommended; the validator catches misconfiguration).

**Symptom:** `Eventually(Milestone reaches Succeeded)` times out after 12m; spec fails.
**Cause:** the planner Job either crashed (auth failure → check API key validity, key expiration, model availability for the chosen model identifier) or stalled (network egress blocked in the cluster, GitHub rate-limit on the fixture-repo clone, image-pull stalls — check the controller logs and `kubectl describe job` for clues).
**Fix:** examine the failure mode. The most common cause in nightly CI is image-pull stalls on the first run of a fresh cluster; warm the image cache in `test-int-kind-prep` before invoking the live spec.

**Symptom:** `Project.Status.budget.costSpentCents` exceeds 100 (budget cap hit).
**Cause:** the milestone planner consumed more tokens than the v1.0 baseline. Either the fixture grew, the planner's prompts got chattier, or the model is returning longer outputs.
**Fix:** investigate WHY the spend climbed. If the fixture rotated, bump the cap in the YAML to match the new baseline. If the planner regressed, fix the regression. Do NOT just raise the cap silently — the cap is a regression alarm.

**Symptom:** Spec passes but `Project.Status.git.lastPushedSHA` stays empty.
**Cause:** the push Job either didn't run (controller didn't dispatch — check `kubectl describe project live-claude` for events) or failed to push (PAT scope mismatch, branch protection, gitleaks rejected the diff).
**Fix:** examine the push Job's logs (`kubectl logs job/tide-push-…`). For PAT issues see `docs/git-hosts.md` "Verifying Secret wiring".

**Symptom:** Spec reports `commit message did not match ^tide: milestone .+ authored$`.
**Cause:** the push Job committed but the message format diverged from D-B2 #3. Either the controller's commit-message generation regressed or the test's regex is too narrow.
**Fix:** inspect the actual commit on the per-run branch (`git log --oneline tide/run-…`). If the new format is intentional, update the regex AND the Phase 3 D-B2 documentation. If unintentional, revert the commit-message change in the controller.

## Related Plans

- **Phase 3 plan 03-07** (`.planning/phases/03-…/03-07-PLAN.md`) — ships the real `tide-claude-subagent` image (`internal/subagent/anthropic/subagent.go`). The live spec consumes this image as `Project.Spec.subagent.image`.
- **Phase 3 plan 03-10** (`03-10-PLAN.md`) — sibling Layer B specs (chaos-resume / push-lease / up-stack-dispatch) that are cost-free (stub-subagent). Two distinct CI cadences: `test-int` = every PR; `test-e2e-live` = nightly cron only.
- **Phase 3 plan 03-08** (`03-08-PLAN.md`) — `ProjectReconciler` push flow that emits the D-B2 `tide: milestone <name> authored` commit shape the live spec asserts.
- **Phase 3 plan 03-09** (`03-09-PLAN.md`) — Helm chart values + `docs/git-hosts.md` (the sibling ART-02 doc this file mirrors structurally).
- **Phase 2 D-D2** (`.planning/phases/02-…`) — budget gate infrastructure consumed at line `Project.Spec.budget.absoluteCapCents=100`.

## See Also

- [`.planning/REQUIREMENTS.md`](../.planning/REQUIREMENTS.md) — TEST-03 requirement traceability.
- [`.planning/phases/03-up-stack-reconcilers-git-integration-real-subagent-resumptio/03-RESEARCH.md`](../.planning/phases/03-up-stack-reconcilers-git-integration-real-subagent-resumptio/03-RESEARCH.md) — Anthropic Claude Code CLI integration research; Phase 2 D-D2 budget tally references.
- [`docs/git-hosts.md`](git-hosts.md) — sibling ART-02 deliverable; the PAT-via-Secret pattern this fixture YAML uses.
- [`test/e2e/live_claude_test.go`](../test/e2e/live_claude_test.go) — the spec implementation.
- [`test/e2e/testdata/live-claude-project.yaml`](../test/e2e/testdata/live-claude-project.yaml) — the fixture manifest.
