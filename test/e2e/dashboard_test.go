//go:build kind_e2e

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

// dashboard_test.go — Phase 4 plan 04-14 Task 2 (DASH-01 / DASH-04 E2E gate).
//
// Smoke surface:
//  1. Dashboard Deployment reaches Ready (kubectl rollout status).
//  2. `kubectl port-forward` to the Service; GET /healthz returns 200.
//  3. Apply a demo Project YAML; GET /api/v1/projects returns it in JSON.
//
// What this test does NOT cover (deferred to Phase 5 acceptance suite):
//  - React Flow side-by-side DAG render (visual; manual verification in Task 3)
//  - SSE event-stream reconnect semantics (covered by hub unit tests in 04-10)
//  - Log streaming via /api/v1/tasks/{name}/log (covered by 04-11 unit tests)

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Phase 4 Dashboard E2E", Ordered, func() {
	const testNamespace = "tide-e2e-dashboard"

	var (
		portForwardCancel context.CancelFunc
		portForwardCmd    *exec.Cmd
		dashboardLocalURL string
	)

	BeforeAll(func() {
		By("creating test namespace " + testNamespace)
		Expect(kindApplyYAML(fmt.Sprintf("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: %s\n", testNamespace))).To(Succeed())

		By("starting kubectl port-forward to svc/" + kindE2EDashboardService)
		localPort := kindFindFreePort()
		dashboardLocalURL = fmt.Sprintf("http://localhost:%d", localPort)

		// kubectl port-forward shells out and stays alive in a goroutine for
		// the duration of this Describe. Cancelled in AfterAll. Using
		// CommandContext (not Command) so AfterAll's cancel() actually kills
		// the subprocess.
		pfCtx, cancel := context.WithCancel(context.Background())
		portForwardCancel = cancel
		portForwardCmd = exec.CommandContext(pfCtx, "kubectl",
			"--kubeconfig", kindE2EKubeconfigPath,
			"-n", kindE2EControllerNamespace,
			"port-forward", "svc/"+kindE2EDashboardService,
			fmt.Sprintf("%d:80", localPort))
		Expect(portForwardCmd.Start()).To(Succeed(), "kubectl port-forward failed to start")

		// port-forward takes a moment to bind; poll until /healthz responds
		// (bounded so a real failure surfaces, not a timeout).
		By("waiting for port-forward to be reachable")
		Eventually(func() error {
			resp, err := http.Get(dashboardLocalURL + "/healthz")
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("status %d", resp.StatusCode)
			}
			return nil
		}, 30*time.Second, 500*time.Millisecond).Should(Succeed(),
			"dashboard /healthz did not respond 200 within 30s — port-forward or readiness failure")
	})

	AfterAll(func() {
		if portForwardCancel != nil {
			portForwardCancel()
		}
		if portForwardCmd != nil && portForwardCmd.Process != nil {
			// Best-effort: kill the kubectl subprocess. cancel() above should
			// already trigger SIGKILL on the context, but be defensive in
			// case context propagation lags.
			_ = portForwardCmd.Process.Kill()
			_ = portForwardCmd.Wait()
		}
		kindDeleteNamespace(testNamespace)
	})

	It("returns 200 on /healthz after the dashboard becomes Ready", func() {
		// Dashboard rollout-status was waited on in BeforeSuite; here we
		// re-verify the API surface is alive via a fresh request (the
		// port-forward channel can flap independently of Pod readiness).
		resp, err := http.Get(dashboardLocalURL + "/healthz")
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
	})

	It("returns 200 on /readyz when the informer cache is synced", func() {
		// Note: this hits the api-port (:8080) readyz; the cache-synced
		// readyz on :8081 is what kubelet probes (and what kindE2EWaitForDeployment
		// implicitly validates). Both should be 200 by this point.
		resp, err := http.Get(dashboardLocalURL + "/readyz")
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
	})

	It("lists an applied Project via GET /api/v1/projects", func() {
		By("applying a demo Project")
		demoProject := fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: tide-provider-secret
  namespace: %[1]s
type: Opaque
data:
  ANTHROPIC_API_KEY: dGVzdA==
---
apiVersion: tideproject.k8s/v1alpha1
kind: Project
metadata:
  name: dashboard-smoke
  namespace: %[1]s
spec:
  targetRepo: "https://github.com/example/dashboard-smoke.git"
  providerSecretRef: tide-provider-secret
  budget:
    absoluteCapCents: 1000
`, testNamespace)
		Expect(kindApplyYAML(demoProject)).To(Succeed())

		By("polling /api/v1/projects until the new project appears")
		// The dashboard's informer cache picks up the new Project async via
		// watch; allow up to 15s for the cache to converge.
		Eventually(func() ([]string, error) {
			resp, err := http.Get(dashboardLocalURL + "/api/v1/projects?namespace=" + testNamespace)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("status %d", resp.StatusCode)
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return nil, err
			}
			// Decode loosely: the API returns an array (or an object wrapping
			// an array). Either way we look for the project name as a substring
			// AND validate it parses as JSON.
			var generic interface{}
			if err := json.Unmarshal(body, &generic); err != nil {
				return nil, fmt.Errorf("non-JSON response: %w (body=%q)", err, string(body))
			}
			names := kindExtractProjectNames(body)
			return names, nil
		}, 15*time.Second, 1*time.Second).Should(ContainElement("dashboard-smoke"),
			"GET /api/v1/projects did not return dashboard-smoke within 15s")
	})
})

// kindFindFreePort returns an available local port. Used to pick a
// port-forward target that won't collide with whatever the operator might
// already have running.
func kindFindFreePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).NotTo(HaveOccurred())
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// kindExtractProjectNames pulls every `name` string from the JSON body —
// works for both `[{name:...}]` and `{items:[{name:...}]}` shapes without
// committing to a specific schema (the dashboard's projectSummary shape
// is owned by cmd/dashboard/api/projects.go and may evolve).
func kindExtractProjectNames(body []byte) []string {
	var names []string
	var generic interface{}
	if err := json.Unmarshal(body, &generic); err != nil {
		return names
	}
	var walk func(v interface{})
	walk = func(v interface{}) {
		switch t := v.(type) {
		case map[string]interface{}:
			if n, ok := t["name"].(string); ok {
				names = append(names, n)
			}
			for _, child := range t {
				walk(child)
			}
		case []interface{}:
			for _, child := range t {
				walk(child)
			}
		}
	}
	walk(generic)
	// dedupe so a single Project with nested `name`s doesn't false-positive.
	seen := map[string]struct{}{}
	out := names[:0]
	for _, n := range names {
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}

// Compile-time guard: strings + http imports used in test body.
var _ = strings.HasPrefix
