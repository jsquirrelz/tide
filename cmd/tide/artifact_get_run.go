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

// artifact_get_run.go — real inspector-pod implementation for `tide artifact-get`.
//
// Architecture:
//   - parseArtifactRef: unchanged; splits <ns>/<project>/<path> into components.
//   - artifactGetRun: testable seam. Validates input, resolves the Project UID,
//     delegates to inspectorPodRunner.
//   - inspectorPodRunner (func var): creates the inspector Pod, waits for readiness,
//     streams log bytes to stdout, defers deletion. Function var so tests can
//     inject a fake without a live apiserver.
//   - defaultInspectorPodRunner: production implementation.
//
// D-10: stdout carries raw artifact bytes only; status/progress messages go to stderr.
// D-11: race-free readiness wait — pod-internal shell loop (exist + stability poll).
// D-12: non-zero error after timeout window is exhausted.
// T-15-08: artifact path delivered via Pod env var (ARTIFACT_PATH), never
//          fmt-interpolated into the sh -c string.
// T-15-09: deferred Delete with context.Background() covers all exit paths.
// T-15-11: image fixed to busybox:1.36; command shape fixed (wait+stat+cat).

package main

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

// inspectorPodRunner creates and streams the inspector pod. Function var so
// tests can inject without a live apiserver (mirrors tail.go's tailStreamer).
var inspectorPodRunner = defaultInspectorPodRunner

// inspectorPodRunnerFunc is the signature for the inspectorPodRunner seam.
type inspectorPodRunnerFunc func(
	ctx context.Context,
	cs kubernetes.Interface,
	ns, projectUID, artifactPath, pvcName string,
	out, errOut io.Writer,
) error

// parseArtifactRef splits the operator-provided "<ns>/<project>/<path>" form
// into its three components. The path component may itself contain slashes;
// the function splits only on the first two.
//
// The PLAN spec (CONTEXT.md + 04-07-PLAN.md) documents this exact form so
// the CLI can resolve the per-project PVC + relative file path without an
// extra round-trip to the apiserver.
//
// Rejected inputs:
//
//   - empty string
//   - strings without at least two '/' separators
//   - strings whose first or second component is empty (e.g. "/foo/bar")
//   - strings whose path component is empty (e.g. "default/proj/")
func parseArtifactRef(ref string) (namespace, project, path string, err error) {
	if ref == "" {
		return "", "", "", fmt.Errorf("artifact ref is empty (expected <namespace>/<project>/<path>)")
	}
	// SplitN with n=3 splits only on the first 2 separators so the path
	// component may itself contain slashes.
	parts := strings.SplitN(ref, "/", 3)
	if len(parts) < 3 {
		return "", "", "", fmt.Errorf("artifact ref %q malformed (expected <namespace>/<project>/<path>)", ref)
	}
	namespace, project, path = parts[0], parts[1], parts[2]
	if namespace == "" || project == "" || path == "" {
		return "", "", "", fmt.Errorf("artifact ref %q has empty component (ns=%q project=%q path=%q)",
			ref, namespace, project, path)
	}
	return namespace, project, path, nil
}

// validateArtifactPath rejects paths that could be exploited for shell
// injection or directory traversal (T-15-08). Rejected patterns:
//   - ".." component (directory traversal)
//   - absolute path (leading "/")
//   - shell metacharacters: single/double quotes, backtick, $, ;, &, |, whitespace
func validateArtifactPath(path string) error {
	// Directory traversal guard.
	for _, part := range strings.Split(path, "/") {
		if part == ".." {
			return fmt.Errorf("artifact path %q contains '..' (directory traversal not allowed)", path)
		}
	}
	// Absolute path guard.
	if strings.HasPrefix(path, "/") {
		return fmt.Errorf("artifact path %q must not be absolute", path)
	}
	// Shell metacharacter guard — path is passed via env var but defense in depth.
	// Use explicit rune checks rather than a raw-string constant to avoid
	// confusing \t/\n escape sequences in backtick literals.
	for _, r := range path {
		switch r {
		case '\'', '"', '`', '$', ';', '&', '|', ' ', '\t', '\n', '\r':
			return fmt.Errorf("artifact path %q contains shell metacharacter %q", path, r)
		}
	}
	return nil
}

// artifactGetRun is the testable seam for `tide artifact-get`.
// Validates the ref, resolves the Project UID, then delegates to
// inspectorPodRunner.
func artifactGetRun(
	ctx context.Context,
	k client.Client,
	cs kubernetes.Interface,
	ref, pvcName string,
	out, errOut io.Writer,
) error {
	ns, projName, artifactPath, err := parseArtifactRef(ref)
	if err != nil {
		return err
	}
	if err := validateArtifactPath(artifactPath); err != nil {
		return err
	}

	// Resolve Project to get its UID — the PVC subPath is the project UID,
	// NOT the project name (RESEARCH Pitfall 4: subPath matches the directory
	// the manager writes artifacts into, which is keyed by UID).
	var proj tidev1alpha1.Project
	if err := k.Get(ctx, types.NamespacedName{Namespace: ns, Name: projName}, &proj); err != nil {
		return fmt.Errorf("get project %s/%s: %w", ns, projName, err)
	}
	projectUID := string(proj.UID)
	if projectUID == "" {
		return fmt.Errorf("project %s/%s has no UID — cannot resolve PVC subPath", ns, projName)
	}

	return inspectorPodRunner(ctx, cs, ns, projectUID, artifactPath, pvcName, out, errOut)
}

