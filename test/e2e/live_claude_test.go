//go:build live_e2e

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

// live_claude_test.go — TEST-03 live nightly E2E.
//
// Skipped by default. Requires ANTHROPIC_API_KEY env. See docs/live-e2e.md
// for the CI recipe + cost baseline. Phase 3 plan 03-11.
//
// Double gate against accidental cost (T-311 / T-312 mitigations):
//
//  1. Build tag `//go:build live_e2e` — without `-tags=live_e2e` this file
//     is excluded from compilation; `make test` / `make test-int` never
//     trigger it. The tag uses an underscore (not hyphen) because Go's
//     build-constraint grammar requires identifier-shaped tags; the
//     Makefile target name `test-e2e-live` remains hyphenated for
//     operator-facing prose.
//  2. Skip-on-missing-creds — even with the build tag, if ANTHROPIC_API_KEY
//     is empty the BeforeEach calls Skip(...) and no Project is applied.
//
// The Makefile `test-e2e-live` target adds a third defense: it exits 1
// before even invoking `go test` when ANTHROPIC_API_KEY is empty.
//
// Budget cap (T-312 third safety net): Project.Spec.budget.absoluteCapCents=100
// (= $1.00). The spec asserts post-run that Project.Status.budget.costSpentCents
// is in (0, 100) — a real Claude call happened AND the budget gate held.
package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha2"
)

// BeforeSuite for the live-e2e suite — wires liveE2EClient + liveE2ECtx
// from the helper in suite_test.go. Only registered when the live-e2e tag
// is active; without the tag, the package-global initLiveE2ESuite is never
// called and the suite runs zero specs.
var _ = BeforeSuite(func() {
	initLiveE2ESuite()
})

var _ = AfterSuite(func() {
	teardownLiveE2ESuite()
})

const (
	// liveTestNamespace is the namespace the fixture Project lives in.
	liveTestNamespace = "live-claude-test"

	// liveProjectName matches the fixture YAML's metadata.name.
	liveProjectName = "live-claude"

	// liveFixturePath is the path (relative to the test binary's working
	// dir = ./test/e2e/) to the YAML template.
	liveFixturePath = "testdata/live-claude-project.yaml"

	// liveBudgetCapCents matches Project.Spec.budget.absoluteCapCents in
	// the fixture YAML. Used in the post-run assertion that costSpentCents
	// is strictly less than the cap.
	liveBudgetCapCents int64 = 100 // $1.00 — T-312 mitigation

	// liveMilestoneCommitPattern is the D-B2 #3 commit message format the
	// push Job uses when the orchestrator's MilestoneReconciler authors
	// a MILESTONE.md artifact. Regex-asserted post-run.
	liveMilestoneCommitPattern = `^tide: milestone .+ authored$`
)

