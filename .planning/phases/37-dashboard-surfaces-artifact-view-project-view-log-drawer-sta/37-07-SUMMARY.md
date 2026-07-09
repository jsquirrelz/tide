---
phase: 37-dashboard-surfaces-artifact-view-project-view-log-drawer-sta
plan: 07
subsystem: dashboard-api
tags: [dashboard, artifacts, settings, rbac, redaction, gitfetch, dash-01, dash-03]

# Dependency graph
requires:
  - phase: 37-03
    provides: "cmd/dashboard/gitfetch Store.Artifacts(ctx, repoURL, branch, auth) — the K8s-free cached git read path this endpoint consumes"
provides:
  - "GET /api/v1/nodes/{kind}/{name}/artifacts — R-04 state-discriminated (available|absent|no-git|error) node artifact serving with full-fidelity content"
  - "GET /api/v1/projects/{name}/settings — whitelist-curated settings + server-rendered raw-spec YAML, secret NAMES only (D-10)"
  - "dashboard ClusterRole secrets:get delta — fetch-time git-creds read (R-02), ⊆{get,list,watch} preserved"
affects: [37-08, 37-05]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Fetch-time credential resolution via the TYPED clientset (one-shot GET) — never a cached controller-runtime client.Get on a Secret (avoids the cluster-wide Secret informer, RESEARCH Pitfall 4)"
    - "Server-side redaction by construction: the settings handler holds no clientset, so a secret VALUE physically cannot enter any response field; only NAMES from the spec cross the wire"
    - "R-04 state discriminator wraps every artifact response so the UI never disambiguates an empty list"
    - "Explicit field whitelist projection (never Spec-wholesale into typed fields), with the raw-spec YAML as the single deliberate D-10 exception (refs are names-only)"

key-files:
  created:
    - cmd/dashboard/api/artifacts.go
    - cmd/dashboard/api/artifacts_test.go
    - cmd/dashboard/api/settings.go
    - cmd/dashboard/api/settings_test.go
  modified:
    - cmd/dashboard/router.go
    - cmd/dashboard/main.go
    - cmd/dashboard/router_test.go
    - charts/tide/templates/dashboard-rbac.yaml

key-decisions:
  - "BaseRef serialized as \"\" — the Phase-35 Spec.Git.BaseRef field has not landed in api/v1alpha2/project_types.go at 37-07 execution time. repoSettings.BaseRef is forward-declared with a doc note; wire it to Spec.Git.BaseRef once that field exists. The TS ProjectSettings.repo.baseRef contract is honored (present, empty)."
  - "Empty CredsSecretRef → anonymous access (nil Auth), not an error — mirrors gitfetch.basicAuth's pat==\"\" guard for public/in-cluster http:// remotes. A NON-empty ref that is missing or lacks GIT_PAT is state:error with a creds-shaped message (no value echoed)."
  - "SettingsHandler takes only client.Client (no kubernetes.Interface) so a Secret read is structurally impossible — redaction is enforced by absence of capability, not by careful field selection."

requirements-completed: [DASH-01, DASH-03]