// defaultInspectorPodRunner is the production implementation.
// Creates a short-lived busybox inspector Pod mounting the per-project PVC
// subPath, streams the container logs (= artifact bytes) to out, and deletes
// the Pod on every exit path (T-15-09).
func defaultInspectorPodRunner(
	ctx context.Context,
	cs kubernetes.Interface,
	ns, projectUID, artifactPath, pvcName string,
	out, errOut io.Writer,
) error {
	podName := fmt.Sprintf("tide-inspect-%s", randSuffix(8))

	// D-11: Race-free readiness wait implemented INSIDE the pod via a shell
	// loop. The loop has two stages:
	//   1. Existence wait: polls until the file appears at /workspace/<path>.
	//   2. Stability wait: polls two consecutive `stat -c %s` samples 2 seconds
	//      apart; proceeds only when both sizes are equal. This guards against
	//      reading a half-written artifact when the writer does not use an
	//      atomic write-then-rename.
	//
	// Once the file is stable, the shell cats it. The pod's container output
	// (stdout) becomes the log stream bytes — pure artifact content (D-10).
	//
	// T-15-08: artifact path is delivered via the ARTIFACT_PATH env var and
	// referenced in the shell command as "$ARTIFACT_PATH" — never
	// fmt.Sprintf-interpolated into the sh -c string (defense in depth).
	podSpec := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: ns,
			Labels: map[string]string{
				"app.kubernetes.io/component":  "tide-inspector",
				"app.kubernetes.io/managed-by": "tide-cli",
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name: "inspector",
					// T-15-11: image fixed to busybox:1.36; not configurable.
					Image: "busybox:1.36",
					// T-15-08: path delivered via env var, never interpolated.
					// Stage 1: wait until file exists.
					// Stage 2: stability poll — two equal stat samples 2s apart.
					// Stage 3: cat the file once stable.
					Command: []string{"sh", "-c",
						`until [ -f "/workspace/$ARTIFACT_PATH" ]; do sleep 1; done; ` +
							`while true; do ` +
							`  s1=$(stat -c %s "/workspace/$ARTIFACT_PATH" 2>/dev/null || echo -1); ` +
							`  sleep 2; ` +
							`  s2=$(stat -c %s "/workspace/$ARTIFACT_PATH" 2>/dev/null || echo -1); ` +
							`  if [ "$s1" = "$s2" ] && [ "$s1" != "-1" ]; then break; fi; ` +
							`done; ` +
							`cat "/workspace/$ARTIFACT_PATH"`,
					},
					Env: []corev1.EnvVar{
						{
							Name:  "ARTIFACT_PATH",
							Value: artifactPath,
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "workspace",
							MountPath: "/workspace",
							// subPath = project UID: each project's artifacts live under
							// <PVC>/<projectUID>/ per the per-project PVC layout.
							SubPath:  projectUID,
							ReadOnly: true,
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "workspace",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcName,
							ReadOnly:  true,
						},
					},
				},
			},
		},
	}

	fmt.Fprintf(errOut, "creating inspector pod %s/%s...\n", ns, podName)
	if _, err := cs.CoreV1().Pods(ns).Create(ctx, podSpec, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("create inspector pod: %w", err)
	}
	// T-15-09: defer Delete with context.Background() so cleanup fires on
	// EVERY exit path — timeout, stream error, or success.
	defer func() {
		_ = cs.CoreV1().Pods(ns).Delete(context.Background(), podName, metav1.DeleteOptions{})
	}()

	// Wait for the pod to leave Pending (Running or Succeeded) before opening
	// the log stream. Follow:true starts delivering bytes as soon as the
	// container writes them; waiting for non-Pending avoids the "pod not
	// started" error from the apiserver.
	fmt.Fprintf(errOut, "waiting for inspector pod to start...\n")
	if err := waitForPodRunning(ctx, cs, ns, podName); err != nil {
		return err
	}

	fmt.Fprintf(errOut, "streaming artifact...\n")
	req := cs.CoreV1().Pods(ns).GetLogs(podName, &corev1.PodLogOptions{
		Follow:    true,
		Container: "inspector",
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		return fmt.Errorf("open log stream for inspector pod %s: %w", podName, err)
	}
	defer func() { _ = stream.Close() }()

	// Ctx-cancel watcher — closes the stream so io.Copy returns on timeout
	// (mirrors tail.go's Pitfall 25 mitigation).
	go func() {
		<-ctx.Done()
		_ = stream.Close()
	}()

	// D-10: stdout receives ONLY raw artifact bytes — no status text.
	if _, err := io.Copy(out, stream); err != nil && ctx.Err() == nil {
		return fmt.Errorf("read artifact stream: %w", err)
	}
	if ctx.Err() != nil {
		return fmt.Errorf("artifact %q was not available within the timeout window", artifactPath)
	}
	return nil
}

// waitForPodRunning polls the pod phase until it transitions out of Pending.
// Returns an error if ctx is cancelled before the pod leaves Pending.
func waitForPodRunning(ctx context.Context, cs kubernetes.Interface, ns, podName string) error {
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for inspector pod %s to start", podName)
		default:
		}
		pod, err := cs.CoreV1().Pods(ns).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get inspector pod %s: %w", podName, err)
		}
		switch pod.Status.Phase {
		case corev1.PodRunning, corev1.PodSucceeded:
			return nil
		case corev1.PodFailed:
			return fmt.Errorf("inspector pod %s failed before streaming", podName)
		}
		// Still Pending — yield and retry.
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for inspector pod %s to start", podName)
		default:
		}
	}
}

// randSuffix returns a random lowercase alphanumeric string of length n.
// Used for inspector pod name generation.
func randSuffix(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}
