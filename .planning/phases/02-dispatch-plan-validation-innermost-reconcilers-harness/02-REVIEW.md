---
phase: 02-dispatch-plan-validation-innermost-reconcilers-harness
reviewed: 2026-05-12T00:00:00Z
depth: standard
files_reviewed: 76
files_reviewed_list:
  - .github/workflows/ci.yaml
  - api/v1alpha1/aggregates_guard_test.go
  - api/v1alpha1/plan_types.go
  - api/v1alpha1/project_types.go
  - api/v1alpha1/shared_types.go
  - api/v1alpha1/task_types.go
  - charts/tide-crds/templates/plan-crd.yaml
  - charts/tide-crds/templates/project-crd.yaml
  - charts/tide-crds/templates/task-crd.yaml
  - charts/tide/templates/deployment.yaml
  - charts/tide/templates/manager-rbac.yaml
  - charts/tide/templates/projects-pvc.yaml
  - charts/tide/templates/serviceaccount-subagent.yaml
  - charts/tide/templates/signing-secret.yaml
  - charts/tide/values.yaml
  - cmd/credproxy/main.go
  - cmd/manager/main.go
  - cmd/stub-subagent/main_test.go
  - cmd/stub-subagent/main.go
  - cmd/tide-lint/main.go
  - config/crd/bases/tideproject.k8s_plans.yaml
  - config/crd/bases/tideproject.k8s_projects.yaml
  - config/crd/bases/tideproject.k8s_tasks.yaml
  - hack/helm/augment-tide-chart.sh
  - hack/helm/projects-pvc.yaml
  - hack/helm/serviceaccount-subagent.yaml
  - hack/helm/signing-secret.yaml
  - hack/helm/tide-values.yaml
  - internal/budget/bucket_test.go
  - internal/budget/bucket.go
  - internal/budget/cap_test.go
  - internal/budget/cap.go
  - internal/budget/doc.go
  - internal/budget/metrics.go
  - internal/budget/precharge_test.go
  - internal/budget/precharge.go
  - internal/budget/tally_test.go
  - internal/budget/tally.go
  - internal/controller/plan_controller_test.go
  - internal/controller/plan_controller.go
  - internal/controller/plan_webhook_test.go
  - internal/controller/project_controller_test.go
  - internal/controller/project_controller.go
  - internal/controller/suite_test.go
  - internal/controller/task_controller_test.go
  - internal/controller/task_controller.go
  - internal/controller/wave_controller_test.go
  - internal/controller/wave_controller.go
  - internal/credproxy/cert_test.go
  - internal/credproxy/cert.go
  - internal/credproxy/doc.go
  - internal/credproxy/server_test.go
  - internal/credproxy/server.go
  - internal/credproxy/token_test.go
  - internal/credproxy/token.go
  - internal/dispatch/dispatcher.go
  - internal/dispatch/doc.go
  - internal/dispatch/podjob/backend_test.go
  - internal/dispatch/podjob/backend.go
  - internal/dispatch/podjob/doc.go
  - internal/dispatch/podjob/jobspec_test.go
  - internal/dispatch/podjob/jobspec.go
  - internal/dispatch/podjob/names_test.go
  - internal/dispatch/podjob/names.go
  - internal/harness/caps_test.go
  - internal/harness/caps.go
  - internal/harness/doc.go
  - internal/harness/envelope_io_test.go
  - internal/harness/envelope_io.go
  - internal/harness/harness_test.go
  - internal/harness/harness.go
  - internal/harness/outputs_test.go
  - internal/harness/outputs.go
  - internal/harness/redact/doc.go
  - internal/harness/redact/patterns.go
  - internal/harness/redact/redact_test.go
  - internal/harness/redact/redact.go
  - internal/webhook/v1alpha1/plan_webhook.go
  - internal/webhook/v1alpha1/strict_mode_test.go
  - internal/webhook/v1alpha1/strict_mode.go
  - pkg/dispatch/doc.go
  - pkg/dispatch/envelope_test.go
  - pkg/dispatch/envelope.go
  - pkg/dispatch/errors.go
  - pkg/dispatch/subagent.go
  - test/integration/envtest/admission_test.go
  - test/integration/envtest/budget_test.go
  - test/integration/envtest/indegree_test.go
  - test/integration/envtest/init_test.go
  - test/integration/envtest/rate_limit_test.go
  - test/integration/envtest/suite_test.go
  - test/integration/kind/caps_test.go
  - test/integration/kind/credproxy_test.go
  - test/integration/kind/failure_test.go
  - test/integration/kind/output_test.go
  - test/integration/kind/suite_test.go
  - test/integration/kind/wave_test.go
  - tools/analyzers/providerfirewall/analyzer_test.go
  - tools/analyzers/providerfirewall/analyzer.go