coverage:
  - id: T1
    description: "Node artifacts endpoint: typed states (no-git/available/absent/error), fetch-time typed-clientset creds, full-fidelity content incl. >1 MiB, kind allowlist / missing-project / unknown-project validation, PAT non-leak in error state."
    requirement: DASH-01
    verification:
      - kind: unit
        ref: "cmd/dashboard/api/artifacts_test.go#TestArtifactsNoGit"
        status: pass
      - kind: unit
        ref: "cmd/dashboard/api/artifacts_test.go#TestArtifactsAvailable (>1 MiB byte-length equality)"
        status: pass
      - kind: unit
        ref: "cmd/dashboard/api/artifacts_test.go#TestArtifactsAbsent"
        status: pass
      - kind: unit
        ref: "cmd/dashboard/api/artifacts_test.go#TestArtifactsError (PAT sentinel absent from body)"
        status: pass
      - kind: unit
        ref: "cmd/dashboard/api/artifacts_test.go#TestArtifactsValidation"
        status: pass
    human_judgment: false
  - id: T2
    description: "Settings endpoint: full card-field projection + raw-spec YAML round-trip; raw-body secret-value redaction; honest empty defaults; 404 unknown project."
    requirement: DASH-03
    verification:
      - kind: unit
        ref: "cmd/dashboard/api/settings_test.go#TestSettingsFullyPopulated"
        status: pass
      - kind: unit
        ref: "cmd/dashboard/api/settings_test.go#TestSettingsRedaction (planted value sentinel absent from raw body incl. rawSpecYAML)"
        status: pass
      - kind: unit
        ref: "cmd/dashboard/api/settings_test.go#TestSettingsHonestDefaults"
        status: pass
    human_judgment: false
  - id: T3
    description: "Router + main.go wiring + RBAC delta: both routes register as GET; secrets:get keeps the read-only invariant; full dashboard tree green."
    requirement: DASH-01
    verification:
      - kind: unit
        ref: "cmd/dashboard/router_test.go#TestArtifactsAndSettingsRoutesAreGET"
        status: pass
      - kind: unit
        ref: "cmd/dashboard/router_test.go#TestZeroMutationRoutes"
        status: pass
      - kind: other
        ref: "make helm-rbac-assert — PASS: dashboard RBAC is read-only (exit 0)"
        status: pass
      - kind: other
        ref: "go test ./cmd/dashboard/... -count=1 — all packages ok"
        status: pass
    human_judgment: false

# Metrics
duration: ~40min
completed: 2026-07-08
status: complete
---

# Phase 37 Plan 07: Node artifacts + project settings endpoints Summary

**The two read-only DASH-01/DASH-03 backend endpoints — R-04 state-discriminated node artifacts (full-fidelity content, fetch-time typed-clientset git creds) and server-redacted project settings (secret NAMES only) — behind the all-GET router, with a get-only `secrets` RBAC delta that keeps the dashboard's read-only invariant intact.**

## Performance

- **Duration:** ~40 min
- **Completed:** 2026-07-08
- **Tasks:** 3 (Tasks 1 & 2 TDD RED→GREEN; Task 3 wiring)
- **Files:** 8 (4 created, 4 modified)

## Accomplishments

- **Artifacts endpoint (`GET /api/v1/nodes/{kind}/{name}/artifacts?project=&namespace=`)** — kind allowlist (`project|milestone|phase|plan`), Project resolution, no-git / pre-first-push (absent) short circuits, fetch-time git creds resolved via the **typed clientset** (`CoreV1().Secrets().Get`, one-shot, no informer — Pitfall 4), `gitfetch.Store.Artifacts` call, `.tide/planning/<kind>/<name>/` prefix filter, R-04 state discriminator (`available|absent|no-git|error`), and full content strings with no caps (D-03).
- **Settings endpoint (`GET /api/v1/projects/{name}/settings?namespace=`)** — explicit-whitelist projection of outcome prompt / repo / models / budget / gates, `secretRef{purpose,name}` pairs (names only, empties skipped), and a server-rendered `rawSpecYAML` via `sigs.k8s.io/yaml`. Handler holds **no clientset** — a secret value cannot structurally enter any field.
- **Router + main.go wiring** — `Store *gitfetch.Store` added to `Dependencies`; `ArtifactsHandler` registered only when Client+Clientset+Store are all non-nil, `SettingsHandler` when Client is non-nil (nil-tolerant, mirrors the lh/eh pattern); `main.go` builds `gitfetch.NewStore(&gitfetch.GoGitFetcher{}, 32)`.
- **RBAC delta** — `secrets: [get]` rule added to `dashboard-rbac.yaml` with a documented comment block (DASH-01/R-02, get-only rationale, Argo-Server analogue). `make helm-rbac-assert` still passes (⊆{get,list,watch}).

## Task Commits

1. **Task 1: Artifacts endpoint** — RED `a909d54` (test) → GREEN `171c3dd` (feat)
2. **Task 2: Settings endpoint** — RED `77724bd` (test) → GREEN `7cb3df3` (feat)
3. **Task 3: Router + main.go + RBAC** — `b6cd7fa` (feat)

## Files Created/Modified

