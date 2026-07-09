/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SecretRefs declares the K8s Secret names that carry credentials.
// Per AUTH-01, populated in Phase 3; Phase 1 ships the field shape only.
type SecretRefs struct {
	// AnthropicAPIKey is the Secret name carrying the LLM API key.
	// +optional
	AnthropicAPIKey string `json:"anthropicAPIKey,omitempty"`
	// GitCredentials is the Secret name carrying git push credentials (PAT or SSH key).
	// +optional
	GitCredentials string `json:"gitCredentials,omitempty"`
}

// ModelSelection picks per-level model identifiers.
// Phase 1 ships the field shape; Phase 2+ consumes.
type ModelSelection struct {
	// +optional
	Milestone string `json:"milestone,omitempty"`
	// +optional
	Phase string `json:"phase,omitempty"`
	// +optional
	Plan string `json:"plan,omitempty"`
	// +optional
	Task string `json:"task,omitempty"`
}

// GatePolicy is one of "auto" | "approve" | "pause" — per-level human gate.
// Phase 1 ships the field shape; Phase 4 consumes.
// +kubebuilder:validation:Enum=auto;approve;pause
type GatePolicy string

// Gates declares per-level gate policy. Phase 1 ships field; Phase 4 wires.
type Gates struct {
	// +optional
	Milestone GatePolicy `json:"milestone,omitempty"`
	// +optional
	Phase GatePolicy `json:"phase,omitempty"`
	// +optional
	Plan GatePolicy `json:"plan,omitempty"`
	// +optional
	Task GatePolicy `json:"task,omitempty"`
	// +optional
	PauseBetweenWaves bool `json:"pauseBetweenWaves,omitempty"`
}

// RouteSpec is a (method, path-prefix) tuple used to extend the credproxy
// upstream allowlist for a Project (Phase 04.1 P4.2). The credproxy sidecar
// always enforces the hardcoded baseline (POST /v1/messages and
// POST /v1/messages/count_tokens); RouteSpec entries are additive extensions,
// never replacements.
type RouteSpec struct {
	// Method is the HTTP method. Currently only GET and POST are accepted.
	// +kubebuilder:validation:Enum=GET;POST
	Method string `json:"method"`

	// PathPrefix is the URL path prefix. Must start with "/v1/" — paths
	// outside the Anthropic API surface are rejected at admission.
	// +kubebuilder:validation:Pattern=`^/v1/[a-zA-Z0-9_/-]+$`
	PathPrefix string `json:"pathPrefix"`
}

// ProviderConfig configures one LLM provider (Phase 2+).
type ProviderConfig struct {
	// Name is the provider identifier (only "anthropic" is supported in v1).
	// +kubebuilder:validation:Enum=anthropic
	Name string `json:"name"`

	// RequestsPerMinute optionally caps API requests per minute for this provider.
	// +optional
	RequestsPerMinute *int32 `json:"requestsPerMinute,omitempty"`

	// TokensPerMinute optionally caps token throughput per minute for this provider.
	// +optional
	TokensPerMinute *int32 `json:"tokensPerMinute,omitempty"`

	// AllowedRoutes optionally extends the hardcoded credproxy allowlist
	// (POST /v1/messages + POST /v1/messages/count_tokens) with additional
	// (method, path-prefix) tuples. Use this to grant the subagent access
	// to newly-released LLM endpoints (Files API, Search Tools, prompt
	// caching) without rebuilding the credproxy image.
	// Hardcoded safe defaults ALWAYS apply — operator additions are
	// additive, never restrictive. Admin/billing paths are rejected at
	// admission (defense in depth).
	// +optional
	AllowedRoutes []RouteSpec `json:"allowedRoutes,omitempty"`
}

