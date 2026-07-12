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

// Phase 38 Plan 02 envtest — COST-02 / D-02 pricing-fallback surface.
//
// Locks the setPricingFallbackIfNeeded contract: an unknown-model fallback
// carried on the envelope stamps a sticky informational
// PricingFallbackActive=True condition naming the model on the Project and
// increments tide_pricing_fallback_total{project, model}. The condition
// dedupes on repeat rollups of the same model (no status churn); the metric
// counts every rollup. Empty model and nil project are safe no-ops.
package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	tidemetrics "github.com/jsquirrelz/tide/internal/metrics"
)

// pricingFallbackProject creates a minimal auto-gated Project and waits for
// cache sync (mirrors childRollupProject in child_rollup_idempotency_test.go).
func pricingFallbackProject(ctx context.Context, name string) *tideprojectv1alpha3.Project {
	proj := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: tideprojectv1alpha3.ProjectSpec{
			SchemaRevision: "v1alpha3",
			TargetRepo:     "https://github.com/example/pricing-fallback.git",
			Subagent:       tideprojectv1alpha3.SubagentConfig{Model: "claude-sonnet-4-6"},
			Gates:          tideprojectv1alpha3.Gates{Milestone: tideprojectv1alpha3.GatePolicy("auto")},
		},
	}
	Expect(k8sClient.Create(ctx, proj)).To(Succeed())
	waitForCacheSync(name, "default", &tideprojectv1alpha3.Project{})
	return proj
}

// cleanupPricingFallbackProject deletes the project (best-effort, finalizers first).
func cleanupPricingFallbackProject(ctx context.Context, name string) {
	p := &tideprojectv1alpha3.Project{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, p); err == nil {
		p.Finalizers = nil
		_ = k8sClient.Update(ctx, p)
		_ = k8sClient.Delete(ctx, p)
	}
}

var _ = Describe("PricingFallback condition + metric (Phase 38 COST-02)", Label("envtest"), func() {
	ctx := context.Background()

	It("stamps PricingFallbackActive=True naming the unmatched model", func() {
		const projName = "pricing-fallback-stamp"
		proj := pricingFallbackProject(ctx, projName)
		defer cleanupPricingFallbackProject(ctx, projName)

		Expect(setPricingFallbackIfNeeded(ctx, k8sClient, proj, "claude-mystery-9")).To(Succeed())

		Eventually(func(g Gomega) {
			got := &tideprojectv1alpha3.Project{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, got)).To(Succeed())
			cond := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha3.ConditionPricingFallbackActive)
			g.Expect(cond).NotTo(BeNil())
			g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			g.Expect(cond.Reason).To(Equal(tideprojectv1alpha3.ReasonUnknownModelPriced))
			g.Expect(cond.Message).To(ContainSubstring("claude-mystery-9"))
		}).Should(Succeed())
	})

	It("is a no-op for an empty fallback model", func() {
		const projName = "pricing-fallback-empty"
		proj := pricingFallbackProject(ctx, projName)
		defer cleanupPricingFallbackProject(ctx, projName)

		Expect(setPricingFallbackIfNeeded(ctx, k8sClient, proj, "")).To(Succeed())

		Consistently(func(g Gomega) {
			got := &tideprojectv1alpha3.Project{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, got)).To(Succeed())
			cond := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha3.ConditionPricingFallbackActive)
			g.Expect(cond).To(BeNil())
		}, "1s", "200ms").Should(Succeed())
	})

	It("is a no-op for a nil project", func() {
		Expect(setPricingFallbackIfNeeded(ctx, k8sClient, nil, "claude-mystery-9")).To(Succeed())
	})

	It("dedupes the condition but counts every rollup of the same model", func() {
		const (
			projName = "pricing-fallback-dedupe"
			model    = "claude-mystery-9"
		)
		proj := pricingFallbackProject(ctx, projName)
		defer cleanupPricingFallbackProject(ctx, projName)

		before := testutil.ToFloat64(tidemetrics.PricingFallbackTotal.WithLabelValues(projName, model))

		Expect(setPricingFallbackIfNeeded(ctx, k8sClient, proj, model)).To(Succeed())
		// Second rollup of the same unknown model: refetch so the helper sees
		// the already-stamped condition (the call-site shape — each rollup
		// operates on a freshly reconciled Project).
		refreshed := &tideprojectv1alpha3.Project{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, refreshed)).To(Succeed())
		Expect(setPricingFallbackIfNeeded(ctx, k8sClient, refreshed, model)).To(Succeed())

		// Metric counts dispatch rollups: exactly 2 increments.
		after := testutil.ToFloat64(tidemetrics.PricingFallbackTotal.WithLabelValues(projName, model))
		Expect(after - before).To(Equal(2.0))

		// Condition present exactly once (deduped, still True).
		got := &tideprojectv1alpha3.Project{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, got)).To(Succeed())
		count := 0
		for _, c := range got.Status.Conditions {
			if c.Type == tideprojectv1alpha3.ConditionPricingFallbackActive {
				count++
			}
		}
		Expect(count).To(Equal(1))
		cond := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha3.ConditionPricingFallbackActive)
		Expect(cond.Status).To(Equal(metav1.ConditionTrue))
	})
})