findings:
  critical: 5
  warning: 11
  info: 7
  total: 23
status: issues_found
---

# Phase 2: Code Review Report

**Reviewed:** 2026-05-12
**Depth:** standard
**Files Reviewed:** ~76 source files (including tests, charts, and CI workflow)
**Status:** issues_found

## Summary

Phase 2 lights up the dogfood-critical innermost dispatch chain — `TaskReconciler` + `WaveReconciler` + plan admission webhook + the in-pod harness/credproxy/budget triplet. The architectural shape is sound: the public `pkg/dispatch` envelope contract, HMAC-bound signed token, single-shared-PVC + subPath isolation, sync.Map rate-limit cache, and observational-only Wave reconciler all match the decision log in `02-CONTEXT.md`.

The defects below cluster in three areas: **(1) admission/dispatch correctness** — `client.IgnoreNotFound` mis-used to filter `AlreadyExists` in Wave create; the post-Reserve dispatch path leaks rate-limit tokens on every downstream error; **(2) security** — the credproxy passes arbitrary URL paths/methods through to upstream without an allowlist; `outputs.Validate`'s symlink-resolution algorithm has an asymmetric comparison that false-positives on workspaces with symlinks in the root (e.g., macOS `/tmp` → `/private/tmp`); the signing-secret Helm template applies a double-base64 transform that produces a non-deterministic effective key; **(3) test reliability** — webhook tests use `Or(rejection, Succeed())` which makes them tautological under Pitfall B, meaning a real regression won't fail CI.

All five BLOCKERs are correctness or security issues that should be fixed before this code drives real subagents.

## Critical Issues

### CR-01: `client.IgnoreNotFound` mis-used to filter `AlreadyExists` in Wave create

**File:** `internal/controller/plan_controller.go:237`
**Issue:** Inside `materializeWaves`, the code path that creates a missing Wave calls `r.Create(ctx, wave)` then checks `if client.IgnoreNotFound(err) != nil`. `Create` returns `AlreadyExists` (not `NotFound`) when a Wave with the same name already exists due to watch-lag racing — `IgnoreNotFound` does NOT filter `AlreadyExists`, so a duplicate-create race surfaces as a hard error. The "Rare race: treat as success" comment is dead code; the actual code path errors out.

**Fix:**
```go
if err := r.Create(ctx, wave); err != nil {
    if !apierrors.IsAlreadyExists(err) {
        return fmt.Errorf("create wave %s: %w", waveName, err)
    }
    // AlreadyExists: idempotent success — watch-lag race.
}
```

---

### CR-02: credproxy ignores upstream URL parse error → nil-pointer panic on bad config

**File:** `internal/credproxy/server.go:43`
**Issue:** `upstream, _ := url.Parse(p.UpstreamBaseURL)` drops the error. If `p.UpstreamBaseURL` is malformed (e.g., empty, leading whitespace, or unsupported scheme), `url.Parse` returns `nil, err` and the subsequent `httputil.NewSingleHostReverseProxy(upstream)` panics with nil-pointer dereference on the first request. The cmd/credproxy/main.go validates env vars but never validates `--upstream-url`; an operator-supplied `--upstream-url=""` produces a runtime panic only when the first request arrives.

**Fix:**
```go
func (p *Proxy) Handler() (http.Handler, error) {
    upstream, err := url.Parse(p.UpstreamBaseURL)
    if err != nil || upstream == nil || upstream.Host == "" {
        return nil, fmt.Errorf("credproxy: invalid upstream URL %q: %w", p.UpstreamBaseURL, err)
    }
    rp := httputil.NewSingleHostReverseProxy(upstream)
    // ...
}
```
And surface the error in `ListenAndServe` / `cmd/credproxy/main.go` so the sidecar fails fast on startup, not on the first auth-token call.