// BudgetConfig declares cost/token caps for the Project (Phase 2+).
type BudgetConfig struct {
	// AbsoluteCapCents is the hard spending limit in USD cents for the project lifetime.
	// +kubebuilder:validation:Minimum=0
	AbsoluteCapCents int64 `json:"absoluteCapCents"`

	// RollingWindowCapCents caps spending over the rolling window defined by
	// BudgetStatus.WindowStart and BudgetConfig.RollingWindowDuration. When
	// the window elapses, ProjectReconciler.handleBudgetGate resets
	// CostSpentCents + TokensSpent and advances WindowStart.
	// +optional
	RollingWindowCapCents int64 `json:"rollingWindowCapCents,omitempty"`

	// RollingWindowDuration is the window length over which
	// RollingWindowCapCents applies. Default: 24h when unset. Must be >= 1h
	// to prevent inadvertent denial-of-dispatch via tiny windows.
	// Validation: enforced semantically in ProjectReconciler (metav1.Duration
	// is a struct; controller-gen Pattern markers require a string type).
	// +optional
	RollingWindowDuration *metav1.Duration `json:"rollingWindowDuration,omitempty"`
}

// PlanAdmissionConfig controls file-touch policy during plan validation (Phase 2+).
type PlanAdmissionConfig struct {
	// FileTouchMode determines how file-touch mismatches are handled:
	//   "strict" — reject plans whose file touches deviate from declarations.
	//   "warn"   — admit but annotate the mismatch on Plan.Status.
	// +kubebuilder:validation:Enum=strict;warn
	// +optional
	FileTouchMode string `json:"fileTouchMode,omitempty"`
}

// SubagentConfig declares per-Project provider+model selection (Phase 3 D-C2).
//
// Resolution chain at dispatch time (in each reconciler before calling
// Dispatcher.Run): Spec.Subagent.Levels.{level}.{image,model} → Spec.Subagent.{image,model}
// → Helm-chart default. Vendor + model are orthogonal axes — Image picks the
// vendor (via the container image's bundled Subagent impl); Model picks the
// model identifier passed to that vendor's CLI/SDK.
type SubagentConfig struct {
	// Image is the default subagent image reference for all levels.
	// Example: "ghcr.io/jsquirrelz/tide-claude-subagent:v0.1.0".
	// +optional
	Image string `json:"image,omitempty"`

	// Model is the default model identifier for all levels.
	// Example: "claude-sonnet-4-6" (anthropic), "o1-mini" (openai).
	// +optional
	Model string `json:"model,omitempty"`

	// Levels carries per-level overrides; any subset is valid.
	// +optional
	Levels LevelOverrides `json:"levels,omitempty"`
}

// LevelOverrides carries per-level provider/model overrides (Phase 3 D-C2).
// Each pointer is nil when no override is set for that level.
type LevelOverrides struct {
	// Milestone optionally overrides settings for the milestone planner.
	// +optional
	Milestone *LevelConfig `json:"milestone,omitempty"`

	// Phase optionally overrides settings for the phase planner.
	// +optional
	Phase *LevelConfig `json:"phase,omitempty"`

	// Plan optionally overrides settings for the plan planner.
	// +optional
	Plan *LevelConfig `json:"plan,omitempty"`

	// Task optionally overrides settings for the task executor.
	// +optional
	Task *LevelConfig `json:"task,omitempty"`
}

// LevelConfig declares the per-level subagent override (Phase 3 D-C2).
type LevelConfig struct {
	// Model identifier for this level (e.g., "claude-haiku-4-5").
	// +optional
	Model string `json:"model,omitempty"`

	// Params carries per-vendor tuning passthrough (temperature, thinking
	// budget, etc.). Validation lives in the provider's Subagent impl.
	// +optional
	Params map[string]string `json:"params,omitempty"`

	// Image override is schema-present-but-not-enforced in v1.0 (deferred
	// to v1.x per CONTEXT.md "Deferred Ideas" — per-level cross-vendor
	// image overrides). Set this in v1.0 only if the consumer is prepared
	// to ignore it.
	// +optional
	Image string `json:"image,omitempty"`
}

