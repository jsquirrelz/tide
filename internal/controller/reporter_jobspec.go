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

// Package controller / reporter_jobspec.go — builds the short-lived
// tide-reporter reader Job that materializes child CRDs from the PVC-side
// out.json into the project namespace (Option C, Phase 09 Plan 06).
//
// Build pattern mirrors internal/controller/push_helpers.go buildCloneJob:
// deterministic-named, project-namespace-scoped, PVC-subPath-mounted,
// owner-referenced to the completion-triggering parent. Diverges from
// buildCloneJob in three ways:
//
//  1. SA is "tide-reporter" (NOT "tide-push") — least-privilege create+get
//     on the five TIDE CRD Kinds only (reporter-rbac.yaml, plan 09-04).
//  2. NO EnvFrom git-creds Secret — reporter needs no PAT.
//  3. SecurityContext: RunAsNonRoot + drop ALL capabilities (mirrors
//     internal/dispatch/podjob/jobspec.go:368-375 hardened subagent context).
//  4. Job + pod template carry tideproject.k8s/role=reporter label so the
//     dispatch-completion handler can discriminate reader Jobs from dispatch
//     Jobs (T-09-13 re-fire mitigation).
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
	// reporterSAName is the dedicated least-privilege SA for the reader Job pod
	// (plan 09-04 creates this SA in the project namespace via reporter-rbac.yaml).
	reporterSAName = "tide-reporter"

	// reporterContainerName is the single container name in the reader Job spec.
	reporterContainerName = "reporter"

	// reporterWorkspaceVolume is the volume name for the per-Project PVC mount.
	// Mirrors pushWorkspaceVolume (push_helpers.go) for consistency.
	reporterWorkspaceVolume = "project-workspace"

	// ReporterRoleLabel is the value of the tideproject.k8s/role label on reader
	// Jobs. Callers use this constant to discriminate reader Jobs from dispatch Jobs
	// in the Job-completion handler (T-09-13 mitigation).
	ReporterRoleLabel = "reporter"
)

// ReporterOptions carries everything the four planner-completion handlers
// supply when constructing the per-parent reader Job (plan 09-06).
//
// ReporterImage is the image ref for the tide-reporter container
// (Helm value images.tideReporter.repository:tag). When empty, the spawn
// site should skip creating the Job (mirrors the TidePushImage skip in
// boundary_push.go:80-88) — but BuildReporterJob itself always returns a
// valid Job so callers control the skip.
type ReporterOptions struct {
	// ReporterImage is the image ref for the tide-reporter container.
	// PROD_OVERRIDE_REQUIRED: set via TIDE_REPORTER_IMAGE env from Helm value
	// images.tideReporter.repository:tag. The :v0.1.0-dev tag tracks main
	// and is NOT a release-stable placeholder.
	ReporterImage string

	// TraceParent is the W3C traceparent for the spawning level's OWN
	// just-synthesized span — the reporter's future Phase 44 message spans
	// nest under THIS level's dispatch, not the grandparent (Phase 43 PROP-01).
	// Consumed starting Phase 44; carried as an Arg, not Env, matching this
	// file's 100% Args-based convention (Pitfall 3).
	TraceParent string
}