---

### CR-03: Reservation leak on every dispatch-path error after Reserve()

**File:** `internal/controller/task_controller.go:263-282 (and 574-586 helper)`
**Issue:** When the bucket has tokens (`d == 0`), `lim.Reserve()` is consumed but never cancelled — the subsequent code path (Step 7–12) might fail at `nextAttempt` (list error), `credproxy.Sign` (HMAC error), `buildEnvelopeIn` (marshal error), `Status().Patch` of `task.Status.Attempt`, or `r.Create(ctx, job)`. Every one of those failures consumes a rate-limit token without dispatching a Job. Under sustained transient errors (e.g., a misconfigured signing key), a Project's bucket drains to zero and stays there until natural refill — the controller will silently throttle itself for reasons unrelated to upstream rate limits.

**Fix:** Hold the `*rate.Reservation` and `rsv.Cancel()` on any error before successful `r.Create(ctx, job)`:
```go
// After successful Reserve with d==0:
deferredCancel := rsv.Cancel
defer func() {
    if !committed {
        deferredCancel()
    }
}()
// ... only set `committed = true` after a successful Create (and AlreadyExists branch).
```

---

### CR-04: credproxy has no upstream URL/path allowlist — any path is proxied with real key

**File:** `internal/credproxy/server.go:42-70`
**Issue:** Once the signed token verifies, the proxy unconditionally forwards the request to `upstream.Host` with the real `ANTHROPIC_API_KEY` injected. There is no path allowlist, no method restriction, no body inspection. A compromised subagent (or a runtime bug that leaks an arbitrary HTTP client) can use the proxy to call any path on `api.anthropic.com` — e.g., `/v1/files/*` for arbitrary file uploads, or any future admin/billing endpoints — using the real org-level key.

This widens the blast radius from "subagent can spend Anthropic tokens" to "subagent can call any Anthropic API the org key is authorized for." Doesn't violate the spec strictly (D-C1 says the proxy validates the token, not the request shape), but is a defense-in-depth gap worth a v1 mitigation.

**Fix:** Add an explicit allowlist of `(method, path-prefix)` tuples; reject everything else with 403:
```go
var allowed = []struct{ method, prefix string }{
    {"POST", "/v1/messages"},
    {"POST", "/v1/messages/count_tokens"},
    // ... explicit set for the SDK surface the subagent legitimately uses.
}
// In handler: reject 403 on any (method, path) outside this set.
```

---

### CR-05: `outputs.Validate` symlink-asymmetry — declared paths and walk targets resolved differently

**File:** `internal/harness/outputs.go:27-83`
**Issue:** When a declared path doesn't exist at Validate-call time, the code retains its un-resolved absolute form (line 38: `declaredAbs = append(declaredAbs, abs)`). When the walk later finds a real file inside that path, it calls `filepath.EvalSymlinks(p)` (line 62), which resolves any symlinks in the *workspace root* prefix.

