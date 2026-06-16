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

package envtest_integration

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha2"
)

const admissionNamespace = "default"

var _ = Describe("Plan Admission Webhook", Label("envtest"), func() {
	ctx := context.Background()

	AfterEach(func() {
		// Best-effort cleanup — webhook tests may leave Plans in various states.
		plans := &tideprojectv1alpha1.PlanList{}
		_ = k8sClient.List(ctx, plans, client.InNamespace(admissionNamespace))
		for i := range plans.Items {
			_ = k8sClient.Delete(ctx, &plans.Items[i])
		}
		tasks := &tideprojectv1alpha1.TaskList{}
		_ = k8sClient.List(ctx, tasks, client.InNamespace(admissionNamespace))
		for i := range tasks.Items {
			_ = k8sClient.Delete(ctx, &tasks.Items[i])
		}
	})

	// PLAN-01: The webhook rejects Plans whose task DAG contains a cycle.
	// Cycle detection is via pkg/dag.ComputeWaves.
	Describe("PLAN-01: cycle rejection", Label("PLAN-01"), func() {
		It("rejects a Plan whose Tasks form a cycle (A→B, B→A)", func() {
			planName := "admission-cyclic-plan"

			plan := &tideprojectv1alpha1.Plan{
				ObjectMeta: metav1.ObjectMeta{
					Name:      planName,
					Namespace: admissionNamespace,
				},
				Spec: tideprojectv1alpha1.PlanSpec{
					PhaseRef: "phase-test",
				},
			}
			Expect(k8sClient.Create(ctx, plan)).To(Succeed())

			// Create two Tasks with a cycle: A depends on B, B depends on A.
			taskA := &tideprojectv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "admission-task-a",
					Namespace: admissionNamespace,
				},
				Spec: tideprojectv1alpha1.TaskSpec{
					PlanRef:             planName,
					PromptPath:          "envelopes/test/children/admission-task-a.json",
					DependsOn:           []string{"admission-task-b"},
					FilesTouched:        []string{"a.go"},
					DeclaredOutputPaths: []string{"a.go"},
				},
			}
			taskB := &tideprojectv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "admission-task-b",
					Namespace: admissionNamespace,
				},
				Spec: tideprojectv1alpha1.TaskSpec{
					PlanRef:             planName,
					PromptPath:          "envelopes/test/children/admission-task-b.json",
					DependsOn:           []string{"admission-task-a"},
					FilesTouched:        []string{"b.go"},
					DeclaredOutputPaths: []string{"b.go"},
				},
			}
			Expect(k8sClient.Create(ctx, taskA)).To(Succeed())
			Expect(k8sClient.Create(ctx, taskB)).To(Succeed())

			// Wait for the Tasks to be indexed by the webhook's field indexer.
			Eventually(func() int {
				taskList := &tideprojectv1alpha1.TaskList{}
				_ = mgrClient.List(ctx, taskList,
					client.InNamespace(admissionNamespace),
					client.MatchingFields{".spec.planRef": planName},
				)
				return len(taskList.Items)
			}, "10s", "200ms").Should(BeNumerically(">=", 2))

			// WR-05: With both cyclic Tasks deterministically indexed, the
			// webhook MUST reject the Plan update. The old test allowed
			// "either reject OR succeed" via Pitfall B fall-through, which
			// hid real regressions (the webhook could silently admit a
			// cyclic Plan and the test still passed). Wait the update out
			// with Eventually so a slow webhook is permitted, but a final
			// nil-error counts as a regression.
			Eventually(func() error {
				freshPlan := &tideprojectv1alpha1.Plan{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: planName, Namespace: admissionNamespace}, freshPlan); err != nil {
					return err
				}
				if freshPlan.Annotations == nil {
					freshPlan.Annotations = map[string]string{}
				}
				freshPlan.Annotations["tide-test/trigger"] = "cycle-test-deterministic"
				return k8sClient.Update(ctx, freshPlan)
			}, "15s", "500ms").Should(Satisfy(func(err error) bool {
				return err != nil &&
					(apierrors.IsBadRequest(err) || apierrors.IsInvalid(err) || isForbiddenOrBadRequest(err))
			}),
				"cyclic Plan update must be rejected once Tasks are indexed (WR-05)")
		})
	})

	// PLAN-01: Acyclic plan is admitted without error.
	Describe("PLAN-01: acyclic plan admitted", Label("PLAN-01"), func() {
		It("admits a Plan whose Tasks form an acyclic DAG (A→B)", func() {
			planName := "admission-acyclic-plan"

			plan := &tideprojectv1alpha1.Plan{
				ObjectMeta: metav1.ObjectMeta{
					Name:      planName,
					Namespace: admissionNamespace,
				},
				Spec: tideprojectv1alpha1.PlanSpec{
					PhaseRef: "phase-test",
				},
			}
			Expect(k8sClient.Create(ctx, plan)).To(Succeed())

			// Create two Tasks with a valid dependency: A then B.
			taskA := &tideprojectv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "acyclic-task-a",
					Namespace: admissionNamespace,
				},
				Spec: tideprojectv1alpha1.TaskSpec{
					PlanRef:             planName,
					PromptPath:          "envelopes/test/children/acyclic-task-a.json",
					FilesTouched:        []string{"a.go"},
					DeclaredOutputPaths: []string{"a.go"},
				},
			}
			taskB := &tideprojectv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "acyclic-task-b",
					Namespace: admissionNamespace,
				},
				Spec: tideprojectv1alpha1.TaskSpec{
					PlanRef:             planName,
					PromptPath:          "envelopes/test/children/acyclic-task-b.json",
					DependsOn:           []string{"acyclic-task-a"},
					FilesTouched:        []string{"b.go"},
					DeclaredOutputPaths: []string{"b.go"},
				},
			}
			Expect(k8sClient.Create(ctx, taskA)).To(Succeed())
			Expect(k8sClient.Create(ctx, taskB)).To(Succeed())

			// Verify the Plan was created successfully (no rejection).
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: planName, Namespace: admissionNamespace}, &tideprojectv1alpha1.Plan{})).To(Succeed())
		})
	})

	// PLAN-02: file-touch strict mode rejects when two tasks share a path without dependsOn.
	Describe("PLAN-02: file-touch strict mode rejection", Label("PLAN-02"), func() {
		It("rejects (strict mode annotation) a Plan where two tasks share a file path without dependsOn", func() {
			planName := "admission-strict-plan"

			plan := &tideprojectv1alpha1.Plan{
				ObjectMeta: metav1.ObjectMeta{
					Name:      planName,
					Namespace: admissionNamespace,
					// strict mode via annotation
					Annotations: map[string]string{
						"tideproject.k8s/file-touch-mode": "strict",
					},
				},
				Spec: tideprojectv1alpha1.PlanSpec{
					PhaseRef: "phase-strict",
				},
			}
			Expect(k8sClient.Create(ctx, plan)).To(Succeed())

			// Both tasks share "shared.go" but have no dependsOn edge.
			taskA := &tideprojectv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "strict-task-a",
					Namespace: admissionNamespace,
				},
				Spec: tideprojectv1alpha1.TaskSpec{
					PlanRef:             planName,
					PromptPath:          "envelopes/test/children/strict-task-a.json",
					FilesTouched:        []string{"shared.go"},
					DeclaredOutputPaths: []string{"shared.go"},
				},
			}
			taskB := &tideprojectv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "strict-task-b",
					Namespace: admissionNamespace,
				},
				Spec: tideprojectv1alpha1.TaskSpec{
					PlanRef:             planName,
					PromptPath:          "envelopes/test/children/strict-task-b.json",
					FilesTouched:        []string{"shared.go"},
					DeclaredOutputPaths: []string{"shared.go"},
				},
			}
			Expect(k8sClient.Create(ctx, taskA)).To(Succeed())
			Expect(k8sClient.Create(ctx, taskB)).To(Succeed())

			// Wait for Tasks to be indexed.
			Eventually(func() int {
				taskList := &tideprojectv1alpha1.TaskList{}
				_ = mgrClient.List(ctx, taskList,
					client.InNamespace(admissionNamespace),
					client.MatchingFields{".spec.planRef": planName},
				)
				return len(taskList.Items)
			}, "10s", "200ms").Should(BeNumerically(">=", 2))

			// WR-05: With both overlapping Tasks indexed, strict-mode MUST
			// reject the Plan update. The old test discarded the update
			// result ("_ = Update(...)") so the Pitfall B fall-through and a
			// real strict-mode regression were indistinguishable.
			Eventually(func() error {
				freshPlan := &tideprojectv1alpha1.Plan{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: planName, Namespace: admissionNamespace}, freshPlan); err != nil {
					return err
				}
				if freshPlan.Labels == nil {
					freshPlan.Labels = map[string]string{}
				}
				freshPlan.Labels["tide-test/trigger"] = "strict-mode-deterministic"
				return k8sClient.Update(ctx, freshPlan)
			}, "15s", "500ms").Should(Satisfy(func(err error) bool {
				return err != nil &&
					(apierrors.IsBadRequest(err) || apierrors.IsInvalid(err) || isForbiddenOrBadRequest(err))
			}),
				"strict-mode file-touch mismatch must be rejected once Tasks are indexed (WR-05)")
		})
	})

	// PLAN-02: warn mode emits warnings but admits the Plan.
	Describe("PLAN-02: file-touch warn mode — warns but admits", Label("PLAN-02"), func() {
		It("admits (warn mode — cluster default) a Plan with file-touch mismatches", func() {
			planName := "admission-warn-plan"

			// warn mode is the cluster default (set in BeforeSuite).
			plan := &tideprojectv1alpha1.Plan{
				ObjectMeta: metav1.ObjectMeta{
					Name:      planName,
					Namespace: admissionNamespace,
				},
				Spec: tideprojectv1alpha1.PlanSpec{
					PhaseRef: "phase-warn",
				},
			}
			Expect(k8sClient.Create(ctx, plan)).To(Succeed())

			// Tasks share a file but have no edge — warn mode should still admit.
			taskA := &tideprojectv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "warn-task-a",
					Namespace: admissionNamespace,
				},
				Spec: tideprojectv1alpha1.TaskSpec{
					PlanRef:             planName,
					PromptPath:          "envelopes/test/children/warn-task-a.json",
					FilesTouched:        []string{"warn-shared.go"},
					DeclaredOutputPaths: []string{"warn-shared.go"},
				},
			}
			taskB := &tideprojectv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "warn-task-b",
					Namespace: admissionNamespace,
				},
				Spec: tideprojectv1alpha1.TaskSpec{
					PlanRef:             planName,
					PromptPath:          "envelopes/test/children/warn-task-b.json",
					FilesTouched:        []string{"warn-shared.go"},
					DeclaredOutputPaths: []string{"warn-shared.go"},
				},
			}
			Expect(k8sClient.Create(ctx, taskA)).To(Succeed())
			Expect(k8sClient.Create(ctx, taskB)).To(Succeed())

			// Plan creation should have succeeded.
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: planName, Namespace: admissionNamespace}, &tideprojectv1alpha1.Plan{})).To(Succeed())
		})
	})

	// PLAN-03: the webhook has no cycle-recovery code path.
	// This test verifies the PLAN-03 invariant by checking the webhook source for
	// absent cycle-recovery patterns. This is a structural correctness assertion.
	//
	// Scanning strategy: load each .go file with go/parser (AST-aware), then walk
	// identifiers (function names, type names, var/const names, struct field names).
	// We deliberately ignore comments because plan_webhook.go's doc comment
	// includes the verification grep pattern as a literal reference example
	// (e.g. "grep -nE 'recoverCycle|cycleRecover|...'"), which would otherwise
	// trip a naive raw substring match.
	Describe("PLAN-03: cycle recovery feature absent", Label("PLAN-03"), func() {
		It("verifies there is no cycle recovery code in the webhook implementation", func() {
			webhookDir := filepath.Join("..", "..", "..", "internal", "webhook", "v1alpha2")
			forbidden := []string{"recoverCycle", "cycleRecover", "fixCycle", "skipCycle"}
			var offenders []string

			err := filepath.WalkDir(webhookDir, func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
					return err
				}
				fset := token.NewFileSet()
				// Parse without comments so doc-block grep examples don't false-positive.
				file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
				if parseErr != nil {
					return parseErr
				}
				ast.Inspect(file, func(n ast.Node) bool {
					id, ok := n.(*ast.Ident)
					if !ok {
						return true
					}
					for _, bad := range forbidden {
						if id.Name == bad {
							offenders = append(offenders, fmt.Sprintf("%s: identifier %q", path, id.Name))
						}
					}
					return true
				})
				return nil
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(offenders).To(BeEmpty(),
				"PLAN-03 violation: cycle recovery identifiers found in webhook — cycles must be rejected, not recovered")
		})
	})
})

