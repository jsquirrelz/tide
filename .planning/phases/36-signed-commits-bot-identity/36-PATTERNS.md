# Phase 36: Signed Commits + Bot Identity (identity-only) - Pattern Map

**Mapped:** 2026-07-04
**Files analyzed:** 17 (1 new, 16 modified + regen)
**Analogs found:** 17 / 17 (every mechanism has an in-repo validated precedent)

## Assumptions

- Headless run: RESEARCH.md's line refs were spot-verified this session against the live worktree; all cited excerpts below were re-read directly. Phase 35 interaction (chart-version bump, `baseRef` dual-version shape) remains conditional per Pitfall 6 / A1 — planner must make the bump task read `hack/helm/tide-chart.yaml` at execution time.
- Chart value shape `agent.name` / `agent.email` (top-level block) adopted per RESEARCH.md's discretion resolution — no collision with `signingKey` (HMAC).

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `pkg/git/identity.go` (NEW) | shared constants/utility | env read → value | `internal/harness/commit.go:56-63` env-fallback block (to be replaced by this file) | exact |
| `internal/harness/commit.go` | in-Job git utility | file I/O (git CLI) | itself — modify in place | exact |
| `pkg/git/integrate.go` | in-Job git utility | file I/O (git CLI) | itself — modify in place | exact |
| `cmd/tide-push/main.go` | in-Job binary | file I/O (go-git) | itself — `tideBotSignature()` replaced | exact |
| `api/v1alpha2/project_types.go` | CRD types | config schema | `GitConfig.LeaksConfigRef` + `RepoURL` Pattern marker (same file, lines 205-224) | exact |
| `api/v1alpha1/project_types.go` | CRD types (parity) | config schema | v1alpha2 sibling; follow Phase 35 `baseRef` shape if landed | exact |
| `internal/controller/dispatch_helpers.go` | controller resolver | config precedence | `resolveImage` (same file, lines 269-291) + `ProviderDefaults` (lines 114-123) | exact |
| `internal/controller/push_helpers.go` | Job builder | K8s object construction | `buildPushJob` (same file, lines 144-264) — add `Env` block | exact |
| `internal/controller/boundary_push.go` | reconciler helper | config threading | `PushOptions` construction at lines 116/168 (has `project` in scope) | exact |
| `internal/controller/{task,project,milestone,phase,plan}_controller.go` | reconcilers | config threading | existing `BuildOptions` population sites (task_controller.go:762, 1414; project_controller.go:1245; milestone_controller.go:486; phase/plan planner paths) | exact |
| `internal/dispatch/podjob/jobspec.go` | Job builder | K8s object construction | `subagentEnv` block (lines 357-378) — pricing-env precedent | exact |
| `internal/dispatch/podjob/backend.go` | fixture backend | config precedence (inline mirror) | inline image walk (lines 267-277) | exact |
| `cmd/manager/env.go` | startup config | env → struct | `tideHelmProviderDefaults` + `envOrDefault` (lines 37-42, 96-101) | exact |
| `hack/helm/tide-values.yaml` | chart values source | config | existing value blocks in same file | exact |
| `hack/helm/augment-tide-chart.sh` | chart generator | template injection | ENV3 marker block (lines 228-283) | exact |
| `hack/helm/tide-chart.yaml` | chart version | config | conditional bump (Pitfall 6 — batch with Phase 35) | exact |
| `docs/project-authoring.md` | docs | — | existing GitConfig table (line ~49) | exact |

Tests (all analogs verified): `internal/harness/commit_test.go:98-109`, `cmd/tide-push/main_test.go:869-873`, `pkg/git/integrate_test.go`, `internal/dispatch/podjob/jobspec_test.go:828+` (pricing transport test — the template for new env-presence tests), `api/v1alpha1/phase3_schema_test.go:303`, `cmd/manager/env_test.go`.

## Pattern Assignments

### `pkg/git/identity.go` (NEW — shared constants + env helper)

**Analog:** the duplicated env-fallback blocks it replaces. Current shape at `internal/harness/commit.go:56-63` (identical block at `pkg/git/integrate.go:85-92`):

```go
botName := os.Getenv("TIDE_BOT_NAME")
if botName == "" {
    botName = "TIDE Bot"
}
botEmail := os.Getenv("TIDE_BOT_EMAIL")
if botEmail == "" {
    botEmail = "tide-bot@tideproject.k8s"
}
```

