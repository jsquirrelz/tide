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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
)

// projectOpt customizes a Project fixture produced by newStubProject.
type projectOpt func(*tideprojectv1alpha2.Project)

// newStubProject returns a v1alpha2 Project wired for the stub subagent: $0
// budget, model=stub, all gates on auto, and — crucially — the required
// schemaRevision already set. Per-spec variation (targetRepo, git, provider
// secret) is applied through opts. Callers persist it via k8sClient.Create.
func newStubProject(ns, name string, opts ...projectOpt) *tideprojectv1alpha2.Project {
	p := &tideprojectv1alpha2.Project{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: tideprojectv1alpha2.ProjectSpec{
			SchemaRevision: "v1alpha2",
			Budget:         tideprojectv1alpha2.BudgetConfig{AbsoluteCapCents: 0},
			Subagent:       tideprojectv1alpha2.SubagentConfig{Model: "stub"},
			Gates: tideprojectv1alpha2.Gates{
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
	return func(p *tideprojectv1alpha2.Project) { p.Spec.TargetRepo = url }
}

// withProviderSecret sets spec.providerSecretRef (the LLM provider credentials Secret).
func withProviderSecret(name string) projectOpt {
	return func(p *tideprojectv1alpha2.Project) { p.Spec.ProviderSecretRef = name }
}

// withGit sets spec.git (repoURL + credsSecretRef) to exercise the git transport path.
func withGit(repoURL, credsSecretRef string) projectOpt {
	return func(p *tideprojectv1alpha2.Project) {
		p.Spec.Git = &tideprojectv1alpha2.GitConfig{RepoURL: repoURL, CredsSecretRef: credsSecretRef}
	}
}
