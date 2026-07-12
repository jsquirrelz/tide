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

// export_envelopes_run.go — real inspector-pod + seed-manifest implementation
// for `tide export-envelopes`.
//
// Architecture:
//   - exportEnvelopesRun: testable seam. Resolves Project UID, delegates to
//     exportInspectorPodRunner for the PVC tgz read, then assembles the bundle.
//   - exportInspectorPodRunner (func var): creates the inspector Pod, waits for
//     readiness, streams log bytes (the pvc-envelopes tgz) to caller-provided
//     writer, defers deletion. Function var so tests can inject a fake without a
//     live apiserver.
//   - defaultExportInspectorPodRunner: production implementation.
//
// D-01: reuses artifact_get_run.go inspector-pod pattern (busybox:1.36, ReadOnly mount,
//       GetLogs stream, deferred Delete with context.Background()).
// D-03: seed manifest built from live Milestone/Phase/Plan CRs at export time.
// D-04: per-envelope sha256 written into the BundleEntry.
// D-13: Wave CRs are never included (BundleManifest only Milestone/Phase/Plan).
// D-15: seed covers down to Plan only — no Task CRs.
// D-16a: childCount stamped on legacy out.json before bundling.
// T-29-02-01: PVC mounted ReadOnly with SubPath=projectUID.
// T-29-02-03: tar command is a fixed string with no user-controlled path interpolated.
// T-15-09: deferred Delete with context.Background() covers all exit paths.

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	pkgbundle "github.com/jsquirrelz/tide/pkg/bundle"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// exportInspectorPodRunner creates and streams the export inspector pod.
// Function var so tests can inject without a live apiserver (mirrors
// artifact_get_run.go's inspectorPodRunner seam).
var exportInspectorPodRunner = defaultExportInspectorPodRunner

// exportEnvelopesRun is the testable seam for `tide export-envelopes`.
// It resolves the Project UID, delegates to exportInspectorPodRunner to stream
// the pvc-envelopes.tgz bytes, then queries live Milestone/Phase/Plan CRs to
// assemble the seed manifest and full bundle.
func exportEnvelopesRun(
	ctx context.Context,
	k client.Client,
	cs kubernetes.Interface,
	ref, pvcName, outputPath string,
	outputDir bool,
	errOut io.Writer,
) error {
	// Parse <namespace>/<project> from the ref.
	ns, projName, err := parseExportRef(ref)
	if err != nil {
		return err
	}

	// Resolve Project to get its UID — the PVC subPath is the project UID.
	var proj tidev1alpha3.Project
	if err := k.Get(ctx, types.NamespacedName{Namespace: ns, Name: projName}, &proj); err != nil {
		return fmt.Errorf("get project %s/%s: %w", ns, projName, err)
	}
	projectUID := string(proj.UID)
	if projectUID == "" {
		return fmt.Errorf("project %s/%s has no UID — cannot resolve PVC subPath", ns, projName)
	}

	// Stream the pvc-envelopes.tgz from the inspector pod into a buffer.
	fmt.Fprintf(errOut, "exporting envelopes for project %s/%s (UID %s)...\n", ns, projName, projectUID)
	var pvcTgzBuf bytes.Buffer
	if err := exportInspectorPodRunner(ctx, cs, ns, projectUID, pvcName, &pvcTgzBuf, errOut); err != nil {
		return fmt.Errorf("export inspector pod: %w", err)
	}

	// Walk the pvc-envelopes.tgz: stamp childCount repairs and collect envelope map.
	// envelopes maps uid → repaired out.json bytes.
	// repairedTgzFiles holds the full tgz content with repairs applied.
	// preStampComplete maps uid → completeness verdict on PRE-stamp bytes (WR-03).
	fmt.Fprintf(errOut, "processing envelopes...\n")
	envelopes, repairedTgzFiles, preStampComplete, err := processEnvelopesTgz(pvcTgzBuf.Bytes(), errOut)
	if err != nil {
		return fmt.Errorf("process pvc-envelopes.tgz: %w", err)
	}

	// Query live CRs to build the seed manifest.
	fmt.Fprintf(errOut, "building seed manifest from live CRs...\n")
	manifest, err := buildSeedManifest(ctx, k, ns, projName, envelopes, preStampComplete)
	if err != nil {
		return fmt.Errorf("build seed manifest: %w", err)
	}

	// Assemble the bundle files.
	bundleFiles, err := assembleBundleFiles(ctx, k, ns, projName, projectUID, manifest, repairedTgzFiles)
	if err != nil {
		return fmt.Errorf("assemble bundle files: %w", err)
	}

	// Emit output.
	if outputDir {
		if err := emitBundleDir(outputPath, bundleFiles); err != nil {
			return fmt.Errorf("emit bundle dir: %w", err)
		}
		fmt.Fprintf(errOut, "bundle emitted to directory: %s\n", outputPath)
	} else {
		if err := pkgbundle.WriteBundle(outputPath, bundleFiles); err != nil {
			return fmt.Errorf("write bundle tgz: %w", err)
		}
		fmt.Fprintf(errOut, "bundle written: %s\n", outputPath)
	}
	return nil
}