On macOS, `/tmp` is a symlink to `/private/tmp`. A workspace at `/tmp/foo/workspace/artifacts/result.txt` will produce:
- `declaredAbs[0]` = `/tmp/foo/workspace/artifacts` (un-resolved — directory didn't pre-exist)
- `real` = `/private/tmp/foo/workspace/artifacts/result.txt` (resolved)
- `filepath.Rel("/tmp/foo/workspace/artifacts", "/private/tmp/...")` returns a path starting with `../` → **flagged as a violation** even though the file is in-scope.

This is a latent FALSE-POSITIVE bug. The tests in `outputs_test.go` use `t.TempDir()` which generally returns `/var/folders/.../T/` on macOS (also symlinked) — but the tests pre-create the declared directories (line 14-17 of `outputs_test.go`), so EvalSymlinks succeeds in the pre-resolve loop and the asymmetry is hidden. Production code uses `task_controller.go:416` `taskWorkspaceRoot := fmt.Sprintf("/workspaces/%s/workspace", ...)` and declared paths are not guaranteed to exist before the harness mkdir's them.

**Fix:** Resolve declared paths *and* walk targets through `filepath.EvalSymlinks` after pre-creating any missing ancestors, OR pass-through the un-resolved form for both sides:
```go
// Option 1: resolve only the existing prefix.
func resolveExistingPrefix(p string) string {
    cur := p
    for {
        if real, err := filepath.EvalSymlinks(cur); err == nil {
            // Replace existing prefix with resolved form, keeping the unresolved suffix.
            suffix := strings.TrimPrefix(p, cur)
            return filepath.Join(real, suffix)
        }
        next := filepath.Dir(cur)
        if next == cur {
            return p // can't resolve anything
        }
        cur = next
    }
}
// Apply to both declaredAbs and `p` before Rel().
```

## Warnings

### WR-01: Empty/zero WallClockSeconds produces 60-second token expiry — far too short for realistic dispatch

**File:** `internal/controller/task_controller.go:311-315`
**Issue:** `wallClock` defaults to 0 when `task.Spec.Caps == nil` or `Caps.WallClockSeconds == 0`. The token is then signed with `validFor = 60s` (`0 + DefaultWallClockGraceSeconds`). If the Job startup is slow (image pull, scheduler delay, init containers), the token will expire before the subagent makes its first request, and `credproxy.Verify` returns `ErrExpired` — task fails with a misleading "auth" error rather than a budget/timing error.

**Fix:** Establish a floor:
```go
wallClock := int32(0)
if task.Spec.Caps != nil {
    wallClock = task.Spec.Caps.WallClockSeconds
}
if wallClock <= 0 {
    wallClock = 300 // sane default — must outlive image pull + cold start
}
```

---

### WR-02: `WindowStart` never reset → rolling-window cap is effectively absolute

**File:** `internal/budget/tally.go:34-39`
**Issue:** Docstring says "Once set, it is preserved — the window resets on the next billing-period boundary, which Plan 10's ProjectReconciler handles separately." But nothing in `ProjectReconciler` resets `WindowStart`. The `RollingWindowCapCents` field on `BudgetConfig` (project_types.go:90) is therefore unenforced — only `AbsoluteCapCents` is checked (by `IsCapExceeded`). The roll-up code unconditionally accumulates `TokensSpent` and `CostSpentCents` forever.

**Fix:** Either (a) document the rolling-window deferral honestly in `BudgetConfig.RollingWindowCapCents` doc, OR (b) add a reset path in `ProjectReconciler` that compares `now - WindowStart` against the configured window and zeros the tally when crossed.

---

### WR-03: `nextAttempt` uses `fmt.Sscanf` with no width / sign / overflow guard

**File:** `internal/controller/task_controller.go:563`
**Issue:** `fmt.Sscanf(attempt, "%d", &n)` will accept negative numbers, hex (with `%d`? no — but `%v` would), or trailing garbage that Sscanf ignores. A malicious label value `tideproject.k8s/attempt=-1` would yield `n=-1` and the max-attempt tracking would silently misbehave. RBAC limits who can set labels, but the subagent SA has no patch verbs so this is more about robustness than security.

Also: this scans every Job in the namespace matching the label, parses each, and tracks max. If two reconcilers race (leader election failure mode), nextAttempt's max-detection plus deterministic naming should still ensure correct behavior — but the parse error is silently swallowed.

**Fix:**
```go
n, err := strconv.Atoi(attempt)
if err != nil || n < 0 {
    logger.V(1).Info("ignoring malformed attempt label", "value", attempt)
    continue
}
if n > maxAttempt {
    maxAttempt = n
}
```

---

### WR-04: `cmd/manager/main.go` decodes signing key from base64, but Helm Secret data is *already* base64-decoded

**File:** `cmd/manager/main.go:66-79`, `charts/tide/templates/signing-secret.yaml:19`
**Issue:** Helm `randAlphaNum 64 | b64enc` produces a base64 of the random alphanum string. Kubernetes Secret `data` is stored as base64 and decoded *automatically* before being injected via `envFrom`. So `TIDE_SIGNING_KEY` env var gets the *plaintext* 64-char alphanum string at runtime — NOT the base64 form.

Now `decodeSigningKeyFromEnv()` (cmd/manager/main.go:66) calls `base64.StdEncoding.DecodeString(raw)` on that plaintext. This *might* succeed by accident if the 64-char alphanum happens to be valid base64 (alphanum + `=`), but the resulting decoded bytes are NOT what was signed against in the credproxy sidecar.

But wait — the credproxy sidecar at `cmd/credproxy/main.go:83` does the SAME `base64.StdEncoding.DecodeString(signingKeyB64)`. So *both* controller and credproxy apply the same (incorrect) decode, and HMACs match.

The bug is then: **the effective signing key strength is whatever `base64.DecodeString(alphanum64)` happens to produce.** For `randAlphaNum 64`, base64 will *truncate* on invalid characters or produce undefined-length output. The 32-byte minimum check (`len(key) < 32`) might pass — but the key is half-random at best. Verify by running `echo -n "$(< /dev/urandom tr -dc A-Za-z0-9 | head -c 64)" | base64 -d | wc -c` — typically yields ~48 bytes of partly-valid decode before hitting an invalid char.

**Fix:** Either:
1. Drop the b64encode in the Helm template (write raw bytes, K8s will base64-encode for transit), and drop the DecodeString in both Go binaries. The env var carries the plaintext 64-char alphanum; use it directly as `[]byte`.
2. OR: change the Helm template to generate proper base64-encoded random bytes: `genCryptoRandomBytes 32 | b64enc` (or equivalent helper) and keep the DecodeString in Go.

Option 1 is simpler; option 2 makes the env var format explicit.

---

### WR-05: Webhook tests use tautological `Or(rejection, Succeed())` — regressions cannot fail

**File:** `internal/controller/plan_webhook_test.go:254-266, 298-306` + `test/integration/envtest/admission_test.go:118-122, 232-237`
**Issue:** Cycle-rejection and strict-mode-rejection tests use `Should(Or(Satisfy(isWebhookRejection), Succeed()))`. This passes whether the webhook rejects OR admits — the "Pitfall B fall-through" rationale. Result: a real regression where the webhook silently admits a cyclic Plan or strict-mode mismatch will not fail these tests.

The Pitfall B mitigation should be a separate test (admission *with* Tasks not visible → admits with warning), not folded into the rejection assertion. The rejection assertion should `Eventually` until the webhook gets the indexed task set and rejects deterministically.

**Fix:** Split into two test specs:
1. "rejects cyclic Plan with Tasks visible" — wait deterministically for the field-indexer cache to populate (via `mgrClient.List` with `MatchingFields`), THEN trigger the update; require rejection.
2. "admits Plan with no Tasks visible (Pitfall B)" — create only the Plan, no Tasks; require admission with a `"no owned Tasks visible"` warning.

---

### WR-06: `dispatch.Dispatcher.Run` is exposed but never called from production code → dead seam

**File:** `internal/dispatch/dispatcher.go`, `internal/dispatch/podjob/backend.go:94-200`
**Issue:** The `Dispatcher.Run` interface is documented as "not for the executor path; for test fixtures and Phase 3 planner-dispatch." But the only callers in-tree are test fixtures. The presence of a non-nil `r.Dispatcher` is used as a gate in every reconciler (`if r.Dispatcher != nil` at task_controller.go:160, plan_controller.go:120, project_controller.go:135, wave_controller.go:124) — but `Dispatcher.Run` itself is never invoked by these reconcilers in Phase 2.

The "seam" works as a phase-2-on/off flag rather than an actual dispatch interface. Confusing for readers; refactor candidate.

**Fix:** Either:
1. Replace the `r.Dispatcher != nil` gate with an explicit `r.PhaseEnabled bool` field (or remove the gate entirely once Phase 2 is the default).
2. Or rename `Dispatcher` to `PlannerDispatcher` and use it for Phase 3 planner dispatch only, leaving the executor-path code free of the no-op gate.

---

### WR-07: `lim.Allow()` drain loop in rate_limit_test is non-deterministic

**File:** `test/integration/envtest/rate_limit_test.go:107-110`
**Issue:** Test drains a token bucket via:
```go
for i := 0; i < 10; i++ {
    _ = limiter.Allow() // drain tokens; most will return false after burst=2
}
```
This depends on time elapsing during the loop being less than one refill interval (Minute/5 = 12s per token). The loop runs in microseconds, so it's fine in practice — but the test is implicitly time-dependent.

More importantly: under high CI load, if the test process is preempted between Limits' `interval := time.Minute / time.Duration(RPM)` setup and the `Allow()` calls, tokens could refill. Use `Reserve()` (which doesn't auto-refill mid-call) for deterministic drain:

**Fix:**
```go
for i := 0; i < 10; i++ {
    rsv := limiter.Reserve()
    _ = rsv // consume without cancelling — deterministic
}
```

---

### WR-08: `WindowStart.Truncate(1000000000)` magic number in tally_test

**File:** `internal/budget/tally_test.go:139`
**Issue:** `Truncate(1000000000)` truncates to second-precision via raw nanoseconds. This is opaque — `time.Second` would be self-documenting.

**Fix:**
```go
if !updated.Status.Budget.WindowStart.Time.Truncate(time.Second).Equal(existingTime.Time.Truncate(time.Second)) {
```

---

### WR-09: `applyController` log line claims "tide-controller-manager" Deployment exists in `tide-system`, but `kindNamespace` is `tide-int-test`

**File:** `test/integration/kind/suite_test.go:58, 247-248, 272`
**Issue:** Constant `kindNamespace = "tide-int-test"` is used for *test fixtures*, but the controller deployment check hard-codes `tide-system` (line 248, 272) — the namespace where `config/default` kustomize manifest installs. If the kustomize default changes, the readiness check breaks silently and `waitForControllerReady` returns without finding the deployment, suite proceeds in "CRDs-only mode" with no warning beyond a GinkgoWriter message that may be invisible in CI.

**Fix:** Extract the controller-namespace as a constant, log explicitly when entering CRDs-only mode (use `t.Logf` not `GinkgoWriter` so it shows in `go test -v`), and ideally fail the kind suite if the controller is expected but missing (controlled by an env var).

