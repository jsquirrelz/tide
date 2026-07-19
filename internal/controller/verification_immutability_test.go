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

// verification_immutability_test.go — Plan 51-01 Task 3 envtest proving the
// CEL x-kubernetes-validations transition rule on VerificationSpec (TASK-01).
//
// CEL admission does NOT run under the fake client (RESEARCH Pitfall 2) — this
// suite runs against the real API server envtest.Environment spins up in
// suite_test.go's shared BeforeSuite, using k8sClient (the direct, uncached
// client) exactly like project_controller_test.go's CEL XValidation regression
// (project_controller_test.go:1004).
package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
)

// newVerificationImmutabilityTask builds a minimal, admission-valid Task
// carrying the given VerificationSpec, honoring every required TaskSpec field
// (PlanRef/FilesTouched/DeclaredOutputPaths/PromptPath — mirrors makeTask's
// required-field shape without sharing its labeling/cache-wait side effects,
// which these CREATE/UPDATE-rejection specs don't need).
func newVerificationImmutabilityTask(name string, v tideprojectv1alpha3.VerificationSpec) *tideprojectv1alpha3.Task {
	return &tideprojectv1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: tideprojectv1alpha3.TaskSpec{
			PlanRef:             "verify-immut-plan",
			FilesTouched:        []string{"src/main.go"},
			DeclaredOutputPaths: []string{"artifacts/out.txt"},
			PromptPath:          "envelopes/test/children/" + name + ".json",
			Verification:        v,
		},
	}
}

var _ = Describe("VerificationSpec — CEL transition-immutability (Plan 51-01 Task 3 / TASK-01)", Label("envtest", "phase51", "verification-immutability"), func() {
	ctx := context.Background()

	AfterEach(func() {
		// Best-effort cleanup; individual specs delete their own Tasks where a
		// later step in the same spec depends on a clean slate.
	})

	It("allows CREATE with phase=Draft unconstrained (transition rules skip on CREATE)", func() {
		task := newVerificationImmutabilityTask("verify-immut-create-draft", tideprojectv1alpha3.VerificationSpec{
			Phase:       "Draft",
			Version:     1,
			GateCommand: "make test",
		})
		Expect(k8sClient.Create(ctx, task)).To(Succeed())
		Expect(k8sClient.Delete(ctx, task)).To(Succeed())
	})

	It("rejects an UPDATE that field-mutates a Locked verification", func() {
		task := newVerificationImmutabilityTask("verify-immut-locked-reject", tideprojectv1alpha3.VerificationSpec{
			Phase:       "Locked",
			Version:     1,
			GateCommand: "make test",
		})
		Expect(k8sClient.Create(ctx, task)).To(Succeed())

		var current tideprojectv1alpha3.Task
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(task), &current)).To(Succeed())
		current.Spec.Verification.GateCommand = "make test-mutated"

		err := k8sClient.Update(ctx, &current)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue(),
			"expected CEL XValidation rejection of a Locked-verification field mutation, got: %v", err)

		Expect(k8sClient.Delete(ctx, task)).To(Succeed())
	})

	It("allows an UPDATE transitioning a Locked verification to Superseded", func() {
		task := newVerificationImmutabilityTask("verify-immut-locked-supersede", tideprojectv1alpha3.VerificationSpec{
			Phase:       "Locked",
			Version:     1,
			GateCommand: "make test",
		})
		Expect(k8sClient.Create(ctx, task)).To(Succeed())

		var current tideprojectv1alpha3.Task
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(task), &current)).To(Succeed())
		current.Spec.Verification.Phase = "Superseded"

		Expect(k8sClient.Update(ctx, &current)).To(Succeed())

		Expect(k8sClient.Delete(ctx, task)).To(Succeed())
	})

	It("allows an UPDATE mutating a Draft verification", func() {
		task := newVerificationImmutabilityTask("verify-immut-draft-mutate", tideprojectv1alpha3.VerificationSpec{
			Phase:       "Draft",
			Version:     1,
			GateCommand: "make test",
		})
		Expect(k8sClient.Create(ctx, task)).To(Succeed())

		var current tideprojectv1alpha3.Task
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(task), &current)).To(Succeed())
		current.Spec.Verification.GateCommand = "make test-edited"

		Expect(k8sClient.Update(ctx, &current)).To(Succeed())

		Expect(k8sClient.Delete(ctx, task)).To(Succeed())
	})
})