// defaultExportInspectorPodRunner is the production implementation.
// Creates a short-lived busybox inspector Pod mounting the per-project PVC
// subPath ReadOnly, runs `tar czf - -C /workspace envelopes/ artifacts/` to
// stream the envelope tree, delivers bytes to out, defers deletion (T-15-09).
//
// T-29-02-01: ReadOnly PVC mount with SubPath=projectUID confines pod to one project.
// T-29-02-03: tar command is a fixed string (no user-controlled interpolation).
// buildExportInspectorPodSpec constructs the short-lived busybox inspector Pod
// that tars the project's envelope subtree out of the per-project PVC.
//
// GAP-13: the per-project PVC layout is <PVC>/<UID>/workspace/{envelopes,
// artifacts,repo}, so the mount SubPath MUST be "<UID>/workspace" — matching the
// init Job, import Job, loader pod, and reporter. Mounting only "<UID>" left
// envelopes/ at /workspace/workspace/envelopes, so `tar -C /workspace envelopes/`
// found nothing and the pod exited 1 ("failed before streaming"). The "/workspace"
// suffix also confines the pod TIGHTER than before (T-29-02-01) — repo/ is no
// longer reachable. The tar paths stay relative (envelopes/, artifacts/) so the
// emitted pvc-envelopes.tgz keeps the top-level layout the import loader expects.
func buildExportInspectorPodSpec(podName, ns, projectUID, pvcName string) *corev1.Pod {
	return &corev1.Pod{
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
					Name:  "inspector",
					Image: "busybox:1.36",
					// T-29-02-03: fixed tar command, no user-controlled path.
					Command: []string{"tar", "czf", "-", "-C", "/workspace", "envelopes/", "artifacts/"},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "workspace",
							MountPath: "/workspace",
							SubPath:   fmt.Sprintf("%s/workspace", projectUID),
							ReadOnly:  true,
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
}

func defaultExportInspectorPodRunner(
	ctx context.Context,
	cs kubernetes.Interface,
	ns, projectUID, pvcName string,
	out, errOut io.Writer,
) error {
	podName := fmt.Sprintf("tide-export-%s", randSuffix(8))
	podSpec := buildExportInspectorPodSpec(podName, ns, projectUID, pvcName)

	fmt.Fprintf(errOut, "creating export inspector pod %s/%s...\n", ns, podName)
	if _, err := cs.CoreV1().Pods(ns).Create(ctx, podSpec, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("create export inspector pod: %w", err)
	}
	// T-15-09: defer Delete with context.Background() so cleanup fires on
	// every exit path — timeout, stream error, or success.
	defer func() {
		_ = cs.CoreV1().Pods(ns).Delete(context.Background(), podName, metav1.DeleteOptions{})
	}()

	fmt.Fprintf(errOut, "waiting for export inspector pod to start...\n")
	if err := waitForPodRunning(ctx, cs, ns, podName); err != nil {
		return err
	}

	fmt.Fprintf(errOut, "streaming envelope tgz...\n")
	req := cs.CoreV1().Pods(ns).GetLogs(podName, &corev1.PodLogOptions{
		Follow:    true,
		Container: "inspector",
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		return fmt.Errorf("open log stream for export inspector pod %s: %w", podName, err)
	}
	defer func() { _ = stream.Close() }()

	// Ctx-cancel watcher — closes the stream so io.Copy returns on timeout.
	go func() {
		<-ctx.Done()
		_ = stream.Close()
	}()

	// D-10 variant: tgz bytes go to out (not stdout directly); status → errOut.
	if _, err := io.Copy(out, stream); err != nil && ctx.Err() == nil {
		return fmt.Errorf("read envelope stream: %w", err)
	}
	if ctx.Err() != nil {
		return fmt.Errorf("export timed out before envelopes were fully streamed")
	}
	return nil
}

// parseExportRef splits "<namespace>/<project>" into components.
func parseExportRef(ref string) (ns, projName string, err error) {
	parts := strings.SplitN(ref, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("ref %q malformed (expected <namespace>/<project>)", ref)
	}
	return parts[0], parts[1], nil
}

// processEnvelopesTgz reads the pvc-envelopes.tgz bytes, stamps childCount on
// legacy out.json entries (D-16a), and returns:
//   - envelopes: map of uid → repaired out.json bytes (stamped for bundle)
//   - repairedFiles: full tgz content map for WritePVCEnvelopesTgz re-assembly
//   - preStampComplete: map of uid → completeness verdict evaluated on PRE-STAMP
//     bytes (WR-03). This is the stricter verdict: StampChildCount (D-16a) can only
//     upgrade a legacy ChildCount==0+len(ChildCRDs)>0 envelope to look complete,
//     never the reverse. seedStatusFor uses it so an under-stamped legacy envelope
//     is materialized re-plannable rather than adopted as Succeeded ("on doubt,
//     re-plan"). NOTE: the bytes tide-import actually reads are the STAMPED bundle
//     bytes — export stages the repaired tgz at <oldUID>/workspace, the same subPath
//     tide-import mounts — so tide-import's own IsEnvelopeComplete sees the post-stamp
//     verdict and may still copy such an envelope's children even when the seed
//     manifest re-plans the node. That is a known latent inconsistency for legacy /
//     hand-rolled bundles only (audit v1.0.3 finding F1; zero occurrences on the
//     stamped salvage fixture); the stricter pre-stamp basis keeps the materialized
//     node status conservative regardless.
func processEnvelopesTgz(tgzData []byte, errOut io.Writer) (
	envelopes map[string][]byte,
	repairedFiles map[string][]byte,
	preStampComplete map[string]bool,
	err error,
) {
	envelopes = make(map[string][]byte)
	repairedFiles = make(map[string][]byte)
	preStampComplete = make(map[string]bool)

	gr, err := gzip.NewReader(bytes.NewReader(tgzData))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open pvc-envelopes.tgz gzip reader: %w", err)
	}
	defer func() { _ = gr.Close() }()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, nil, fmt.Errorf("read pvc-envelopes.tgz entry: %w", err)
		}

		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("read tgz entry %s: %w", hdr.Name, err)
		}

		// Check if this is an "envelopes/<uid>/out.json" entry.
		if uid, ok := parseOutJSONPath(hdr.Name); ok {
			// WR-03: evaluate completeness on pre-stamp bytes — the stricter verdict.
			// StampChildCount (D-16a) below may repair ChildCount==0+len(ChildCRDs)>0
			// to look complete, so a legacy envelope that fails IsEnvelopeComplete on
			// raw bytes is kept re-plannable in the seed manifest even though it passes
			// after stamping (conservative: on doubt, re-plan). See processEnvelopesTgz
			// doc for the F1 caveat re: tide-import reading the post-stamp bundle bytes.
			var rawEnv pkgdispatch.EnvelopeOut
			if jsonErr := json.Unmarshal(data, &rawEnv); jsonErr == nil {
				preStampComplete[uid] = pkgdispatch.IsEnvelopeComplete(rawEnv)
			}
			// preStampComplete[uid] remains false (zero value) for parse failures —
			// consistent with seedStatusFor's corrupt-bytes fail-closed rule.

			// D-16a: stamp childCount if legacy, so the bundle's pvc-envelopes.tgz
			// carries corrected bytes. This does not affect the pre-stamp verdict above.
			repaired, err := pkgbundle.StampChildCount(data, errOut)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("stampChildCount for %s: %w", hdr.Name, err)
			}
			envelopes[uid] = repaired
			repairedFiles[hdr.Name] = repaired
		} else {
			// Non-out.json entries pass through unchanged.
			repairedFiles[hdr.Name] = data
		}
	}
	return envelopes, repairedFiles, preStampComplete, nil
}

