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

package kind_integration

// artifact_staging_test.go — Layer B kind integration spec for plan 37-09.
//
// Coverage: DASH-02 (reworded, per CONTEXT D-01/D-03 and plan 37-02's objective).
//
// The truth under regression lock:
//
//	After a level's planner completes, the bare remote's run branch carries
//	.tide/planning/<kind>/<name>/ planning artifacts byte-identical to the
//	planner's envelope *.md — materialized at planner-completion time (D-01),
//	full-fidelity (no size cap / truncation, D-03), excluding the dispatch-internal
//	in.json/out.json envelopes (D-04) — and the artifact pushes coexist with the
//	boundary-push --force-with-lease machinery without lease interference (Pitfall 2).
//
// Fixture idiom: this reuses the medium_http_test.go bare-remote fixture — a real
// in-cluster git-http server (git-http-backend + nginx + fcgiwrap) serving a bare
// repo over http:// — so the push path is the pure-Go go-git HTTP transport hitting
// a REAL remote (no mocking). A stub-subagent Project is driven to Complete; the
// stub planner emits a deterministic planning *.md per level (cmd/stub-subagent
// plannerDoc), so the staged bytes are known and byte-fidelity is assertable
// without reading the PVC.
//
// The bare repo lives on demo-remote-pvc, mounted at /srv/git in the git-http
// server pod, so the run branch's tree is inspected directly with git against the
// bare repo (ls-tree / cat-file) via kubectl exec into that pod.

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
)

const (
	// artifactStagingNS is the isolated namespace for the artifact-staging spec.
	// Distinct from mediumHTTPNamespace so the two git-http fixtures don't collide.
	artifactStagingNS = "artifact-staging-test"

	// artifactBareRepoGitDir is the bare repo's --git-dir inside the git-http
	// server pod (demo-remote-pvc mounted at /srv/git; demo-init seeds
	// demo-remote.git at the PVC root).
	artifactBareRepoGitDir = "/srv/git/demo-remote.git"
)

