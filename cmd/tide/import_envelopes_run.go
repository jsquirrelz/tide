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

// import_envelopes_run.go — live + dry-run implementation for `tide import-envelopes`.
//
// Architecture:
//   - importEnvelopesDryRun: offline branch (no cluster, no pods, D-07).
//     Extracts the bundle, calls pkg/bundle.ValidateBundle, renders a table or
//     JSON. Hard-rejects on cycle (D-09).
//   - importEnvelopesRun: live stage-only branch (D-05).
//     Extracts the bundle, reads project.yaml for the seedConfigMapName +
//     pvcSubPath, creates the seed ConfigMap, delegates to loaderPodRunner for
//     the SPDY exec, surfaces project.yaml + the apply next-step. Never applies
//     the Project.
//   - loaderPodRunner (func var): SPDY exec seam. Function var so tests can
//     inject a fake without a live apiserver (mirrors inspectorPodRunner seam).
//   - defaultLoaderPodRunner: production implementation — busybox:1.36 RW PVC
//     mount, tar xzf - -C /workspace, NewSPDYExecutor StreamWithContext.
//
// D-05: stage-only — never creates/applies the Project CR.
// D-06: loader pod inverts the inspector pod (write vs read, RW vs RO mount,
//       SPDY exec vs GetLogs, SubPath=<oldUID>/workspace vs <projectUID>).
// D-07: dry-run is offline — no K8s client constructed in the dry-run path.
// D-08: table output level|name|verdict|reason + summary; --output json for
//       machine-readable report.
// D-09: cycle hard-rejects entire import.
// T-29-03-02: loader pod SubPath=<oldUID>/workspace (RW confined to that subtree).
// T-15-09: deferred Delete with context.Background() on every exit path.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	pkgbundle "github.com/jsquirrelz/tide/pkg/bundle"
	dag "github.com/jsquirrelz/tide/pkg/dag"
)

// loaderPodRunner creates the loader pod, waits for Running, streams the
// pvc-envelopes.tgz file into the pod's stdin via SPDY exec, and defers
// deletion. Function var so tests can inject a fake without a live apiserver.
//
// Signature: (ctx, cs, ns, oldUID, pvcName, pvcTgzPath string, errOut io.Writer) error
//   - pvcTgzPath: local path to the pvc-envelopes.tgz file to stream in.
//   - oldUID: the old project UID — used as the PVC SubPath prefix.
var loaderPodRunner = defaultLoaderPodRunner

// importEnvelopesDryRun validates the bundle at bundlePath offline (D-07).
// No K8s client or pods. Renders a table (default) or JSON (outputFormat=="json").
// Returns a non-nil error only on hard cycle (D-09).
//
// stdout carries the report (machine-parseable for --output json, D-08/D-10).
// stderr carries progress/status messages.
func importEnvelopesDryRun(bundlePath, outputFormat string, out, errOut io.Writer) error {
	fmt.Fprintf(errOut, "validating bundle offline: %s\n", bundlePath)

	// Unpack tgz or use directory directly (Pitfall 6 / D-02).
	bundleDir, cleanFn, err := pkgbundle.OpenBundleDir(bundlePath)
	if err != nil {
		return fmt.Errorf("open bundle: %w", err)
	}
	defer cleanFn()

	result, err := pkgbundle.ValidateBundle(bundleDir)
	if err != nil {
		return fmt.Errorf("validate bundle: %w", err)
	}

	// D-09: cycle hard-rejects the entire import — print and return an error.
	if result.CycleRejected {
		// Print cycle signal to stdout (machine-parseable, D-10).
		fmt.Fprintf(out, "CYCLE DETECTED — import would fail\n")
		// Extract InvolvedNodes from the CycleError for operator diagnosis.
		var cycleErr *dag.CycleError
		if errors.As(result.CycleError, &cycleErr) {
			fmt.Fprintf(out, "Involved nodes: %v\n", cycleErr.InvolvedNodes)
		} else {
			fmt.Fprintf(out, "Cycle error: %v\n", result.CycleError)
		}
		return fmt.Errorf("cyclic DAG: %w", result.CycleError)
	}

	// Render output.
	if strings.EqualFold(outputFormat, "json") {
		return renderDryRunJSON(result, out)
	}
	return renderDryRunTable(result, out)
}