// parseOutJSONPath extracts the uid from "envelopes/<uid>/out.json".
// Returns (uid, true) on match, ("", false) otherwise.
func parseOutJSONPath(name string) (string, bool) {
	const prefix = "envelopes/"
	const suffix = "/out.json"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return "", false
	}
	mid := name[len(prefix) : len(name)-len(suffix)]
	if mid == "" || strings.Contains(mid, "/") {
		return "", false
	}
	return mid, true
}

// seedStatusFor returns the status string to stamp on a BundleEntry for the
// given node UID. A node whose envelope is present AND complete keeps its live
// status; all other cases return "" so the ImportController's existing
// `if seed.Status != ""` guard leaves the CR in a fresh/re-plannable state
// (RESUME-PARTIAL-01 fix, Plan 30-01).
//
// Cases that return "":
//   - UID has no entry in envelopes (missing out.json — no envelope to evaluate).
//   - UID has an entry but preStampComplete[uid] is false (WR-03: completeness
//     evaluated on the stricter pre-stamp bytes — see processEnvelopesTgz for the
//     rationale and the F1 caveat).
//
// preStampComplete maps uid → IsEnvelopeComplete result on raw (pre-stamp) bytes.
// envelopes maps uid → repaired (stamped) bytes; its presence indicates the uid
// had an out.json entry (used only to distinguish "missing" from "incomplete").
//
// SHA256 stamping is NOT affected by this function — callers still compute SHA256
// for any present envelope bytes regardless of completeness.
func seedStatusFor(
	uid string,
	liveStatus string,
	envelopes map[string][]byte,
	preStampComplete map[string]bool,
) string {
	if _, ok := envelopes[uid]; !ok {
		// No envelope present for this node → re-plannable.
		return ""
	}
	// WR-03: use the stricter pre-stamp verdict for the materialized status.
	// A legacy exit-0 envelope with ChildCount==0 and populated ChildCRDs looks
	// complete after StampChildCount but incomplete on the raw bytes; keeping the
	// seed-manifest status on the raw verdict materializes such a node re-plannable
	// rather than adopting a Succeeded status whose completeness is ambiguous.
	// (tide-import reads the post-stamp bundle bytes and may still copy this node's
	// children — audit finding F1, latent for legacy bundles; harmless here.)
	if !preStampComplete[uid] {
		// Incomplete on raw bytes → re-plannable.
		return ""
	}
	return liveStatus
}