var _ = Describe("Live Claude E2E (TEST-03)", Label("live-e2e"), Ordered, func() {
	var (
		apiKey string
	)

	BeforeEach(func() {
		// Skip-on-missing-creds — second gate. The first gate is the
		// `//go:build live_e2e` tag on this file; the third is the
		// Project.Spec.budget cap. See docs/live-e2e.md for the full
		// double-gate protocol.
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			Skip("ANTHROPIC_API_KEY not set — skipping live E2E (T-311 / T-312 gate)")
		}
		// Defense against accidentally logging the key in CI output:
		// never print apiKey itself, only its presence/length.
		GinkgoWriter.Printf("ANTHROPIC_API_KEY present (length=%d)\n", len(apiKey))
	})

	AfterEach(func() {
		// Best-effort cleanup. We delete the Project + Namespace so a
		// follow-up nightly run starts clean. If KEEP_LIVE_NAMESPACE=true
		// is set, leave the artifacts for debug inspection.
		if os.Getenv("KEEP_LIVE_NAMESPACE") == "true" {
			GinkgoWriter.Println("KEEP_LIVE_NAMESPACE=true; keeping namespace for inspection")
			return
		}
		if liveE2EClient == nil {
			return
		}
		_ = liveE2EClient.Delete(liveE2ECtx, &tideprojectv1alpha1.Project{
			ObjectMeta: metav1.ObjectMeta{Name: liveProjectName, Namespace: liveTestNamespace},
		})
		_ = liveE2EClient.Delete(liveE2ECtx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: liveTestNamespace},
		})
	})

	It("dispatches Claude planner + writes Markdown commit + respects budget", func() {
		By("Applying the live-claude fixture YAML with env substitution")
		applyLiveFixture(apiKey)

		By("Waiting for the Project to be picked up (Status.phase populated)")
		Eventually(func() string {
			p := &tideprojectv1alpha1.Project{}
			err := liveE2EClient.Get(liveE2ECtx,
				client.ObjectKey{Name: liveProjectName, Namespace: liveTestNamespace}, p)
			if err != nil {
				return ""
			}
			return p.Status.Phase
		}, 2*time.Minute, 5*time.Second).ShouldNot(BeEmpty(),
			"Project.Status.phase should populate within 2m of apply")

		By("Waiting for the Milestone planner Job to complete and emit MILESTONE.md")
		// Phase 3 plan 03-02 / 03-05: the MilestoneReconciler dispatches a
		// planner Job; on success the Job emits a Milestone child CRD with
		// Status.phase=Succeeded.
		Eventually(func() (string, error) {
			milestones := &tideprojectv1alpha1.MilestoneList{}
			if err := liveE2EClient.List(liveE2ECtx, milestones,
				client.InNamespace(liveTestNamespace)); err != nil {
				return "", err
			}
			if len(milestones.Items) == 0 {
				return "no-milestones-yet", nil
			}
			return milestones.Items[0].Status.Phase, nil
		}, 12*time.Minute, 10*time.Second).Should(Equal("Succeeded"),
			"At least one Milestone should reach Succeeded within 12m")

		By("Waiting for the push Job to land a commit on the per-run branch")
		// Phase 3 D-B6: Project.Status.git.lastPushedSHA is populated after
		// the push Job's --force-with-lease push succeeds.
		Eventually(func() string {
			p := &tideprojectv1alpha1.Project{}
			err := liveE2EClient.Get(liveE2ECtx,
				client.ObjectKey{Name: liveProjectName, Namespace: liveTestNamespace}, p)
			if err != nil {
				return ""
			}
			return p.Status.Git.LastPushedSHA
		}, 13*time.Minute, 10*time.Second).ShouldNot(BeEmpty(),
			"Project.Status.git.lastPushedSHA should be set after push completes")

		// Refetch the Project for the post-run assertions.
		final := &tideprojectv1alpha1.Project{}
		Expect(liveE2EClient.Get(liveE2ECtx,
			client.ObjectKey{Name: liveProjectName, Namespace: liveTestNamespace}, final)).
			To(Succeed())

		By("Asserting D-B2 #3 milestone-authored commit message lands on the branch")
		// Inspect the commit message on the per-run branch. The push Job's
		// envelope-out is written under /workspace/envelopes/push/<uid>/out.json
		// on the workspace PVC; the in-cluster fixture-repo also carries the
		// commit. We assert the commit message matches the D-B2 #3 format
		// (Project.Status.git.branchName + LastPushedSHA combined with a
		// regex check on the commit message harvested via kubectl exec into
		// a debug pod — but for the canonical assertion we check the
		// Conditions list for the LastMilestoneCommit message.
		//
		// Simpler approach: check the controller logs for the structured
		// "milestone-authored-commit" log line; harness redaction (Phase 2
		// D-F4 + internal/harness/redact patterns) keeps ANTHROPIC_API_KEY
		// out of these logs (T-311 mitigation).
		commitMsg := captureLastMilestoneCommitMessage(final)
		Expect(commitMsg).To(MatchRegexp(liveMilestoneCommitPattern),
			"Expected D-B2 #3 commit message shape; got %q", commitMsg)

		By("Asserting budget tally is in the safe window (0, 100) cents")
		// Phase 2 D-D2: real Claude dispatch records spend in
		// Project.Status.budget.costSpentCents. Real spend > 0 proves the
		// API call happened; spend < cap proves the budget gate held
		// (T-312 mitigation: budget cap is the third safety net after the
		// build tag + ANTHROPIC_API_KEY env gate). The plan's vocabulary
		// refers to this as Status.budget.usdSpent; the on-CRD field is
		// costSpentCents (int64 cents). One USD-cent ≈ 1 unit here.
		Expect(final.Status.Budget.CostSpentCents).To(BeNumerically(">", int64(0)),
			"Project.Status.budget.costSpentCents (usdSpent) should be > 0 — real Claude call happened")
		Expect(final.Status.Budget.CostSpentCents).To(BeNumerically("<", liveBudgetCapCents),
			"Project.Status.budget.costSpentCents (usdSpent) should be < cap (%d cents) — budget held",
			liveBudgetCapCents)

		GinkgoWriter.Printf("Live E2E success: phase=%q lastPushedSHA=%q costSpentCents=%d (cap=%d)\n",
			final.Status.Phase, final.Status.Git.LastPushedSHA,
			final.Status.Budget.CostSpentCents, liveBudgetCapCents)
	})
})

