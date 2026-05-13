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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

const initNamespace = "default"

var _ = Describe("Project init Job lifecycle", Label("envtest"), func() {
	ctx := context.Background()

	AfterEach(func() {
		projects := &tideprojectv1alpha1.ProjectList{}
		_ = k8sClient.List(ctx, projects, client.InNamespace(initNamespace))
		for i := range projects.Items {
			_ = k8sClient.Delete(ctx, &projects.Items[i])
		}
		jobs := &batchv1.JobList{}
		_ = k8sClient.List(ctx, jobs, client.InNamespace(initNamespace))
		for i := range jobs.Items {
			_ = k8sClient.Delete(ctx, &jobs.Items[i])
		}
	})

	// ART-01: the init Job is created once on the first reconcile.
	Describe("ART-01: init Job created on first reconcile", Label("ART-01"), func() {
		It("creates a tide-init-{UID} Job on the first reconcile", func() {
			projectName := "init-job-project-01"
			makeBoundPVC(ctx, "tide-projects", initNamespace) // idempotent

			project := &tideprojectv1alpha1.Project{
				ObjectMeta: metav1.ObjectMeta{
					Name:      projectName,
					Namespace: initNamespace,
				},
				Spec: tideprojectv1alpha1.ProjectSpec{
					TargetRepo: "https://github.com/example/init-test.git",
				},
			}
			Expect(k8sClient.Create(ctx, project)).To(Succeed())

			// Wait for the Project to get a UID.
			Eventually(func() string {
				p := &tideprojectv1alpha1.Project{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: projectName, Namespace: initNamespace}, p); err != nil {
					return ""
				}
				return string(p.UID)
			}, "5s", "100ms").ShouldNot(BeEmpty())

			// Wait for the init Job to be created.
			Eventually(func() int {
				jobs := &batchv1.JobList{}
				_ = k8sClient.List(ctx, jobs, client.InNamespace(initNamespace))
				count := 0
				for _, j := range jobs.Items {
					if containsOwnerRef(j.OwnerReferences, projectName) ||
						hasLabelWithPrefix(j.Labels, "tide-init") {
						count++
					}
				}
				// Also count by prefix matching the job name pattern.
				for _, j := range jobs.Items {
					if len(j.Name) > 10 && j.Name[:9] == "tide-init" {
						count++
					}
				}
				return count
			}, "20s", "500ms").Should(BeNumerically(">=", 1),
				"An init Job (tide-init-*) should be created on first reconcile")
		})
	})

	// ART-01: the init Job creation is idempotent — applying the Project again
	// does not create a second Job.
	Describe("ART-01: init Job idempotent on re-apply", Label("ART-01"), func() {
		It("does not create a duplicate init Job on re-reconcile (idempotent)", func() {
			projectName := "init-job-idempotent-02"
			makeBoundPVC(ctx, "tide-projects", initNamespace)

			project := &tideprojectv1alpha1.Project{
				ObjectMeta: metav1.ObjectMeta{
					Name:      projectName,
					Namespace: initNamespace,
				},
				Spec: tideprojectv1alpha1.ProjectSpec{
					TargetRepo: "https://github.com/example/idempotent.git",
				},
			}
			Expect(k8sClient.Create(ctx, project)).To(Succeed())

			// Wait for at least one Job to be created.
			Eventually(func() int {
				jobs := &batchv1.JobList{}
				_ = k8sClient.List(ctx, jobs, client.InNamespace(initNamespace))
				count := 0
				for _, j := range jobs.Items {
					if len(j.Name) >= 9 && j.Name[:9] == "tide-init" {
						count++
					}
				}
				return count
			}, "20s", "500ms").Should(BeNumerically(">=", 1))

			// Record how many init-prefixed Jobs exist.
			var initialCount int
			jobs := &batchv1.JobList{}
			Expect(k8sClient.List(ctx, jobs, client.InNamespace(initNamespace))).To(Succeed())
			for _, j := range jobs.Items {
				if len(j.Name) >= 9 && j.Name[:9] == "tide-init" {
					initialCount++
				}
			}

			// Trigger another reconcile by updating an annotation.
			Eventually(func() error {
				p := &tideprojectv1alpha1.Project{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: projectName, Namespace: initNamespace}, p); err != nil {
					return err
				}
				if p.Annotations == nil {
					p.Annotations = map[string]string{}
				}
				p.Annotations["tide-test/re-reconcile"] = "1"
				return k8sClient.Update(ctx, p)
			}, "5s", "200ms").Should(Succeed())

			// The Job count should remain the same (no duplicate).
			// We use Consistently to check over a brief window.
			jobCount := func() int {
				jl := &batchv1.JobList{}
				_ = k8sClient.List(ctx, jl, client.InNamespace(initNamespace))
				count := 0
				for _, j := range jl.Items {
					if len(j.Name) >= 9 && j.Name[:9] == "tide-init" {
						count++
					}
				}
				return count
			}
			Consistently(jobCount, "3s", "500ms").Should(Equal(initialCount),
				"Re-reconcile should not create additional init Jobs (idempotent)")
		})
	})

	// ART-01: completing the init Job sets Project.Status.Phase = "Initialized".
	Describe("ART-01: init Job completion sets Project phase to Initialized", Label("ART-01"), func() {
		It("sets Project.Status.Phase=Initialized when the init Job completes successfully", func() {
			projectName := "init-job-complete-03"
			makeBoundPVC(ctx, "tide-projects", initNamespace)

			project := &tideprojectv1alpha1.Project{
				ObjectMeta: metav1.ObjectMeta{
					Name:      projectName,
					Namespace: initNamespace,
				},
				Spec: tideprojectv1alpha1.ProjectSpec{
					TargetRepo: "https://github.com/example/complete.git",
				},
			}
			Expect(k8sClient.Create(ctx, project)).To(Succeed())

			// Wait for init Job to be created.
			var jobName string
			Eventually(func() bool {
				jobs := &batchv1.JobList{}
				_ = k8sClient.List(ctx, jobs, client.InNamespace(initNamespace))
				for _, j := range jobs.Items {
					if len(j.Name) >= 9 && j.Name[:9] == "tide-init" {
						jobName = j.Name
						return true
					}
				}
				return false
			}, "20s", "500ms").Should(BeTrue(), "init Job should be created")

			// Patch the Job status to Complete.
			Eventually(func() error {
				j := &batchv1.Job{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: jobName, Namespace: initNamespace}, j); err != nil {
					return err
				}
				j.Status.Conditions = append(j.Status.Conditions, batchv1.JobCondition{
					Type:   batchv1.JobComplete,
					Status: "True",
				})
				j.Status.Succeeded = 1
				return k8sClient.Status().Update(ctx, j)
			}, "10s", "200ms").Should(Succeed())

			// The Project.Status.Phase should become Initialized.
			Eventually(func() string {
				p := &tideprojectv1alpha1.Project{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: projectName, Namespace: initNamespace}, p); err != nil {
					return ""
				}
				return p.Status.Phase
			}, "20s", "500ms").Should(Equal("Initialized"),
				"Project.Status.Phase should be Initialized when init Job completes")
		})
	})
})

// containsOwnerRef checks if an OwnerReference list contains a reference to ownerName.
func containsOwnerRef(refs []metav1.OwnerReference, ownerName string) bool {
	for _, r := range refs {
		if r.Name == ownerName {
			return true
		}
	}
	return false
}

// hasLabelWithPrefix checks if any label key contains the prefix.
func hasLabelWithPrefix(labels map[string]string, prefix string) bool {
	for k := range labels {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}