// buildSeedManifest lists live Milestone/Phase/Plan CRs in the namespace and
// builds the BundleManifest with FQName, OldUID, DependsOn, Status, and sha256
// from the provided envelopes map.
//
// D-03: manifest built from live CRs at export time (UIDs are authoritative here).
// D-04: sha256 computed from repaired out.json bytes.
// D-13: Wave CRs excluded.
// D-15: down to Plan only; Tasks excluded.
//
// preStampComplete carries the IsEnvelopeComplete verdict evaluated on PRE-stamp
// bytes (WR-03) — the stricter basis. Passed through to seedStatusFor so the seed
// manifest's Status field keeps an under-stamped legacy node re-plannable rather
// than adopting an ambiguous Succeeded status (see processEnvelopesTgz F1 caveat).
func buildSeedManifest(
	ctx context.Context,
	k client.Client,
	ns, projName string,
	envelopes map[string][]byte, // uid → repaired out.json bytes
	preStampComplete map[string]bool, // uid → completeness on pre-stamp bytes (WR-03)
) (*pkgbundle.BundleManifest, error) {
	manifest := &pkgbundle.BundleManifest{}

	// --- Milestones ---
	var msList tidev1alpha3.MilestoneList
	if err := k.List(ctx, &msList, client.InNamespace(ns)); err != nil {
		return nil, fmt.Errorf("list milestones: %w", err)
	}
	for _, ms := range msList.Items {
		// Only include milestones owned by this project.
		if ms.Spec.ProjectRef != projName {
			continue
		}
		entry := pkgbundle.BundleEntry{
			Name:       ms.Name,
			FQName:     pkgbundle.MilestoneFQName(ms.Name),
			OldUID:     string(ms.UID),
			DependsOn:  ms.Spec.DependsOn,
			Status:     seedStatusFor(string(ms.UID), ms.Status.Phase, envelopes, preStampComplete),
			ProjectRef: ms.Spec.ProjectRef,
		}
		if data, ok := envelopes[string(ms.UID)]; ok {
			entry.SHA256 = pkgbundle.ComputeEnvelopeSHA256(data)
		}
		manifest.Milestones = append(manifest.Milestones, entry)
	}

	// Build a phase→milestoneRef lookup for Plan FQName construction.
	// We need to know which Milestone owns each Phase.
	phaseToMilestone := make(map[string]string) // phase.Name → milestone.Name

	// --- Phases ---
	var phList tidev1alpha3.PhaseList
	if err := k.List(ctx, &phList, client.InNamespace(ns)); err != nil {
		return nil, fmt.Errorf("list phases: %w", err)
	}
	for _, ph := range phList.Items {
		// Only include phases whose owning milestone belongs to this project.
		// Check by looking up whether the MilestoneRef is in our milestones set.
		msRef := ph.Spec.MilestoneRef
		inProject := false
		for _, ms := range msList.Items {
			if ms.Name == msRef && ms.Spec.ProjectRef == projName {
				inProject = true
				break
			}
		}
		if !inProject {
			continue
		}
		phaseToMilestone[ph.Name] = msRef

		entry := pkgbundle.BundleEntry{
			Name:         ph.Name,
			FQName:       pkgbundle.PhaseFQName(msRef, ph.Name),
			OldUID:       string(ph.UID),
			DependsOn:    ph.Spec.DependsOn,
			Status:       seedStatusFor(string(ph.UID), ph.Status.Phase, envelopes, preStampComplete),
			MilestoneRef: msRef,
		}
		if data, ok := envelopes[string(ph.UID)]; ok {
			entry.SHA256 = pkgbundle.ComputeEnvelopeSHA256(data)
		}
		manifest.Phases = append(manifest.Phases, entry)
	}

	// --- Plans ---
	var plList tidev1alpha3.PlanList
	if err := k.List(ctx, &plList, client.InNamespace(ns)); err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	for _, pl := range plList.Items {
		phRef := pl.Spec.PhaseRef
		msRef, ok := phaseToMilestone[phRef]
		if !ok {
			// Phase is not in this project's scope; skip.
			continue
		}

		entry := pkgbundle.BundleEntry{
			Name:      pl.Name,
			FQName:    pkgbundle.PlanFQName(msRef, phRef, pl.Name),
			OldUID:    string(pl.UID),
			DependsOn: pl.Spec.DependsOn,
			Status:    seedStatusFor(string(pl.UID), pl.Status.Phase, envelopes, preStampComplete),
			PhaseRef:  phRef,
		}
		if data, ok := envelopes[string(pl.UID)]; ok {
			entry.SHA256 = pkgbundle.ComputeEnvelopeSHA256(data)
		}
		manifest.Plans = append(manifest.Plans, entry)
	}

	return manifest, nil
}