// isForbiddenOrBadRequest is a helper to check for common webhook-rejection status codes.
func isForbiddenOrBadRequest(err error) bool {
	if err == nil {
		return false
	}
	return apierrors.IsForbidden(err) || apierrors.IsBadRequest(err) ||
		strings.Contains(err.Error(), "cyclic") ||
		strings.Contains(err.Error(), "cycle")
}

// Project CEL targetRepo admission tests.
//
// GREEN after 08-03 lands — CEL marker change + controller-gen + make helm regenerates CRD schema.
// Test A will be RED (file:// not yet rejected at admission) until the new XValidation marker
// is codegen'd into the CRD. Tests B, C, D should be GREEN immediately (the current validator
// already admits http(s) and git@ — this block just makes it explicit and adds regression cover).
var _ = Describe("Project CEL targetRepo admission", Label("envtest"), func() {
	ctx := context.Background()

	var createdProjects []*tideprojectv1alpha1.Project

	AfterEach(func() {
		// Best-effort cleanup for successfully-created Projects.
		for _, proj := range createdProjects {
			_ = k8sClient.Delete(ctx, proj)
		}
		createdProjects = nil
	})

	// Helper: build a minimal Project with the given targetRepo.
	// ProviderSecretRef is set to "any-secret" — CEL validation does not check
	// the secret exists. Budget.AbsoluteCapCents=0 is valid (0 = disabled per
	// sample notes). No spec.git block (D-04: git ops skipped when spec.git is nil).
	newProject := func(name, targetRepo string) *tideprojectv1alpha1.Project {
		return &tideprojectv1alpha1.Project{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: admissionNamespace,
			},
			Spec: tideprojectv1alpha1.ProjectSpec{SchemaRevision: "v1alpha2",
				TargetRepo:        targetRepo,
				ProviderSecretRef: "any-secret",
				Budget: tideprojectv1alpha1.BudgetConfig{
					AbsoluteCapCents: 0,
				},
			},
		}
	}

	// Test A: file:// targetRepo must be rejected.
	// RED until 08-03 lands (CEL marker change + controller-gen + make helm regenerates CRD schema).
	It("rejects a Project with targetRepo file:///tmp/test", func() {
		proj := newProject(fmt.Sprintf("cel-reject-%d", GinkgoRandomSeed()), "file:///tmp/test")
		err := k8sClient.Create(ctx, proj)
		// RED until 08-03: the current CEL rule admits file:// so this assertion
		// fails at runtime until the new XValidation marker is codegen'd.
		// After 08-03: err must be non-nil AND IsInvalid AND contain the new message.
		Expect(err).To(HaveOccurred(),
			"file:// targetRepo must be rejected at admission (RED until 08-03 CEL marker change)")
		Expect(apierrors.IsInvalid(err)).To(BeTrue(),
			"rejection must be an Invalid status error (CEL XValidation violation)")
		Expect(err.Error()).To(ContainSubstring("file:// is not a supported production transport"),
			"error message must reference the production transport constraint")
		// No cleanup needed — object was not created.
	})

	// Test B: https:// sentinel must be accepted.
	It("admits a Project with targetRepo https://git.example.internal/stub/no-such-repo.git", func() {
		proj := newProject(fmt.Sprintf("cel-accept-https-%d", GinkgoRandomSeed()), "https://git.example.internal/stub/no-such-repo.git")
		Expect(k8sClient.Create(ctx, proj)).To(Succeed(),
			"https:// targetRepo must be admitted at admission")
		createdProjects = append(createdProjects, proj)
	})

	// Test C: http:// (in-cluster git-http server URL shape) must be accepted.
	It("admits a Project with targetRepo http://git-http-server/demo-remote.git", func() {
		proj := newProject(fmt.Sprintf("cel-accept-http-%d", GinkgoRandomSeed()), "http://git-http-server/demo-remote.git")
		Expect(k8sClient.Create(ctx, proj)).To(Succeed(),
			"http:// targetRepo must be admitted at admission (in-cluster git server URL shape)")
		createdProjects = append(createdProjects, proj)
	})

	// Test D: git@ SSH URL must be accepted.
	It("admits a Project with targetRepo git@github.com:owner/repo.git", func() {
		proj := newProject(fmt.Sprintf("cel-accept-ssh-%d", GinkgoRandomSeed()), "git@github.com:owner/repo.git")
		Expect(k8sClient.Create(ctx, proj)).To(Succeed(),
			"git@ targetRepo must be admitted at admission (SSH URL shape)")
		createdProjects = append(createdProjects, proj)
	})
})