---

### WR-10: `_ = rsv` in test code leaks `*rate.Reservation` without Cancel — readers can't tell intent

**File:** `internal/controller/task_controller_test.go:401-402, 481-483`, `test/integration/envtest/rate_limit_test.go:107-110`
**Issue:** Pattern `rsv := lim.Reserve(); _ = rsv` is used to drain the bucket. To a reader, `_ = rsv` looks like a deliberate non-use, which it is — but if someone refactors to `rsv.Cancel()` thinking it's a leaked variable, the bucket no longer drains. Comment the intent explicitly.

**Fix:**
```go
rsv := lim.Reserve()
// Intentionally NOT cancelled — we want this reservation to permanently consume the token.
_ = rsv
```

---

### WR-11: `predicate.AnnotationChangedPredicate` on Project means every annotation triggers reconcile

**File:** `internal/controller/project_controller.go:444-450`
**Issue:** The Project predicate is `Or(GenerationChangedPredicate, AnnotationChangedPredicate)`. Every annotation change (kubectl add labels, helm upgrade re-stamping, monitoring tools, etc.) reconciles the Project. For active dev this generates an issue: every `kubectl annotate project foo bar=baz` reads the PVC list, attempts an init Job get, re-runs the budget gate. Fine for v1 throughput, but unbounded annotation-driven reconciles could mask real signals.

Narrow the annotation predicate to the budget-bypass annotations only:
```go
predicate.AnnotationChangedPredicate{} → predicate.NewPredicateFuncs(func(_ client.Object) bool { /* check only bypass-budget* keys */ })
```

## Info

### IN-01: `predicate.Or(GenerationChangedPredicate, AnnotationChangedPredicate)` only on Project — other reconcilers default to GenerationOnly

**File:** `internal/controller/{plan,wave,task}_controller.go` SetupWithManager
**Issue:** Only `ProjectReconciler` uses the layered annotation-changed predicate. `TaskReconciler` and `PlanReconciler` use the default (status-change generates Update events without Generation bump, but Owns/Watches drive secondary triggers). Worth a comment that the asymmetry is intentional — Project is the only Kind with operator-facing annotation policies.

---

### IN-02: `Task.Spec.Caps` is a pointer; `Task.Spec.Dev` is a struct — inconsistent

