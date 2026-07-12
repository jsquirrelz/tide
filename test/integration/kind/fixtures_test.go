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

// Typed test-fixture builders for the Layer B kind suite.
//
// Project fixtures are constructed here via a functional-options builder rather
// than hand-copied inline YAML. This is the consensus pattern for Go test
// fixtures (a constructor with sane defaults + per-test overrides) and for
// controller-runtime CR fixtures — the Kubebuilder book builds the
// CR-under-test as a typed struct applied through the client, not a YAML
// manifest string. Centralizing the defaults here means a new Project fixture
// cannot silently omit a required field: schemaRevision (the field whose
// absence made the medium_http Project fail admission) is set by the builder,
// not by every caller.
package kind_integration

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
)

// createFixture persists a fixture object via the shared k8sClient, treating
// AlreadyExists as success so hierarchy helpers stay idempotent on a reused
// cluster — matching the prior `kubectl apply` behavior the builders replace.
func createFixture(ctx context.Context, obj client.Object) error {
	if err := k8sClient.Create(ctx, obj); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// projectOpt customizes a Project fixture produced by newStubProject.
type projectOpt func(*tideprojectv1alpha3.Project)

// newStubProject returns a v1alpha3 Project wired for the stub subagent: $0
// budget, model=stub, all gates on auto, and — crucially — the required
// schemaRevision already set. Per-spec variation (targetRepo, git, provider
// secret) is applied through opts. Callers persist it via k8sClient.Create.
func newStubProject(ns, name string, opts ...projectOpt) *tideprojectv1alpha3.Project {
	p := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: tideprojectv1alpha3.ProjectSpec{
			SchemaRevision: "v1alpha3",
			Budget:         tideprojectv1alpha3.BudgetConfig{AbsoluteCapCents: 0},
			Subagent:       tideprojectv1alpha3.SubagentConfig{Model: "stub"},
			Gates: tideprojectv1alpha3.Gates{
				Milestone:         "auto",
				Phase:             "auto",
				Plan:              "auto",
				Task:              "auto",
				PauseBetweenWaves: false,
			},
		},
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// withTargetRepo sets spec.targetRepo — the repo the clone/push Jobs operate on.
func withTargetRepo(url string) projectOpt {
	return func(p *tideprojectv1alpha3.Project) { p.Spec.TargetRepo = url }
}

// withProviderSecret sets spec.providerSecretRef (the LLM provider credentials Secret).
func withProviderSecret(name string) projectOpt {
	return func(p *tideprojectv1alpha3.Project) { p.Spec.ProviderSecretRef = name }
}

// withGit sets spec.git (repoURL + credsSecretRef) to exercise the git transport path.
func withGit(repoURL, credsSecretRef string) projectOpt {
	return func(p *tideprojectv1alpha3.Project) {
		p.Spec.Git = &tideprojectv1alpha3.GitConfig{RepoURL: repoURL, CredsSecretRef: credsSecretRef}
	}
}

// withBaseRef sets spec.git.baseRef — the ref the run branch is created from
// (Phase 35 BASE-01). Nil-safe: it lazily creates the GitConfig so it composes
// with or without withGit, and regardless of option order.
func withBaseRef(ref string) projectOpt {
	return func(p *tideprojectv1alpha3.Project) {
		if p.Spec.Git == nil {
			p.Spec.Git = &tideprojectv1alpha3.GitConfig{}
		}
		p.Spec.Git.BaseRef = ref
	}
}

// withBudget overrides the default $0 absolute cap (hierarchy fixtures use a
// non-zero cap so planner/executor dispatch is not budget-halted).
func withBudget(cents int64) projectOpt {
	return func(p *tideprojectv1alpha3.Project) { p.Spec.Budget.AbsoluteCapCents = cents }
}

// Owner/index label keys mirror internal/owner — kept as literals here so the
// fixtures match the labels the reconcilers select on without importing the
// production package into test fixtures.
const (
	labelProject   = "tideproject.k8s/project"
	labelWaveIndex = "tideproject.k8s/wave-index"
)

// newStubMilestone returns a Milestone owned (by name ref) by projectRef.
func newStubMilestone(ns, name, projectRef string) *tideprojectv1alpha3.Milestone {
	return &tideprojectv1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       tideprojectv1alpha3.MilestoneSpec{ProjectRef: projectRef},
	}
}

// newStubPhase returns a Phase referencing milestoneRef.
func newStubPhase(ns, name, milestoneRef string) *tideprojectv1alpha3.Phase {
	return &tideprojectv1alpha3.Phase{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       tideprojectv1alpha3.PhaseSpec{MilestoneRef: milestoneRef},
	}
}

// planOpt customizes a Plan fixture produced by newStubPlan.
type planOpt func(*tideprojectv1alpha3.Plan)

// newStubPlan returns a Plan referencing phaseRef.
func newStubPlan(ns, name, phaseRef string, opts ...planOpt) *tideprojectv1alpha3.Plan {
	p := &tideprojectv1alpha3.Plan{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: map[string]string{}},
		Spec:       tideprojectv1alpha3.PlanSpec{PhaseRef: phaseRef},
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// withPlanProjectLabel stamps the tideproject.k8s/project selector label.
func withPlanProjectLabel(project string) planOpt {
	return func(p *tideprojectv1alpha3.Plan) { p.Labels[labelProject] = project }
}

// taskOpt customizes a Task fixture produced by newStubTask.
type taskOpt func(*tideprojectv1alpha3.Task)

// newStubTask returns a wave-0, success-mode Task referencing planRef, with
// filesTouched == declaredOutputPaths == [name.go] and promptPath defaulted.
// The project selector label and wave index are required by the reconcilers, so
// callers in a hierarchy pass withTaskProjectLabel; per-task variation
// (testMode, caps, dependsOn, files, wave index) goes through opts.
func newStubTask(ns, name, planRef string, opts ...taskOpt) *tideprojectv1alpha3.Task {
	t := &tideprojectv1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    map[string]string{labelWaveIndex: "0"},
		},
		Spec: tideprojectv1alpha3.TaskSpec{
			PlanRef:             planRef,
			PromptPath:          "children/task-01.json",
			FilesTouched:        []string{name + ".go"},
			DeclaredOutputPaths: []string{name + ".go"},
			Dev:                 tideprojectv1alpha3.TaskDev{TestMode: "success"},
		},
	}
	for _, o := range opts {
		o(t)
	}
	return t
}