// GitConfig declares the per-Project git target + creds (Phase 3 D-B6).
// HTTPS+PAT is the primary v1.0 path; SSH is documented with host-key caveats
// per REQUIREMENTS.md ART-05 but defaults to HTTPS+PAT.
type GitConfig struct {
	// RepoURL is the URL of the target git remote. Supports http:// and https://
	// (pure-Go go-git transport, production default) and SSH
	// (git@host:owner/repo.git, documented with host-key caveats).
	// file:// is NOT supported: go-git's file:// transport shells out to a
	// system git binary absent from production images.
	// +kubebuilder:validation:Pattern=`^(https?://|git@).+`
	RepoURL string `json:"repoURL"`

	// CredsSecretRef is the K8s Secret name (same-namespace) carrying the
	// HTTPS PAT in data key GIT_PAT. Cross-namespace Secret refs are NOT
	// permitted in v1.0 (threat-mitigation deferred to push Job — plan 03-06).
	CredsSecretRef string `json:"credsSecretRef"`

	// LeaksConfigRef is an optional ConfigMap name carrying gitleaks rule
	// overrides (D-B3). Default gitleaks rules are embedded in the push image
	// when this is empty.
	// +optional
	LeaksConfigRef string `json:"leaksConfigRef,omitempty"`

	// BaseRef optionally names the ref the run branch is created from: an
	// existing branch name, tag name, full 40-hex commit SHA, or a fully
	// qualified refs/... path. Absent means the remote default branch (HEAD).
	// Resolution happens in the clone Job; an unresolvable value surfaces as
	// CloneFailed/BaseRefUnresolvable. Edits after a successful clone are
	// inert — the run branch is created once (see docs/project-authoring.md).
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=250
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9][A-Za-z0-9._+@/-]*$`
	// +optional
	BaseRef string `json:"baseRef,omitempty"`

	// AgentName is the committer/author name TIDE stamps at all three commit
	// sites — the harness task commit, the integrate merge commit, and the
	// tide-push boundary commit (SIGN-01). Precedence (D-03): this field →
	// chart agent.name → the compiled-in "TIDE Agent" default. Angle brackets
	// and newlines are rejected because they corrupt git commit headers
	// (T-36-01).
	// +kubebuilder:validation:MaxLength=100
	// +kubebuilder:validation:Pattern=`^[^<>\r\n]+$`
	// +optional
	AgentName string `json:"agentName,omitempty"`

	// AgentEmail is the committer/author email paired with AgentName at all
	// three commit sites (SIGN-01). Precedence (D-03): this field → chart
	// agent.email → the compiled-in "tide-agent@tideproject.k8s" default.
	// Operators should choose a real, routable address: deferred commit
	// signing will require the committer email to match a verified
	// machine-account email, so picking one now avoids churn. Angle brackets,
	// whitespace, and non x@y shapes are rejected (T-36-01).
	// +kubebuilder:validation:MaxLength=254
	// +kubebuilder:validation:Pattern=`^[^<>@\s]+@[^<>@\s]+$`
	// +optional
	AgentEmail string `json:"agentEmail,omitempty"`
}

// GitStatus records the per-Project git push state (Phase 3 D-B6).
//
// One branch per Project lifetime: BranchName is fixed at Project creation
// as "tide/run-<project-name>-<unix-epoch>" (Unix epoch keeps refnames
// colon-free per RFC 3987 refname rules). LastPushedSHA + LeaseFailureCount
// drive the --force-with-lease push contract: each push uses lastPushedSHA
// as the lease, increments leaseFailureCount on rejection, and resets to 0
// on success.
type GitStatus struct {
	// BranchName is the lifetime branch fixed at Project creation.
	// Format: "tide/run-<project>-<unix-epoch>".
	// +optional
	BranchName string `json:"branchName,omitempty"`

	// LastPushedSHA is the head SHA recorded on the most recent successful push.
	// Used as the lease for subsequent `git push --force-with-lease`.
	// +optional
	LastPushedSHA string `json:"lastPushedSHA,omitempty"`

	// LeaseFailureCount tallies consecutive push lease rejections; reset to 0
	// on successful push. Reconciler halts (Phase=PushLeaseFailed) when this
	// count exceeds the configured retry budget.
	// +optional
	LeaseFailureCount int32 `json:"leaseFailureCount,omitempty"`

	// CloneComplete is true when the clone Job completed successfully.
	// This durable flag gates clone Job re-dispatch on resume, replacing the
	// TTL-unreliable Job-existence check (BYPASS-02 / Phase 27).
	// +optional
	CloneComplete bool `json:"cloneComplete,omitempty"`

	// BaseSHA is the commit SHA the run branch was created from, stamped on
	// every run (annotated tags record the peeled commit). Provenance only.
	// +optional
	BaseSHA string `json:"baseSHA,omitempty"`
}

// BudgetStatus records running spend tallies for the Project (Phase 2+).
// PERSIST-02 / Pitfall 4: this is a TALLY object, not an aggregate schedule.
// The PERSIST-02 denylist (enforced by `make verify-no-aggregates`) does not
// apply to this struct — it carries only spend counters (tokensSpent,
// costSpentCents, windowStart), not a derived execution plan.
type BudgetStatus struct {
	// TokensSpent is the cumulative token count spent since WindowStart.
	// +optional
	TokensSpent int64 `json:"tokensSpent,omitempty"`

	// CostSpentCents is the cumulative cost in USD cents since WindowStart.
	// +optional
	CostSpentCents int64 `json:"costSpentCents,omitempty"`

	// WindowStart marks the beginning of the current rolling budget window.
	// +optional
	WindowStart *metav1.Time `json:"windowStart,omitempty"`

	// PlannerRolledUpUID is the name of the most recent planner Job whose Usage
	// was successfully rolled up into CostSpentCents. Prevents double-counting
	// when the reporter Job has TTL-GC'd during a halt→resume cycle (BYPASS-03 / Phase 27).
	// +optional
	PlannerRolledUpUID string `json:"plannerRolledUpUID,omitempty"`

	// BypassBaselineCents is the CostSpentCents value at the time the most recent
	// budget-exceeded bypass was applied. Re-halt fires only when spend exceeds this
	// acknowledged baseline, preventing instant re-halt on cap-raise (BYPASS-04 / Phase 27).
	// +optional
	BypassBaselineCents int64 `json:"bypassBaselineCents,omitempty"`
}

// BoundaryPushStatus records the bounded auto-retry state of the project-level
// boundary push (debug defect #13b). The boundary push lands the already-
// integrated run branch on the remote AFTER the Project reaches Complete; it is
// observability + bounded recovery, NEVER a gate on Complete.
//
// PERSIST-02 / Pitfall 4: this is a per-project retry tally object, NOT an
// aggregate schedule. Attempts is re-derived from .status on every reconcile so
// the bounded retry survives a controller restart (no in-memory counter).
type BoundaryPushStatus struct {
	// Attempts is the number of boundary-push Jobs dispatched so far for this
	// Project's run branch. The controller stops dispatching once Attempts
	// reaches the in-controller cap (maxBoundaryPushAttempts) — bounded retry,
	// no push-loop.
	// +optional
	Attempts int32 `json:"attempts,omitempty"`

	// LastAttemptTime is the timestamp of the most recently dispatched boundary
	// push attempt. Drives the capped exponential requeue backoff.
	// +optional
	LastAttemptTime *metav1.Time `json:"lastAttemptTime,omitempty"`

	// LastError carries the most recent boundary-push failure reason (from the
	// push-result envelope) for operator visibility. Cleared on success.
	// +optional
	LastError string `json:"lastError,omitempty"`
}

// ProjectSpec defines the desired state of Project.
// +kubebuilder:validation:XValidation:rule="self.targetRepo.startsWith('http://') || self.targetRepo.startsWith('https://') || self.targetRepo.startsWith('git@')",message="targetRepo must be an http(s) or SSH (git@) URL; file:// is not a supported production transport (go-git's file:// transport requires a system git binary absent from production images)"
type ProjectSpec struct {
	// SchemaRevision identifies the v1alpha2 schema shape. Required in v1alpha2;
	// its absence on a reconciled object signals a v1alpha1-authored Project that
	// slipped into etcd before the CRD upgrade. The Plan-03 Project reconciler
	// head guard checks this field: if SchemaRevision != "v1alpha2" the reconciler
	// fail-closes with RequiresReinstall condition + reconcile.TerminalError (no
	// requeue). Reinstall: kubectl delete project <name> && kubectl apply -f
	// <project.yaml> (with SchemaRevision: v1alpha2 set). This field is absent in
	// v1alpha1 ProjectSpec, making it the clean discriminator for D-09.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=v1alpha2
	SchemaRevision string `json:"schemaRevision"`

	// TargetRepo is the URL of the repo this Project operates on. Supports
	// http:// and https:// (pure-Go go-git transport, production default) and
	// SSH (`git@host:owner/repo.git`, documented with host-key caveats).
	// file:// is NOT supported: go-git's file:// transport requires a system
	// git binary absent from production images.
	// +kubebuilder:validation:MinLength=1
	TargetRepo string `json:"targetRepo"`

	// OutcomePrompt is the human-authored outcome statement that TIDE turns
	// into a phase brief + PLAN.md + tasks (Phase 5 BOOT-04). The planner
	// subagent reads this verbatim from .spec; the v1.0 stub-subagent ignores
	// it (small samples can leave it empty for the smoke-test path).
	// Multi-line YAML literals (`|`) are the conventional shape.
	// +optional
	OutcomePrompt string `json:"outcomePrompt,omitempty"`

	// SecretRefs references K8s Secrets for credentials (AUTH-01 — Phase 3).
	// +optional
	SecretRefs SecretRefs `json:"secretRefs,omitempty"`

	// ModelSelection picks per-level model identifiers (Phase 2+).
	// +optional
	ModelSelection ModelSelection `json:"modelSelection,omitempty"`

	// Gates declares per-level human gate policy (Phase 4).
	// +optional
	Gates Gates `json:"gates,omitempty"`

	// ProviderSecretRef is the name of the K8s Secret carrying provider credentials (Phase 2+).
	// +optional
	ProviderSecretRef string `json:"providerSecretRef,omitempty"`

	// Providers lists per-provider configuration (rate limits, etc.) (Phase 2+).
	// +optional
	Providers []ProviderConfig `json:"providers,omitempty"`

	// Budget declares cost/token caps for this project (Phase 2+).
	// +optional
	Budget BudgetConfig `json:"budget,omitempty"`

	// PlanAdmission controls file-touch policy during plan validation (Phase 2+).
	// +optional
	PlanAdmission PlanAdmissionConfig `json:"planAdmission,omitempty"`

	// MaxAttemptsPerTask caps the number of dispatch attempts per Task before
	// the Task is marked failed (Phase 2+, consumed by TaskReconciler in Plan 09).
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10
	// +optional
	MaxAttemptsPerTask int32 `json:"maxAttemptsPerTask,omitempty"`

	// Subagent declares provider+model selection for planner/executor dispatch
	// (Phase 3 D-C2). When empty, Helm-chart defaults apply.
	// +optional
	Subagent SubagentConfig `json:"subagent,omitempty"`

	// Git declares the per-Project target repo + creds for artifact push
	// (Phase 3 D-B6). Required for any Project whose lifecycle reaches push;
	// optional in v1.0 only for purely transient/test Projects.
	// Pointer so omitempty fully elides the field when absent — value-type
	// GitConfig{} would serialize as `git: {repoURL: ""}` and trip the
	// RepoURL pattern validation on existing Phase 2 test fixtures.
	// +optional
	Git *GitConfig `json:"git,omitempty"`

	// FailureProfile controls how a task execution failure affects non-dependent
	// work in later waves. strict (default): non-dependents continue dispatching
	// (enforced automatically by the indegree model — a failed task never reaches
	// Succeeded, so only its dependents are blocked). conservative: first failure
	// stamps ConditionFailureHalt and halts all new dispatch project-wide until
	// `tide resume --retry-failed` is run.
	// +kubebuilder:validation:Enum=strict;conservative
	// +kubebuilder:default=strict
	// +optional
	FailureProfile FailureProfileType `json:"failureProfile,omitempty"`

	// ImportSource declares envelope salvage source for this Project.
	// When non-nil, ImportController runs the one-shot UID-rewrite import
	// state machine and all five planner-dispatch sites park until
	// ImportComplete=True (Phase 28 D-01/D-02).
	// +optional
	ImportSource *ImportSourceRef `json:"importSource,omitempty"`
}

// Project Phase constants for Project.Status.Phase (Plan 10 — init Job + budget gate).
const (
	// PhasePending is the initial phase before any reconcile has run.
	PhasePending = "Pending"
	// PhaseInitialized is set when the init Job completes successfully.
	PhaseInitialized = "Initialized"
	// PhaseInitFailed is set when the init Job exits non-zero.
	PhaseInitFailed = "InitFailed"
	// PhaseBudgetExceeded is set when the project's absolute cost cap is exceeded.
	PhaseBudgetExceeded = "BudgetExceeded"
	// PhaseRunning is set when dispatch is actively proceeding.
	PhaseRunning = "Running"
	// PhasePushLeaseFailed is set when a push Job rejects due to --force-with-lease
	// mismatch (Phase 3 D-B6). Recovery: kubectl annotate project
	// tideproject.k8s/bypass-push-lease=true (mirrors D-D4 bypass-budget pattern).
	PhasePushLeaseFailed = "PushLeaseFailed"
	// PhasePushLeakBlocked is set when a push Job exits with code 10 (gitleaks
	// finding — envelope.reason=leak-detected). Distinct from PhasePushLeaseFailed
	// (exit-11) so the project_controller switch can fire
	// tide_secret_leak_blocked_total only on the leak-class outcome (Phase 4 D-W1).
	// Recovery: operator inspects the diff and either drops the leaked secret
	// artifact or rotates the leaked credential, then clears the phase.
	PhasePushLeakBlocked = "PushLeakBlocked"
	// PhaseComplete is the terminal success phase — set when all Milestones
	// reach Succeeded and the final push Job lands (Phase 3 D-B2 #4).
	PhaseComplete = "Complete"
)

// ProjectStatus defines the observed state of Project.
// PERSIST-02 / Pitfall 4: NO aggregate schedule fields here.
type ProjectStatus struct {
	// Phase is a high-level state label ("Pending", "Running", "Complete", "Failed").
	// +optional
	Phase string `json:"phase,omitempty"`

	// Conditions follow the standard K8s convention.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Budget records running spend tallies (Phase 2+).
	// PERSIST-02 / Pitfall 4: BudgetStatus is a tally object, not an aggregate schedule.
	// +optional
	Budget BudgetStatus `json:"budget,omitempty"`

	// Git records per-Project push state — branch name, last pushed SHA, and
	// lease failure counter (Phase 3 D-B6). PERSIST-02 / Pitfall 4: this is a
	// per-project tally object, NOT an aggregate schedule.
	// +optional
	Git GitStatus `json:"git,omitempty"`

	// BoundaryPush records the bounded auto-retry state of the project-level
	// boundary push (debug defect #13b). NEVER gates Complete — surfaced via the
	// non-terminal BoundaryPushed condition for observability + bounded recovery.
	// +optional
	BoundaryPush BoundaryPushStatus `json:"boundaryPush,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// Project is the Schema for the projects API
type Project struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of Project
	// +required
	Spec ProjectSpec `json:"spec"`

	// status defines the observed state of Project
	// +optional
	Status ProjectStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ProjectList contains a list of Project
type ProjectList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Project `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Project{}, &ProjectList{})
}
