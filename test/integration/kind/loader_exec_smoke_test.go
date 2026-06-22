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

// loader_exec_smoke_test.go — LIVE kind smoke test for the SPDY loader-exec path.
//
// Purpose: gates the A1/A2 RESEARCH assumptions in Plan 29-03 by exercising the
// exact pods/exec URL construction + NewSPDYExecutor + StreamWithContext call
// pattern against a REAL apiserver (not a unit-test fake). A wrong URL, wrong
// RBAC verb, or wrong PodExecOptions causes this smoke to fail in 29-03's own
// make test-int run, not silently surfacing for the first time in 29-05's full
// drain.
//
// What it does:
//  1. Creates a throwaway namespace.
//  2. Creates a busybox:1.36 pod with an emptyDir at /workspace, Stdin=true,
//     command `tar xzf - -C /workspace` — mirrors the production loader pod.
//  3. Waits for the pod to reach Running.
//  4. Builds the exec URL via cs.CoreV1().RESTClient().Post()...SubResource("exec")
//     .VersionedParams(&corev1.PodExecOptions{...}, runtime.NewParameterCodec(scheme))
//     — the EXACT same pattern as cmd/tide/import_envelopes_run.go:execLoaderPod.
//  5. remotecommand.NewSPDYExecutor(restCfg, "POST", url).StreamWithContext(...)
//     sends an in-memory tgz containing marker.txt.
//  6. Asserts the stream completes without error — proving URL/verb/codec work
//     against a REAL apiserver (A1/A2 live gate).
//
// Verification criterion: the file compiles + go vet passes + contains
// SubResource("exec") + remotecommand (build-only gate per plan). The live
// kind run (StreamWithContext success) is exercised by `make test-int`.
//
// Gating: Label("kind","long") + testing.Short() skip + skipIfCRDsOnlyMode().

package kind_integration

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
)

// loaderSmokeNS is the throwaway namespace for this smoke test.
const loaderSmokeNS = "loader-exec-smoke"

// loaderSmokeMarker is the expected content of the unpacked marker file.
const loaderSmokeMarker = "tide-loader-exec-smoke-marker-29-03\n"

