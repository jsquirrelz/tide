# Phase 3: Up-Stack Reconcilers, Git Integration, Real Subagent, Resumption — Pattern Map

**Mapped:** 2026-05-15
**Files analyzed:** 29 new/extended files
**Analogs found:** 28 / 29 (one — `pkg/git` HTTPS+PAT — has no in-repo analog; RESEARCH.md library patterns drive that one)

Phase 3 is a fanout, not a greenfield rebuild — every new file copies a Phase 1 or Phase 2 shape. The dispatch-bump (`pkg/dispatch`), reconciler-body fill (up-stack controllers), push-Job + clone-Job (mirrors `tide-init`), `wait-for-signal` stub mode (extends Phase 2 D-F3), Claude subagent (extends `cmd/stub-subagent` shim shape), and chaos-resume test (extends `internal/controller/leader_election_test.go` + `test/integration/kind/wave_test.go`) all have direct analogs to copy from. Per-file pattern assignments below cite the exact line ranges to mirror.

---

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `pkg/dispatch/envelope.go` (EXTEND) | model | transform | `pkg/dispatch/envelope.go` (Phase 2) | exact — self-extension |
| `pkg/dispatch/childcrd.go` (NEW) | model | transform | `pkg/dispatch/envelope.go` Caps/Usage struct shape | exact |
| `pkg/dispatch/provider.go` (NEW) | model | transform | `pkg/dispatch/envelope.go` Dev struct shape | exact |
| `pkg/git/` package (NEW) | utility | request-response | RESEARCH.md Patterns 1–3 (go-git/v5) — no in-repo analog | role-match (none in-repo) |
| `internal/gitleaks/scanner.go` (NEW) | utility | transform | `internal/harness/redact/redact.go` + RESEARCH.md Pattern 4 | role-match |
| `cmd/tide-push/main.go` (NEW) | controller (job entrypoint) | batch | `cmd/stub-subagent/main.go` | exact — same shim shape |
| `images/tide-push/Dockerfile` (NEW) | config | build | `images/stub-subagent/Dockerfile` | exact |
| `internal/subagent/anthropic/subagent.go` (NEW) | service (provider) | streaming | `internal/harness/harness.go` `Runtime` impl + `cmd/stub-subagent/main.go` dispatch | role-match |
| `internal/subagent/anthropic/stream_parser.go` (NEW) | utility | streaming | `internal/harness/envelope_io.go` `json.Decoder` pattern | role-match |
| `internal/subagent/common/stream_reader.go` (NEW) | utility | streaming | `internal/harness/envelope_io.go` line-by-line JSON read | role-match |
| `internal/subagent/common/prompt_templates.go` (NEW) | utility | transform | `internal/config/*` (go:embed config loader) | role-match |
| `cmd/claude-subagent/main.go` (NEW) | controller (job entrypoint) | request-response | `cmd/stub-subagent/main.go` shim | exact |
| `images/claude-subagent/Dockerfile` (NEW) | config | build | `images/stub-subagent/Dockerfile` | role-match (different base image) |
| `cmd/stub-subagent/main.go` (EXTEND wait-for-signal) | controller | event-driven | `cmd/stub-subagent/main.go` `dispatchHang` (lines 240–250) | exact — self-extension |
| `internal/controller/milestone_controller.go` (EXTEND body) | controller | request-response | `internal/controller/task_controller.go` `reconcileDispatch` (lines 192–425) | exact — sibling-reconciler |
| `internal/controller/phase_controller.go` (EXTEND body) | controller | request-response | `internal/controller/task_controller.go` `reconcileDispatch` | exact |
| `internal/controller/plan_controller.go` (EXTEND planner dispatch) | controller | request-response | `internal/controller/task_controller.go` `reconcileDispatch` (Phase 3 adds dispatch BEFORE Wave materialization) | exact |
| `internal/controller/project_controller.go` (EXTEND clone+push) | controller | request-response | `internal/controller/project_controller.go` `ensureInitJob` + `handleInitJobCompletion` (lines 207–259) | exact — self-extension |
| `internal/controller/dispatch_helpers.go` (NEW) | utility (controller helper) | transform | `internal/controller/task_controller.go` `buildEnvelopeIn`/`ensureJob` (lines 638–727) | exact |
| `internal/controller/push_helpers.go` (NEW) | utility (controller helper) | transform | `internal/controller/project_controller.go` `buildInitJob` (lines 335–396) | exact |
| `api/v1alpha1/project_types.go` (EXTEND) | model | transform | `api/v1alpha1/project_types.go` (Phase 1/2 `ProviderConfig`, `BudgetStatus`, `SecretRefs` shape) | exact — self-extension |
| `api/v1alpha1/shared_types.go` (EXTEND conditions) | model | transform | `api/v1alpha1/shared_types.go` (existing `Condition*`/`Reason*` constants) | exact |
| `test/integration/kind/chaos_resume_test.go` (NEW) | test | event-driven | `internal/controller/leader_election_test.go` + `test/integration/kind/wave_test.go` | exact (composes both) |
| `test/integration/kind/push_lease_test.go` (NEW) | test | request-response | `test/integration/kind/wave_test.go` | exact |
| `test/integration/kind/up_stack_dispatch_test.go` (NEW) | test | request-response | `test/integration/kind/wave_test.go` (lines 39–91) | exact |
| `cmd/manager/main.go` (EXTEND wiring) | config | request-response | `cmd/manager/main.go` Phase 2 flag-+-Helm wiring (lines 86–113) | exact — self-extension |
| `charts/tide/values.yaml` (EXTEND) | config | transform | `charts/tide/values.yaml` `images.{stubSubagent,credProxy}` block (lines 117–133) | exact |
| `charts/tide/templates/push-rbac.yaml` (NEW) | config | request-response | `charts/tide/templates/serviceaccount-subagent.yaml` | exact |
| `charts/tide/templates/projects-pvc.yaml` (UNCHANGED — referenced for clone Job mount) | config | — | self | — |

---

## Pattern Assignments

### `pkg/dispatch/envelope.go` (EXTEND — additive)

**Analog:** itself at HEAD (Phase 2). Phase 3 adds three field clusters; the existing `EnvelopeIn` / `EnvelopeOut` field comment style and JSON tag convention are the load-bearing patterns.

**Existing struct shape to mirror** (`pkg/dispatch/envelope.go:23-80`):
```go
type EnvelopeIn struct {
    APIVersion string `json:"apiVersion"`
    Kind       string `json:"kind"`
    TaskUID    string `json:"taskUID"`
    Role       string `json:"role"`
    Level      string `json:"level"`
    // ...
    Caps  Caps `json:"caps"`
    Dev   *Dev `json:"dev,omitempty"`
}
```

**Phase 3 additions follow this convention** — every new field MUST:
1. Have a one-paragraph godoc above it referencing the D-code (D-A1, D-C3, etc.).
2. Use `json:"..."` lower-camelCase tags matching existing fields.
3. Optional fields use pointer + `,omitempty` (mirrors `Dev *Dev`).

**Schema-rev rejection pattern** (`pkg/dispatch/envelope.go:184-192`) — Phase 3's additions stay under `APIVersionV1Alpha1` and remain **additive only**; `ValidateAPIVersionKind` continues to gate consumers. Do not bump the apiVersion constant.

---

### `pkg/dispatch/childcrd.go` (NEW)

**Analog:** `pkg/dispatch/envelope.go` `Caps` struct (lines 124–139) for the pure-data + godoc style.

**Pattern to copy** (`pkg/dispatch/envelope.go:121-139`):
```go
// Caps carries per-task resource limits (HARN-02). The orchestrator derives
// these from Project.Spec.budget and injects them into every EnvelopeIn so
// the in-pod harness can enforce them without querying the K8s API.
type Caps struct {
    WallClockSeconds int   `json:"wallClockSeconds"`
    Iterations       int   `json:"iterations"`
    InputTokens      int64 `json:"inputTokens"`
    OutputTokens     int64 `json:"outputTokens"`
}
```

**Phase 3 shape (per D-A1):**
```go
type ChildCRDSpec struct {
    Kind string                `json:"kind"`               // "Milestone" | "Phase" | "Plan" | "Task" | "Wave"
    Name string                `json:"name"`               // child CRD name
    Spec runtime.RawExtension  `json:"spec"`               // raw JSON for the child CRD's Spec
}
```

