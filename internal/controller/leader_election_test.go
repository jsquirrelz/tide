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

package controller

import (
	"context"
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	coordinationv1 "k8s.io/api/coordination/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// Leader Election test verifies CTRL-03 by starting two Managers against the
// same envtest cluster, killing the first, and asserting that the second
// acquires the lease with a *different* holder identity than the first.
//
// Why "different holder identity" rather than a label match:
// controller-runtime generates lease HolderIdentity as `${hostname}_${uuid}`
// — neither user-controllable via Logger options nor via NewManager fields.
// Both test Managers run on the same host (same hostname) but pick distinct
// UUIDs at construction time, so `id1 != id2` is the strongest assertion the
// envtest harness can make without monkey-patching controller-runtime
// internals. That contract — "lease holder transferred to a fresh identity
// after the previous leader stopped" — is exactly what CTRL-03 requires.
//
// Slow (~60 seconds, dominated by lease-acquisition timing) so it's gated by
// testing.Short() — `make test` passes -short and skips this; the dedicated
// `make test-leader-election` target runs without -short and includes it.
// TEST-01 (30s budget on `make test`) is preserved.
var _ = Describe("Leader Election (CTRL-03)", func() {
	It("transfers leadership when the current leader's manager stops", func() {
		if testing.Short() {
			Skip("skipping leader election in short mode (run via `make test-leader-election`)")
		}

		const (
			leaseName      = "tide-controller-leader.tideproject.k8s"
			leaseNamespace = "default"
		)

		// First manager — should acquire the lease.
		ctx1, cancel1 := context.WithCancel(context.Background())
		defer cancel1()

		mgr1, err := buildLeaderTestManager()
		Expect(err).NotTo(HaveOccurred())
		go func() {
			defer GinkgoRecover()
			_ = mgr1.Start(ctx1)
		}()

		var firstHolder string
		Eventually(func(g Gomega) {
			var lease coordinationv1.Lease
			g.Expect(k8sClient.Get(context.Background(),
				types.NamespacedName{Name: leaseName, Namespace: leaseNamespace},
				&lease)).To(Succeed())
			g.Expect(lease.Spec.HolderIdentity).ToNot(BeNil())
			g.Expect(*lease.Spec.HolderIdentity).NotTo(BeEmpty())
			firstHolder = *lease.Spec.HolderIdentity
		}).WithTimeout(20*time.Second).WithPolling(500*time.Millisecond).Should(Succeed(),
			"first manager should acquire leadership within the lease window")

		// Kill the first manager — its lease should expire and the second
		// manager should take over.
		cancel1()

		ctx2, cancel2 := context.WithCancel(context.Background())
		defer cancel2()

		mgr2, err := buildLeaderTestManager()
		Expect(err).NotTo(HaveOccurred())
		go func() {
			defer GinkgoRecover()
			_ = mgr2.Start(ctx2)
		}()

		// The second manager picks a *different* holder identity (different
		// UUID suffix). Lease durations default to 15s; allow ~5x that for
		// the lease renewal grace period + acquisition retry on a busy CI.
		Eventually(func(g Gomega) {
			var lease coordinationv1.Lease
			g.Expect(k8sClient.Get(context.Background(),
				types.NamespacedName{Name: leaseName, Namespace: leaseNamespace},
				&lease)).To(Succeed())
			g.Expect(lease.Spec.HolderIdentity).ToNot(BeNil())
			g.Expect(*lease.Spec.HolderIdentity).NotTo(BeEmpty())
			g.Expect(*lease.Spec.HolderIdentity).NotTo(Equal(firstHolder),
				"lease holder identity should change after failover")
		}).WithTimeout(90*time.Second).WithPolling(time.Second).Should(Succeed(),
			"second manager should acquire leadership with a fresh holder identity after first stops")
	})
})

// buildLeaderTestManager constructs a Manager identical in shape to
// cmd/manager/main.go's production Manager (CTRL-03 LeaderElectionID), but
// with metrics off (port "0") and the leader-election lease pinned to the
// "default" namespace so the test can Get() the Lease by a known key.
//
// The Manager runs no controllers — leader election is a Manager-level
// concern, independent of the reconcilers registered.
func buildLeaderTestManager() (ctrl.Manager, error) {
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                  scheme.Scheme,
		LeaderElection:          true,
		LeaderElectionID:        "tide-controller-leader.tideproject.k8s",
		LeaderElectionNamespace: "default",
		Metrics:                 metricsserver.Options{BindAddress: "0"},
	})
	if err != nil {
		return nil, fmt.Errorf("build leader test manager: %w", err)
	}
	return mgr, nil
}