var _ = Describe("Loader SPDY exec smoke", Label("kind", "long"), func() {
	BeforeEach(func() {
		skipIfCRDsOnlyMode()
		if testing.Short() {
			Skip("Skipping long SPDY loader-exec smoke in short mode")
		}
		createNamespace(loaderSmokeNS)
	})

	AfterEach(func() {
		deleteNamespace(loaderSmokeNS)
	})

	It("streams a tgz into a busybox pod via SPDY exec and unpacks marker.txt (A1/A2 gate)", func() {
		smokeCtx, smokeCancel := context.WithTimeout(ctx, 3*time.Minute)
		defer smokeCancel()

		// Build a rest.Config from the suite-level kubeconfigPath.
		restCfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		Expect(err).NotTo(HaveOccurred(), "build REST config from kubeconfig")

		// Build a kubernetes clientset for pod operations.
		cs, err := kubernetes.NewForConfig(restCfg)
		Expect(err).NotTo(HaveOccurred(), "build kubernetes clientset")

		podName := fmt.Sprintf("loader-smoke-%d", GinkgoRandomSeed())

		// Create the loader pod — mirrors production execLoaderPod pod spec:
		//   - busybox:1.36
		//   - RestartPolicy Never
		//   - command: sleep (idle; the tgz is unpacked by a SPDY-exec'd tar,
		//     NOT the pod-main process — a pod-main `tar xzf -` would block
		//     forever on an unattached container stdin and never complete)
		//   - emptyDir at /workspace (no PVC needed for this smoke)
		podSpec := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: loaderSmokeNS,
				Labels: map[string]string{
					"app.kubernetes.io/component":  "tide-loader-smoke",
					"app.kubernetes.io/managed-by": "tide-cli",
				},
			},
			Spec: corev1.PodSpec{
				RestartPolicy: corev1.RestartPolicyNever,
				Containers: []corev1.Container{
					{
						Name:    "loader",
						Image:   "busybox:1.36",
						Command: []string{"sleep", "600"},
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "workspace",
								MountPath: "/workspace",
							},
						},
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: "workspace",
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					},
				},
			},
		}

		By("Creating loader smoke pod")
		_, err = cs.CoreV1().Pods(loaderSmokeNS).Create(smokeCtx, podSpec, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), "create loader smoke pod")
		defer func() {
			_ = cs.CoreV1().Pods(loaderSmokeNS).Delete(context.Background(), podName, metav1.DeleteOptions{})
		}()

		By("Waiting for loader smoke pod to reach Running")
		Eventually(func(g Gomega) {
			pod, err := cs.CoreV1().Pods(loaderSmokeNS).Get(smokeCtx, podName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			// Pod must be Running (not Failed) before we can exec into it.
			g.Expect(pod.Status.Phase).NotTo(Equal(corev1.PodFailed),
				"loader pod must not have failed before stream")
			g.Expect(pod.Status.Phase).To(BeElementOf(corev1.PodRunning, corev1.PodPending),
				"loader pod should reach Running before exec")
		}, 90*time.Second, 2*time.Second).Should(Succeed())

		// Wait specifically for Running (not just non-Failed).
		Eventually(func(g Gomega) {
			pod, err := cs.CoreV1().Pods(loaderSmokeNS).Get(smokeCtx, podName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(pod.Status.Phase).To(Equal(corev1.PodRunning),
				"loader pod should be Running before SPDY exec")
		}, 90*time.Second, 2*time.Second).Should(Succeed())

		By("Building in-memory tgz with marker.txt")
		tgzBytes := buildLoaderSmokeMarkerTgz()

		// Build the exec URL — EXACT same pattern as cmd/tide/import_envelopes_run.go:execLoaderPod.
		// This is the A1/A2 live gate: wrong URL, verb, or VersionedParams fails here.
		By("Building SPDY exec URL via SubResource(\"exec\") + VersionedParams")
		execScheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(execScheme)).To(Succeed())

		execURL := cs.CoreV1().RESTClient().Post().
			Resource("pods").Name(podName).Namespace(loaderSmokeNS).
			SubResource("exec").
			VersionedParams(&corev1.PodExecOptions{
				Container: "loader",
				Command:   []string{"tar", "xzf", "-", "-C", "/workspace"},
				Stdin:     true,
				Stdout:    true,
				Stderr:    true,
			}, runtime.NewParameterCodec(execScheme)).URL()

		By("Creating SPDY executor (remotecommand.NewSPDYExecutor)")
		executor, err := remotecommand.NewSPDYExecutor(restCfg, "POST", execURL)
		Expect(err).NotTo(HaveOccurred(), "create SPDY executor")

		By("Streaming tgz into pod stdin via SPDY StreamWithContext")
		var execStdout, execStderr bytes.Buffer
		err = executor.StreamWithContext(smokeCtx, remotecommand.StreamOptions{
			Stdin:  bytes.NewReader(tgzBytes),
			Stdout: &execStdout,
			Stderr: &execStderr,
		})
		// A1/A2 proof: StreamWithContext succeeds → URL, verb, and codec are correct
		// against the real apiserver. tar exits 0 only when the tgz was valid.
		Expect(err).NotTo(HaveOccurred(),
			"SPDY StreamWithContext must succeed (A1/A2 proven): stdout=%q stderr=%q",
			execStdout.String(), execStderr.String())

		// The pod stays Running (sleep); the exec'd tar already unpacked the tgz
		// into /workspace. Prove the file landed by a SECOND exec (`cat
		// /workspace/marker.txt`) into the SAME pod — this reads back the exact
		// emptyDir the first exec wrote to, which a separate pod could not see.
		By("Reading marker.txt back via a second SPDY exec (cat) into the same pod")
		catURL := cs.CoreV1().RESTClient().Post().
			Resource("pods").Name(podName).Namespace(loaderSmokeNS).
			SubResource("exec").
			VersionedParams(&corev1.PodExecOptions{
				Container: "loader",
				Command:   []string{"cat", "/workspace/marker.txt"},
				Stdout:    true,
				Stderr:    true,
			}, runtime.NewParameterCodec(execScheme)).URL()

		catExec, err := remotecommand.NewSPDYExecutor(restCfg, "POST", catURL)
		Expect(err).NotTo(HaveOccurred(), "create cat SPDY executor")

		var catOut, catErr bytes.Buffer
		Expect(catExec.StreamWithContext(smokeCtx, remotecommand.StreamOptions{
			Stdout: &catOut,
			Stderr: &catErr,
		})).To(Succeed(), "cat marker.txt via exec: stderr=%q", catErr.String())
		Expect(catOut.String()).To(Equal(loaderSmokeMarker),
			"unpacked /workspace/marker.txt must contain the exact marker the tgz carried (A1/A2 + unpack proof)")
	})
})

// buildLoaderSmokeMarkerTgz builds an in-memory .tgz containing marker.txt.
// The tgz root is workspace content directly (no prefix) matching the
// production pvc-envelopes.tgz contract: `tar xzf - -C /workspace` unpacks
// marker.txt at /workspace/marker.txt.
func buildLoaderSmokeMarkerTgz() []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	data := []byte(loaderSmokeMarker)
	hdr := &tar.Header{
		Name:     "marker.txt",
		Mode:     0o644,
		Size:     int64(len(data)),
		Typeflag: tar.TypeReg,
	}
	ExpectWithOffset(1, tw.WriteHeader(hdr)).To(Succeed(), "write tar header")
	_, err := tw.Write(data)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "write tar content")
	ExpectWithOffset(1, tw.Close()).To(Succeed(), "close tar writer")
	ExpectWithOffset(1, gz.Close()).To(Succeed(), "close gzip writer")
	return buf.Bytes()
}
