# TIDE Codebase Audit Report

## 1. Executive Summary

This report presents a thorough technical audit of the **TIDE** (Kubernetes-native hierarchical agentic coding orchestrator) operator codebase located at `/Users/justinsearles/Projects/tide`. 

TIDE represents an exceptionally high-quality Kubernetes operator implementation. It demonstrates strong architectural discipline, meticulous guardrail design, and compliance with Kubernetes controller best practices. The codebase features a well-defined custom static analysis tool suite (`tide-lint`), robust multi-level reconciler designs, and highly secure provider-credential proxying.

Below, we detail our architectural findings, document the precise resolution of six planned verification questions, highlight the system's defensive boundaries, and offer targeted recommendations to elevate operational stability and readiness.

---

## 2. Codebase Architecture & Structural Review

### 2.1 CRD Hierarchy & Derived State
TIDE models its workflow using a five-level custom resource hierarchy:
$$\text{Project} \longrightarrow \text{Milestone} \longrightarrow \text{Phase} \longrightarrow \text{Plan} \longrightarrow \text{Task}$$

* **Derived View (No Cycle Recovery)**: Rather than storing predictions or pre-scheduled execution structures in `etcd`, which are highly prone to cache-invalidation bugs and state-desynchronization under controller restarts, TIDE dynamically computes dispatch waves. The waves are computed dynamically using Kahn's topological sort during each reconcile pass.
* **Separation of Concerns**: The core graph package `pkg/dag` is generic (`O(V+E)` complexity) and completely decoupled from Kubernetes types or LLM provider SDKs. This decoupling is strictly enforced by static import-firewall rules.

### 2.2 Reconciler Design Best Practices
Every reconciler in `internal/controller/` (`Project`, `Milestone`, `Phase`, `Plan`, `Wave`, `Task`) implements a rigorous six-step "Standard-Depth" reconciliation pattern:
1. **Fetch**: Fetches the target resource, gracefully ignoring `NotFound` errors.
2. **Handle Deletion**: Invokes `finalizer.HandleDeletion` under a bounded-deadline cleanup context (preventing namespace deletion-stuck states).
3. **Ensure Finalizer**: Registers the finalizer on resource creation.
4. **Ensure Owner Reference**: Establishes parent-child relationships in the same namespace via `owner.EnsureOwnerRef` (preventing cross-namespace garbage collection leakage).
5. **Reconciliation Body**: Drives state changes based on current observations.
6. **Status Patch/Update**: Saves observations using optimistic concurrency-safe patches.

---

## 3. High-Fidelity Fact-Checking & Verification

To validate initial assumptions and eliminate audit bias, we conducted detailed code inspections targeting six key technical areas. The verified facts are documented below:

### 3.1 Webhook & Cycle Validation (`PlanCustomValidator`)
* **File Location**: [plan_webhook.go](file:///Users/justinsearles/Projects/tide/internal/webhook/v1alpha1/plan_webhook.go)
* **Implementation Details**: The validator is stateful and fully integrated. It converts a Plan's child Tasks into a directed acyclic graph (DAG) structure where nodes are `Task.Name` and edges are `(DependsOn[i], Task.Name)`. It runs `dag.ComputeWaves(nodes, edges)` to detect cyclic dependencies.
* **Cycle Handling**: If a cycle is detected, the webhook raises a `CycleError`, rejects admission, and emits a `CycleDetected` Warning Event on the Plan resource. There is no cycle "recovery" code path; the controller purely rejects invalid state changes, enforcing architectural correctness.
* **Path Overlap Check**: It computes exact-equality intersections between `filesTouched` across tasks without a declared `depends_on` edge. It enforces this based on strict/warn mode, returning warning headers or outright rejecting admission.

### 3.2 Bounded Deletions (`finalizer.HandleDeletion`)
* **File Location**: [finalizer.go](file:///Users/justinsearles/Projects/tide/internal/finalizer/finalizer.go)
* **Implementation Details**: The finalizer utility manages deletions under a derived context with a 5-minute timeout (`context.WithTimeout`).
* **Timeout Behavior**: If the cleanup callback exceeds the deadline and returns `context.DeadlineExceeded`, the helper logs the failure loudly, **forcibly removes the finalizer**, and updates the resource. This ensures that a hung external system (e.g., unreachable provider API) cannot hold the Kubernetes namespace hostage or leave resources stuck in a `Terminating` state forever (solving Pitfall 21).

### 3.3 Task Webhook & Informer Cache Gaps
* **File Location**: [plan_webhook.go](file:///Users/justinsearles/Projects/tide/internal/webhook/v1alpha1/plan_webhook.go#L135-L145)
* **Implementation Details**: When listing child Tasks for validation, the `PlanCustomValidator` may find zero items due to informer cache lag or standard `kubectl apply -k` ordering (where the Plan is admitted before its Tasks have synced).
* **Mitigation**: Rather than rejecting the Plan (which would break multi-resource apply pipelines), the webhook returns an **admission warning** (`admission.Warnings`) but returns a `nil` error, allowing the Plan to be created. The cycle validation will execute safely when the Tasks reconcile and update the Plan's status.

### 3.4 Credential Proxy Sidecar
* **File Locations**: [cmd/credproxy/main.go](file:///Users/justinsearles/Projects/tide/cmd/credproxy/main.go) and [server.go](file:///Users/justinsearles/Projects/tide/internal/credproxy/server.go)
* **Implementation Details**: The credential proxy sidecar is fully implemented as a native Go TLS proxy (running alongside every Task execution Job pod). It mints a self-signed TLS cert and key on startup via `credproxy.MintSelfSignedCert` and listens on `127.0.0.1:8443`.
* **Token Structure**: It uses a secure HMAC-SHA256 bearer token. The token byte layout comprises a 16-byte random `nonce`, an 8-byte big-endian Unix timestamp `expiry`, and a 32-byte `mac`.
* **Replay Protection**: The `taskUID` is bound in the HMAC signature but is *not* stored inside the token. The proxy uses its own pod-injected `TIDE_TASK_UID` environment variable during verification. If a token is stolen and replayed against a different Task pod, verification fails immediately due to a UID mismatch (preventing cross-pod replay).
* **API Redaction**: It replaces the HMAC token with the real `ANTHROPIC_API_KEY` before forwarding requests to `api.anthropic.com`.
* **Allowlist Defense**: The proxy limits API surface exposure by checking against a strict allowed-route list: only `POST /v1/messages` and `POST /v1/messages/count_tokens` are forwarded. All other paths (e.g. administrative billing, file management) return a `403 Forbidden` response, severely limiting the blast radius of a compromised subagent.

### 3.5 Push-Boundary Gitleaks Protection
* **File Location**: [cmd/tide-push/main.go](file:///Users/justinsearles/Projects/tide/cmd/tide-push/main.go#L319-L335)
* **Implementation Details**: Secret leak scanning occurs inside the isolated `tide-push` Job container (the git push boundary). Before staging files to go-git and pushing them to upstream, `tide-push` computes a unified diff between the new commit and the previous head commit.
* **Leak Blocking**: It passes this diff to `gitleaks.ScanDiff(diff)`, which uses the embedded gitleaks v8.30.1 default ruleset (150+ secret-shape signatures).
* **Fail-Safe Mechanism**: If a leak is found, it does *not* invoke git push. It writes a structured push envelope JSON file detailing `leak-detected` with exit code `10`. The main controller parses the envelope, sets the Project's phase to `LeakDetected`, and increments the `tide_secret_leak_blocked_total` Prometheus counter. This design guarantees that credentials (like `GIT_PAT`) remain isolated in the push pod and never leak to the remote repository.

### 3.6 Budget Window & Cap Enforcements
* **File Location**: [cap.go](file:///Users/justinsearles/Projects/tide/internal/budget/cap.go) and [tally.go](file:///Users/justinsearles/Projects/tide/internal/budget/tally.go)
* **Implementation Details**: The operator enforces budget limits at dispatch time. The `TaskReconciler` verifies `budget.IsCapExceeded` before launching any worker jobs.
* **Bypass Annotations**: Operators can bypass budget gates using `tideproject.k8s/bypass-budget=true` (a one-shot bypass consumed by the reconciler) or `tideproject.k8s/bypass-budget-until=<RFC3339>` (a TTL-based bypass that expires naturally).
* **Deferred Rolling Window**: The `BudgetConfig.RollingWindowCapCents` field is currently **documentation-only**. The tally logic in `RollUpUsage` notes that Phase 2 does *not* support rolling-window reset logic; the cumulative cost spent `CostSpentCents` currently accumulates forever, and only `AbsoluteCapCents` enforces structural halts.

---

## 4. Guardrails & Custom Static Analysis (`tide-lint`)

TIDE features a custom linter suite (`tide-lint`) registered under `tools/analyzers/` and run as a CI gate. The analyzers enforce four highly specialized architectural properties:

| Linter | Rule ID | Target / Defense | Violation Action |
| :--- | :--- | :--- | :--- |
| `crosspool` | `POOL-03` | Rejects any `select` statement waiting on both the planner and executor semaphores in the same case block. Prevents pool unification and deadlocks. | Rejects code build in CI |
| `dagimports` | `DAG-05` | Blocks imports of `k8s.io/*`, `sigs.k8s.io/*`, or `github.com/anthropics/*` inside the `pkg/dag` library. Maintains generic decoupling. | Rejects code build in CI |
| `metriccardinality` | `OBS-02` | Prevents registering a literal `"task"` string inside the label slice of a Prometheus vector constructor (`NewCounterVec`, `NewGaugeVec`). Prevents high-cardinality `etcd`/Prometheus crashes. | Rejects code build in CI |
| `providerfirewall` | `SUB-05` | Rejects provider SDK imports (`github.com/anthropics/*`, `github.com/openai/*`, etc.) inside orchestrator packages (`internal/controller`, `pkg/dispatch`). Blocks auto-loading credentials in the controller manager. | Rejects code build in CI |

---

## 5. Findings & Recommendations

### 5.1 Observed Strengths
1. **Outstanding Isolation**: Secrets (`GIT_PAT` and `ANTHROPIC_API_KEY`) are structurally isolated. The central controller manager never sees the repository PAT (restricted to `tide-push` pods), and the subagent pods only see transient HMAC tokens, with the real API key residing behind the TLS credential proxy sidecar.
2. **Defensive Reconciler Design**: The use of a standard-depth reconciler pattern coupled with bounded-deadline finalizers avoids common Operator pitfalls, such as objects hanging indefinitely during deletion.
3. **Robust Static Code Enforcement**: Integrating custom AST-based linters (`tide-lint`) into CI prevents critical architectural regressions (like pool unification or import leaks) before they hit production.

### 5.2 Areas for Improvement & Recommendations
While the codebase is exceptionally well-engineered, we recommend the following enhancements:

* **Recommendation 1: Implement the Rolling Window Reset**
  * *Context*: Currently, `RollingWindowCapCents` is documentation-only, causing the spent budget to accumulate indefinitely.
  * *Action*: Implement the rolling window logic in `ProjectReconciler`. Compare `now.Sub(Status.Budget.WindowStart)` against the configured rolling window duration and reset `Status.Budget.CostSpentCents` and `Status.Budget.TokensSpent` when the boundary is crossed.
* **Recommendation 2: Enhance Proxy Allowlist Paths dynamically**
  * *Context*: The proxy currently restricts calls to `/v1/messages` and `/v1/messages/count_tokens`.
  * *Action*: Ensure that as new LLM features (such as caching, prompt caching, or search tools) are adopted by the subagents, the `allowedRoutes` list in `internal/credproxy/server.go` is expanded programmatically (e.g., using annotations on the Task resource) rather than relying strictly on hardcoded prefixes, maintaining agility.
* **Recommendation 3: Standardize Logging Format**
  * *Context*: The codebase generally aligns with Kubernetes logging guidelines.
  * *Action*: Review the log strings in `internal/controller/` to ensure they strictly follow standard conventions (e.g., active/past voice, capitalized, no ending period, and balanced key-value pairs). For example, ensure all messages like `"finalizer cleanup deadline exceeded; forcibly removing finalizer"` do not end in periods.