**Note:** `runtime.RawExtension` is the K8s-idiomatic typed-but-deferred-decode escape hatch — keeps `pkg/dispatch` from importing `api/v1alpha1` (which would invert the dependency arrow).

---

### `pkg/dispatch/provider.go` (NEW)

**Analog:** `pkg/dispatch/envelope.go` `Dev` struct (lines 162–175) — small typed sub-struct embedded in `EnvelopeIn`.

**Pattern to copy** (`pkg/dispatch/envelope.go:162-175`):
```go
type Dev struct {
    TestMode string `json:"testMode,omitempty"`
}
```

**Phase 3 shape (per D-C3):**
```go
type ProviderSpec struct {
    Vendor string            `json:"vendor"`           // "anthropic" | "openai" | ...
    Model  string            `json:"model"`            // "claude-opus-4-7" | ...
    Params map[string]string `json:"params,omitempty"` // per-vendor tuning passthrough
}
```

**Add to `EnvelopeIn`** as `Provider ProviderSpec \`json:"provider"\`` (NOT pointer — every dispatch carries a provider).

---

### `pkg/git/` package (NEW)

**Analog:** No in-repo analog. Use RESEARCH.md Patterns 1–3 (verified go-git/v5 v5.19.0 API).

**Imports + auth pattern** (RESEARCH.md Pattern 1, lines 380–397):
```go
import (
    git "github.com/go-git/go-git/v5"
    "github.com/go-git/go-git/v5/config"
    "github.com/go-git/go-git/v5/plumbing"
    "github.com/go-git/go-git/v5/plumbing/transport/http"
)

func Clone(ctx context.Context, repoURL, destDir, pat string) (*git.Repository, error) {
    return git.PlainCloneContext(ctx, destDir, true /* bare */, &git.CloneOptions{
        URL: repoURL,
        Auth: &http.BasicAuth{
            Username: "x-access-token",
            Password: pat,
        },
    })
}
```

**Push with `ForceWithLease` pattern** (RESEARCH.md Pattern 3, lines 436–460):
```go
func Push(ctx context.Context, repo *git.Repository, branch, lastPushedSHA, pat string) error {
    refSpec := config.RefSpec(fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch))
    opts := &git.PushOptions{
        RemoteName: "origin",
        RefSpecs:   []config.RefSpec{refSpec},
        Auth:       &http.BasicAuth{Username: "x-access-token", Password: pat},
    }
    if lastPushedSHA != "" {  // first push: omit lease entirely (Pitfall 2)
        opts.ForceWithLease = &git.ForceWithLease{
            RefName: plumbing.NewBranchReferenceName(branch),
            Hash:    plumbing.NewHash(lastPushedSHA),
        }
    }
    return repo.PushContext(ctx, opts)
}
```

**Package conventions from existing TIDE packages:**
- One file per exported function (mirrors `pkg/dispatch/{envelope,subagent,errors}.go` split).
- `doc.go` describes the package and pins HTTPS+PAT as default (SSH deferred per ART-05).
- No LLM-SDK imports — `cmd/tide-lint`'s `providerfirewall` analyzer should extend its deny-list to `pkg/git/...` (Phase 3 hardening, per Claude's Discretion).

---

### `internal/gitleaks/scanner.go` (NEW)

**Analog:** `internal/harness/redact/redact.go` — same shape (single-binary library wrapping a third-party scanner).

**Pattern to copy** (RESEARCH.md Pattern 4, lines 470–481):
```go
import "github.com/zricethezav/gitleaks/v8/detect"

func ScanDiff(diffContent string) (bool, []detect.Finding, error) {
    detector, err := detect.NewDetectorDefaultConfig()
    if err != nil {
        return false, nil, err
    }
    findings := detector.DetectString(diffContent)
    return len(findings) > 0, findings, nil
}
```

**Config override pattern (D-B3):** embed default rules via `go:embed default_rules.toml` (mirrors how `internal/config/*` would compile defaults in); per-Project override via ConfigMap mounted at `/etc/tide/gitleaks-config.toml`. ConfigMap content uses gitleaks' `[extend] useDefault = true` mechanism so embedded + custom rules compose.

---

### `cmd/tide-push/main.go` (NEW)

**Analog:** `cmd/stub-subagent/main.go` — same shim shape (single main, parses flags/env, executes one bounded job, exits with structured code).

**Top-of-file structure to copy** (`cmd/stub-subagent/main.go:30-70`):
```go
package main

import (
    "context"
    "flag"
    "os"
    "os/signal"
    "syscall"
    // ...
)

func main() {
    fs := flag.NewFlagSet("tide-push", flag.ExitOnError)
    // flags: --workspace, --branch, --last-pushed-sha, --leaks-config
    if err := fs.Parse(os.Args[1:]); err != nil { /* ... */ }

    ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
    defer cancel()

    os.Exit(run(ctx, /* config */, os.Stdout, os.Stderr))
}
```

**Exit-code contract to mirror** (`cmd/stub-subagent/main.go:6-11`):
- `0` — push succeeded; emits `{headSHA, branch}` envelope on PVC.
- `1` — gitleaks finding OR push lease failure; structured reason on stdout.
- `2` — envelope error or invariant violation (bad PVC layout, missing creds).

**Envelope-shaped output (D-B6 lastPushedSHA writeback):** `tide-push` writes a small `push-result.json` to `/workspace/envelopes/push/{project-uid}.json` — the reconciler observes Job completion and reads this file to patch `Project.Status.git.lastPushedSHA`. Mirrors Phase 2 D-A2 envelope-on-PVC pattern.

**Credentials:** `envFrom: secretRef: {name: <Project.Spec.git.credsSecretRef>}` on the push Job's Pod — PAT lands in env as e.g. `GIT_PAT`. **Push Job is the ONLY pod that touches git creds** (mirrors Phase 2 D-C4 credproxy-sidecar-only-touches-LLM-key isolation).

---

### `images/tide-push/Dockerfile` (NEW)

**Analog:** `images/stub-subagent/Dockerfile` (lines 1–37) — pure-Go static binary on `gcr.io/distroless/static:nonroot`.

**Pattern to copy verbatim** (`images/stub-subagent/Dockerfile:7-37`):
```dockerfile
FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY pkg/dispatch/ pkg/dispatch/
COPY pkg/git/ pkg/git/
COPY internal/gitleaks/ internal/gitleaks/
COPY cmd/tide-push/ cmd/tide-push/
RUN CGO_ENABLED=0 GOOS=linux go build \
      -ldflags="-s -w" \
      -o /out/tide-push \
      ./cmd/tide-push

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /out/tide-push /usr/local/bin/tide-push
USER 1000
ENTRYPOINT ["/usr/local/bin/tide-push"]
```