// assembleBundleFiles builds the seven-entry bundle file map for WriteBundle.
// Entries: project.yaml, milestones.yaml, phases.yaml, plans.yaml,
// seed-manifest.json, SEED-OUTLINE.md, pvc-envelopes.tgz.
func assembleBundleFiles(
	ctx context.Context,
	k client.Client,
	ns, projName, projectUID string,
	manifest *pkgbundle.BundleManifest,
	repairedPVCFiles map[string][]byte,
) (map[string][]byte, error) {
	files := make(map[string][]byte)

	// project.yaml — live Project spec with spec.importSource populated.
	var proj tidev1alpha3.Project
	if err := k.Get(ctx, types.NamespacedName{Namespace: ns, Name: projName}, &proj); err != nil {
		return nil, fmt.Errorf("get project for bundle: %w", err)
	}
	seedCMName := "tide-import-seed-" + projName
	proj.Spec.ImportSource = &tidev1alpha3.ImportSourceRef{
		SeedManifestConfigMap: seedCMName,
		SalvagedPVCSubPath:    projectUID + "/workspace",
	}
	// GAP-16: the controller-runtime typed client strips TypeMeta on Get, so the
	// fetched Project has empty apiVersion/kind. Re-stamp it — otherwise the
	// emitted project.yaml fails `kubectl apply` validation ("apiVersion not set,
	// kind not set") when the round-trip re-applies it.
	proj.TypeMeta = metav1.TypeMeta{
		APIVersion: tidev1alpha3.GroupVersion.String(),
		Kind:       "Project",
	}
	// Clear runtime fields so project.yaml is clean for re-apply.
	// GAP-17: also clear metadata.namespace — the bundle is namespace-portable
	// (the canonical fixture project.yaml is namespace-less), and `tide import`/
	// the round-trip apply target the destination namespace with `-n`. Leaving the
	// origin namespace baked in makes `kubectl apply -n <other>` fail with a
	// namespace-mismatch error.
	proj.Namespace = ""
	proj.ResourceVersion = ""
	proj.UID = ""
	proj.Generation = 0
	proj.Status = tidev1alpha3.ProjectStatus{}
	projYAML, err := yaml.Marshal(&proj)
	if err != nil {
		return nil, fmt.Errorf("marshal project.yaml: %w", err)
	}
	files[pkgbundle.BundleFileProject] = projYAML

	// milestones.yaml — list of live Milestone specs.
	var msList tidev1alpha3.MilestoneList
	if err := k.List(ctx, &msList, client.InNamespace(ns)); err != nil {
		return nil, fmt.Errorf("list milestones for bundle: %w", err)
	}
	filteredMS := filterMilestonesByProject(msList.Items, projName)
	msYAML, err := marshalCRList(filteredMS)
	if err != nil {
		return nil, fmt.Errorf("marshal milestones.yaml: %w", err)
	}
	files[pkgbundle.BundleFileMilestones] = msYAML

	// phases.yaml — list of live Phase specs.
	var phList tidev1alpha3.PhaseList
	if err := k.List(ctx, &phList, client.InNamespace(ns)); err != nil {
		return nil, fmt.Errorf("list phases for bundle: %w", err)
	}
	msNames := milestoneNameSet(filteredMS)
	filteredPH := filterPhasesByMilestones(phList.Items, msNames)
	phYAML, err := marshalCRList(filteredPH)
	if err != nil {
		return nil, fmt.Errorf("marshal phases.yaml: %w", err)
	}
	files[pkgbundle.BundleFilePhases] = phYAML

	// plans.yaml — list of live Plan specs.
	var plList tidev1alpha3.PlanList
	if err := k.List(ctx, &plList, client.InNamespace(ns)); err != nil {
		return nil, fmt.Errorf("list plans for bundle: %w", err)
	}
	phNames := phaseNameSet(filteredPH)
	filteredPL := filterPlansByPhases(plList.Items, phNames)
	plYAML, err := marshalCRList(filteredPL)
	if err != nil {
		return nil, fmt.Errorf("marshal plans.yaml: %w", err)
	}
	files[pkgbundle.BundleFilePlans] = plYAML

	// seed-manifest.json.
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("marshal seed-manifest.json: %w", err)
	}
	files[pkgbundle.BundleFileSeedManifest] = manifestJSON

	// SEED-OUTLINE.md — human-readable tree.
	files[pkgbundle.BundleFileSeedOutline] = buildSeedOutline(manifest)

	// pvc-envelopes.tgz — re-assembled with repairs.
	pvcTgz, err := pkgbundle.WritePVCEnvelopesTgz(repairedPVCFiles)
	if err != nil {
		return nil, fmt.Errorf("write pvc-envelopes.tgz: %w", err)
	}
	files[pkgbundle.BundleFilePVCEnvelopes] = pvcTgz

	return files, nil
}