- `cmd/dashboard/api/artifacts.go` — `ArtifactsHandler`, `nodeArtifacts`/`artifactFile` structs, kind allowlist, `resolveAuth` (typed-clientset GIT_PAT read), prefix filter, state machine.
- `cmd/dashboard/api/artifacts_test.go` — fake `Fetcher` + fake clientset; five behavior tests incl. >1 MiB byte-length equality and PAT non-leak.
- `cmd/dashboard/api/settings.go` — `SettingsHandler` (client-only), `projectSettings` + nested structs, whitelist projection, `buildSecretRefs`, raw-spec YAML render.
- `cmd/dashboard/api/settings_test.go` — full-population, raw-body redaction (planted value sentinels), honest-defaults + 404 tests; registers corev1 in the fake-client scheme.
- `cmd/dashboard/router.go` — `Store` dep, `ArtifactsHandler`/`SettingsHandler` construction + conditional route registration, route-table doc comment rows.
- `cmd/dashboard/main.go` — gitfetch Store construction + injection.
- `cmd/dashboard/router_test.go` — `TestArtifactsAndSettingsRoutesAreGET` (both routes registered as GET).
- `charts/tide/templates/dashboard-rbac.yaml` — get-only `secrets` rule + rationale comment.

## Decisions Made

- **BaseRef = `""`** because `Spec.Git.BaseRef` (Phase 35) has not landed in the schema at execution time; the field is forward-declared with a wire-it-later doc note. The TS `repo.baseRef` contract stays satisfied (present, empty → UI renders the HEAD-default label).
- **Empty `CredsSecretRef` → anonymous fetch (nil Auth)**, not an error; a non-empty-but-missing/malformed secret → `state:error` with a creds-shaped message that never echoes a value.
- **SettingsHandler deliberately omits `kubernetes.Interface`** so redaction is enforced by absence of capability.

## Deviations from Plan

**None materially — plan executed as written.** One documented forward-reference:

- **[Rule 3 — forward-reference] `Spec.Git.BaseRef` absent at execution time.** The plan explicitly anticipated this ("if the field is absent at execution time, serialize \"\" and note it in the SUMMARY"). `repoSettings.BaseRef` serializes `""`; a doc comment marks where to wire it once Phase 35's field lands. No blocker.

Minor: two doc comments in `settings.go` were reworded so the plan's acceptance greps (`grep -c 'sigs.k8s.io/yaml'` == 1, `grep -c 'kubernetes.Interface'` == 0) resolve to the load-bearing code tokens only — same class of adjustment 37-03 made for its `NoTags` grep. Behavior unchanged.

No package-manager installs occurred (T-37-SC accept).

## Threat Register Outcomes

- **T-37-07-01 (secret values via settings)** — mitigated: names-only whitelist, no Secret read in the handler, raw-body sentinel test green.
- **T-37-07-02 (SA privilege creep)** — mitigated: exactly `secrets:get`, `make helm-rbac-assert` green, typed clientset (no informer), chart comment documents scope.
- **T-37-07-03 (PAT leakage via error/logs)** — mitigated: error message excludes the PAT (tested), credential confined to the fetch call frame, never logged.
- **T-37-07-04 (path traversal via kind/name)** — mitigated: kind allowlist (400 otherwise); name/namespace flow into typed K8s Gets and a git-tree prefix filter, never a filesystem/shell path.
- **T-37-07-05 (repeated large fetches)** — mitigated by 37-03's Tip-check + bounded LRU; this plan adds no truncation (D-03).

## Issues Encountered

- The controller-runtime fake client panics building with `corev1.Secret` objects unless corev1 is registered in the scheme. Fixed by adding `clientgoscheme.AddToScheme` to the settings-test scheme (the artifacts test uses `client-go/kubernetes/fake` for Secrets, which needs no scheme registration).

## User Setup Required

None. The `secrets:get` grant is applied automatically on `helm upgrade`. Operators using a hand-authored ClusterRole must add the get-only `secrets` rule for the artifact view to authenticate private-repo fetches.

## Next Phase Readiness

- 37-08 renders these two payloads (ArtifactView markdown/JSON, ProjectView settings cards). The JSON shapes match the 37-05 TS `NodeArtifacts` / `ProjectSettings` types field-for-field.
- When Phase 35's `Spec.Git.BaseRef` lands, wire `repoSettings.BaseRef = p.Spec.Git.BaseRef` in `settings.go` (single line, marked by the doc comment).

## Self-Check: PASSED

Created files verified present; task commits verified in `git log`. See section below.