**Note:** `go-git/v5` is pure-Go and works on `distroless/static` — no `/bin/git` shell-out, no CA bundle (go-git uses Go's `crypto/tls`). gitleaks v8 is also pure-Go.

---

### `internal/subagent/anthropic/subagent.go` (NEW)

**Analog:** `internal/harness/harness.go` `Runtime` interface impl + `cmd/stub-subagent/main.go` `dispatchSuccess` (lines 173–211) for the envelope-construction shape.

**Interface contract** (`pkg/dispatch/subagent.go:17-26` — preserved verbatim per D-A3):
```go
type Subagent interface {
    Run(ctx context.Context, in EnvelopeIn) (EnvelopeOut, error)
}
```

**Runtime impl shape** (`internal/harness/harness.go:27-29`):
```go
type Runtime interface {
    Execute(ctx context.Context, in pkgdispatch.EnvelopeIn, stdout, stderr io.Writer) (pkgdispatch.Usage, error)
}
```

**Claude invocation pattern** (RESEARCH.md Pattern 6, lines 580–607):
```go
func (a *Anthropic) Execute(ctx context.Context, in pkgdispatch.EnvelopeIn, stdout, stderr io.Writer) (pkgdispatch.Usage, error) {
    if in.Provider.Vendor != "anthropic" {
        return pkgdispatch.Usage{}, fmt.Errorf("anthropic subagent: refusing vendor=%q", in.Provider.Vendor)
    }
    cmd := exec.CommandContext(ctx,
        "claude", "-p", in.Prompt,
        "--model", in.Provider.Model,
        "--output-format", "stream-json",
        "--verbose",
        "--include-partial-messages",
        "--bare",  // skip auto-discovery — anti-pattern guardrail
    )
    cmd.Env = append(cmd.Environ(),
        "ANTHROPIC_BASE_URL="+in.ProxyEndpoint,
        "ANTHROPIC_API_KEY="+in.SignedToken,
        "NODE_EXTRA_CA_CERTS=/etc/tide/proxy/ca.crt",
    )
    // Tee stream-json to events.jsonl + parse usage + result.
    // ...
}
```

**Fail-fast vendor check:** mirrors `pkg/dispatch/envelope.go:184-192` `ValidateAPIVersionKind` style — refuse to proceed if envelope and image disagree.

---

### `internal/subagent/anthropic/stream_parser.go` (NEW)

**Analog:** `internal/harness/envelope_io.go` (lines 20–37) — `json.Decoder` pattern + structured error wrapping.

**stream-json parse pattern** (RESEARCH.md Pattern 5, lines 545–569):
```go
func ParseStream(r io.Reader, rawSink io.Writer) (usage pkgdispatch.Usage, resultText string, err error) {
    scanner := bufio.NewScanner(r)
    scanner.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024) // 16MB line budget
    for scanner.Scan() {
        line := scanner.Bytes()
        if _, werr := rawSink.Write(append(line, '\n')); werr != nil {
            return usage, resultText, werr
        }
        var ev streamEvent
        if jerr := json.Unmarshal(line, &ev); jerr != nil {
            continue // tolerate non-JSON lines (defensive)
        }
        if ev.Type == "result" {
            resultText = ev.Result
            if ev.Usage != nil {
                usage.InputTokens = ev.Usage.InputTokens
                usage.OutputTokens = ev.Usage.OutputTokens
                // Phase 3 new fields (per D-C5):
                // usage.CacheReadTokens = ev.Usage.CacheReadInputTokens
                // usage.CacheCreationTokens = ev.Usage.CacheCreationInputTokens
            }
        }
    }
    return usage, resultText, scanner.Err()
}
```

**Field-naming convention** (per RESEARCH.md §"stream-json Parse" notes): top-level stream-json uses snake_case (`input_tokens`); per-model nested uses camelCase. Phase 3 only parses top-level — Phase 4 OpenInference parsing reads the raw `events.jsonl` for the rest.

---

### `internal/subagent/common/stream_reader.go` (NEW)

**Analog:** `internal/harness/envelope_io.go:20-37` for the `os.Open` → `json.Decoder` → wrapped-error pattern.

**Pattern to copy** (`internal/harness/envelope_io.go:20-37`):
```go
func ReadEnvelopeIn(path string) (pkgdispatch.EnvelopeIn, error) {
    f, err := os.Open(path)
    if err != nil {
        return pkgdispatch.EnvelopeIn{}, fmt.Errorf("harness: open envelope %q: %w", path, err)
    }
    defer f.Close()

    var env pkgdispatch.EnvelopeIn
    if err := json.NewDecoder(f).Decode(&env); err != nil {
        return pkgdispatch.EnvelopeIn{}, fmt.Errorf("harness: decode envelope %q: %w", path, err)
    }
    if err := pkgdispatch.ValidateAPIVersionKind(env.APIVersion, env.Kind, pkgdispatch.KindTaskEnvelopeIn); err != nil {
        return pkgdispatch.EnvelopeIn{}, fmt.Errorf("harness: validate envelope %q: %w", path, err)
    }
    return env, nil
}
```

**common/stream_reader.go** is the JSONL line-by-line reader (used by `anthropic`, future `openai`/`google` providers). Keep it provider-agnostic — accept `io.Reader` and a typed event handler callback; don't inline Anthropic-specific event shapes.

---

### `internal/subagent/common/prompt_templates.go` (NEW)

**Analog:** the `go:embed` pattern already used in `internal/credproxy/` (cert templates). The TIDE convention is `//go:embed templates/*.tmpl` declared at package scope with a single exported `Load(name string)` accessor.

**Pattern shape (drawn from Go idiom; no exact in-repo analog yet but consistent with `internal/config` style):**
```go
package common

import (
    _ "embed"
    "embed"
    "fmt"
    "text/template"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

func LoadPromptTemplate(role, level string) (*template.Template, error) {
    name := fmt.Sprintf("templates/%s_%s.tmpl", level, role)
    return template.ParseFS(templateFS, name)
}
```

**Template files to ship (D-A2 × 2):**
- `templates/milestone_planner.tmpl`
- `templates/phase_planner.tmpl`
- `templates/plan_planner.tmpl`
- `templates/task_executor.tmpl` (Phase 2 lived without — Phase 3 wires it through the same loader)

**Anti-pattern guardrail:** **Don't vendor GSD Markdown.** Prompts are compiled into the Go binary, not read from disk or repo at runtime. CLAUDE.md and STACK.md both lock this.

---

### `cmd/claude-subagent/main.go` (NEW)

**Analog:** `cmd/stub-subagent/main.go` (the WHOLE shim shape — ~50 LOC of envelope-load → instantiate-Subagent → run-harness → write-out.json).

**Shim shape to copy** (`cmd/stub-subagent/main.go:74-126` is the run() function — Phase 3's claude-subagent replaces the switch-on-testMode with a single `anthropic.New().Run()` call):
```go
func run(ctx context.Context, envelopePath string, stdout, stderr io.Writer) int {
    outPath := filepath.Join(filepath.Dir(envelopePath), "out.json")
    env, err := loadEnvelope(envelopePath)
    if err != nil {
        // best-effort failure envelope, exit 2 (mirrors stub line 81-92)
    }
    sa := anthropic.New(/* config */)
    out, runErr := sa.Run(ctx, env)
    if runErr != nil {
        // wrap as failure envelope
    }
    if err := writeEnvelope(outPath, out); err != nil {
        return 2
    }
    return out.ExitCode
}
```

**loadEnvelope/writeEnvelope helpers** (`cmd/stub-subagent/main.go:130-161`): copy verbatim. Same termination-message-write hook (`writeTerminationMessage`) — Phase 2 D-C-fallback PodStatusEnvelopeReader path stays live for the claude image.

**Real Claude image MUST ignore `env.Dev` entirely** — `cmd/stub-subagent/main.go:27-29` documents this; Phase 3's claude-subagent honors it (no testMode switching).

---

### `images/claude-subagent/Dockerfile` (NEW)

**Analog:** `images/stub-subagent/Dockerfile` for the Go-build stage; the runtime stage differs because Claude Code CLI requires Node.js.

**Pattern to mirror** (`images/stub-subagent/Dockerfile:7-23` for builder stage):
```dockerfile
FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY pkg/dispatch/ pkg/dispatch/
COPY internal/harness/ internal/harness/
COPY internal/subagent/ internal/subagent/
COPY cmd/claude-subagent/ cmd/claude-subagent/
RUN CGO_ENABLED=0 GOOS=linux go build \
      -ldflags="-s -w" \
      -o /out/claude-subagent \
      ./cmd/claude-subagent
```

**Runtime stage (different from stub — needs Node):**
```dockerfile
FROM node:22-slim
RUN npm install -g @anthropic-ai/claude-code@2.1.142
COPY --from=builder /out/claude-subagent /usr/local/bin/claude-subagent
USER 1000
ENTRYPOINT ["/usr/local/bin/claude-subagent"]
```

**Version pin (D-claude-cli):** `@anthropic-ai/claude-code@2.1.142` — RESEARCH.md flags v2.1.139 as STACK.md floor; planner picks the exact pin. **Never use `npm install -g @anthropic-ai/claude-code` without `@<version>`** (Pitfall 3).

---

### `cmd/stub-subagent/main.go` (EXTEND — `wait-for-signal` mode per D-D3)

**Analog:** itself — specifically `dispatchHang` (lines 240–250), which is the closest existing "block until external event" pattern.

**Existing hang pattern to extend** (`cmd/stub-subagent/main.go:240-250`):
```go
func dispatchHang(ctx context.Context) int {
    for {
        select {
        case <-ctx.Done():
            return 0
        case <-time.After(time.Hour):
            // Keep looping — hang forever until killed.
        }
    }
}
```

**Phase 3 `dispatchWaitForSignal` extension shape:**
```go
func dispatchWaitForSignal(ctx context.Context, env pkgdispatch.EnvelopeIn, outPath string, stderr io.Writer) int {
    signalPath := filepath.Join("/workspace/envelopes", env.TaskUID, "release")
    ticker := time.NewTicker(500 * time.Millisecond)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return 0
        case <-ticker.C:
            if _, err := os.Stat(signalPath); err == nil {
                // signal arrived — write canned success envelope, mirror dispatchSuccess
                return dispatchSuccess(ctx, env, outPath, stderr)
            }
        }
    }
}
```

**Wire into switch statement** (`cmd/stub-subagent/main.go:100-114`):
```go
case "wait-for-signal":
    return dispatchWaitForSignal(ctx, env, outPath, stderr)
```

---

### `internal/controller/milestone_controller.go` (EXTEND — body fill)

**Analog:** `internal/controller/task_controller.go` `reconcileDispatch` (lines 192–425) — the 12-step dispatch body is the structural template.

**Reconciler-body fill seam** (existing `milestone_controller.go:113-116`):
```go
// 5. Phase 1: dispatcher seam nil-guarded for Phase 2 body fill (REQ-SUB-01).
if r.Dispatcher != nil {
    // Phase 2 fills.    ← Phase 3 fills this
}
```

**Pattern to copy from `task_controller.go:192-425`:**

1. **Terminal short-circuit** (lines 195–198):
   ```go
   if milestone.Status.Phase == "Succeeded" || milestone.Status.Phase == "Failed" {
       return ctrl.Result{}, nil
   }
   ```

2. **On Running: check Job terminal state** (lines 200–218) — for milestone, the Job is the **planner** Job (`tide-milestone-<milestone-uid>-<attempt>`); on completion, parse `EnvelopeOut.childCRDs` and materialize `Phase` CRDs (then dispatch push Job at the level boundary).

3. **Resolve owning Project** (lines 220–224) — Phase 3 milestone reconciler walks `Milestone.Spec.ProjectRef`.

4. **Acquire `plannerPool`** (NEW per D-A4 — pattern: same `pool.Pool.Acquire` shape `task_controller.go` uses for `executorPool`):
   ```go
   slot, err := r.PlannerPool.Acquire(ctx, milestone.Name)
   if err != nil { return ctrl.Result{}, err }
   defer slot.Release()
   ```

5. **Resolve provider** via shared helper (`dispatch_helpers.go` — see below): per-level → Project default → Helm-chart default.

6. **Build EnvelopeIn** (mirrors `task_controller.go:691-727` `buildEnvelopeIn`) — set `Role: "planner"`, `Level: "milestone"`, populate `Provider` from resolved config, `Prompt` from `common.LoadPromptTemplate("planner", "milestone")`.

7. **Job creation** with deterministic name `tide-milestone-<milestone-uid>-<attempt>` (mirrors Phase 2 D-B5 `JobName` shape — see `internal/dispatch/podjob/names.go:21-23`).

8. **Status patch** — `Phase=Running`, condition `AuthoringPlanner=True` (new condition vocabulary per CONTEXT.md "Established Patterns").

9. **handleJobCompletion** (mirrors `task_controller.go:429-530`):
   - Read `EnvelopeOut` from PVC via `EnvReader` (Phase 2 PodJobBackend pattern).
   - For each `ChildCRDSpec` in `EnvelopeOut.childCRDs` with `Kind: "Phase"`: server-side-create a Phase CRD with owner ref `Milestone`.
   - On all-Phases-Succeeded (watched via `Owns(&Phase{})` — add this to SetupWithManager), fire push Job per D-B2 cadence.

**SetupWithManager extension** (existing `milestone_controller.go:135-149`):
```go
return ctrl.NewControllerManagedBy(mgr).
    For(&tideprojectv1alpha1.Milestone{}).
    Owns(&batchv1.Job{}).
    Owns(&tideprojectv1alpha1.Phase{}).  // ← NEW: child-status drives parent reconcile
    WithEventFilter(nsPred).
    WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles}).
    Named("milestone").
    Complete(r)
```

---

### `internal/controller/phase_controller.go` (EXTEND — body fill)

**Analog:** `milestone_controller.go` after Phase 3 fill — same shape, one level down.

Phase Reconciler is structurally identical to Milestone Reconciler. Substitutions:
- `plannerPool` (same — both up-stack).
- Job name `tide-phase-<phase-uid>-<attempt>`.
- Envelope `Role: "planner"`, `Level: "phase"`, prompt template `phase_planner.tmpl`.
- Owns `Plan` instead of `Phase`.
- Materializes `Plan` CRDs from `EnvelopeOut.childCRDs`.
- Push Job fires at all-Plans-done boundary (D-B2 #2).

**Single helper extraction (per dispatch_helpers.go below) keeps these reconcilers from drifting in lockstep.**

---

### `internal/controller/plan_controller.go` (EXTEND — planner dispatch BEFORE existing Wave materialization)

**Analog:** itself (existing `reconcileWaveMaterialization` at lines 145–207) + `task_controller.go` `reconcileDispatch` for the new planner-Job dispatch step.

**Phase 3 sequence change** (preserves Phase 2 `reconcileWaveMaterialization`):

1. **If Plan has no Tasks yet** → dispatch planner Job (new) → on completion, materialize Task + Wave CRDs from `EnvelopeOut.childCRDs`.
2. **If Plan has Tasks** → call existing `reconcileWaveMaterialization` (Phase 2, unchanged) which validates DAG and creates Waves.
3. **If Plan all-Tasks-Succeeded** → fire push Job (D-B2 #1).

**Key invariant:** Plan admission webhook (Phase 2 D-E1) runs unchanged on the planner-emitted Task list — DAG cycle detection still gates wave materialization.

**Existing `reconcileWaveMaterialization` to leave intact** (`plan_controller.go:145-207`):
```go
func (r *PlanReconciler) reconcileWaveMaterialization(ctx context.Context, plan *tideprojectv1alpha1.Plan) (ctrl.Result, error) {
    if plan.Status.ValidationState != "Validated" {
        return ctrl.Result{}, nil
    }
    // ... List Tasks → ComputeWaves → materializeWaves ...
}
```

**New `reconcilePlanAuthoring` step (Phase 3, called BEFORE `reconcileWaveMaterialization`):**
- Mirror `task_controller.go:192-425` 12-step dispatch flow for the planner Job.
- On EnvelopeOut: server-side-create Task CRDs (with `OwnerRef: Plan`). DO NOT create Waves directly — let the existing `reconcileWaveMaterialization` re-run on the next reconcile and create Waves once Tasks exist + admission webhook stamps `ValidationState=Validated`.

---

### `internal/controller/project_controller.go` (EXTEND — clone Job + push Job + git Status)

**Analog:** existing `ensureInitJob` (lines 207–219) + `buildInitJob` (lines 335–396) + `handleInitJobCompletion` (lines 224–259).

**Clone Job pattern (D-B4 PVC layout init)** — mirrors `buildInitJob` (lines 335–396) but the Pod container runs `tide-clone` (or `tide-push` in clone mode) instead of busybox `mkdir`:

**Pattern to copy** (`project_controller.go:335-396`):
```go
func (r *ProjectReconciler) buildCloneJob(project *tideprojectv1alpha1.Project, pvcName string) *batchv1.Job {
    // Mirrors buildInitJob shape but:
    //   - Container image: opts.TidePushImage (clone command flag selects clone mode)
    //   - EnvFrom: project.Spec.git.credsSecretRef Secret (PAT in env)
    //   - Job name: tide-clone-{project-uid}
    //   - subPath: {project-uid}/workspace (same as init)
    //   - Command: tide-push --mode=clone --repo-url=... --dest=/workspace/repo.git
}
```

**Push Job pattern (D-B5 serialization)** — mirrors `buildInitJob` but with deterministic name `tide-push-{project-uid}` (NO level suffix — that's the per-Project serialization key). `AlreadyExists` is success (`project_controller.go:213-218`):
```go
if err := r.Create(ctx, job); err != nil {
    if apierrors.IsAlreadyExists(err) {
        return nil // idempotent success — second push during first-push is a requeue trigger
    }
    return fmt.Errorf("create push job: %w", err)
}
```

**Branch name initialization (D-B6)** — at Project creation (before clone Job), patch `Project.Status.git.branchName = fmt.Sprintf("tide/run-%s-%d", project.Name, time.Now().Unix())`. **Unix epoch only**, not RFC3339 (refnames cannot contain `:`).

**Status writeback after push Job completion** — mirror `handleInitJobCompletion` (lines 224–259):
- On push Job Succeeded: read push-result envelope from PVC → patch `Project.Status.git.lastPushedSHA`.
- On push Job Failed with reason="lease-rejected": patch `Project.Status.Phase = "PushLeaseFailed"`, set `PushLeaseFailed` condition + halt dispatch.
- Bypass annotation: `tideproject.k8s/bypass-push-lease=true` (mirrors Phase 2 `tideproject.k8s/bypass-budget=true` pattern — see `internal/budget/IsBypassed` for the structural template).

**Phase 4 condition vocabulary** — Phase 3 adds these condition types to `api/v1alpha1/shared_types.go`:
- `ConditionCloned` ("Cloned" — Project, when clone Job succeeded)
- `ConditionAuthoringPlanner` ("AuthoringPlanner" — Milestone/Phase/Plan when planner Job dispatched)
- `ConditionPushLeaseFailed` ("PushLeaseFailed" — Project, when push lease rejected)

---

### `internal/controller/dispatch_helpers.go` (NEW)

**Analog:** `task_controller.go` `buildEnvelopeIn` + `ensureJob` (lines 638–727) — refactored into a controller-package-shared helper so the three up-stack reconcilers don't drift.

**Functions to expose:**

```go
// ResolveProvider walks Project.Spec.subagent precedence chain.
// Per D-C2: levels.{level}.{image,model} → Project default → Helm chart default.
func ResolveProvider(project *tideprojectv1alpha1.Project, level string, helmDefaults ProviderDefaults) pkgdispatch.ProviderSpec {
    // ...
}

// BuildPlannerEnvelope is the planner-side analog of task_controller.go:691-727 buildEnvelopeIn.
// Copies the same shape: Caps + ProxyEndpoint + SignedToken; sets Role/Level/Provider.
func BuildPlannerEnvelope(level string, ref UpstackRef, project *tideprojectv1alpha1.Project, attempt int, token string) (pkgdispatch.EnvelopeIn, []byte, error) {
    // mirror task_controller.go:691-727
}

// MaterializeChildCRDs server-side-creates child CRDs from EnvelopeOut.childCRDs.
// Each child gets OwnerRef pointing back at the parent (mirrors internal/owner.EnsureOwnerRef
// from plan_controller.go:234).
func MaterializeChildCRDs(ctx context.Context, c client.Client, scheme *runtime.Scheme, parent metav1.Object, children []pkgdispatch.ChildCRDSpec) error {
    // For each child: decode runtime.RawExtension to typed Spec, set OwnerRef, Create.
    // AlreadyExists → idempotent success (mirrors task_controller.go:397-403 pattern).
}
```

**Why a helper, not generics:** the three up-stack reconcilers each have distinct types (`Milestone`, `Phase`, `Plan`), but the dispatch flow is structurally identical. Helpers take interfaces; callers pass concrete types. Mirrors how `internal/owner.EnsureOwnerRef` works in Phase 1.

---

### `internal/controller/push_helpers.go` (NEW)

**Analog:** `project_controller.go:335-396` `buildInitJob`.

**`buildPushJob` pattern to mirror** (project_controller.go:335-396):
```go
func buildPushJob(project *tideprojectv1alpha1.Project, opts PushOptions) *batchv1.Job {
    return &batchv1.Job{
        ObjectMeta: metav1.ObjectMeta{
            Name:      fmt.Sprintf("tide-push-%s", project.UID),  // D-B5 serialization key
            Namespace: project.Namespace,
        },
        Spec: batchv1.JobSpec{
            BackoffLimit:            ptr.To(int32(2)),
            TTLSecondsAfterFinished: ptr.To(int32(300)),
            Template: corev1.PodTemplateSpec{
                Spec: corev1.PodSpec{
                    RestartPolicy:      corev1.RestartPolicyNever,
                    ServiceAccountName: "tide-push",  // dedicated SA, least-privilege
                    Volumes: []corev1.Volume{ /* project-workspace from PVC */ },
                    Containers: []corev1.Container{{
                        Name:  "push",
                        Image: opts.TidePushImage,
                        EnvFrom: []corev1.EnvFromSource{
                            // git creds Secret — only the push Job pod sees the PAT
                            {SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{
                                Name: project.Spec.Git.CredsSecretRef,
                            }}},
                        },
                        VolumeMounts: []corev1.VolumeMount{{
                            Name: "project-workspace", MountPath: "/workspace",
                            SubPath: fmt.Sprintf("%s/workspace", project.UID),
                        }},
                        // Args: --branch, --last-pushed-sha, --leaks-config
                    }},
                },
            },
        },
    }
}
```

**Key invariants:**
- Job name `tide-push-{project-uid}` — no level suffix (D-B5 serialization).
- ServiceAccount `tide-push` — `secrets get` on `Project.Spec.git.credsSecretRef` only (Claude's Discretion locked).
- PVC subPath isolation preserved (matches Phase 2 D-G2).
- `EnvFrom` for git creds — never on controller Pod.

---

### `api/v1alpha1/project_types.go` (EXTEND)

**Analog:** existing `ProjectSpec` (lines 132–171) for new field-addition convention; `BudgetStatus` (lines 116–128) for new sub-status block convention.

**Phase 3 additions per D-B6, D-C2:**

```go
// SubagentConfig is the per-Project provider+model selection (Phase 3 D-C2).
type SubagentConfig struct {
    // Image is the default subagent image reference for all levels.
    // +optional
    Image string `json:"image,omitempty"`
    // Model is the default model identifier for all levels (e.g., "claude-sonnet-4-6").
    // +optional
    Model string `json:"model,omitempty"`
    // Levels carries per-level overrides (any subset; resolution: per-level → default → Helm).
    // +optional
    Levels LevelOverrides `json:"levels,omitempty"`
}

type LevelOverrides struct {
    // +optional
    Milestone *LevelConfig `json:"milestone,omitempty"`
    // +optional
    Phase *LevelConfig `json:"phase,omitempty"`
    // +optional
    Plan *LevelConfig `json:"plan,omitempty"`
    // +optional
    Task *LevelConfig `json:"task,omitempty"`
}

type LevelConfig struct {
    // +optional
    Model  string            `json:"model,omitempty"`
    // +optional
    Params map[string]string `json:"params,omitempty"`
    // Image override deferred to v1.x — schema present, not enforced in v1.0.
    // +optional
    Image string `json:"image,omitempty"`
}

// GitConfig declares the per-Project git target + creds (Phase 3 D-B6).
type GitConfig struct {
    // RepoURL is the HTTPS URL of the target repo (e.g., "https://github.com/owner/repo.git").
    // +kubebuilder:validation:Pattern=`^https?://.+`
    RepoURL string `json:"repoURL"`
    // CredsSecretRef is the Secret name carrying the PAT in data key GIT_PAT.
    CredsSecretRef string `json:"credsSecretRef"`
    // LeaksConfigRef is an optional ConfigMap name with gitleaks rule overrides.
    // +optional
    LeaksConfigRef string `json:"leaksConfigRef,omitempty"`
}

// GitStatus records the per-Project git push state (Phase 3 D-B6).
type GitStatus struct {
    // BranchName is the lifetime branch name fixed at Project creation: tide/run-<project>-<unix-epoch>.
    // +optional
    BranchName string `json:"branchName,omitempty"`
    // LastPushedSHA is the head SHA of the most recent successful push.
    // +optional
    LastPushedSHA string `json:"lastPushedSHA,omitempty"`
    // LeaseFailureCount counts consecutive push lease rejections; reset on success.
    // +optional
    LeaseFailureCount int32 `json:"leaseFailureCount,omitempty"`
}
```

**Add to `ProjectSpec`:**
```go
type ProjectSpec struct {
    // ... existing fields ...
    // Subagent declares provider+model selection (Phase 3 D-C2).
    // +optional
    Subagent SubagentConfig `json:"subagent,omitempty"`
    // Git declares the per-Project target repo + creds (Phase 3 D-B6).
    // +optional
    Git GitConfig `json:"git,omitempty"`
}
```

**Add to `ProjectStatus`:**
```go
type ProjectStatus struct {
    // ... existing fields ...
    // Git records per-Project push state (Phase 3 D-B6).
    // +optional
    Git GitStatus `json:"git,omitempty"`
}
```

**Add to `Phase` constants** (lines 174–185):
```go
// PhasePushLeaseFailed is set when a push Job rejects due to lease mismatch (D-B6).
PhasePushLeaseFailed = "PushLeaseFailed"
// PhaseComplete is set when all Milestones reach Succeeded (terminal).
PhaseComplete = "Complete"
```

**Field-comment convention** — every new field MUST have a godoc citing the D-code (matches existing `BudgetStatus`, `PlanAdmissionConfig` style).

---

### `test/integration/kind/chaos_resume_test.go` (NEW — Layer B kind, D-D1..D4)

**Analog:** composes `internal/controller/leader_election_test.go` (envtest pod-kill pattern) + `test/integration/kind/wave_test.go` (kind Layer B test shape).

**Two strong analogs to splice together:**

**From `leader_election_test.go:53-114` — pod-kill + lease-handoff pattern:**
```go
ctx1, cancel1 := context.WithCancel(context.Background())
mgr1, err := buildLeaderTestManager()
go func() { _ = mgr1.Start(ctx1) }()

var firstHolder string
Eventually(func(g Gomega) {
    var lease coordinationv1.Lease
    g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: leaseName, Namespace: leaseNamespace}, &lease)).To(Succeed())
    firstHolder = *lease.Spec.HolderIdentity
}).WithTimeout(20*time.Second).Should(Succeed())

cancel1()  // kill first leader

// Second manager takes over with a different holder identity.
Eventually(func(g Gomega) {
    // ... assert *lease.Spec.HolderIdentity != firstHolder
}).WithTimeout(90*time.Second).Should(Succeed())
```

**From `wave_test.go:39-91` — kind-tier fixture + wait-for-Succeeded:**
```go
var _ = Describe("Three-task wave success (AC1)", Label("kind"), func() {
    const testNS = "wave-success-test"
    BeforeEach(func() {
        skipIfCRDsOnlyMode()
        Expect(applyFile(filepath.Join("testdata", "three-task-wave.yaml"))).To(Succeed())
    })
    It("AC1: ...", func() {
        for _, taskName := range []string{"alpha", "beta", "gamma"} {
            Eventually(func() string {
                t := &tideprojectv1alpha1.Task{}
                _ = k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: "tide-int-test"}, t)
                return t.Status.Phase
            }, 3*time.Minute, 5*time.Second).Should(Equal("Succeeded"))
        }
    })
})
```

**Phase 3 chaos-resume test composition (D-D2/D-D3/D-D4):**

```go
var _ = Describe("Chaos-resume (PERSIST-04 / TEST-04)", Label("kind", "chaos-resume"), func() {
    const testNS = "chaos-resume-test"

    BeforeEach(func() {
        skipIfCRDsOnlyMode()
        // Apply 3-task fixture: α (testMode=success), β (testMode=wait-for-signal),
        // γ (testMode=wait-for-signal, depends_on=[α])
        Expect(applyFile(filepath.Join("testdata", "chaos-resume-3task.yaml"))).To(Succeed())
    })

    It("survives orchestrator pod kill mid-wave (D-D4 four-pillar)", func() {
        // 1. Wait for pre-kill states: α=Succeeded, β=Running, γ=Running
        // 2. Snapshot {jobUID, attempt, phase, jobCreationTimestamp} per Task
        // 3. kubectl delete pod -l app=tide-controller-manager -n tide-system
        // 4. Wait for new leader (lease HolderIdentity changes — copy leader_election_test.go assertion)
        // 5. Re-snapshot; assert four pillars + algorithmic invariant
        // 6. touch /workspace/envelopes/{β-uid}/release + {γ-uid}/release
        // 7. Wait for β, γ to reach Succeeded; verify Wave reaches Succeeded
        // 8. Final assertion: exactly 3 Jobs with status.succeeded=1, zero orphans
    })
})
```

**Algorithmic-invariant pillar #5:** call `pkg/dag.ComputeWaves` against the live Task CRDs pre- and post-restart; compare wave structure via golden-file. Mirrors PERSIST-03 "schedule is re-derived, never cached" — this test is the existence proof.

**Test wall-clock budget** (per D-D1): ~60s total. Lease window is ~15s + 30s buffer + signal-release wait.

---

### `test/integration/kind/push_lease_test.go` (NEW)

**Analog:** `test/integration/kind/wave_test.go:39-126` for the kind-tier fixture+assertion shape.

**Test scenario** (D-B6 lease semantics):
1. Apply Project with valid `git.repoURL` + `git.credsSecretRef` pointing at a test fixture repo (e.g., a kind-internal git server, or a real GitHub fixture under the test PAT).
2. Wait for first push → Succeeded; `Status.git.lastPushedSHA` populated.
3. Externally advance the remote branch (out-of-band `git push --force`).
4. Trigger second push → expect `Status.Phase=PushLeaseFailed` condition + halt.
5. Apply `kubectl annotate project foo tideproject.k8s/bypass-push-lease=true`.
6. Wait for push retry → Succeeded with new `lastPushedSHA`.

**Bypass-annotation pattern to mirror** (`internal/budget/IsBypassed` + project_controller.go:269-301):
```go
if project.Status.Phase == tideprojectv1alpha1.PhasePushLeaseFailed && pushBypassed {
    // Clear PushLeaseFailed condition; consume one-shot annotation; resume dispatch.
}
```

---

### `test/integration/kind/up_stack_dispatch_test.go` (NEW)

**Analog:** `test/integration/kind/wave_test.go:39-91` for the structure.

**Test scenario** (D-A2 up-stack dispatch end-to-end with stub-subagent shaped for planner role):
1. Apply Project + Milestone CRDs.
2. Wait for MilestoneReconciler to dispatch its planner Job.
3. (Stub-subagent extension: a `testMode=plan-emit-children` mode that emits canned `ChildCRDSpec` for two Phases — extension parallel to D-D3's `wait-for-signal`.)
4. Wait for Phase CRDs to materialize (created server-side from `EnvelopeOut.childCRDs`).
5. Verify OwnerRef chain: each Phase has Milestone as owner.

**This test is the regression guard for the "four dispatch sites" structural invariant** (D-A2) — if any future change collapses the per-level reconciler-is-sole-Job-creator pattern, this test catches it.

---

### `cmd/manager/main.go` (EXTEND — wiring)

**Analog:** itself (lines 86–113 Phase 2 flag-+-Helm wiring).

**Existing pattern to extend** (`cmd/manager/main.go:92-109`):
```go
var subagentImage string
var credproxyImage string
flag.StringVar(&subagentImage, "subagent-image", "", "Image ref for the subagent container ...")
flag.StringVar(&credproxyImage, "credproxy-image", "", "Image ref for the tide-credproxy sidecar ...")
```

**Phase 3 additions:**
```go
var tidePushImage string
var claudeSubagentImage string
var milestoneModel string  // default per level — D-C4
var phaseModel string
var planModel string
var taskModel string
flag.StringVar(&tidePushImage, "tide-push-image", "", "Image ref for the push Job (images.tidePush in values.yaml)")
flag.StringVar(&claudeSubagentImage, "claude-subagent-image", "", "Image ref for the Claude subagent (images.claudeSubagent)")
flag.StringVar(&milestoneModel, "milestone-model", "claude-opus-4-7", "Default model for milestone planner (subagent.levels.milestone.model)")
flag.StringVar(&phaseModel, "phase-model", "claude-sonnet-4-6", "Default model for phase planner")
flag.StringVar(&planModel, "plan-model", "claude-sonnet-4-6", "Default model for plan planner")
flag.StringVar(&taskModel, "task-model", "claude-haiku-4-5", "Default model for task executor")
```

**Wire into reconciler structs:** add `TidePushImage`, `ClaudeSubagentImage`, `PerLevelDefaults` fields to `ProjectReconciler` / `MilestoneReconciler` / `PhaseReconciler` / `PlanReconciler` / `TaskReconciler` and pass through from the manager setup.

---

### `charts/tide/values.yaml` (EXTEND)

**Analog:** existing `images.{stubSubagent,credProxy}` block (lines 117–133) + `rateLimits.defaults` block (lines 108–115).

**Pattern to copy** (charts/tide/values.yaml:117-133):
```yaml
images:
  stubSubagent:
    repository: ghcr.io/jsquirrelz/tide-stub-subagent
    tag: v0.1.0-dev
    pullPolicy: IfNotPresent
  credProxy:
    repository: ghcr.io/jsquirrelz/tide-credproxy
    tag: v0.1.0-dev
    pullPolicy: IfNotPresent