// applyLiveFixture reads the fixture YAML, substitutes ${ANTHROPIC_API_KEY}
// + ${GIT_PAT_OR_DUMMY} from env, and applies the rendered manifest via
// kubectl. Substitution happens in-memory; the rendered manifest is NEVER
// written to disk and the apiKey is NEVER logged (T-311 mitigation —
// real-API-key leak via test logs).
func applyLiveFixture(apiKey string) {
	rawBytes, err := os.ReadFile(liveFixturePath)
	Expect(err).NotTo(HaveOccurred(), "Failed to read fixture %s", liveFixturePath)

	// os.Expand substitutes ${VAR} references from a mapping function. We
	// intentionally don't use os.ExpandEnv so the substitution is precise:
	// only the two placeholders we control.
	rendered := os.Expand(string(rawBytes), func(key string) string {
		switch key {
		case "ANTHROPIC_API_KEY":
			return apiKey
		case "GIT_PAT_OR_DUMMY":
			if v := os.Getenv("GIT_PAT_OR_DUMMY"); v != "" {
				return v
			}
			return "dummy-not-used-for-file-fixtures"
		default:
			return ""
		}
	})

	// Pipe rendered YAML to `kubectl apply -f -`. We pipe via stdin
	// instead of writing to a temp file so the apiKey value never lands
	// on disk (one more T-311 defense — even a stray temp file could end
	// up in CI artifacts).
	//nolint:gosec // kubectl is on the operator's PATH; argv is fixed.
	cmd := exec.CommandContext(liveE2ECtx, "kubectl",
		"--kubeconfig", liveE2EKubeconfigPath,
		"apply", "-f", "-", "--timeout=60s")
	cmd.Stdin = strings.NewReader(rendered)
	out, err := cmd.CombinedOutput()
	Expect(err).NotTo(HaveOccurred(),
		"kubectl apply failed: %v\n%s", err, redactedOutput(string(out), apiKey))
}

// captureLastMilestoneCommitMessage probes the Project's Conditions list
// for a Condition with type=LastMilestoneCommit (Phase 3 D-B2 convention)
// and returns its message field. If no such condition exists, returns an
// empty string — the caller's MatchRegexp will fail with a clear message.
//
// This helper deliberately avoids cloning the per-run branch with go-git
// (which would require the GIT_PAT and add cross-cluster network surface).
// The Condition reflects the push Job's envelope-out, which the controller
// already imported into status.
func captureLastMilestoneCommitMessage(p *tideprojectv1alpha1.Project) string {
	for _, c := range p.Status.Conditions {
		if c.Type == "LastMilestoneCommit" || c.Type == "MilestoneAuthored" {
			return c.Message
		}
	}
	// Fallback: if the condition is not yet wired (Phase 3 deferred),
	// synthesize a conventional commit string from the Status.git fields
	// so the regex assertion can fail with a clear "got X" message.
	if p.Status.Git.LastPushedSHA != "" {
		return fmt.Sprintf("tide: milestone live-claude authored (sha=%s)", p.Status.Git.LastPushedSHA)
	}
	return ""
}

// redactedOutput scrubs the apiKey from a string (defense-in-depth on top
// of harness redaction in internal/harness/redact patterns.go which already
// handles sk-ant-* shapes). Applied to all stdout/stderr we surface to
// Ginkgo logs (T-311 mitigation).
func redactedOutput(s, apiKey string) string {
	if apiKey == "" {
		return s
	}
	return strings.ReplaceAll(s, apiKey, "[REDACTED-ANTHROPIC-API-KEY]")
}

// Compile-time guards: keep otherwise-unused imports reachable so the
// detailed-debug branch can call them without a rebuild.
var _ = apierrors.IsNotFound
var _ = filepath.Base
var _ = regexp.MustCompile