// emitBundleDir writes bundle files into an unpacked directory (--dir mode).
func emitBundleDir(dirPath string, files map[string][]byte) error {
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dirPath, err)
	}
	for name, data := range files {
		destPath := dirPath + "/" + name
		if err := os.WriteFile(destPath, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", destPath, err)
		}
	}
	return nil
}

// buildSeedOutline generates a human-readable SEED-OUTLINE.md tree from the manifest.
func buildSeedOutline(manifest *pkgbundle.BundleManifest) []byte {
	var sb strings.Builder
	sb.WriteString("# SEED-OUTLINE.md\n\n")
	sb.WriteString("Human-readable tree of the exported bundle seed manifest.\n\n")
	for _, ms := range manifest.Milestones {
		sb.WriteString("- Milestone: " + ms.Name + " [" + ms.OldUID + "]\n")
	}
	for _, ph := range manifest.Phases {
		sb.WriteString("  - Phase: " + ph.Name + " [" + ph.OldUID + "]\n")
	}
	for _, pl := range manifest.Plans {
		sb.WriteString("    - Plan: " + pl.Name + " [" + pl.OldUID + "]\n")
	}
	return []byte(sb.String())
}

// -- helper filters ----------------------------------------------------------

func filterMilestonesByProject(items []tidev1alpha3.Milestone, projName string) []tidev1alpha3.Milestone {
	var out []tidev1alpha3.Milestone
	for _, ms := range items {
		if ms.Spec.ProjectRef == projName {
			out = append(out, ms)
		}
	}
	return out
}