// renderDryRunTable writes the adopt/re-plan table + summary to out.
func renderDryRunTable(result *pkgbundle.ValidationResult, out io.Writer) error {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "level\tname\tverdict\treason\n")
	fmt.Fprintf(tw, "-----\t----\t-------\t------\n")
	for _, row := range result.Rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", row.Level, row.Name, row.Verdict, row.Reason)
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("flush table: %w", err)
	}
	fmt.Fprintf(out, "\nSummary: %d adopt, %d re-plan (total %d)\n",
		result.AdoptCount(), result.RePlanCount(), len(result.Rows))
	return nil
}

// dryRunJSONReport is the JSON-serializable form of a ValidationResult.
type dryRunJSONReport struct {
	CycleRejected bool            `json:"cycleRejected"`
	AdoptCount    int             `json:"adoptCount"`
	RePlanCount   int             `json:"rePlanCount"`
	Rows          []dryRunJSONRow `json:"rows"`
}

type dryRunJSONRow struct {
	Level   string `json:"level"`
	Name    string `json:"name"`
	FQName  string `json:"fqName"`
	Verdict string `json:"verdict"`
	Reason  string `json:"reason,omitempty"`
}

// renderDryRunJSON writes the validation result as a JSON document to out.
func renderDryRunJSON(result *pkgbundle.ValidationResult, out io.Writer) error {
	report := dryRunJSONReport{
		CycleRejected: result.CycleRejected,
		AdoptCount:    result.AdoptCount(),
		RePlanCount:   result.RePlanCount(),
	}
	for _, row := range result.Rows {
		report.Rows = append(report.Rows, dryRunJSONRow{
			Level:   row.Level,
			Name:    row.Name,
			FQName:  row.FQName,
			Verdict: row.Verdict,
			Reason:  row.Reason,
		})
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

// importEnvelopesRun is the live stage-only path for `tide import-envelopes`.
// It extracts the bundle, reads project.yaml to learn seedConfigMapName and
// pvcSubPath, creates the seed ConfigMap (idempotent), delegates to
// loaderPodRunner for the PVC write, writes project.yaml to disk, and prints
// the follow-up `tide apply` command.
//
// D-05: NEVER applies the Project. The operator runs `tide apply project.yaml`.
// restCfg may be nil in unit tests (loaderPodRunner is faked).
func importEnvelopesRun(
	ctx context.Context,
	k client.Client,
	cs kubernetes.Interface,
	restCfg *rest.Config,
	bundlePath string,
	namespace, pvcName string,
	out, errOut io.Writer,
) error {
	fmt.Fprintf(errOut, "importing bundle (live mode): %s\n", bundlePath)

	// 1. Unpack the bundle.
	bundleDir, cleanFn, err := pkgbundle.OpenBundleDir(bundlePath)
	if err != nil {
		return fmt.Errorf("open bundle: %w", err)
	}
	defer cleanFn()

	// 2. Read project.yaml from the bundle to learn seedConfigMapName + pvcSubPath.
	projectYAMLPath := filepath.Join(bundleDir, pkgbundle.BundleFileProject)
	projectYAMLBytes, err := os.ReadFile(projectYAMLPath)
	if err != nil {
		return fmt.Errorf("read project.yaml from bundle: %w", err)
	}

	seedCMName, oldUID, err := parseSeedInfoFromProjectYAML(projectYAMLBytes)
	if err != nil {
		return fmt.Errorf("parse project.yaml importSource: %w", err)
	}

	// Determine the target namespace: flag > project.yaml metadata.namespace > error.
	ns := namespace
	if ns == "" {
		// Try to read it from project.yaml.
		ns, err = parseNamespaceFromProjectYAML(projectYAMLBytes)
		if err != nil || ns == "" {
			return fmt.Errorf("--namespace is required when project.yaml has no namespace metadata")
		}
	}

	// 3. Read seed-manifest.json.
	manifestPath := filepath.Join(bundleDir, pkgbundle.BundleFileSeedManifest)
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read seed-manifest.json: %w", err)
	}

	// 4. Create the seed ConfigMap (idempotent — AlreadyExists swallowed, D-05).
	fmt.Fprintf(errOut, "creating seed ConfigMap %s/%s...\n", ns, seedCMName)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      seedCMName,
			Namespace: ns,
		},
		Data: map[string]string{
			"manifest": string(manifestBytes),
		},
	}
	if _, err := cs.CoreV1().ConfigMaps(ns).Create(ctx, cm, metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("create seed ConfigMap: %w", err)
		}
		fmt.Fprintf(errOut, "seed ConfigMap already exists (idempotent re-run)\n")
	}

	// 5. Delegate to loaderPodRunner to stream pvc-envelopes.tgz into the PVC.
	pvcTgzPath := filepath.Join(bundleDir, pkgbundle.BundleFilePVCEnvelopes)
	fmt.Fprintf(errOut, "staging envelopes onto PVC via loader pod (SPDY exec)...\n")
	if err := loaderPodRunner(ctx, cs, ns, oldUID, pvcName, pvcTgzPath, errOut); err != nil {
		return fmt.Errorf("loader pod exec: %w", err)
	}

	// 6. Write project.yaml to the current directory (D-05: operator applies it).
	outYAMLPath := "project.yaml"
	if err := os.WriteFile(outYAMLPath, projectYAMLBytes, 0o644); err != nil {
		return fmt.Errorf("write project.yaml: %w", err)
	}

	fmt.Fprintf(errOut, "\nBundle staged successfully.\n")
	fmt.Fprintf(errOut, "Run the following to trigger adoption:\n")
	fmt.Fprintf(errOut, "  tide apply project.yaml\n")
	return nil
}