```

**Phase 3 additions:**
```yaml
images:
  # ... existing entries ...
  claudeSubagent:
    repository: ghcr.io/jsquirrelz/tide-claude-subagent
    tag: v0.1.0-dev
    pullPolicy: IfNotPresent
  tidePush:
    repository: ghcr.io/jsquirrelz/tide-push
    tag: v0.1.0-dev
    pullPolicy: IfNotPresent

# Per-level model defaults (D-C4). Override per Project via Project.Spec.subagent.levels.
subagent:
  defaults:
    image: ghcr.io/jsquirrelz/tide-claude-subagent
    model: claude-sonnet-4-6
  levels:
    milestone:
      model: claude-opus-4-7      # heaviest planning, lowest fan-out
    phase:
      model: claude-sonnet-4-6
    plan:
      model: claude-sonnet-4-6
    task:
      model: claude-haiku-4-5     # highest fan-out, cost-bounded

# Optional gitleaks config override (D-B3). Per-Project ConfigMap via Project.Spec.git.leaksConfigRef.
gitleaks:
  configMapName: ""  # empty → use compiled-in default ruleset

# Leader-election tuning (D-D1 chaos-resume — controller-runtime defaults suffice in v1.0).
leaderElection:
  enabled: true
  leaseDurationSeconds: 15
  renewDeadlineSeconds: 10
  retryPeriodSeconds: 2