**File:** `api/v1alpha1/task_types.go:88, 94`
**Issue:** `Caps *Caps` vs `Dev TaskDev`. Reads inconsistent. The reasoning is documented at lines 92-94 ("Zero-value embed (not pointer) — field presence is governed by the TestMode enum constraint"), but the inconsistency still surfaces in callers (`if task.Spec.Caps != nil` vs `if task.Spec.Dev.TestMode != ""`). Worth a more prominent comment on Caps explaining why pointer-vs-not.

---

### IN-03: `dispatchHang` uses `time.After(time.Hour)` in a loop

**File:** `cmd/stub-subagent/main.go:237`
**Issue:** `time.After` creates a fresh timer on every iteration; old timers are not GC'd until they fire. With a one-hour interval, this is essentially fine (one timer at a time, lifetime bounded by hang scenario), but pattern-wise prefer `time.NewTimer`/`Stop` or `signal.Notify` for clarity:

**Fix:**
```go
func dispatchHang(ctx context.Context) int {
    <-ctx.Done()
    return 0
}
```
(`time.After` loop is unnecessary — context cancellation is the only exit.)

---

### IN-04: `cmd/stub-subagent/main.go:213-217` reads `/workspace/escape/leak.txt` from production filesystem in tests

**File:** `cmd/stub-subagent/main_test.go:212-218`
**Issue:** `TestStub_ExceedOutputPathsMode` reads from the hard-coded path `/workspace/escape/leak.txt` (line 212) — which is the production path the stub writes to. If a parent test ran in the same process and that path doesn't exist, the test silently proceeds; if it does exist with old content, false-positives. The stub itself writes to the hard-coded path (`dispatchExceedOutputPaths` line 247) — testable only if the test process has write access to `/workspace/`. In CI containers this is fine; on a dev box it fails the WriteFile silently.

The test would be more robust if `dispatchExceedOutputPaths` accepted a workspace-root parameter via env (`TIDE_WORKSPACE_ROOT`) and the test pointed it at a tmpdir.

**Fix:** Plumb workspace root via env var so the stub is portable and the test is hermetic.

---

### IN-05: Aggressive `task.Spec.DeclaredOutputPaths` minItems=1 requirement may force test boilerplate

**File:** `api/v1alpha1/task_types.go:81-85`
**Issue:** `+kubebuilder:validation:MinItems=1` on `DeclaredOutputPaths` is correct for HARN-05 enforcement but pushes test boilerplate: every test fixture has to declare at least one path even when the test doesn't exercise output validation. Tests use `[]string{filesTouched}` as a convention but it's not enforced — the conventional alignment isn't a property. Worth documenting that "writeable paths" is a *superset* of `FilesTouched`.

---

### IN-06: `summariseMismatches` produces unbounded strings on large mismatch sets

**File:** `internal/webhook/v1alpha1/plan_webhook.go:312-318`
**Issue:** A Plan with N tasks all touching the same file would generate `O(N²)` mismatch entries in the joined string. K8s API has a limit on AdmissionResponse body size; on a Plan with 20+ tasks all overlapping, the response might exceed the limit. The 20-task ceiling is documented elsewhere but the unbounded `strings.Join` is not defended.

**Fix:** Truncate at e.g. 10 entries with `... and N more`.

---

### IN-07: `tide-subagent` ServiceAccount has `automountServiceAccountToken: true`

**File:** `charts/tide/templates/serviceaccount-subagent.yaml:8`, `hack/helm/serviceaccount-subagent.yaml:8`
**Issue:** Comment says "No Role, no RoleBinding — subagent pods have zero K8s API verbs" — true. But `automountServiceAccountToken: true` still mounts the JWT projected token at `/var/run/secrets/kubernetes.io/serviceaccount/`. The subagent (or a compromised subagent) can read its own SA token; without any granted permissions the token can only `whoami`/SelfSubjectReview-type calls. Defense-in-depth would set `automountServiceAccountToken: false` on the SA AND on the Pod spec (subagent containers don't need the token at all). Phase 2 spec doesn't require this; flagging for hardening.

**Fix:**
```yaml
automountServiceAccountToken: false
```
And ensure subagent pod spec doesn't override with `automountServiceAccountToken: true`.

---

_Reviewed: 2026-05-12_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