New file consolidates: exported consts `DefaultAgentName = "TIDE Agent"`, `DefaultAgentEmail = "tide-agent@tideproject.k8s"`, `EnvAgentName = "TIDE_AGENT_NAME"`, `EnvAgentEmail = "TIDE_AGENT_EMAIL"`, plus `AgentIdentity() (name, email string)`. Comment style: decision-tagged doc comments (`(D-04)`, `(W11)`) matching repo convention.

**Import direction:** `pkg/git` is a leaf — importable by harness, controller, podjob, cmd/tide-push without cycles. Run `make lint` (import firewalls) after wiring.

### `internal/harness/commit.go` + `pkg/git/integrate.go` (rename sites)

Replace the 8-line env block with `pkggit.AgentIdentity()`; the git CLI invocation pattern stays exactly as-is (`-c` flags precede `-C`, per the existing comment at commit.go:65):

```go
// commit.go:66-71 — keep this shape, only the name/email source changes
commitArgs := []string{
    "-c", "user.name=" + botName,
    "-c", "user.email=" + botEmail,
    "-C", worktreeDir,
    "commit", "-m", msg,
}
```

Update the doc comments at commit.go:32-34 and integrate.go:55-59 (both currently cite `tideBotSignature()` / `TIDE_BOT_*` — stale after this phase).

### `cmd/tide-push/main.go` (hardcoded identity removal)

**Analog:** `tideBotSignature()` at lines 128-137 — the function being replaced:

```go
// tideBotSignature returns the fixed TIDE-bot author signature used for
// every boundary commit (W11 — name+email are stable across runs; only
// the timestamp varies).
func tideBotSignature() object.Signature {
    return object.Signature{
        Name:  "tide-bot",
        Email: "tide-bot@tideproject.k8s",
        When:  time.Now(),
    }
}
```

Replacement builds `object.Signature` from `pkggit.AgentIdentity()` + `time.Now()`. **Preserve the W11 comment contract verbatim in intent** ("name+email stable across runs; only timestamp varies" — D-05). Set only `Author` (go-git copies to Committer — Pitfall 8; do NOT set a distinct Committer). Callsite at line ~521; package doc at lines 32-34 also mentions the bot identity — update.

### `api/v1alpha2/project_types.go` + `api/v1alpha1/project_types.go` (CRD fields)

**Analog:** `GitConfig` at v1alpha2/project_types.go:205-224. Copy the optional-field + Pattern-marker style from `LeaksConfigRef` and `RepoURL`:

```go
// RepoURL ... (line 211 — the Pattern precedent)
// +kubebuilder:validation:Pattern=`^(https?://|git@).+`
RepoURL string `json:"repoURL"`

// LeaksConfigRef ... (lines 219-223 — the +optional/omitempty precedent)
// +optional
LeaksConfigRef string `json:"leaksConfigRef,omitempty"`
```

New fields (both API versions, identical — type parity, NO conversion webhook exists):

```go
// +kubebuilder:validation:MaxLength=100
// +kubebuilder:validation:Pattern=`^[^<>\r\n]+$`
// +optional
AgentName string `json:"agentName,omitempty"`

// +kubebuilder:validation:MaxLength=254
// +kubebuilder:validation:Pattern=`^[^<>@\s]+@[^<>@\s]+$`
// +optional
AgentEmail string `json:"agentEmail,omitempty"`
```

Pattern markers, not CEL — matches the `RepoURL` precedent (discretion question resolved in RESEARCH.md). Regen: `make generate manifests helm-crds`. Note `ProjectSpec.Git` is `*GitConfig` (nil-able) — resolver must nil-check (Pitfall 7). **Check Phase 35's landed `baseRef` first** and slot into the same dual-version shape.

### `internal/controller/dispatch_helpers.go` (resolver + ProviderDefaults extension)

**Analog 1 — struct extension:** `ProviderDefaults` (lines 114-123):

```go
type ProviderDefaults struct {
    // Image is the default subagent image ref. Empty string means
    // "no Helm default" ...
    Image string
    // Models maps level→model. ...
    Models map[string]string
}
```

Add `AgentName`, `AgentEmail string` fields with the same "empty = no Helm default" comment convention.

**Analog 2 — the precedence walk:** `resolveImage` (lines 269-291):

```go
// resolveImage walks Project.Spec.Subagent precedence chain for the given
// level, returning the resolved subagent container image reference.
//
//	Levels.<level>.Image → Spec.Subagent.Image → helmDefaults.Image → ""
func resolveImage(project *tideprojectv1alpha2.Project, level string, helmDefaults ProviderDefaults) string {
    ...
    switch {
    case levelCfg != nil && levelCfg.Image != "":
        return levelCfg.Image
    case project != nil && project.Spec.Subagent.Image != "":
        return project.Spec.Subagent.Image
    default:
        return helmDefaults.Image
    }
}
```

New `resolveAgentIdentity(project, helmDefaults) (name, email string)`: start from `pkggit.DefaultAgentName/Email`, override with non-empty `helmDefaults.AgentName/Email`, then non-empty `project.Spec.Git.AgentName/Email` (nil-check `project` and `project.Spec.Git`). Doc-comment the D-03 chain the same way resolveImage documents its chain.

### `internal/controller/push_helpers.go` (PushOptions + buildPushJob Env)

**Analog 1:** `PushOptions` struct (lines 38-73) — add `AgentName`, `AgentEmail string` fields with decision-tagged comments matching e.g. `IntegrateTaskBranches` (lines 68-72).

**Analog 2:** the push container (lines 234-259) currently has **`EnvFrom` only, no `Env`**:

```go
Containers: []corev1.Container{
    {
        Name:  pushContainerName,
        Image: opts.TidePushImage,
        Args:  args,
        EnvFrom: []corev1.EnvFromSource{
            {
                SecretRef: &corev1.SecretEnvSource{
                    LocalObjectReference: corev1.LocalObjectReference{
                        Name: project.Spec.Git.CredsSecretRef,
                    },
                },
            },
        },
        ...
```

Add an `Env: []corev1.EnvVar{{Name: pkggit.EnvAgentName, Value: opts.AgentName}, {Name: pkggit.EnvAgentEmail, Value: opts.AgentEmail}}` block before `EnvFrom`. Unconditional injection (resolved value is never empty — compiled default backstops; the conditional pricing pattern does NOT apply here).

### `internal/controller/boundary_push.go` (threading)

**Analog:** the two `PushOptions{` construction sites at lines 116 and 168 (`triggerBoundaryPush` — sole choke point, `project` in scope, called from Milestone/Phase/Plan reconcilers which carry `HelmProviderDefaults`). Call `resolveAgentIdentity` once, stamp both fields at both sites.

### `internal/dispatch/podjob/jobspec.go` (BuildOptions + subagentEnv)

**Analog:** `subagentEnv` (lines 357-362) + the pricing transport (lines 370-378):

```go
subagentEnv := []corev1.EnvVar{
    {Name: "TIDE_TASK_UID", Value: envelopeUID},
    // D-C1: signed token replaces the real API key in the subagent process.
    {Name: "ANTHROPIC_API_KEY", Value: opts.SignedToken},
    {Name: "ANTHROPIC_AUTH_TOKEN", Value: opts.SignedToken},
}
...
// D-02: stamp TIDE_PRICING_OVERRIDES_JSON only when the operator has configured
if opts.PricingOverridesJSON != "" {
    subagentEnv = append(subagentEnv, corev1.EnvVar{...})
}
```

Add `AgentName`/`AgentEmail` to `BuildOptions` (struct at line 80) and append the two vars **unconditionally** to the base `subagentEnv` slice (unlike pricing — resolved value never empty). ~7 `BuildOptions` construction sites must populate them: task_controller.go:762 and :1414, project_controller.go:1245, milestone_controller.go:486, phase/plan planner paths, and backend.go:278.

### `internal/dispatch/podjob/backend.go` (fixture inline mirror)

**Analog:** the inline image walk (lines 267-277) — copy this comment-and-walk shape exactly for identity:

```go
// Inline image precedence walk — mirrors controller.resolveImage for the
// task level. PodJobBackend is fixture-only but must stay consistent with
// the chain: Levels.Task.Image → Spec.Subagent.Image → b.SubagentImage.
resolvedImage := b.SubagentImage
if project.Spec.Subagent.Image != "" {
    resolvedImage = project.Spec.Subagent.Image
}
...
```

podjob must NOT import `internal/controller` (cycle); mirror the walk inline against `pkggit.DefaultAgent*` and new backend fields.

### `cmd/manager/env.go` (chart env → ProviderDefaults)

**Analog:** `envOrDefault` (lines 37-42) + `tideHelmProviderDefaults` (lines 96-101):

```go
// envOrDefault returns the value of the env var named key, or fallback when
// the env var is unset or empty. Empty-string is treated as unset so a Helm
// value left at its zero default (e.g. `value: ""`) cleanly falls through to
// the binary's compile-time default rather than overriding it with "".
func envOrDefault(key, fallback string) string { ... }

func tideHelmProviderDefaults(claudeImage string) controller.ProviderDefaults {
    return controller.ProviderDefaults{
        Image:  claudeImage,
        Models: resolvePerLevelModels(),
    }
}
```

Add `AgentName: envOrDefault("TIDE_AGENT_NAME", "")` / `AgentEmail: envOrDefault("TIDE_AGENT_EMAIL", "")` — empty means "chart tier unset", falling through to compiled default at resolve time (documented empty-is-unset convention). Extend `cmd/manager/env_test.go`.

### `hack/helm/augment-tide-chart.sh` + `hack/helm/tide-values.yaml` (chart wiring)

**Analog:** the ENV3 marker-guarded heredoc block (lines 228-283). Copy the mechanism — new marker (e.g. `# phase36-agent-env-injected`), block inserted before the same `envFrom:` anchor:

```python
ENV3_MARKER = "# phase3-env-injected"
ENV3_BLOCK = """        - name: TIDE_PUSH_IMAGE
          value: "{{ .Values.images.tidePush.repository }}:..."
        ...
        # phase3-env-injected
"""
if ENV3_MARKER not in content:
    content = re.sub(r'(\n        envFrom:\n)', ...)
```

New rendered env:

```yaml
- name: TIDE_AGENT_NAME
  value: "{{ .Values.agent.name }}"
- name: TIDE_AGENT_EMAIL
  value: "{{ .Values.agent.email }}"
```

Values block in `hack/helm/tide-values.yaml` (`agent.name: ""` / `agent.email: ""` with precedence comment). **NEVER edit `charts/tide/` directly** — run `make helm-controller` and commit regenerated output together; `make verify-chart-reproducible` gates drift.

### `hack/helm/tide-chart.yaml` (version bump — CONDITIONAL)

Currently `1.0.6`. D-06 batches with Phase 35 into ONE bump: the task must read the file at execution time and bump only if still `1.0.6` (both `version` and `appVersion`; also the CRD subchart `hack/helm/tide-crds-chart.yaml` since CRD schema changes).

### `docs/project-authoring.md`

**Analog:** the existing GitConfig table (~line 49). Add `agentName`/`agentEmail` rows + the routable-email note (future signing requires the email to match a verified machine-account email).

## Shared Patterns

### Env-presence builder tests
**Source:** `internal/dispatch/podjob/jobspec_test.go:828-870` (`TestBuildJobSpec_PricingOverridesJSON_PresentWhenSet`)
**Apply to:** new tests for BOTH job builders (subagent Job executor+planner kinds, AND push Job — Pitfall 3: one-sided testing masks missed injection):

```go
job := BuildJobSpec(opts)
subagent := job.Spec.Template.Spec.Containers[0]
var found bool
for _, e := range subagent.Env {
    if e.Name == "TIDE_PRICING_OVERRIDES_JSON" { found = true; ... }
}
```

Use a **non-default** configured identity in precedence tests — the compiled-default fallback silently masks missed surfaces.

### Pinned-identity test updates
**Sources:** `cmd/tide-push/main_test.go:869-873` (asserts `tide-bot`), `internal/harness/commit_test.go:98-109` (uses `TIDE_BOT_*`). Update in the same task as the site change. Post-phase grep gate: `grep -rn 'tide-bot\|TIDE Bot\|TIDE_BOT' --include='*.go'` returns hits only in `cmd/tide-demo-init` (deliberately distinct seeding identity — DO NOT rename, see main.go:135) — nothing else.

### Decision-tagged comments
All new code carries decision refs in doc comments — `(D-03)`, `(D-04)`, `(W11)`, `SIGN-01` — matching the dense, decision-referenced comment style visible in every analog above.

## No Analog Found

None — every file has an exact in-repo precedent. This is a wiring/rename phase; no invented mechanisms.

## Metadata

**Analog search scope:** `internal/{controller,harness,dispatch/podjob}`, `pkg/git`, `cmd/{tide-push,manager}`, `api/v1alpha{1,2}`, `hack/helm`, test files
**Files scanned:** 12 read directly this session (targeted ranges); RESEARCH.md's repo-wide grep findings relied on for negative claims (nothing sets `TIDE_BOT_*`)
**Pattern extraction date:** 2026-07-04