```

---

### `charts/tide/templates/push-rbac.yaml` (NEW)

**Analog:** `charts/tide/templates/serviceaccount-subagent.yaml` (entire file — same shape, different verbs).

**Pattern to copy** (`charts/tide/templates/serviceaccount-subagent.yaml:1-10`):
```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: tide-subagent
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "tide.labels" . | nindent 4 }}
automountServiceAccountToken: true
# No Role, no RoleBinding — subagent pods have zero K8s API verbs (D-A4).
```

**Phase 3 push-rbac.yaml — least-privilege variant (Claude's Discretion: dedicated SA):**
```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: tide-push
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "tide.labels" . | nindent 4 }}
automountServiceAccountToken: true
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: tide-push
  namespace: {{ .Release.Namespace }}
rules:
- apiGroups: [""]
  resources: ["secrets"]
  verbs: ["get"]
  # No list/watch — push Job knows its Secret name from envFrom resolution at Pod admission.
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: tide-push
  namespace: {{ .Release.Namespace }}
subjects:
- kind: ServiceAccount
  name: tide-push
  namespace: {{ .Release.Namespace }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: tide-push
```

---

## Shared Patterns

### Authentication / Credential Isolation
**Source:** `internal/dispatch/podjob/jobspec.go:160-202` (credproxy sidecar) + `internal/controller/project_controller.go:262-301` (bypass-annotation pattern).
**Apply to:** All Phase 3 Job creation sites (`claude-subagent`, `tide-push`, `tide-clone`).

**Pattern excerpt** (jobspec.go:168-181):
```go
EnvFrom: func() []corev1.EnvFromSource {
    srcs := []corev1.EnvFromSource{
        {SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "tide-signing-key"}}},
    }
    if opts.Project.Spec.ProviderSecretRef != "" {
        srcs = append(srcs, corev1.EnvFromSource{
            SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: opts.Project.Spec.ProviderSecretRef}},
        })
    }
    return srcs
}(),
```

**Rules:**
- Anthropic key only on the credproxy sidecar (envFrom Project.Spec.ProviderSecretRef).
- Git PAT only on the `tide-push` / `tide-clone` Job pods (envFrom Project.Spec.Git.CredsSecretRef).
- Subagent main container env carries SIGNED TOKEN only, never raw keys (D-C4).
- Controller Pod never holds either secret.

---

### Deterministic Job Naming (Idempotency / Dedup)
**Source:** `internal/dispatch/podjob/names.go:21-23` (`tide-task-{taskUID}-{attempt}`) and `internal/controller/project_controller.go:189` (`tide-init-{projectUID}`).
**Apply to:** All Phase 3 Job creators.

**Pattern excerpt** (`names.go:21-23`):
```go
func JobName(taskUID types.UID, attempt int) string {
    return fmt.Sprintf("tide-task-%s-%d", taskUID, attempt)
}
```

**Phase 3 names:**
| Job | Name | Serialization |
|-----|------|---------------|
| Clone | `tide-clone-{project-uid}` | per Project, one-time at init |
| Push  | `tide-push-{project-uid}`  | per Project, **NO level suffix** (D-B5 serialization key) |
| Milestone planner | `tide-milestone-{milestone-uid}-{attempt}` | per Milestone, per attempt |
| Phase planner | `tide-phase-{phase-uid}-{attempt}` | per Phase, per attempt |
| Plan planner  | `tide-plan-{plan-uid}-{attempt}` | per Plan, per attempt |
| Task executor | `tide-task-{task-uid}-{attempt}` (Phase 2, unchanged) | per Task, per attempt |

**AlreadyExists handling pattern** (`task_controller.go:397-403` + `project_controller.go:213-218`):
```go
if err := r.Create(ctx, job); err != nil {
    if !apierrors.IsAlreadyExists(err) {
        return ctrl.Result{}, fmt.Errorf("create job: %w", err)
    }
    // AlreadyExists: idempotent success — watch-lag race.
}
```

---

### Reconciler Body Shape (Six-Step Pattern)
**Source:** `internal/controller/task_controller.go:122-188` (the canonical six-step), `internal/controller/project_controller.go:105-152` (variant).
**Apply to:** Every Phase 3 reconciler body fill.

**Pattern excerpt** (task_controller.go:122-188):
```go
func (r *TaskReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // 1. Fetch.
    var task tideprojectv1alpha1.Task
    if err := r.Get(ctx, req.NamespacedName, &task); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }
    // 2. Handle deletion with a bounded-deadline cleanup (CTRL-05).
    if !task.DeletionTimestamp.IsZero() {
        return finalizer.HandleDeletion(/* ... */)
    }
    // 3. Ensure finalizer is set on create.
    if !controllerutil.ContainsFinalizer(&task, taskFinalizer) { /* ... */ }
    // 4. Ensure owner ref to parent (CRD-02).
    // 5. Phase 2 dispatch body inside the Dispatcher seam (REQ-SUB-01).
    if r.Dispatcher != nil {
        return r.reconcileDispatch(ctx, &task)
    }
    // 6. Update status conditions and persist via Status().Update.
}
```

**Phase 3 fill rule:** the up-stack reconcilers extend step 5 — the body inside the `if r.Dispatcher != nil` guard. Do NOT touch steps 1–4 or 6 (Phase 1 contract).

---

### Status Patch Pattern
**Source:** `internal/controller/task_controller.go:249-261, 363-371, 408-422` — repeated three times in the dispatch body.
**Apply to:** All Phase 3 status writes.

**Pattern excerpt** (task_controller.go:408-422):
```go
{
    patch := client.MergeFrom(task.DeepCopy())
    task.Status.Phase = "Running"
    meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
        Type:               tideprojectv1alpha1.ConditionRunning,
        Status:             metav1.ConditionTrue,
        Reason:             "Dispatched",
        Message:            fmt.Sprintf("Job %s dispatched (attempt %d)", job.Name, attempt),
        LastTransitionTime: metav1.Now(),
    })
    if err := r.Status().Patch(ctx, task, patch); err != nil {
        return ctrl.Result{}, err
    }
}
```

**Rules:**
- Always `client.MergeFrom(obj.DeepCopy())` before mutation, `r.Status().Patch(ctx, obj, patch)` after.
- Never `r.Status().Update` (Update is for non-status; Patch is for status — controller-runtime convention).
- Condition `Type` constants from `api/v1alpha1/shared_types.go`; Phase 3 adds `ConditionPushLeaseFailed`, `ConditionCloned`, `ConditionAuthoringPlanner`.

---

### Owner-Ref Cascade
**Source:** `internal/owner.EnsureOwnerRef` (call sites: `task_controller.go:161, 394, 661`, `plan_controller.go:234`, `project_controller.go:210`).
**Apply to:** All server-side-created child CRDs from `EnvelopeOut.childCRDs`.

**Pattern excerpt** (plan_controller.go:234-235):
```go
if err := owner.EnsureOwnerRef(wave, plan, r.Scheme); err != nil {
    return fmt.Errorf("ensure owner ref wave %s: %w", waveName, err)
}
```

**Phase 3 chain extension:**
- Milestone planner Job emits Phase CRDs → Milestone owns Phases via EnsureOwnerRef.
- Phase planner Job emits Plan CRDs → Phase owns Plans.
- Plan planner Job emits Task CRDs → Plan owns Tasks (Phase 2 already owns Waves; Phase 3 extends to Tasks).

---

### Idempotent Wave/Plan/Task Materialization
**Source:** `plan_controller.go:228-253` `materializeWaves`.
**Apply to:** `MaterializeChildCRDs` helper in `dispatch_helpers.go`.

**Pattern excerpt** (plan_controller.go:228-253):
```go
var existing tideprojectv1alpha1.Wave
if err := r.Get(ctx, client.ObjectKey{Namespace: plan.Namespace, Name: waveName}, &existing); err != nil {
    if client.IgnoreNotFound(err) != nil {
        return fmt.Errorf("get wave %s: %w", waveName, err)
    }
    // Wave does not exist — set owner ref and create.
    if err := owner.EnsureOwnerRef(wave, plan, r.Scheme); err != nil {
        return fmt.Errorf("ensure owner ref wave %s: %w", waveName, err)
    }
    if err := r.Create(ctx, wave); err != nil {
        if !apierrors.IsAlreadyExists(err) {
            return fmt.Errorf("create wave %s: %w", waveName, err)
        }
    }
}
```

**Rules:**
- Get-first → 404 → Create (with AlreadyExists tolerance).
- Owner ref set ONLY at create time (not on observed-existing path).
- Names are deterministic so retries are AlreadyExists, not duplicate-creates.

---

### Test Fixture Shape (Layer B kind)
**Source:** `test/integration/kind/wave_test.go:39-126` + `test/integration/kind/suite_test.go:642-734` (`applyHierarchy`).
**Apply to:** All three new Phase 3 kind tests (chaos_resume, push_lease, up_stack_dispatch).

**Pattern excerpt** (wave_test.go:39-58):
```go
var _ = Describe("Three-task wave success (AC1)", Label("kind"), func() {
    const testNS = "wave-success-test"
    BeforeEach(func() {
        skipIfCRDsOnlyMode()
        fixturePath := filepath.Join("testdata", "three-task-wave.yaml")
        Expect(applyFile(fixturePath)).To(Succeed())
    })
    AfterEach(func() {
        deleteNamespace(testNS)
        deleteNamespace("tide-int-test")
        if CurrentSpecReport().Failed() {
            exportKindLogs()
        }
    })
})
```

**Conventions:**
- `Label("kind")` so `ginkgo --label-filter=kind` picks them up.
- `skipIfCRDsOnlyMode()` for CRDs-only-mode tolerance.
- Fixture YAML under `testdata/`.
- `exportKindLogs()` on failure (`suite_test.go:861-871`).
- Namespace cleanup in AfterEach.

---

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| `pkg/git/{clone,push,worktree}.go` | utility | request-response | No in-repo go-git/v5 usage yet; RESEARCH.md Patterns 1–3 are the source. |

Even this isn't truly analog-less — the per-file split (`clone.go`, `push.go`, `worktree.go`, `commit.go`, `fetch.go`, `doc.go`) mirrors how `pkg/dispatch` is split (`envelope.go`, `subagent.go`, `errors.go`, `doc.go`). Use the same package convention.

---

## Anti-Pattern Guardrails (from Pattern Mapping Context)

These guardrails are encoded across multiple files; flagged here so the planner enforces them in every plan that touches the relevant package:

1. **TIDE K8s API group is `tideproject.k8s`** — every new CRD field, every RBAC verb, every label. Never `tide.io`. Grep targets: `tideproject.k8s/` in all new YAML; `tideprojectv1alpha1` import path in all new Go.
2. **Subagent pods have zero K8s verbs (D-A4)** — `internal/subagent/anthropic/` MUST NOT import `sigs.k8s.io/controller-runtime/pkg/client`. The `ServiceAccount: tide-subagent` in `jobspec.go:43` ships with no Role/RoleBinding; Phase 3 preserves this.
3. **Never mount host `~/.claude/`** — `images/claude-subagent/Dockerfile` uses `--bare` flag (RESEARCH.md Pattern 6) to skip auto-discovery; `ANTHROPIC_API_KEY` only from signed token via credproxy sidecar.
4. **`pkg/git` is host-agnostic** — HTTPS+PAT only (no GitHub API client, no GitLab webhook client). Username `"x-access-token"` is the universal convention.
5. **gitleaks is a Go library, not a binary** (D-B3) — `tide-push` Dockerfile has zero `RUN apt-get install gitleaks` lines. `import "github.com/zricethezav/gitleaks/v8/detect"` is the seam.
6. **Deterministic Job names** — every new Job creator uses the `tide-{kind}-{owner-uid}[-{attempt}]` pattern. Push Job has NO level suffix (D-B5 serialization).
7. **Don't vendor GSD Markdown** — prompts are `go:embed`-ed `text/template` files in `internal/subagent/common/templates/`.
8. **Don't cache the schedule** — `pkg/dag.ComputeWaves` is called on every reconcile, never stored in `.status` (PERSIST-03). Chaos-resume D-D4 pillar #5 is the existence proof.

---

## Metadata

**Analog search scope:**
- `/Users/justinsearles/Projects/tide/pkg/dispatch/` (envelope, subagent, errors)
- `/Users/justinsearles/Projects/tide/internal/controller/` (all 6 reconcilers + tests)
- `/Users/justinsearles/Projects/tide/internal/dispatch/podjob/` (backend, jobspec, names)
- `/Users/justinsearles/Projects/tide/internal/harness/` (harness, envelope_io, redact)
- `/Users/justinsearles/Projects/tide/cmd/stub-subagent/main.go`
- `/Users/justinsearles/Projects/tide/api/v1alpha1/project_types.go`
- `/Users/justinsearles/Projects/tide/images/stub-subagent/Dockerfile`
- `/Users/justinsearles/Projects/tide/charts/tide/` (values.yaml + templates/)
- `/Users/justinsearles/Projects/tide/test/integration/kind/` (suite_test, wave_test, testdata)
- `/Users/justinsearles/Projects/tide/cmd/manager/main.go` (head — flag wiring)
- RESEARCH.md Patterns 1–6 for go-git/v5, gitleaks, stream-json, Claude CLI

**Files scanned:** ~30 source files across 8 packages; per-file extraction non-overlapping (no re-reads).
**Pattern extraction date:** 2026-05-15

---

## PATTERN MAPPING COMPLETE

**Phase:** 3 — Up-Stack Reconcilers, Git Integration, Real Subagent, Resumption
**Files classified:** 29
**Analogs found:** 28 / 29

### Coverage
- Files with exact analog: 24 (self-extensions of Phase 1/2 files + direct sibling patterns)
- Files with role-match analog: 4 (`pkg/git/*` uses RESEARCH.md library patterns; `internal/gitleaks`, `internal/subagent/common`, `internal/subagent/anthropic/stream_parser` borrow nearest-shape from harness/redact/envelope_io)
- Files with no analog: 1 (`pkg/git` — net-new, but per-file split mirrors `pkg/dispatch`)

### Key Patterns Identified
- **Six-step reconciler body** (`task_controller.go:122-188`) is the canonical template for every up-stack body fill; only step 5 (`if r.Dispatcher != nil`) changes per reconciler.
- **Deterministic Job names** (`internal/dispatch/podjob/names.go`) extend to all five new Job classes; push Job omits attempt suffix (D-B5 serialization).
- **Idempotent server-side create** (`plan_controller.go:228-253`) is the materialization shape for every `ChildCRDSpec` → child-CRD path; AlreadyExists is success.
- **Init-Job dispatch shape** (`project_controller.go:207-219, 335-396`) is the template for both clone Job and push Job.
- **Provider-pluggable layering** (D-C1) — `pkg/dispatch.Subagent` interface is stable; `internal/subagent/anthropic/` is the first provider; `internal/subagent/common/` shares stream-reader + prompt-template loader across future providers.
- **Credential isolation** — credproxy sidecar holds Anthropic key (Phase 2); push Job pod holds git PAT (Phase 3); controller Pod holds neither.
- **Chaos-resume test composition** — `leader_election_test.go` (envtest lease handoff) + `wave_test.go` (kind Layer B fixture) splice cleanly into the four-pillar+invariant assertion set.

### File Created
`/Users/justinsearles/Projects/tide/.planning/phases/03-up-stack-reconcilers-git-integration-real-subagent-resumptio/03-PATTERNS.md`

### Ready for Planning
Pattern mapping complete. Planner can now author Phase 3 PLAN.md files with concrete per-file analog references, code excerpts to copy, and a clear path to preserve every Phase 1/2 invariant under Phase 3 extension.
