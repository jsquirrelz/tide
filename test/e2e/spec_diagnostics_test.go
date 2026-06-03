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

// spec_diagnostics_test.go — spec-failure-triggered diagnostics dump for the
// Phase 4 kind_e2e suite.
//
// WHY this exists (observability, NOT behavior): the existing
// dumpE2EControllerDiagnostics() fires only on the BeforeSuite helm-install
// failure path. A spec-level failure (e.g. gate_flow_test.go's
// "reaches Status.Phase=AwaitingApproval" timing out with the Milestone parked
// at Running) currently captures NO runtime evidence — the AfterSuite deletes
// the tide-e2e-phase4 cluster and the nightly workflow's `kind export logs`
// step targets only tide-test, so by the time anyone looks the cluster is gone.
//
// dumpE2ESpecFailureDiagnostics() dumps, to stdout (which survives AfterSuite
// teardown, mirroring dumpControllerDiagnostics / dumpE2EControllerDiagnostics):
//   - jobs + pods in the test namespace (THE key signal: an authoring/task Job
//     stuck in ImagePullBackOff vs Running vs Completed)
//   - describe pods (pull errors / scheduling events) in the test namespace
//   - events in the test namespace, time-sorted
//   - the full CR ladder (projects → milestones → phases → plans → tasks → waves)
//     plus the Milestone YAML so its .status conditions reveal WHY it's parked
//   - current controller-manager logs (so the reconcile decisions are visible)
//
// Best-effort: every kubectl invocation tolerates errors. Plain exec.Command
// (no ctx) is intentional — when a spec fails the suite ctx may be cancelling.

package e2e

import (
	"fmt"
	"os"
	"os/exec"

	. "github.com/onsi/ginkgo/v2"
)

// dumpE2ESpecFailureDiagnostics emits authoring-Job / CR-ladder / controller
// state for a failed spec. It is intended to be called from an AfterEach guarded
// by CurrentSpecReport().Failed(), so the evidence reaches stdout before the
// AfterSuite tears the cluster down.
//
// testNs is the spec's fixture namespace (e.g. tide-e2e-gates), where the
// Project/Milestone and any dispatched authoring Jobs live. The controller
// namespace (kindE2EControllerNamespace) is always dumped too for the manager
// logs.
func dumpE2ESpecFailureDiagnostics(reason, testNs string) {
	hdr := "=== E2E SPEC-FAILURE DIAGNOSTICS (" + reason + ") testNs=" + testNs +
		" controllerNs=" + kindE2EControllerNamespace + " ==="
	fmt.Fprintln(os.Stdout, hdr)
	GinkgoWriter.Println(hdr)

	run := func(label string, args ...string) {
		full := append([]string{"--kubeconfig", kindE2EKubeconfigPath}, args...)
		out, err := exec.Command("kubectl", full...).CombinedOutput()
		block := "--- " + label + " ---\n" + string(out)
		if err != nil {
			block += "(kubectl error: " + err.Error() + ")\n"
		}
		fmt.Fprintln(os.Stdout, block)
		GinkgoWriter.Println(block)
	}

	// THE key evidence: are the authoring/task Jobs (and their pods) pulling,
	// running, completed, or stuck in ImagePullBackOff?
	run("test-ns jobs + pods", "get", "jobs,pods", "-n", testNs, "-o", "wide")
	run("test-ns describe pods", "describe", "pods", "-n", testNs)
	run("test-ns events", "get", "events", "-n", testNs, "--sort-by=.lastTimestamp")

	// The CR ladder: WHY is the Milestone parked? Conditions / waiting-on state.
	run("test-ns CR ladder",
		"get", "projects,milestones,phases,plans,tasks,waves",
		"-n", testNs, "-o", "wide")
	run("test-ns milestones (yaml)", "get", "milestones", "-n", testNs, "-o", "yaml")

	// Current controller-manager logs — the reconcile decisions behind the parked
	// state. Not namespace-filterable server-side (logs are per-pod), so the full
	// tail is dumped; greppable by the test namespace token in post-run analysis.
	run("controller-manager logs (current)", "logs",
		"-n", kindE2EControllerNamespace,
		"-l", "control-plane=controller-manager", "--all-containers=true",
		"--tail=300", "--prefix=true")

	footer := "=== END E2E SPEC-FAILURE DIAGNOSTICS ==="
	fmt.Fprintln(os.Stdout, footer)
	GinkgoWriter.Println(footer)
}