// parseSeedInfoFromProjectYAML extracts the seedManifestConfigMap name and
// the oldProjectUID from the salvagedPVCSubPath in the project.yaml importSource.
// pvcSubPath format: "<oldProjectUID>/workspace".
func parseSeedInfoFromProjectYAML(yamlBytes []byte) (seedCMName, oldUID string, err error) {
	// Decode into a minimal untyped struct to avoid importing internal/controller.
	var proj struct {
		Metadata struct {
			Namespace string `json:"namespace" yaml:"namespace"`
		} `json:"metadata" yaml:"metadata"`
		Spec struct {
			ImportSource *struct {
				SeedManifestConfigMap string `json:"seedManifestConfigMap" yaml:"seedManifestConfigMap"`
				SalvagedPVCSubPath    string `json:"salvagedPVCSubPath" yaml:"salvagedPVCSubPath"`
			} `json:"importSource" yaml:"importSource"`
		} `json:"spec" yaml:"spec"`
	}
	if err := yaml.Unmarshal(yamlBytes, &proj); err != nil {
		return "", "", fmt.Errorf("unmarshal project.yaml: %w", err)
	}

	if proj.Spec.ImportSource == nil {
		return "", "", fmt.Errorf("project.yaml missing spec.importSource")
	}

	seedCMName = proj.Spec.ImportSource.SeedManifestConfigMap
	subPath := proj.Spec.ImportSource.SalvagedPVCSubPath

	// pvcSubPath is "<oldUID>/workspace".
	parts := strings.SplitN(subPath, "/", 2)
	if len(parts) < 2 || parts[0] == "" {
		return "", "", fmt.Errorf("spec.importSource.salvagedPVCSubPath %q malformed (expected <uid>/workspace)", subPath)
	}
	oldUID = parts[0]
	return seedCMName, oldUID, nil
}

// parseNamespaceFromProjectYAML extracts the metadata.namespace field.
func parseNamespaceFromProjectYAML(yamlBytes []byte) (string, error) {
	var proj struct {
		Metadata struct {
			Namespace string `json:"namespace" yaml:"namespace"`
		} `json:"metadata" yaml:"metadata"`
	}
	if err := yaml.Unmarshal(yamlBytes, &proj); err != nil {
		return "", fmt.Errorf("unmarshal project.yaml for namespace: %w", err)
	}
	return proj.Metadata.Namespace, nil
}

// defaultLoaderPodRunner is the production SPDY exec implementation.
// Creates a busybox:1.36 loader pod with a RW PVC mount at SubPath=
// <oldUID>/workspace, waits for Running, then streams the tgz file into the
// pod's stdin via remotecommand SPDY exec (pods/exec: create). Defers pod
// deletion (T-15-09).
//
// D-06: inverted inspector pod — ReadOnly:false, SubPath=<oldUID>/workspace,
//
//	command `tar xzf - -C /workspace`, SPDY stdin stream instead of GetLogs.
//
// T-29-03-02: SubPath confines the RW mount to <oldUID>/workspace — no other
//
//	projects' data is reachable.
//
// T-29-03-05: RBAC pods/exec: create is documented here; callers must grant it.
func defaultLoaderPodRunner(
	ctx context.Context,
	cs kubernetes.Interface,
	ns, oldUID, pvcName, pvcTgzPath string,
	errOut io.Writer,
) error {
	// This function is accessed through the loaderPodRunner var — it needs the
	// restCfg for NewSPDYExecutor. We capture it via closure from importEnvelopesRun
	// by using a different approach: the func signature doesn't carry restCfg,
	// so we build it here from the package-level RESTConfig() (same as other commands).
	restCfg, err := RESTConfig()
	if err != nil {
		return fmt.Errorf("resolve REST config for SPDY executor: %w", err)
	}
	return execLoaderPod(ctx, cs, restCfg, ns, oldUID, pvcName, pvcTgzPath, errOut)
}

