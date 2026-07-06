# Phase 40: Deprecate v1alpha1 API - Pattern Map

**Mapped:** 2026-07-06
**Files analyzed:** 34 (new + modified, grouped; consumer-migration is a mechanical repoint across ~47 non-test + ~65 test files not individually itemized below)
**Analogs found:** 34 / 34 (this phase's defining trait: Phase 23 already ran this exact crank once — every new file has a byte-for-byte precedent to diff against)

**How to read this file:** Phase 40 is "run Phase 23's shape again, then delete the two prior
versions." Every pattern assignment below cites the exact Phase 23 commit/file that did the
analogous work last time, not a generic idiom — the planner should diff v1alpha3 against
v1alpha2, not invent shape.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `api/v1alpha3/*.go` (groupversion_info, milestone/phase/plan/project/task/wave_types, shared_types, import_types, schema_test, zz_generated.deepcopy) | model (CRD schema) | CRUD | `api/v1alpha2/*.go` (same file set) | exact — copy-and-reshape precedent (commit `67cb313`) |
| `internal/webhook/v1alpha3/*.go` (project_webhook, plan_webhook, wave_webhook, strict_mode, file_touch_utils + 2 test files) | middleware (admission webhook) | request-response | `internal/webhook/v1alpha2/*.go` (same file set) | exact — whole-package rename precedent (commit `2de10b9`) |
| `internal/controller/project_controller.go` (`checkSchemaRevisionGuard`) | controller (runtime guard) | request-response | same file, prior shape (Phase 23 Plan 23-03, commit `dc49fcd`) | exact — parameterize, don't reinvent |
| `internal/controller/task_controller.go` (`walkOwnerChainToProjectDepth`) | controller (owner-ref resolution) | CRUD | same file, prior dual-accept shape | exact — delete one disjunct |
| `internal/dispatch/podjob/backend.go` (`walkOwnerChain`) | service (owner-ref resolution) | CRUD | same file, prior dual-accept shape | exact — delete one disjunct |
| `internal/controller/dispatch_helpers.go` (`ResolveProvider`, `resolveImage`) | service (dispatch resolution) | request-response | same file — switch cases unchanged, only callers' passed level-string changes | exact — no diff needed in this file itself |
| `internal/controller/{project,milestone,phase,plan}_controller.go` (4 dispatch call sites) | controller | request-response | each file's own prior dispatch block | exact — 3-line string-literal edit per file |
| `pkg/dispatch/envelope.go` (`APIVersionV1Alpha1` const + `ValidateAPIVersionKind`) | utility (contract constant) | transform | same file, prior value | exact — value-only change, propagates to all consumers automatically |
| `pkg/dispatch/doc.go` (package doc comment) | config (doc-as-contract) | — | same file, prior comment | exact — comment rewrite, no code change |
| `cmd/tide-push/main.go:116`, `cmd/tide-eval/main.go:83` | utility (literal envelope-version copies) | transform | `pkg/dispatch/envelope.go`'s constant (source of truth these two drifted from) | role-match — fix drift by referencing the constant, not just updating the string |
| `cmd/manager/main.go` (scheme registration + webhook wiring + comments) | config (composition root) | request-response | same file, Phase 23 diff (commit `c5f136e`) | exact — same 3 call sites, same file |
| `cmd/dashboard/main.go` (scheme registration) | config (composition root) | request-response | same file, Phase 23 diff | exact |
| `cmd/tide/root_flags.go` (CLI scheme registration) | config (composition root) | request-response | same file, Phase 23 diff | exact |
| `docs/migration/v1alpha2-to-v1alpha3.md` (new) | config (docs) | — | `docs/migration/v1alpha1-to-v1alpha2.md` | exact — structural template named in CONTEXT.md canonical_refs |
| `docs/INSTALL.md`, `docs/gates.md`, `docs/git-hosts.md`, `docs/project-authoring.md`, `README.md` | config (docs) | — | each file's own current v1alpha1 example block | exact — same doc, bump apiVersion + add schemaRevision |
| `config/samples/tide_v1alpha3_*.yaml` (11 files, renamed from `tide_v1alpha1_*.yaml`) | config (sample manifests) | — | `config/samples/tide_v1alpha1_*.yaml` (same 11, content+filename) | exact |
| `config/samples/kustomization.yaml` | config | — | same file, prior `resources:` list | exact — filename list must move in lockstep with D-06 renames |
| `PROJECT` (kubebuilder metadata) | config | — | same file, prior version/path entries | exact — `version: v1alpha1` → `v1alpha3`, path suffix, drop stale `Plan.webhooks.conversion: true` |
| `Makefile` (`verify-no-aggregates` target) | config (build tooling) | batch | same file, prior glob | exact — glob pattern swap (see Open Question 2 in RESEARCH.md re: `v1alpha*` durable form) |
| `api/v1alpha3/schema_test.go` (new) | test | CRUD | `api/v1alpha2/schema_test.go` | exact — mirror structure, Wave 0 gap per RESEARCH.md |
| relocated dogfood-manifest schema test (new location TBD, e.g. `test/integration/schema/`) | test | file-I/O | `api/v1alpha1/dogfood_manifests_test.go` (dies with package; relocate, don't delete) | exact — same fixture-strict-decode pattern, collapse `supportedProjectAPIVersions` to v1alpha3-only |
| `internal/controller/project_controller_v2_guard_test.go` (rename/extend for v1alpha3; currently imports `api/v1alpha1` — that import must be removed) | test | CRUD | same file, prior shape | exact — extend, don't replace (RESEARCH.md Validation Architecture table) |
| `test/integration/envtest/planner_dispatch_test.go` (extend with rename assertions) | test | request-response | same file, existing `SchemaRevision: "v1alpha2"` fixture literal | exact — bump literal + add per-level assertion |
| `test/integration/envtest/spec_conformance_test.go` (bump 2 `SchemaRevision` literals) | test | CRUD | same file | exact |
| `pkg/dispatch/envelope_test.go` (already exists — extend, not create) | test | transform | same file's `TestValidateAPIVersionKind_*` block | exact — Wave-0-gap note in RESEARCH.md was conservative; file exists, confirmed by `ls pkg/dispatch/` |

## Pattern Assignments

### `api/v1alpha3/*.go` (model, CRUD)

**Analog:** `api/v1alpha2/` (whole package, introduced Phase 23 Plan 23-01, commit `67cb313`)

**Package doc + scheme registration pattern** (`api/v1alpha2/groupversion_info.go`, full file, 41 lines):
```go
// Package v1alpha2 contains API Schema definitions for the tideproject v1alpha2 API group.
// +kubebuilder:object:generate=true
// +groupName=tideproject.k8s
package v1alpha2

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// SchemeGroupVersion is group version used to register these objects.
	// This name is used by applyconfiguration generators (e.g. controller-gen).
	SchemeGroupVersion = schema.GroupVersion{Group: "tideproject.k8s", Version: "v1alpha2"}

	// GroupVersion is an alias for SchemeGroupVersion, for backward compatibility.
	GroupVersion = SchemeGroupVersion

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme.
	//nolint:staticcheck // SA1019: kubebuilder-scaffolded scheme.Builder; canonical CRD registration pattern
	SchemeBuilder = &scheme.Builder{GroupVersion: SchemeGroupVersion}

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)
```
For v1alpha3: same file, `package v1alpha2` → `package v1alpha3`, `Version: "v1alpha2"` → `"v1alpha3"`, doc comment bumped. Zero other structural change.

**Storageversion marker placement** (verified via grep across all 6 Kind files):
```go
// api/v1alpha2/plan_types.go:91-98 (identical marker block shape on all 6 Kinds:
// Milestone, Phase, Plan, Project, Task, Wave)
// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"
// +kubebuilder:validation:XValidation:rule="!has(self.spec.dependsOn) || !(self.metadata.name in self.spec.dependsOn)",message="a plan cannot depend on itself"
```
For v1alpha3: `+kubebuilder:storageversion` moves onto the new package's 6 Kinds in the SAME
commit that regenerates manifests (RESEARCH.md: "do not leave an intermediate state with two
versions both claiming storage"). The v1alpha2 package's marker is removed in that same commit
if v1alpha2 is being deleted in this phase (D-01 full lifecycle turn) rather than kept around a
release cycle (contrast with Phase 23, which kept v1alpha1 registered without storageversion).

**SchemaRevision discriminator field** (`api/v1alpha2/project_types.go:316-329`):
```go
// ProjectSpec defines the desired state of Project.
type ProjectSpec struct {
	// SchemaRevision identifies the v1alpha2 schema shape. Required in v1alpha2;
	// its absence on a reconciled object signals a v1alpha1-authored Project that
	// slipped into etcd before the CRD upgrade. ...
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=v1alpha2
	SchemaRevision string `json:"schemaRevision"`
	...
```
For v1alpha3: `+kubebuilder:validation:Enum=v1alpha2` → `Enum=v1alpha3`; comment text updated to
describe the v1alpha2→v1alpha3 transition (mirrors the exact prose shape, just one crank later).

**Schema test structure** (`api/v1alpha2/schema_test.go:1-33`, full header + first test):
```go
// Package v1alpha2_test carries structural unit tests for the v1alpha2 API type
// package. These tests validate the type-level shape of the Spring Tide schema
// reshape (SCHEMA-01, DEPS-01, DEPS-02) without spinning up an envtest harness.
//
// Test name map (matches VALIDATION.md run-name map):
//   - TestWaveSpec   — SCHEMA-01: WaveSpec.ProjectRef replaces PlanRef; WaveIndex is global
//   - TestTaskDependsOn — DEPS-01: TaskSpec.DependsOn accepts cross-scope names
//   - TestPlanDependsOn — DEPS-02: PlanSpec.DependsOn field present and validates
package v1alpha2_test

import (
	"reflect"
	"testing"

	tidev1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
)

func TestWaveSpec(t *testing.T) {
	w := tidev1alpha2.Wave{
		Spec: tidev1alpha2.WaveSpec{ProjectRef: "my-project", WaveIndex: 3},
	}
	if got := w.Spec.ProjectRef; got != "my-project" {
		t.Errorf("Spec.ProjectRef = %q, want %q", got, "my-project")
	}
	// reflect-based negative assertion pattern: confirm a field does NOT exist,
	// catching regressions that reintroduce old shape.
	waveSpecType := reflect.TypeFor[tidev1alpha2.WaveSpec]()
	if _, ok := waveSpecType.FieldByName("PlanRef"); ok {
		t.Errorf("WaveSpec has field PlanRef — old Plan-scoped ownership must be removed (SCHEMA-01)")
	}
}
```
For `api/v1alpha3/schema_test.go`: same reflect-based positive/negative field-assertion idiom;
new tests should assert `ProjectSpec` no longer has `ModelSelection` (if W1 is taken) and that
`LevelOverrides`/`SubagentConfig` field names are unchanged (D-02 is a semantic rename, not a
struct rename — this test should PROVE that by asserting the fields Milestone/Phase/Plan/Task
still exist by those names on `LevelOverrides`).

---

### `internal/webhook/v1alpha3/*.go` (middleware, request-response)

**Analog:** `internal/webhook/v1alpha2/` (whole package, ported from v1alpha1 Phase 23 Plan 23-02, commit `2de10b9`)

**Full webhook registration + validator pattern** (`internal/webhook/v1alpha2/project_webhook.go`, full file, 91 lines):
```go
package v1alpha2

import (
	"context"
	"fmt"
	"strings"

	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
)

var projectlog = logf.Log.WithName("project-webhook-v1alpha2") //nolint:logcheck // controller-runtime logf idiom

func SetupProjectWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &tideprojectv1alpha2.Project{}).
		WithValidator(&ProjectCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-tideproject-k8s-v1alpha2-project,mutating=false,failurePolicy=fail,sideEffects=None,groups=tideproject.k8s,resources=projects,verbs=create;update,versions=v1alpha2,name=vproject-v1alpha2.kb.io,admissionReviewVersions=v1

type ProjectCustomValidator struct{}

func (v *ProjectCustomValidator) ValidateCreate(_ context.Context, obj *tideprojectv1alpha2.Project) (admission.Warnings, error) {
	return v.validate(obj)
}
// ... ValidateUpdate / ValidateDelete / validate() — denylist-check business logic
// is version-agnostic and copies verbatim.
```
For `internal/webhook/v1alpha3/project_webhook.go`: package rename, import alias rename, the
`+kubebuilder:webhook:path=...versions=v1alpha2...name=vproject-v1alpha2.kb.io` marker's TWO
`v1alpha2` occurrences both become `v1alpha3` (path segment AND `versions=` field AND the
`name=` suffix — three occurrences per marker line, easy to under-edit with a naive
find-replace that only catches one). The validator business logic (denylist enforcement) is
untouched — it is schema-version-agnostic.

**Manager wiring call sites** (`cmd/manager/main.go:592-604`, exact current text):
```go
	// 8. Register both webhooks (CRD-04, CRD-05).
	// Plan webhook moved to v1alpha2 (Spring Tide breaking change, Plan 23-02).
	if err := webhookv1alpha2.SetupPlanWebhookWithManager(mgr, defaultFileTouchMode); err != nil {
		setupLog.Error(err, "unable to create webhook", "kind", "Plan")
		os.Exit(1)
	}
	// Wave webhook re-registered for v1alpha2 (D-B1 re-registration, Plan 23-02).
	if err := webhookv1alpha2.SetupWaveWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "kind", "Wave")
		os.Exit(1)
	}
	if err := webhookv1alpha2.SetupProjectWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "kind", "Project")
		os.Exit(1)
```
All 3 call sites' `webhookv1alpha2.` alias becomes `webhookv1alpha3.`; the import alias at
`cmd/manager/main.go:67` and comment text ("Plan 23-02" → new plan number) both need the D-07
sweep. Same 3-callsite pattern repeats verbatim in `test/integration/envtest/suite_test.go`
(RESEARCH.md, confirmed).

---

### `internal/controller/project_controller.go::checkSchemaRevisionGuard` (controller, request-response)

**Analog:** same function, current shape (`internal/controller/project_controller.go:1662-1704`)

**Current implementation** (full function, 43 lines):
```go
// checkSchemaRevisionGuard is the SCHEMA-03 / D-09 fail-closed guard.
// It rejects any v1alpha2 Project whose Spec.SchemaRevision is not "v1alpha2" —
// the absence of this field signals an object that was authored under the
// v1alpha1 schema and slipped into etcd before the CRD upgrade.
func (r *ProjectReconciler) checkSchemaRevisionGuard(
	ctx context.Context,
	project *tidev1alpha2.Project,
) (blocked bool, err error) {
	if project.Spec.SchemaRevision == "v1alpha2" {
		return false, nil
	}

	// The SchemaRevision is absent or wrong — this object was authored under
	// v1alpha1. Surface a permanent failure condition and halt reconciliation.
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:   tidev1alpha2.ConditionReady,
		Status: metav1.ConditionFalse,
		Reason: tidev1alpha2.ReasonRequiresReinstall,
		Message: "Project was created with v1alpha1 schema; reinstall required: " +
			"kubectl delete project " + project.Name +
			" && kubectl apply -f <project.yaml> (with schemaRevision: v1alpha2 set). " +
			"See docs/migration/v1alpha1-to-v1alpha2.md.",
		LastTransitionTime: metav1.Now(),
	})
	if updateErr := r.Status().Update(ctx, project); updateErr != nil {
		logf.FromContext(ctx).Error(updateErr,
			"failed to update RequiresReinstall condition; condition may not be visible yet",
			"project", project.Name)
	}
	return true, reconcile.TerminalError(
		fmt.Errorf("project %s/%s requires reinstall (v1alpha1 schema; schemaRevision must be v1alpha2)",
			project.Namespace, project.Name),
	)
}
```
**D-04 generalization target** (from RESEARCH.md Code Examples, verbatim — this is the plan-ready shape):
```go
const (
	expectedSchemaRevision = "v1alpha3"
	migrationGuideDocPath  = "docs/migration/v1alpha2-to-v1alpha3.md"
)

func (r *ProjectReconciler) checkSchemaRevisionGuard(
	ctx context.Context,
	project *tidev1alpha3.Project,
) (blocked bool, err error) {
	if project.Spec.SchemaRevision == expectedSchemaRevision {
		return false, nil
	}
	// message references expectedSchemaRevision + migrationGuideDocPath — a future
	// v1alpha4 crank changes exactly these two constants and nothing else.
	...
}
```
**Test analog** (`internal/controller/project_controller_v2_guard_test.go:38-51`, scheme setup —
NOTE this file currently imports `api/v1alpha1` for the "old shape" simulation; that import
dies with package removal and must be replaced by constructing a v1alpha3 Project with an
empty/wrong `SchemaRevision` string directly, no cross-version import needed):
```go
tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
tidev1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
...
if err := tideprojectv1alpha1.AddToScheme(s); err != nil {
	t.Fatalf("AddToScheme v1alpha1: %v", err)
}
if err := tidev1alpha2.AddToScheme(s); err != nil {
	t.Fatalf("AddToScheme v1alpha2: %v", err)
}
```
Post-removal, the "old shape" test (`TestOldShapeRejection`, line 59) constructs
`SchemaRevision: ""` directly on the CURRENT type — it never actually needed the v1alpha1
import to express "empty/wrong revision"; drop the v1alpha1 scheme registration and import
entirely, keep the empty-string construction.

---

### `internal/controller/task_controller.go` + `internal/dispatch/podjob/backend.go` (D-05 owner-ref simplification)

**Analog:** same two functions, current dual-accept shape

**`task_controller.go:1200-1205`** (`walkOwnerChainToProjectDepth`):
```go
func (r *TaskReconciler) walkOwnerChainToProjectDepth(ctx context.Context, obj client.Object, depth int) (*tideprojectv1alpha2.Project, error) {
	if depth <= 0 || obj == nil {
		return nil, nil
	}
	for _, ref := range obj.GetOwnerReferences() {
		if ref.Kind == "Project" && (ref.APIVersion == "tideproject.k8s/v1alpha1" || ref.APIVersion == tideprojectv1alpha2.GroupVersion.String()) {
```
**`internal/dispatch/podjob/backend.go:372-377`** (`walkOwnerChain`, identical pattern):
```go
func (b *PodJobBackend) walkOwnerChain(ctx context.Context, obj client.Object, depth int) (*tidev1alpha2.Project, error) {
	if depth <= 0 || obj == nil {
		return nil, nil
	}
	for _, ref := range obj.GetOwnerReferences() {
		if ref.Kind == "Project" && (ref.APIVersion == "tideproject.k8s/v1alpha1" || ref.APIVersion == tidev1alpha2.GroupVersion.String()) {
```
**Target (both sites, per D-05 + RESEARCH.md Code Examples):**
```go
if ref.Kind == "Project" && ref.APIVersion == tideprojectv1alpha3.GroupVersion.String() {
```
Both sites are otherwise byte-identical outside the receiver type — one diff pattern applies
twice.

---

### `internal/controller/dispatch_helpers.go` + 4 controller call sites (RENAME, request-response)

**Analog:** same functions/files, current level-string wiring

**`ResolveProvider` switch** (`internal/controller/dispatch_helpers.go:138-152`, unchanged by this rename — included so the planner does NOT touch it):
```go
func ResolveProvider(project *tideprojectv1alpha2.Project, level string, helmDefaults ProviderDefaults) pkgdispatch.ProviderSpec {
	var levelCfg *tideprojectv1alpha2.LevelConfig
	if project != nil {
		switch level {
		case "milestone":
			levelCfg = project.Spec.Subagent.Levels.Milestone
		case "phase":
			levelCfg = project.Spec.Subagent.Levels.Phase
		case "plan":
			levelCfg = project.Spec.Subagent.Levels.Plan
		case "task":
			levelCfg = project.Spec.Subagent.Levels.Task
		}
	}
	...
```
**`resolveImage`** (`internal/controller/dispatch_helpers.go:260-291`, same switch shape, same
untouched cases — confirms today `level="project"` matches NEITHER switch, silently falling
through to `Spec.Subagent.Image`/`helmDefaults.Image` with no per-level override, exactly the
bug the rename fixes):
```go
// Level "project" has no entry in the switch (the CRD has no Levels.Project);
// resolution falls straight to Spec.Subagent.Image, then helmDefaults.Image.
func resolveImage(project *tideprojectv1alpha2.Project, level string, helmDefaults ProviderDefaults) string {
	var levelCfg *tideprojectv1alpha2.LevelConfig
	if project != nil {
		switch level {
		case "milestone":
			levelCfg = project.Spec.Subagent.Levels.Milestone
		case "phase":
			levelCfg = project.Spec.Subagent.Levels.Phase
		case "plan":
			levelCfg = project.Spec.Subagent.Levels.Plan
		case "task":
			levelCfg = project.Spec.Subagent.Levels.Task
		}
	}
	...
```
**Neither function needs to change** — only the literal strings passed in by the 4 call sites
change, and after the rename `"project"` is never passed anywhere (it moves to `"milestone"`),
so the switch's coverage becomes complete instead of partial.

**Exact 4 call-site edits** (verified current line numbers, all four planner reconcilers):
```go
// project_controller.go:1214, 1241, 1246 — authors MILESTONE.md
BuildPlannerEnvelope("project", project, project, attempt, "", project.Spec.OutcomePrompt, ...)
Level: "project",
resolveImage(project, "project", r.HelmProviderDefaults)
// AFTER: "milestone" (levels.milestone = "authors MILESTONE.md")

// milestone_controller.go:450, 481, 486 — authors phase briefs
BuildPlannerEnvelope("milestone", ms, project, attempt, "", plannerPrompt, ...)
Level: "milestone",
resolveImage(project, "milestone", r.HelmProviderDefaults)
// AFTER: "phase"

// phase_controller.go:413, 443, 448 — authors PLAN.md
BuildPlannerEnvelope("phase", ph, project, attempt, "", plannerPrompt, ...)
Level: "phase",
resolveImage(project, "phase", r.HelmProviderDefaults)
// AFTER: "plan"

// plan_controller.go:440, 470, 475 — authors the task DAG
BuildPlannerEnvelope("plan", plan, project, attempt, "", plannerPrompt, ...)
Level: "plan",
resolveImage(project, "plan", r.HelmProviderDefaults)
// AFTER: unchanged — collapses with phase_controller.go's new "plan" under one
// levels.plan override slot (Claude's Discretion per CONTEXT.md)

// task_controller.go:1485 (and resolveImage call sites at 762, 1414) — unchanged, no rename
Level: "task",
resolveImage(project, "task", r.Deps.HelmProviderDefaults)
```
**Sites that must NOT be touched** (same vocabulary, different meaning — verified by reading in
full): `internal/controller/push_helpers.go:402` `buildCommitMessage(boundary, name)`, and
`internal/gates/policy.go`'s `EvaluatePolicy`/`DefaultGates` (Gates.Milestone gates the
Milestone CR's OWN boundary, no parent-child indirection — no off-by-one bug exists there).

---

### `pkg/dispatch/envelope.go` + `pkg/dispatch/doc.go` (D-08 envelope decoupling)

**Analog:** same files, current single-group shape

**Constant + validator** (`pkg/dispatch/envelope.go:21-24, 424-439`, full relevant excerpt):
```go
// APIVersionV1Alpha1 is the envelope contract version shipped in TIDE v1alpha1.
// Consumers MUST reject envelopes whose apiVersion field does not match this
// constant via [ValidateAPIVersionKind].
const APIVersionV1Alpha1 = "tideproject.k8s/v1alpha1"
...
// ValidateAPIVersionKind checks that apiVersion equals [APIVersionV1Alpha1]
// and that kind equals expectedKind. It is the first call an envelope consumer
// must make before accessing any other field (D-A3).
func ValidateAPIVersionKind(apiVersion, kind, expectedKind string) error {
	if apiVersion != APIVersionV1Alpha1 {
		return &UnknownAPIVersionError{APIVersion: apiVersion}
	}
	if kind != expectedKind {
		return &UnknownKindError{Kind: kind}
	}
	return nil
}
```
For D-08: `APIVersionV1Alpha1 = "tideproject.k8s/v1alpha1"` → `"dispatch.tideproject.k8s/v1alpha1"`
(group changes, version component stays `v1alpha1` per CONTEXT.md — "pure group decoupling, no
stability claim"). Every consumer that imports this constant (confirmed: `internal/subagent/`,
`cmd/claude-subagent/`, `cmd/stub-subagent/` reference it programmatically, zero literal
strings) picks up the new value automatically — no per-consumer edit needed there.

**Package doc — the exact comment this phase supersedes** (`pkg/dispatch/doc.go:22-32`):
```go
// The contract is versioned by the apiVersion / kind discriminator (D-A3):
// every envelope JSON carries explicit "apiVersion: tideproject.k8s/v1alpha1"
// and "kind: TaskEnvelopeIn | TaskEnvelopeOut". Consumers MUST call
// [ValidateAPIVersionKind] before processing any field. ...
//
// JSON tag stability is the public contract. Field names under v1alpha1 are
// frozen after this plan ships. Future breaking changes (e.g., new required
// fields) ride a v1beta1 apiVersion bump via the same hub/spoke conversion
// path the CRDs use (CRD-05 scaffold).
```
Rewrite target: replace the "ride a v1beta1 apiVersion bump via the same hub/spoke conversion
path the CRDs use" sentence with group-decoupling rationale, citing the kubeadm precedent
(`kubeadm.k8s.io/v1beta4` — a K8s-shaped document that is not a served resource gets its own
subdomain group) per CONTEXT.md D-08.

**Independent literal copies that will NOT follow the constant** (must hand-edit both):
```go
// cmd/tide-push/main.go:116 — does NOT import pkg/dispatch, legitimately independent
envelopeAPIVersion = "tideproject.k8s/v1alpha1"

// cmd/tide-eval/main.go:83 — DOES import pkg/dispatch (line 61) yet hardcodes the
// literal anyway (pre-existing drift wart, Pitfall 4) — fix by referencing the
// constant instead of just updating the string value:
APIVersion: "tideproject.k8s/v1alpha1",
// →  APIVersion: pkgdispatch.APIVersionV1Alpha1,
```

**Existing test coverage to extend** (`pkg/dispatch/envelope_test.go:329-376` — file already
exists, contradicting RESEARCH.md's "verify at plan time" hedge; confirmed present via `ls`):
```go
func TestValidateAPIVersionKind_RejectsUnknownAPIVersion(t *testing.T) {
	err := ValidateAPIVersionKind("tideproject.k8s/v2", KindTaskEnvelopeIn, KindTaskEnvelopeIn)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var target *UnknownAPIVersionError
	if !errors.As(err, &target) {
		t.Fatalf("expected *UnknownAPIVersionError, got %T: %v", err, err)
	}
	...
}
func TestEnvelopeIn_Constants(t *testing.T) {
	if APIVersionV1Alpha1 != "tideproject.k8s/v1alpha1" {
		t.Errorf("APIVersionV1Alpha1 = %q, want %q", APIVersionV1Alpha1, "tideproject.k8s/v1alpha1")
	}
```
Add a new subtest asserting the DECOUPLED value (`"dispatch.tideproject.k8s/v1alpha1"`) and that
the OLD undecoupled group string (`"tideproject.k8s/v1alpha1"`) is now rejected by
`ValidateAPIVersionKind` — same `errors.As(&UnknownAPIVersionError{})` idiom as the existing
`TestValidateAPIVersionKind_RejectsUnknownAPIVersion`.

---

### `cmd/manager/main.go`, `cmd/dashboard/main.go`, `cmd/tide/root_flags.go` (scheme registration, config)

**Analog:** each file's own current registration block (Phase 23 diff, commit `c5f136e`)

**`cmd/manager/main.go:303-311`** (current — NOTE the duplicate `AddToScheme` call and the
factually-wrong comment RESEARCH.md flagged; this is a real pre-existing bug this phase should
fix while touching the block anyway):
```go
	// 2. Build scheme with v1alpha1 + corev1 + batchv1.
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(tidev1alpha2.AddToScheme(scheme))
	// Register v1alpha2 as the served+storage version (Spring Tide breaking CRD change,
	// Plan 23-02). v1alpha1 remains registered so the manager can decode any surviving
	// v1alpha1 objects for the Plan-03 reinstall guard (RequiresReinstall path).
	utilruntime.Must(tidev1alpha2.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
```
Target: single `tidev1alpha3.AddToScheme(scheme)` call (the duplicate line and the "v1alpha1
remains registered" comment are both stale — D-01's reinstall-only model means v1alpha1/v1alpha2
are NOT kept registered at all, unlike Phase 23's transitional posture).

**`cmd/dashboard/main.go:97-102`** (same shape, single registration already — no duplicate bug here):
```go
	// 1. Build scheme with v1alpha1 + corev1 (needed for pods/log proxy
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(tidev1alpha2.AddToScheme(scheme))
```
**`cmd/tide/root_flags.go:50-57`** (CLI typed client, same shape):
```go
// builtins and api/v1alpha1 (Project, Milestone, Phase, Plan, Task, Wave).
...
utilruntime.Must(clientgoscheme.AddToScheme(s))
utilruntime.Must(tidev1alpha2.AddToScheme(s))
```
All three: import alias `tidev1alpha2`/`tideprojectv1alpha2` → `tidev1alpha3`, comment text
updated (D-07 sweep — "v1alpha1" in comments at lines like `root_flags.go:50,86` is stale prose,
not code).

---

### Docs & samples (D-06, config, no runtime tier)

**Analog:** `docs/migration/v1alpha1-to-v1alpha2.md` (full file, 196 lines) — structural template
for the new `docs/migration/v1alpha2-to-v1alpha3.md` chapter. Section order to mirror exactly:
"What changed and why" → "Version bump" → "Reinstall Procedure" (numbered, with `kubectl`
snippets) → "Fail-Closed Safety Net" (shows the `RequiresReinstall` condition YAML) → "Dogfood
Cluster Note". The new chapter adds a **levels-remap table** (old key meaning → new key meaning)
per D-06 — no existing table to copy verbatim, but the "What changed and why" subsections (e.g.
"### Wave ownership: Plan → Project (D-07, SCHEMA-01)") are the exact prose-block shape to reuse
for "### subagent.levels semantic rename (D-02)".

**Doc example blocks needing apiVersion bump + schemaRevision addition** (verified grep hits):
```
docs/gates.md:14              apiVersion: tideproject.k8s/v1alpha1
README.md:108                 apiVersion: tideproject.k8s/v1alpha1
docs/INSTALL.md:210            apiVersion: tideproject.k8s/v1alpha1
docs/git-hosts.md:68           apiVersion: tideproject.k8s/v1alpha1
docs/project-authoring.md:132,210,320   apiVersion: tideproject.k8s/v1alpha1
docs/project-authoring.md:5     **Status:** v1.0; covers `Project.Spec` at the `tideproject.k8s/v1alpha1` schema lock
docs/project-authoring.md:11,33 links to api/v1alpha1/project_types.go
```
Every one of these examples currently lacks `spec.schemaRevision` entirely (v1alpha1 has no such
field) — the v1alpha3 replacement examples must ADD `schemaRevision: v1alpha3` alongside the
apiVersion bump, or they will trip the fail-closed guard if a reader copy-pastes them.

**Sample manifest pattern** (`config/samples/tide_v1alpha1_project.yaml`, full file, 14 lines):
```yaml
# tide_v1alpha1_project.yaml — root of the α…θ worked-example hierarchy.
# Project → Milestone → Phase → Plan → 8 Tasks (alpha…theta).
apiVersion: tideproject.k8s/v1alpha1
kind: Project
metadata:
  name: sample-project
  namespace: tide-samples
  labels:
    app.kubernetes.io/name: tide
    app.kubernetes.io/managed-by: kustomize
    tideproject.k8s/sample: "true"
spec:
  targetRepo: https://github.com/jsquirrelz/tide.git
```
Target `config/samples/tide_v1alpha3_project.yaml`: filename + `apiVersion` + header comment
bump, PLUS add `spec.schemaRevision: v1alpha3` (currently absent because v1alpha1 predates that
field — this is a required-field addition, not just a version-string swap, or the sample fails
the guard on apply).

**`config/samples/kustomization.yaml`** (full file, 37 lines) — the `resources:` list and its
header comment both enumerate all 11 `tide_v1alpha1_*.yaml` filenames; every filename must be
renamed in lockstep with the D-06 sample renames (easy to miss — RESEARCH.md flags this
explicitly as "a kustomization manifest, not prose").

---

### Build tooling (BUILD, config, batch)

**Analog:** same Makefile target, current hardcoded glob (`Makefile:552-561`, full target):
```makefile
.PHONY: verify-no-aggregates
verify-no-aggregates: ## Assert api/v1alpha1 and api/v1alpha2 declare no aggregate schedule fields (PERSIST-02 / Pitfall 4).
	@echo "verifying no aggregate schedule fields on api/v1alpha1 and api/v1alpha2 types (PERSIST-02)..."
	@MATCHES=$$(grep -nE 'Schedule|Waves *\[\]|IndegreeMap|CachedDag|DerivedDag' api/v1alpha1/*_types.go api/v1alpha2/*_types.go || true); \
	if [ -n "$$MATCHES" ]; then \
		echo "PERSIST-02 violation: aggregate schedule fields detected:"; \
		echo "$$MATCHES"; \
		exit 1; \
	fi
	@echo "OK: no aggregate schedule fields"
```
Target: glob updates to `api/v1alpha3/*_types.go` (or the more durable `api/v1alpha*/*_types.go`
per RESEARCH.md Open Question 2 — planner/user decides which). **This MUST land in the same
commit as the `api/v1alpha1`/`api/v1alpha2` directory deletion**, not a follow-up — the `|| true`
swallows a "No such file" grep error, so a stale glob produces a false-green CI run
(RESEARCH.md Pitfall 3).

**`PROJECT` file** (kubebuilder metadata, full relevant block, 6 `resources[]` entries all
share this shape):
```yaml
- api:
    crdVersion: v1
    namespaced: true
  controller: true
  domain: k8s
  group: tideproject
  kind: Project
  path: github.com/jsquirrelz/tide/api/v1alpha1
  version: v1alpha1
```
Plus the stale `Plan` entry's decorative claim:
```yaml
  kind: Plan
  path: github.com/jsquirrelz/tide/api/v1alpha1
  version: v1alpha1
  webhooks:
    conversion: true
    validation: true
    webhookVersion: v1
```
Target: all 6 entries' `path`/`version` → `api/v1alpha3`/`v1alpha3`; drop `conversion: true` from
the `Plan` entry (the conversion webhook was fully retired in Phase 23 commit `c5f136e` — this
line has been decorative-but-wrong since then, cheap to fix in the same pass per RESEARCH.md).
Non-functional (doesn't feed `make manifests`/`make generate`, which use explicit `paths=`
flags) but worth fixing per RESEARCH.md's "cheap in the same pass" framing.

---

### Test relocation (VERIFY, test, file-I/O)

**Analog:** `api/v1alpha1/dogfood_manifests_test.go` (dies with package removal — relocate, don't delete)

**Multi-version strict-decode pattern to preserve** (full relevant excerpt, lines 47-52, 120-154):
```go
// supportedProjectAPIVersions is the set of apiVersions a dogfood Project may
// declare. New schema versions get added here as they ship.
var supportedProjectAPIVersions = map[string]bool{
	"tideproject.k8s/v1alpha1": true,
	"tideproject.k8s/v1alpha2": true,
}

func TestDogfoodManifests_StrictDecode(t *testing.T) {
	for _, path := range dogfoodGlob(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			...
			switch av := projectAPIVersion(t, doc); av {
			case "tideproject.k8s/v1alpha1":
				var proj tideprojectv1alpha1.Project
				if err := sigsyaml.UnmarshalStrict(doc, &proj); err != nil {
					t.Errorf("UnmarshalStrict (v1alpha1) failed for Project doc in %s: %v", path, err)
				}
			case "tideproject.k8s/v1alpha2":
				var proj tideprojectv1alpha2.Project
				if err := sigsyaml.UnmarshalStrict(doc, &proj); err != nil {
					t.Errorf("UnmarshalStrict (v1alpha2) failed for Project doc in %s: %v", path, err)
				}
			default:
				t.Errorf("Project doc in %s declares unsupported apiVersion %q", path, av)
			}
			...
```
For the relocated test (new package, e.g. `test/integration/schema/dogfood_manifests_test.go`,
`package schema_test` or similar — NOT `package v1alpha1_test` since that package dies): collapse
`supportedProjectAPIVersions` to `{"tideproject.k8s/v1alpha3": true}` only (single-entry map,
single switch case), drop the `tideprojectv1alpha1`/`tideprojectv1alpha2` imports, decode only
against `tideprojectv1alpha3.Project`. The `dogfoodGlob`/`splitYAMLDocs`/`isProjectDoc`/
`hasTopLevelKey`/`findRepoRoot` helper functions are version-agnostic and copy verbatim.

**Note:** `examples/projects/dogfood/02-codex-runtime-project.yaml` (currently
`apiVersion: tideproject.k8s/v1alpha2`) needs its OWN second conversion pass to v1alpha3 as part
of this relocation — it's one of the 3 fixture files this test validates.

---

## Shared Patterns

### Fail-closed schema-revision guard (applies to: SCHEMA, GUARD work areas)
**Source:** `internal/controller/project_controller.go:1662-1704` (`checkSchemaRevisionGuard`)
**Apply to:** `ProjectReconciler.Reconcile` head-guard call site (`project_controller.go:260`,
unchanged call shape: `if blocked, gErr := r.checkSchemaRevisionGuard(ctx, &v2project); blocked`)
— generalize the callee, not the call site's shape. Sets `Ready=False`/`RequiresReinstall` +
returns `reconcile.TerminalError` (no requeue storm). This is the ONLY execution-time schema
gate in the system — no other reconciler needs an equivalent (Milestone/Phase/Plan/Task CRs are
all children created post-guard).

### Scheme registration triple (applies to: cmd/manager, cmd/dashboard, cmd/tide)
**Source:** `cmd/manager/main.go:303-311`, `cmd/dashboard/main.go:97-102`, `cmd/tide/root_flags.go:50-57`
**Apply to:** all three composition roots — same `runtime.NewScheme()` → `clientgoscheme.AddToScheme` →
`tidev1alphaN.AddToScheme` → (webhook wiring, manager-only) shape. Single source of truth per
binary; no cross-binary shared scheme-builder function exists today (each composition root
independently constructs its own `*runtime.Scheme`) — don't introduce one as part of this crank,
it's out of scope.

### Owner-ref GroupVersion check (applies to: task_controller.go, podjob/backend.go)
**Source:** both files' `walkOwnerChain*` functions (see Pattern Assignments above)
**Apply to:** both — identical one-line diff (`ref.APIVersion == "tideproject.k8s/v1alpha1" ||
ref.APIVersion == tideprojectv1alpha2.GroupVersion.String()` → single-clause
`ref.APIVersion == tideprojectv1alphaN.GroupVersion.String()`). No third site exists — grep
confirmed only these two functions perform this check.

### Envelope-version literal drift (applies to: cmd/tide-push, cmd/tide-eval, pkg/dispatch)
**Source:** `pkg/dispatch/envelope.go:24` (`APIVersionV1Alpha1` constant, the single source of truth)
**Apply to:** `cmd/tide-push/main.go:116` (independent literal, no `pkg/dispatch` import — must
hand-edit), `cmd/tide-eval/main.go:83` (imports `pkg/dispatch` already but hardcodes a duplicate
literal — fix by referencing the constant, closing the drift vector permanently). All other
consumers (subagent images) reference the constant programmatically and update automatically.

### Migration-doc chapter structure (applies to: docs/migration)
**Source:** `docs/migration/v1alpha1-to-v1alpha2.md` (full file — section order: What changed
and why → Version bump → Reinstall Procedure → Fail-Closed Safety Net → Dogfood Cluster Note)
**Apply to:** new `docs/migration/v1alpha2-to-v1alpha3.md`, PLUS a new "levels-remap table"
subsection under "What changed and why" (no prior analog for this specific table — author fresh
using the folded todo's DECIDED mapping table as source data).

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| levels-remap table content (migration doc subsection) | config (docs) | — | New content — the mapping itself is decided (folded todo), but no prior migration doc chapter had to explain a semantic (not structural) field-meaning rename; author fresh from the todo's table |
| `docs/audit/README.md`, `docs/audit/operator.md` annotation (if user takes bonus scope per Open Question 1) | config (docs) | — | These are point-in-time audit snapshots with no "living doc" analog in this repo to copy an update pattern from; RESEARCH.md recommends a one-line staleness header, not a rewrite — ask the user before scoping in |
| `SECURITY.md:40`, `docs/rbac.md:210-224` conversion-webhook staleness fix (if user takes bonus scope) | config (docs) | — | Pre-existing staleness predating this phase (Phase 23 never fixed it); no in-phase analog, straightforward prose deletion of the "no-op conversion webhook" claim once user confirms in-scope |

## Metadata

**Analog search scope:** `api/v1alpha1/`, `api/v1alpha2/`, `internal/webhook/v1alpha2/`,
`internal/controller/{project,task,milestone,phase,plan}_controller.go`,
`internal/controller/dispatch_helpers.go`, `internal/dispatch/podjob/backend.go`,
`pkg/dispatch/{envelope,doc,envelope_test}.go`, `cmd/{manager,dashboard,tide,tide-push,tide-eval}/*.go`,
`config/samples/`, `docs/migration/`, `docs/{INSTALL,gates,git-hosts,project-authoring}.md`,
`README.md`, `PROJECT`, `Makefile`, `test/integration/envtest/{spec_conformance,planner_dispatch}_test.go`
**Files scanned:** ~45 read/grepped directly; ~112 more (consumer-migration inventory) confirmed
by `grep -rl` count only, not individually read — their pattern is the single mechanical
import-repoint shown in the `dispatch_helpers.go` / owner-ref sections above, not a distinct
pattern per file.
**Pattern extraction date:** 2026-07-06