// withTaskProjectLabel stamps the tideproject.k8s/project selector label.
func withTaskProjectLabel(project string) taskOpt {
	return func(t *tideprojectv1alpha3.Task) { t.Labels[labelProject] = project }
}

// withWaveIndex overrides the wave-index label (default "0").
func withWaveIndex(idx string) taskOpt {
	return func(t *tideprojectv1alpha3.Task) { t.Labels[labelWaveIndex] = idx }
}

// withTestMode sets the dev test-mode harness behavior (success, hang,
// fail-exit-1, exceed-output-paths, wait-for-signal).
func withTestMode(mode string) taskOpt {
	return func(t *tideprojectv1alpha3.Task) { t.Spec.Dev.TestMode = mode }
}

// withWallClockCap sets caps.wallClockSeconds (HARN-02 wall-clock cap).
func withWallClockCap(sec int32) taskOpt {
	return func(t *tideprojectv1alpha3.Task) {
		t.Spec.Caps = &tideprojectv1alpha3.Caps{WallClockSeconds: sec}
	}
}

// withTaskDependsOn sets spec.dependsOn (task-level execution edges).
func withTaskDependsOn(deps ...string) taskOpt {
	return func(t *tideprojectv1alpha3.Task) { t.Spec.DependsOn = deps }
}

// withPromptPath overrides the default children/task-01.json prompt path.
func withPromptPath(path string) taskOpt {
	return func(t *tideprojectv1alpha3.Task) { t.Spec.PromptPath = path }
}

// withFiles overrides both filesTouched and declaredOutputPaths (kept equal —
// the common fixture shape; use the field setters directly if they must differ).
func withFiles(files ...string) taskOpt {
	return func(t *tideprojectv1alpha3.Task) {
		t.Spec.FilesTouched = files
		t.Spec.DeclaredOutputPaths = files
	}
}
