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

// import_jobspec.go — builds the short-lived tide-import Job that copies and
// re-keys envelope trees from the old PVC subPath to the new PVC subPath
// (Phase 28, IMPORT-05 / D-05 / Pitfall 7 / Anti-Pattern 5).
//
// Build pattern mirrors BuildReporterJob (reporter_jobspec.go:121-224):
// deterministic-named, project-namespace-scoped, dual-subPath PVC mount,
// owner-referenced to the Project, hardened securityContext.
//
// Key differences from BuildReporterJob:
//  1. TWO PVC VolumeMounts instead of one: old subPath (ReadOnly) + new subPath (read-write).
//     Neither mount covers the PVC root — this enforces the IMPORT-05 namespace-scoped
//     containment invariant (no cross-project reads, no path-traversal escape).
//  2. Rekey ConfigMap mounted as a volume at /rekey; the binary reads the rekey
//     table directly from /rekey/rekey.json via its --rekey-file flag (the
//     distroless base has no shell, so there is no `cat … | tide-import` pipe).
//  3. No ServiceAccount token needed (no K8s API calls from the binary — pure filesystem I/O).
//  4. SA is "tide-import" (least-privilege; no K8s API calls required from the binary).
package controller

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/internal/owner"
)

const (
	// importContainerName is the single container name in the tide-import Job pod.
	importContainerName = "tide-import"

	// importWorkspaceVolume is the volume name for the per-project PVC.
	importWorkspaceVolume = "tide-projects"

	// importRekeyCMVolume is the volume name for the rekey ConfigMap.
	importRekeyCMVolume = "rekey-table"
)

// ImportJobOptions carries everything BuildImportJob needs to construct the Job.
type ImportJobOptions struct {
	// ImportImage is the image ref for the tide-import container.
	// PROD_OVERRIDE_REQUIRED: set via TIDE_IMPORT_IMAGE env from Helm value
	// images.tideImport.repository:tag.
	ImportImage string

	// SharedPVCName is the name of the cluster-wide PVC (default "tide-projects").
	SharedPVCName string

	// OldSubPath is the sub-path within the PVC where the salvaged envelopes reside,
	// e.g. "<oldProjectUID>/workspace". Mounted read-only at /old-workspace.
	OldSubPath string

	// NewSubPath is the sub-path within the PVC for the new project,
	// e.g. "<newProjectUID>/workspace". Mounted read-write at /new-workspace.
	NewSubPath string

	// RekeyCMName is the name of the ConfigMap carrying the rekey table JSON.
	// The ConfigMap key "rekey.json" mounts as the file /rekey/rekey.json; the
	// binary reads it directly via --rekey-file=/rekey/rekey.json.
	RekeyCMName string
}