var _ = Describe("Planning artifacts land on the run branch at planner completion (DASH-02, reworded)", Label("kind"), Ordered, func() {

	BeforeAll(func() {
		skipIfCRDsOnlyMode()
		// createNamespace provisions the namespace + tide-subagent SA + tide-projects
		// PVC + tide-signing-key Secret + PVC prewarm (shared suite helper).
		createNamespace(artifactStagingNS)

		By("Loading tide-demo-init + tide-git-http-server images into kind cluster")
		loadRequiredImage(mediumHTTPDemoInitImage)
		loadRequiredImage(mediumHTTPServerImage)

		By("Creating demo-remote-pvc in " + artifactStagingNS)
		Expect(applyYAML(mediumDemoRemotePVCYAML(artifactStagingNS))).To(Succeed(),
			"demo-remote-pvc must be created in "+artifactStagingNS)

		By("Applying demo-remote-init Job to bootstrap the bare repo")
		Expect(applyYAML(mediumDemoRemoteInitJobYAML(artifactStagingNS))).To(Succeed())
		Eventually(func() error {
			cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
				"wait", "--for=condition=Complete",
				"job/demo-remote-init", "-n", artifactStagingNS, "--timeout=10s")
			out, err := cmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("demo-remote-init not yet Complete: %w\n%s", err, out)
			}
			return nil
		}, 2*time.Minute, 5*time.Second).Should(Succeed(),
			"demo-remote-init Job must reach Complete within 2 minutes")

		By("Applying git-http-server Deployment + Service")
		Expect(applyYAML(mediumGitHTTPServerYAML(artifactStagingNS))).To(Succeed())
		Eventually(func() error {
			cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
				"wait", "--for=condition=Available",
				"deployment/"+mediumHTTPServiceName, "-n", artifactStagingNS, "--timeout=10s")
			out, err := cmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("git-http-server not yet Available: %w\n%s", err, out)
			}
			return nil
		}, 2*time.Minute, 5*time.Second).Should(Succeed(),
			"git-http-server Deployment must reach Available within 2 minutes")
	})

	AfterAll(func() {
		deleteNamespace(artifactStagingNS)
		if CurrentSpecReport().Failed() {
			exportKindLogs()
		}
	})

	It("commits full-fidelity .tide/planning/<kind>/<name>/*.md to the run branch, coexisting with the boundary-push lease", func() {
		skipIfCRDsOnlyMode()

		By("Creating tide-secrets Secret (empty GIT_PAT → anonymous in-cluster http push)")
		tideSecretsYAML := fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: tide-secrets
  namespace: %s
type: Opaque
data:
  ANTHROPIC_API_KEY: dGVzdC1hcGkta2V5LXN0dWItc3ViYWdlbnQtZG9lcy1ub3QtdXNlLWl0
  GIT_PAT: ""
`, artifactStagingNS)
		Expect(applyYAML(tideSecretsYAML)).To(Succeed(),
			"tide-secrets Secret must be created in "+artifactStagingNS)

		targetRepo := fmt.Sprintf("http://git-http-server.%s.svc.cluster.local/demo-remote.git", artifactStagingNS)
		projName := fmt.Sprintf("artifact-staging-project-%d", GinkgoRandomSeed())
		proj := newStubProject(artifactStagingNS, projName,
			withTargetRepo(targetRepo),
			withProviderSecret("tide-secrets"),
			withGit(targetRepo, "tide-secrets"))
		By("Creating stub Project (http:// targetRepo) in " + artifactStagingNS)
		Expect(k8sClient.Create(ctx, proj)).To(Succeed(),
			"stub Project must be admitted (http:// targetRepo passes CEL rule)")
		defer func() { _ = k8sClient.Delete(ctx, proj) }()

		// Drive the full cascade to Complete over the real http:// transport — every
		// planner level (project/milestone/phase/plan) completes and the cumulative
		// artifact push rides the boundary tide-push Job to the run branch.
		By("Waiting for the Project to reach Complete over http://")
		var completed tideprojectv1alpha2.Project
		Eventually(func() error {
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: projName, Namespace: artifactStagingNS}, &completed); err != nil {
				return err
			}
			if completed.Status.Phase != tideprojectv1alpha2.PhaseComplete {
				return fmt.Errorf("Project %s: want Phase=Complete, got %q (%s)",
					projName, completed.Status.Phase, mediumLastConditionMessage(completed))
			}
			return nil
		}, 12*time.Minute, 5*time.Second).Should(Succeed(),
			"stub Project must reach Complete within 12 minutes (stub + http:// transport)")

		// The run branch is stamped on Status well before Complete; read it off the
		// Complete snapshot.
		runBranch := completed.Status.Git.BranchName
		Expect(runBranch).To(HavePrefix("tide/run-"),
			"Project must carry a tide/run-<name>-<unix> run branch")

		// Pitfall 2 guard: artifact pushes must NOT interfere with the boundary
		// --force-with-lease machinery. The boundary push lands ASYNCHRONOUSLY
		// AFTER Complete — #13b makes Complete the control-plane succession roll-up,
		// NOT gated on the push outcome; reconcileBoundaryPush runs the bounded
		// retry state machine post-Complete and only then advances LastPushedSHA
		// (from the push-result envelope headSHA). So poll the LIVE CR until the
		// push lands rather than asserting on the stale Complete snapshot.
		By("Polling the Project CR until the boundary push lands and advances LastPushedSHA")
		// In-poll diagnostics prerequisites, captured ONCE before the poll: the
		// git-http-server pod (for the git-receive-pack request count) and the Project
		// UID (the shared tide-push-<uid> / tide-clone-<uid> Job/pod name key).
		diagPod := gitHTTPServerPod(artifactStagingNS)
		diagUID := string(completed.UID)
		Eventually(func(g Gomega) {
			var p tideprojectv1alpha2.Project
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: projName, Namespace: artifactStagingNS}, &p)).To(Succeed())
			// Per-tick diagnostics — fire WHILE the namespace still exists (run-4's
			// DeferCleanup dump ran AFTER teardown and captured nothing). This yields the
			// decisive evidence for the orchestrator's next single run: does the shared
			// push pod SUCCEED, did a git-receive-pack reach the remote, and is a headSHA
			// reaching the CR? A diagnostic error degrades to a logged marker — it never
			// fails the poll.
			logPushDiagnostics(artifactStagingNS, diagUID, diagPod, p.Status.Git.LastPushedSHA)
			g.Expect(p.Status.Phase).NotTo(Equal(tideprojectv1alpha2.PhasePushLeaseFailed),
				"no push must have been rejected by --force-with-lease (Pitfall 2: artifact/boundary push must not interfere)")
			g.Expect(p.Status.Git.LeaseFailureCount).To(BeNumerically("==", int32(0)),
				"LeaseFailureCount must be 0 — no lease-rejected pushes across artifact + boundary staging")
			g.Expect(p.Status.Git.LastPushedSHA).NotTo(BeEmpty(),
				"lastPushedSHA must have advanced — the boundary-push machinery landed at least one push")
		}, 5*time.Minute, 5*time.Second).Should(Succeed(),
			"the boundary push must land and advance Status.Git.LastPushedSHA after Complete")

		pod := gitHTTPServerPod(artifactStagingNS)
		GinkgoWriter.Printf("artifact-staging-test: git-http-server pod=%s, run branch=%s\n", pod, runBranch)

		// Materialization-time (D-01): poll the bare remote's run branch until the
		// planning artifacts appear. The artifact push fires at planner completion
		// (before/independent of any approve gate — this Project's gates are auto).
		By("Polling the bare remote run branch for .tide/planning/ artifacts")
		var tidePaths []string
		Eventually(func() error {
			all := lsTreeRunBranch(pod, artifactStagingNS, runBranch)
			tidePaths = filterPrefix(all, ".tide/")
			mdCount := 0
			for _, p := range tidePaths {
				if strings.HasSuffix(p, ".md") {
					mdCount++
				}
			}
			if mdCount == 0 {
				return fmt.Errorf("no .tide/planning/*.md on run branch %s yet (paths: %v)", runBranch, tidePaths)
			}
			return nil
		}, 5*time.Minute, 5*time.Second).Should(Succeed(),
			"planning artifacts must materialize under .tide/planning/ on the run branch")

		GinkgoWriter.Printf("artifact-staging-test: .tide/ paths on run branch: %v\n", tidePaths)

		// Assertion 1 — at least one top-level *.md under .tide/planning/<kind>/<name>/.
		var mdPaths []string
		for _, p := range tidePaths {
			if strings.HasSuffix(p, ".md") {
				mdPaths = append(mdPaths, p)
			}
		}
		Expect(mdPaths).NotTo(BeEmpty(),
			"the run branch must carry at least one planning *.md under .tide/planning/")

		// Assertion 2 — D-04 exclusion: no dispatch-internal envelope JSON anywhere
		// under .tide/ (in.json / out.json are never staged).
		for _, p := range tidePaths {
			base := p[strings.LastIndex(p, "/")+1:]
			Expect(base).NotTo(Equal("in.json"),
				"dispatch-internal in.json must never be staged under .tide/ (D-04): %s", p)
			Expect(base).NotTo(Equal("out.json"),
				"dispatch-internal out.json must never be staged under .tide/ (D-04): %s", p)
		}

		// Assertion 3 — layout + full-fidelity content. Every staged *.md sits at
		// .tide/planning/<kind>/<name>/<Doc>.md; its bytes equal the deterministic
		// stub planner doc for that <kind> (level) — proving no truncation / size
		// cap / mutation in the staging pipeline (D-03).
		kindsSeen := map[string]bool{}
		for _, p := range mdPaths {
			parts := strings.Split(p, "/")
			// .tide / planning / <kind> / <name> / <Doc>.md
			Expect(len(parts)).To(BeNumerically(">=", 5),
				"staged *.md must live at .tide/planning/<kind>/<name>/<Doc>.md: %s", p)
			Expect(parts[0]).To(Equal(".tide"))
			Expect(parts[1]).To(Equal("planning"))
			level := parts[2]
			kindsSeen[level] = true

			blob := catFileRunBranch(pod, artifactStagingNS, runBranch, p)
			parent := scanEnvelopeField(blob, "parent")
			Expect(parent).NotTo(BeEmpty(),
				"staged planning doc %s must retain its 'parent:' line (truncation guard)", p)
			Expect(string(blob)).To(Equal(expectedStubPlanningDoc(level, parent)),
				"staged planning doc %s must be byte-identical to the deterministic stub planner output (D-03 full fidelity)", p)
		}

		// Assertion 4 — the staged kinds are a subset of the four planner levels,
		// and the milestone level (which reaches Succeeded first and most reliably)
		// is present.
		for k := range kindsSeen {
			Expect([]string{"project", "milestone", "phase", "plan"}).To(ContainElement(k),
				"staged kind %q must be one of the four planner levels", k)
		}
		Expect(kindsSeen).To(HaveKey("milestone"),
			"the milestone planning artifact must be staged on the run branch")

		staged := make([]string, 0, len(kindsSeen))
		for k := range kindsSeen {
			staged = append(staged, k)
		}
		sort.Strings(staged)
		GinkgoWriter.Printf("artifact-staging-test: staged planner levels: %v\n", staged)
	})
})

// ---- helpers (artifact_staging_test.go-local) ----

// logPushDiagnostics dumps, per poll tick and WHILE the namespace still exists, the
// evidence needed to decide whether the shared tide-push-<uid> Job pod actually
// SUCCEEDS and whether a headSHA reaches the CR (DASH-02 run-5 instrumentation): the
// push/clone pod phases, the git-http-server git-receive-pack request count, and the
// current Status.Git.LastPushedSHA. It writes to GinkgoWriter — NOT DeferCleanup /
// AfterAll, which fire AFTER teardown (the run-4 flaw where the ns/CR were already
// gone). Every exec failure degrades to a logged marker so a diagnostic error never
// fails the enclosing poll.
func logPushDiagnostics(ns, projUID, gitHTTPPod, lastPushedSHA string) {
	// Push/clone pod phases (the Jobs are tide-push-<uid> / tide-clone-<uid>; their
	// pods carry that name prefix). Proves whether the push/clone pods ran at all and
	// their terminal phase (Succeeded/Failed/Running).
	phases := "(no tide-push/tide-clone pods)"
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
		"get", "pods", "-n", ns, "-o",
		`jsonpath={range .items[*]}{.metadata.name}={.status.phase} {end}`)
	if out, err := cmd.CombinedOutput(); err == nil {
		var relevant []string
		for tok := range strings.FieldsSeq(string(out)) {
			if strings.Contains(tok, "tide-push-"+projUID) || strings.Contains(tok, "tide-clone-"+projUID) {
				relevant = append(relevant, tok)
			}
		}
		if len(relevant) > 0 {
			phases = strings.Join(relevant, " ")
		}
	} else {
		phases = fmt.Sprintf("(pod-get err: %v)", err)
	}

	// git-http-server git-receive-pack request count — direct proof a push actually
	// reached the bare remote (vs. a push pod that failed before the transport).
	recvCount := "(n/a)"
	if gitHTTPPod != "" {
		lc := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
			"logs", gitHTTPPod, "-c", "git-http-server", "-n", ns)
		if out, err := lc.CombinedOutput(); err == nil {
			recvCount = fmt.Sprintf("%d", strings.Count(string(out), "git-receive-pack"))
		} else {
			recvCount = fmt.Sprintf("(logs err: %v)", err)
		}
	}

	GinkgoWriter.Printf("artifact-staging diag: push/clone pods=[%s] git-receive-pack=%s LastPushedSHA=%q\n",
		phases, recvCount, lastPushedSHA)
}

// gitHTTPServerPod returns the name of the running git-http-server Pod in ns.
func gitHTTPServerPod(ns string) string {
	var name string
	Eventually(func() error {
		cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
			"get", "pods", "-n", ns, "-l", "app=git-http-server",
			"-o", "jsonpath={.items[0].metadata.name}")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("get git-http-server pod: %w\n%s", err, out)
		}
		name = strings.TrimSpace(string(out))
		if name == "" {
			return fmt.Errorf("no git-http-server pod found in %s", ns)
		}
		return nil
	}, 60*time.Second, 2*time.Second).Should(Succeed(),
		"git-http-server Pod must be discoverable in "+ns)
	return name
}

// lsTreeRunBranch lists the run branch's tree (recursive, names only) by running
// git against the bare repo inside the git-http-server pod. Returns nil if the
// branch does not yet exist (the push has not landed).
func lsTreeRunBranch(pod, ns, branch string) []string {
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
		"exec", pod, "-c", "git-http-server", "-n", ns, "--",
		"git", "--git-dir="+artifactBareRepoGitDir, "ls-tree", "-r", "--name-only", branch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Branch absent yet (push not landed) or transient exec error — caller polls.
		return nil
	}
	var paths []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			paths = append(paths, line)
		}
	}
	return paths
}

// catFileRunBranch returns the raw blob bytes of path at branch from the bare repo.
func catFileRunBranch(pod, ns, branch, path string) []byte {
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
		"exec", pod, "-c", "git-http-server", "-n", ns, "--",
		"git", "--git-dir="+artifactBareRepoGitDir, "cat-file", "blob", branch+":"+path)
	out, err := cmd.Output() // stdout only — raw blob bytes, no stderr contamination
	Expect(err).NotTo(HaveOccurred(),
		"git cat-file blob %s:%s must succeed", branch, path)
	return out
}

// filterPrefix returns the subset of paths that start with prefix.
func filterPrefix(paths []string, prefix string) []string {
	var out []string
	for _, p := range paths {
		if strings.HasPrefix(p, prefix) {
			out = append(out, p)
		}
	}
	return out
}

// scanEnvelopeField returns the value of a "field: value" line in the doc body,
// or "" if absent.
func scanEnvelopeField(body []byte, field string) string {
	for line := range strings.SplitSeq(string(body), "\n") {
		if v, ok := strings.CutPrefix(line, field+": "); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// expectedStubPlanningDoc mirrors cmd/stub-subagent's plannerDoc body EXACTLY.
// It is duplicated here (not imported — cmd/stub-subagent is package main) so the
// byte-fidelity assertion pins the staged bytes to the stub's deterministic output.
// If plannerDoc's format changes, this must change in lockstep.
func expectedStubPlanningDoc(level, parent string) string {
	return fmt.Sprintf(
		"# %s planning artifact (stub-subagent)\n\n"+
			"level: %s\n"+
			"parent: %s\n\n"+
			"Deterministic planning document emitted by the stub-subagent so the\n"+
			"artifact-staging pipeline (37-02 --stage-envelopes / 37-06 trigger) has a\n"+
			"full-fidelity *.md to commit onto the run branch under\n"+
			".tide/planning/<kind>/<name>/ (DASH-02).\n",
		level, level, parent,
	)
}