// BuildReporterJob constructs the K8s batchv1.Job that runs the in-namespace
// reporter process in the project namespace.
//
// Identity:
//
//   - Name: "tide-reporter-<parentUID>" — deterministic per parent; AlreadyExists
//     on Create is idempotent success (mirrors tide-push-%s / tide-clone-%s naming
//     in push_helpers.go:90 / :249). One reader Job per planner-Job completion.
//
//   - Namespace: project.Namespace — the reporter runs in the TENANT namespace
//     so it can read the per-project PVC and call the K8s API to create child CRDs
//     under the tide-reporter SA (same-namespace constraint, Pitfall 23).
//
//   - OwnerReferences: cascade-delete to the parent (Milestone / Phase / Plan /
//     Project) via owner.EnsureOwnerRef (Controller=true, BlockOwnerDeletion=true).
//
// Pod spec:
//
//   - ServiceAccountName: "tide-reporter" (least-privilege create+get on the
//     five TIDE CRD Kinds — plan 09-04 SA+Role+RoleBinding).
//
//   - PVC subPath: <project.UID>/workspace → /workspace (same layout the dispatch
//     Job wrote out.json into — the load-bearing same-namespace fix).
//
//   - Args: --workspace, --project-uid, --task-uid (envelope key), --parent-name,
//     --parent-namespace, --parent-kind. Mirrors plan 09-05 reporter flag set.
//
//   - SecurityContext: RunAsNonRoot + drop ALL capabilities (mirrors jobspec.go
//     hardened subagent context — reporter is trusted write; least-privilege).
//     NO AllowPrivilegeEscalation, NO readOnlyRootFilesystem=true (reporter writes
//     nothing to its own fs, but container image may need tmpfs — keep false to
//     match subagent precedent).
//
//   - NO EnvFrom git-creds Secret (reporter needs no PAT).
//
//   - Label tideproject.k8s/role=reporter on both Job and pod template so the
//     dispatch-completion watch can discriminate reader Jobs from dispatch Jobs
//     (T-09-13 — prevents the handler from re-firing on the reader Job's own
//     completion event).
func BuildReporterJob(
	parent metav1.Object,
	project *tideprojectv1alpha3.Project,
	pvcName string,
	taskUID string,
	parentKind string,
	opts ReporterOptions,
	scheme *runtime.Scheme,
) *batchv1.Job {
	args := []string{
		"--workspace=/workspace",
		"--project-uid=" + string(project.UID),
		"--task-uid=" + taskUID,
		"--parent-name=" + parent.GetName(),
		"--parent-namespace=" + parent.GetNamespace(),
		"--parent-kind=" + parentKind,
	}
	// Phase 43 PROP-01: --traceparent Arg, not Env (Pitfall 3 — this file is
	// 100% Args-based via stdlib flag; zero Env entries on the reporter container).
	if opts.TraceParent != "" {
		args = append(args, "--traceparent="+opts.TraceParent)
	}

	// resolvedPVCName defaults to the project-shared PVC name when the caller
	// passes an empty string (mirrors defaultSharedPVCName pattern in push_helpers).
	resolvedPVCName := pvcName
	if resolvedPVCName == "" {
		resolvedPVCName = defaultSharedPVCName
	}

	// backoffLimitVal + ttlVal: pointers required by the JobSpec API.
	backoffLimitVal := int32(2)
	ttlVal := int32(300)

	// Role label shared by both Job metadata and pod template (T-09-13).
	roleLabels := map[string]string{
		"tideproject.k8s/role": ReporterRoleLabel,
	}

	// Hardened security context mirroring jobspec.go:368-375.
	runAsNonRoot := true
	allowPrivEsc := false
	sc := &corev1.SecurityContext{
		RunAsNonRoot:             &runAsNonRoot,
		AllowPrivilegeEscalation: &allowPrivEsc,
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			// Deterministic name keyed on the parent UID so that two concurrent
			// reconcile passes for the same parent both produce the same name and
			// only one Create succeeds (AlreadyExists = idempotent, D-B5 pattern).
			Name:      fmt.Sprintf("tide-reporter-%s", parent.GetUID()),
			Namespace: project.Namespace,
			Labels:    roleLabels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimitVal,
			TTLSecondsAfterFinished: &ttlVal,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: roleLabels,
				},
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: reporterSAName,
					Volumes: []corev1.Volume{
						{
							Name: reporterWorkspaceVolume,
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: resolvedPVCName,
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:  reporterContainerName,
							Image: opts.ReporterImage,
							Args:  args,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      reporterWorkspaceVolume,
									MountPath: "/workspace",
									// Same subPath the dispatch Job wrote out.json into —
									// the load-bearing cross-namespace fix: reader Job is
									// in the project namespace so it can access the PVC.
									SubPath: fmt.Sprintf("%s/workspace", project.UID),
								},
							},
							// NO EnvFrom: reporter needs no git credentials PAT.
							// SecurityContext: hardened, mirrors jobspec.go:368-375.
							SecurityContext: sc,
						},
					},
				},
			},
		},
	}

	// Owner ref: parent → Job. EnsureOwnerRef enforces same-namespace (Pitfall 23)
	// and sets Controller=true + BlockOwnerDeletion=true (CRD-02). Best-effort:
	// pure builder does not propagate scheme errors (nil scheme in test fixtures).
	_ = owner.EnsureOwnerRef(job, parent, scheme)

	return job
}