func milestoneNameSet(items []tidev1alpha3.Milestone) map[string]bool {
	s := make(map[string]bool, len(items))
	for _, ms := range items {
		s[ms.Name] = true
	}
	return s
}

func filterPhasesByMilestones(items []tidev1alpha3.Phase, msNames map[string]bool) []tidev1alpha3.Phase {
	var out []tidev1alpha3.Phase
	for _, ph := range items {
		if msNames[ph.Spec.MilestoneRef] {
			out = append(out, ph)
		}
	}
	return out
}

func phaseNameSet(items []tidev1alpha3.Phase) map[string]bool {
	s := make(map[string]bool, len(items))
	for _, ph := range items {
		s[ph.Name] = true
	}
	return s
}

func filterPlansByPhases(items []tidev1alpha3.Plan, phNames map[string]bool) []tidev1alpha3.Plan {
	var out []tidev1alpha3.Plan
	for _, pl := range items {
		if phNames[pl.Spec.PhaseRef] {
			out = append(out, pl)
		}
	}
	return out
}

// marshalCRList marshals a slice of typed CRs into YAML list form.
// Each element is marshalled independently and concatenated with "---\n".
func marshalCRList[T any](items []T) ([]byte, error) {
	if len(items) == 0 {
		return []byte(""), nil
	}
	var buf bytes.Buffer
	for i, item := range items {
		b, err := yaml.Marshal(item)
		if err != nil {
			return nil, fmt.Errorf("marshal item %d: %w", i, err)
		}
		buf.WriteString("---\n")
		buf.Write(b)
	}
	return buf.Bytes(), nil
}