// execLoaderPod is the factored implementation shared by defaultLoaderPodRunner
// and the kind smoke test. It builds the exec URL, creates the pod, waits for
// Running, and streams the tgz via SPDY.
//
// Exported as a package-internal function so the smoke test can call it with a
// test-provided restCfg (Task 3 requirement: "mirror Task 2's exact URL/exec
// construction so the smoke and production paths cannot diverge").
func execLoaderPod(
	ctx context.Context,
	cs kubernetes.Interface,
	restCfg *rest.Config,
	ns, oldUID, pvcName, pvcTgzPath string,
	errOut io.Writer,
) error {
	podName := fmt.Sprintf("tide-loader-%s", randSuffix(8))

	// T-29-03-02: SubPath=<oldUID>/workspace — RW mount confined to that subtree.
	// T-29-03-01: pod command is a fixed string (no user-controlled interpolation).
	podSpec := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: ns,
			Labels: map[string]string{
				"app.kubernetes.io/component":  "tide-loader",
				"app.kubernetes.io/managed-by": "tide-cli",
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:  "loader",
					Image: "busybox:1.36",
					// T-29-03-01: fixed tar command — no user-controlled path interpolation.
					Command: []string{"tar", "xzf", "-", "-C", "/workspace"},
					// Stdin=true required for tar to read from stdin (SPDY exec).
					Stdin: true,
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "workspace",
							MountPath: "/workspace",
							// T-29-03-02: SubPath=<oldUID>/workspace confines the RW
							// mount to this project's old workspace subtree only.
							SubPath:  oldUID + "/workspace",
							ReadOnly: false,
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
							ReadOnly:  false,
						},
					},
				},
			},
		},
	}

	fmt.Fprintf(errOut, "creating loader pod %s/%s...\n", ns, podName)
	if _, err := cs.CoreV1().Pods(ns).Create(ctx, podSpec, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("create loader pod: %w", err)
	}
	// T-15-09: defer Delete with context.Background() so cleanup fires on
	// every exit path — timeout, stream error, or success.
	defer func() {
		_ = cs.CoreV1().Pods(ns).Delete(context.Background(), podName, metav1.DeleteOptions{})
	}()

	fmt.Fprintf(errOut, "waiting for loader pod to start...\n")
	if err := waitForPodRunning(ctx, cs, ns, podName); err != nil {
		return err
	}

	// Build SPDY exec URL (A1/A2 from RESEARCH Q1 RESOLVED).
	// VersionedParams encodes PodExecOptions into the query string (stdin/command params).
	// The URL form is: /api/v1/namespaces/<ns>/pods/<name>/exec?...
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("build exec scheme: %w", err)
	}
	execURL := cs.CoreV1().RESTClient().Post().
		Resource("pods").Name(podName).Namespace(ns).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: "loader",
			Command:   []string{"tar", "xzf", "-", "-C", "/workspace"},
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
		}, runtime.NewParameterCodec(scheme)).URL()

	fmt.Fprintf(errOut, "streaming pvc-envelopes.tgz into loader pod via SPDY exec...\n")

	// Open the pvc-envelopes.tgz for streaming.
	tgzFile, err := os.Open(pvcTgzPath)
	if err != nil {
		return fmt.Errorf("open pvc-envelopes.tgz: %w", err)
	}
	defer tgzFile.Close()

	// NewSPDYExecutor uses "POST" (same as kubectl exec/cp, A2 resolved).
	executor, err := remotecommand.NewSPDYExecutor(restCfg, "POST", execURL)
	if err != nil {
		return fmt.Errorf("create SPDY executor: %w", err)
	}

	if err := executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  tgzFile,
		Stdout: errOut,
		Stderr: errOut,
	}); err != nil {
		return fmt.Errorf("stream tgz to loader pod: %w", err)
	}

	fmt.Fprintf(errOut, "loader pod stream complete\n")
	return nil
}