// BuildImportJob constructs the K8s batchv1.Job that runs the in-namespace
// tide-import binary to copy and re-key envelope trees.
//
// Identity:
//   - Name: "tide-import-<project.UID>" — deterministic per Project; AlreadyExists
//     on Create is idempotent success (mirrors tide-reporter-<parentUID> in reporter_jobspec.go:169).
//   - Namespace: project.Namespace — same namespace as the PVC so the mount is reachable.
//   - OwnerReferences: cascade-delete to the Project via owner.EnsureOwnerRef.
//
// Pod spec:
//   - ServiceAccountName: "tide-import" (least-privilege; no K8s API calls).
//   - PVC mounts: two subPaths, both from the cluster-wide PVC:
//     /old-workspace subPath=OldSubPath  ReadOnly=true
//     /new-workspace subPath=NewSubPath  ReadOnly=false
//     Never mounts the PVC root — IMPORT-05 / Pitfall 7 containment invariant.
//   - Rekey ConfigMap: mounted at /rekey (key rekey.json → /rekey/rekey.json);
//     binary reads it via --rekey-file=/rekey/rekey.json (no shell, no stdin pipe).
//   - SecurityContext: RunAsNonRoot + drop ALL capabilities (mirrors reporter_jobspec.go:156-162).
//
// Note: D-09 / Wave CRs: this Job operates purely on the filesystem (envelope copy + UID
// rewrite); it never creates K8s objects. Wave CR creation is handled by deriveGlobalWaves
// on the first reconcile after Tasks exist.
func BuildImportJob(
	project *tideprojectv1alpha3.Project,
	opts ImportJobOptions,
	scheme *runtime.Scheme,
) *batchv1.Job {
	resolvedPVCName := opts.SharedPVCName
	if resolvedPVCName == "" {
		resolvedPVCName = defaultSharedPVCName
	}

	backoffLimit := int32(2)
	ttl := int32(300)
	// ActiveDeadlineSeconds force-terminates a hung copy (e.g. stuck NFS mount,
	// enormous envelope tree) so it cannot pin the RW PVC mount indefinitely
	// and stall the reconcile poll loop (WR-04). 600s is sized to the largest
	// expected salvage; expiry surfaces as JobFailed → ReasonImportFailed.
	activeDeadline := int64(600)

	// Hardened security context — mirrors reporter_jobspec.go:156-162.
	runAsNonRoot := true
	allowPrivEsc := false
	sc := &corev1.SecurityContext{
		RunAsNonRoot:             &runAsNonRoot,
		AllowPrivilegeEscalation: &allowPrivEsc,
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
	}

	roleLabels := map[string]string{
		"tideproject.k8s/role": importJobRoleLabel,
	}

	// Args against the image ENTRYPOINT (mirrors reporter_jobspec.go) — the
	// distroless base has no shell, so there is no sh/cat pipe. The binary reads
	// the rekey table directly from the mounted file via --rekey-file.
	args := []string{
		"--old-workspace=/old-workspace",
		"--new-workspace=/new-workspace",
		"--rekey-file=/rekey/rekey.json",
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			// Deterministic name: AlreadyExists = idempotent (Pitfall 6 / D-12).
			Name:      fmt.Sprintf("tide-import-%s", project.UID),
			Namespace: project.Namespace,
			Labels:    roleLabels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttl,
			ActiveDeadlineSeconds:   &activeDeadline,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: roleLabels,
				},
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: importSAName,
					// Pod-level securityContext so the nonroot tide-import container can
					// write to the PVC (GAP-6): FSGroup makes kubelet chown+setgid the
					// mounted new-workspace subPath at startup so MkdirAll on the fresh
					// new-UID envelope tree succeeds. RunAsUser=65532 is the tide-import
					// distroless image's nonroot user; RunAsGroup MUST accompany it (the
					// CRI rejects "runAsGroup without runAsUser") and pins the primary gid
					// to FSGroup. Mirrors push_helpers.go:218 (same distroless base).
					SecurityContext: &corev1.PodSecurityContext{
						FSGroup:    new(int64(1000)),
						RunAsUser:  new(int64(65532)),
						RunAsGroup: new(int64(1000)),
					},
					Volumes: []corev1.Volume{
						{
							// Single PVC volume; two mounts below use different subPaths
							// (Pitfall 7 — never mount the root; IMPORT-05 containment).
							Name: importWorkspaceVolume,
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: resolvedPVCName,
								},
							},
						},
						{
							// Rekey table ConfigMap as a file volume.
							Name: importRekeyCMVolume,
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: opts.RekeyCMName,
									},
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:  importContainerName,
							Image: opts.ImportImage,
							// Command nil → use the image ENTRYPOINT (tide-import binary).
							Args: args,
							VolumeMounts: []corev1.VolumeMount{
								{
									// Old-workspace: salvaged envelopes (ReadOnly — no writes
									// to source envelopes; IMPORT-05 / Anti-Pattern 5).
									Name:      importWorkspaceVolume,
									MountPath: "/old-workspace",
									SubPath:   opts.OldSubPath, // e.g. "<oldProjectUID>/workspace"
									ReadOnly:  true,
								},
								{
									// New-workspace: destination for rekey'd envelopes (read-write).
									Name:      importWorkspaceVolume,
									MountPath: "/new-workspace",
									SubPath:   opts.NewSubPath, // e.g. "<newProjectUID>/workspace"
									// ReadOnly: false (default)
								},
								{
									// Rekey ConfigMap mounted at /rekey; the CM key
									// rekey.json becomes /rekey/rekey.json, which the
									// binary's --rekey-file flag references.
									Name:      importRekeyCMVolume,
									MountPath: "/rekey",
									ReadOnly:  true,
								},
							},
							// NO EnvFrom git-creds Secret (import needs no PAT — pure filesystem I/O).
							// SecurityContext: hardened, mirrors reporter_jobspec.go:156-162.
							SecurityContext: sc,
						},
					},
				},
			},
		},
	}

	// Owner ref: Project → Job. EnsureOwnerRef enforces same-namespace (Pitfall 23)
	// and sets Controller=true + BlockOwnerDeletion=true. Best-effort: does not
	// propagate scheme errors in pure builder context.
	_ = owner.EnsureOwnerRef(job, project, scheme)

	return job
}
