/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	"github.com/jsquirrelz/tide/internal/owner"
)

// PushOptions carries everything the ProjectReconciler (plan 03-08) supplies
// when constructing a per-Project push Job.
//
// Branch + LastPushedSHA flow into the tide-push --branch / --last-pushed-sha
// args (D-B6 — the lease anchor that protects against external pushes per
// Pitfall 13). LeaksConfigMap is the optional per-Project gitleaks override
// (D-B3); when empty, tide-push uses its embedded default ruleset.
//
// CommitMessage + ArtifactPaths carry the W11 boundary-commit contract
// (D-B2 — one of 4 fixed message shapes — and the CSV list of
// workspace-relative artifact paths to stage). Each push at a level boundary
// commits with the W11 message and the fixed TIDE-bot author signature.
type PushOptions struct {
	// TidePushImage is the image ref for the tide-push container
	// (Helm value images.tidePush.repository:tag).
	TidePushImage string

	// Branch is the per-run branch (D-B6 format `tide/run-<project>-<unix>`).
	Branch string

	// LastPushedSHA is the lease anchor for --force-with-lease (D-B6).
	// Empty on first push; subsequent pushes carry the prior successful
	// HEAD SHA so the remote rejects external mutations (Pitfall 13).
	LastPushedSHA string

	// CommitMessage is the W11 boundary commit message string (one of 4
	// fixed shapes per D-B2). Required; empty triggers tide-push's
	// missing-commit-message invariant.
	CommitMessage string

	// ArtifactPaths is the CSV-passed list of workspace-relative paths
	// the push Job stages into the per-run worktree before committing.
	// Defensive empty-list semantics: tide-push still creates a commit
	// (even if there's nothing new staged) so every level boundary
	// records a commit per D-B2.
	ArtifactPaths []string

	// LeaksConfigMap is the optional ConfigMap name carrying a gitleaks
	// override TOML mounted into the Job at /etc/tide/gitleaks-config.toml.
	// When empty, tide-push falls back to its embedded default ruleset.
	LeaksConfigMap string
}

// CloneOptions carries the clone-mode arguments. Clone is a one-time op
// at Project creation; no Branch/SHA, no commit message — the push Job
// binary handles cloning into <workspace>/repo.git.
type CloneOptions struct {
	// TidePushImage is the image ref for the tide-push container (same
	// image as push-mode — the binary dispatches on --mode).
	TidePushImage string
}

// pushSAName is the dedicated ServiceAccount for the tide-push Job pod.
// Helm chart in plan 03-09 grants it `secrets get` on
// project.Spec.Git.CredsSecretRef ONLY (D-B1 least-privilege).
const pushSAName = "tide-push"

// pushContainerName is the single container's name in both push- and
// clone-mode Job specs.
const pushContainerName = "push"

// pushWorkspaceVolume is the volume name for the per-Project PVC mount.
// Matches the buildInitJob pattern in project_controller.go.
const pushWorkspaceVolume = "project-workspace"

// buildPushJob constructs the K8s batchv1.Job that drives a single
// level-boundary push for a Project.
//
// The Job's identity:
//
//   - Name: fmt.Sprintf("tide-push-%s", project.UID) — D-B5 serialization
//     key. Two concurrent push attempts for the same Project collide on
//     this name; the second hits K8s API AlreadyExists and the calling
//     reconciler requeues. Per-Project push serialization is enforced by
//     the K8s API server (no in-controller mutex needed).
//
//   - Namespace: project.Namespace. Secrets are namespace-scoped; the
//     credsSecretRef MUST live in the same namespace as the Project
//     (per CRD schema, plan 03-02).
//
//   - OwnerReferences: point at the Project with Controller=true,
//     BlockOwnerDeletion=true. Project deletion cascades to the push Job
//     and its Pod (CRD-02 / Pitfall 23 same-namespace enforcement via
//     internal/owner.EnsureOwnerRef).
//
// The Job's pod spec:
//
//   - ServiceAccountName: "tide-push" (D-B1 — dedicated SA with
//     `secrets get` on credsSecretRef only; Helm chart in plan 03-09
//     creates the SA + Role + RoleBinding).
//
//   - Containers[0].EnvFrom: SecretRef → project.Spec.Git.CredsSecretRef.
//     The PAT lands in env as GIT_PAT on this pod ONLY. The controller
//     pod NEVER has the Secret bound — buildPushJob just writes the
//     Secret NAME (a string field) into the Job spec.
//
//   - Containers[0].VolumeMounts: the per-Project PVC mounted at
//     /workspace with SubPath "<project.UID>/workspace" — matches Phase 2
//     D-G2 PVC layout convention.
//
//   - Containers[0].Args: --mode=push, --branch, --last-pushed-sha,
//     --project-uid, --workspace, --commit-message, --artifact-paths,
//     and optionally --leaks-config when opts.LeaksConfigMap is set.
//
// Plan 03-08 ProjectReconciler is the sole caller. This is a pure
// builder — no client.Create here.
func buildPushJob(project *tideprojectv1alpha1.Project, pvcName string, opts PushOptions, scheme *runtime.Scheme) *batchv1.Job {
	args := []string{
		"--mode=push",
		"--branch=" + opts.Branch,
		"--last-pushed-sha=" + opts.LastPushedSHA,
		"--project-uid=" + string(project.UID),
		"--workspace=/workspace",
		"--commit-message=" + opts.CommitMessage,
	}
	if len(opts.ArtifactPaths) > 0 {
		args = append(args, "--artifact-paths="+joinCSV(opts.ArtifactPaths))
	}

	volumes := []corev1.Volume{
		{
			Name: pushWorkspaceVolume,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: pvcName,
				},
			},
		},
	}
	mounts := []corev1.VolumeMount{
		{
			Name:      pushWorkspaceVolume,
			MountPath: "/workspace",
			SubPath:   fmt.Sprintf("%s/workspace", project.UID),
		},
	}

	// Optional per-Project gitleaks override (D-B3). Wire as a ConfigMap
	// mount at /etc/tide/gitleaks-config.toml and pass --leaks-config so
	// tide-push picks it up. Empty LeaksConfigMap → embedded default
	// ruleset (gitleaks v8 upstream config).
	if opts.LeaksConfigMap != "" {
		volumes = append(volumes, corev1.Volume{
			Name: "leaks-config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: opts.LeaksConfigMap,
					},
				},
			},
		})
		mounts = append(mounts, corev1.VolumeMount{
			Name:      "leaks-config",
			MountPath: "/etc/tide",
			ReadOnly:  true,
		})
		args = append(args, "--leaks-config=/etc/tide/gitleaks-config.toml")
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("tide-push-%s", project.UID),
			Namespace: project.Namespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            ptr.To(int32(2)),
			TTLSecondsAfterFinished: ptr.To(int32(300)),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: pushSAName,
					Volumes:            volumes,
					Containers: []corev1.Container{
						{
							Name:  pushContainerName,
							Image: opts.TidePushImage,
							Args:  args,
							EnvFrom: []corev1.EnvFromSource{
								{
									SecretRef: &corev1.SecretEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: project.Spec.Git.CredsSecretRef,
										},
									},
								},
							},
							VolumeMounts: mounts,
						},
					},
				},
			},
		},
	}

	// Owner ref: Project → Job. internal/owner.EnsureOwnerRef enforces
	// same-namespace (Pitfall 23) and sets Controller=true +
	// BlockOwnerDeletion=true (CRD-02). Best-effort: if it fails (e.g.
	// nil scheme in test fixtures that forgot to register the type), the
	// caller's reconciler will surface the error at Create time. Pure
	// builders don't propagate scheme errors out of their return value.
	_ = owner.EnsureOwnerRef(job, project, scheme)

	return job
}

// buildCloneJob constructs the K8s batchv1.Job that performs the
// Project's one-time initial bare-clone into /workspace/repo.git.
//
// Identity:
//
//   - Name: fmt.Sprintf("tide-clone-%s", project.UID). Clone is a
//     one-time op; not strictly serialization-keyed like push, but
//     keeping a deterministic-per-Project name lets the reconciler
//     idempotently detect "clone already attempted" via Get on the
//     known name.
//
// Pod spec mirrors buildPushJob: same SA (tide-push), same envFrom for
// the PAT (private repo clones need it; public repos ignore it), same
// PVC mount at /workspace. Args differ: --mode=clone --repo-url=...
// and no branch/sha/message/artifacts.
func buildCloneJob(project *tideprojectv1alpha1.Project, pvcName string, opts CloneOptions, scheme *runtime.Scheme) *batchv1.Job {
	args := []string{
		"--mode=clone",
		"--repo-url=" + project.Spec.Git.RepoURL,
		"--workspace=/workspace",
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("tide-clone-%s", project.UID),
			Namespace: project.Namespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            ptr.To(int32(2)),
			TTLSecondsAfterFinished: ptr.To(int32(300)),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: pushSAName,
					Volumes: []corev1.Volume{
						{
							Name: pushWorkspaceVolume,
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: pvcName,
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:  pushContainerName,
							Image: opts.TidePushImage,
							Args:  args,
							EnvFrom: []corev1.EnvFromSource{
								{
									SecretRef: &corev1.SecretEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: project.Spec.Git.CredsSecretRef,
										},
									},
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      pushWorkspaceVolume,
									MountPath: "/workspace",
									SubPath:   fmt.Sprintf("%s/workspace", project.UID),
								},
							},
						},
					},
				},
			},
		},
	}

	_ = owner.EnsureOwnerRef(job, project, scheme)

	return job
}

// buildCommitMessage returns the W11 / D-B2 boundary commit-message
// string for the given level boundary. Plan 03-08 locks in the four
// shapes:
//
//   - "plan":      "tide: plan <name> authored + executed"  ← only one
//                                                             with "+ executed"
//                                                             suffix (Tasks
//                                                             have already
//                                                             executed by
//                                                             the time Plan
//                                                             boundary fires).
//   - "phase":     "tide: phase <name> authored"
//   - "milestone": "tide: milestone <name> authored"
//   - "project":   "tide: project complete"                 ← no name suffix;
//                                                             final commit.
//
// Returns an error for unknown boundary names (e.g., "wave" — Tasks
// ship in their parent Plan's commit) or for an empty name when one
// of the three name-required boundaries (plan/phase/milestone) is
// requested. "project" allows empty name (the name is ignored).
//
// The ProjectReconciler (plan 03-08) is the sole caller; it computes
// the message string once per level boundary and threads it through
// PushOptions.CommitMessage into the push Job's --commit-message arg.
// The push Job binary (cmd/tide-push) consumes the arg and writes the
// commit verbatim.
func buildCommitMessage(boundary, name string) (string, error) {
	switch boundary {
	case "plan":
		if name == "" {
			return "", fmt.Errorf("buildCommitMessage: plan boundary requires non-empty name (D-B2 #1)")
		}
		return fmt.Sprintf("tide: plan %s authored + executed", name), nil
	case "phase":
		if name == "" {
			return "", fmt.Errorf("buildCommitMessage: phase boundary requires non-empty name (D-B2 #2)")
		}
		return fmt.Sprintf("tide: phase %s authored", name), nil
	case "milestone":
		if name == "" {
			return "", fmt.Errorf("buildCommitMessage: milestone boundary requires non-empty name (D-B2 #3)")
		}
		return fmt.Sprintf("tide: milestone %s authored", name), nil
	case "project":
		// Project boundary is the final commit; name is ignored
		// because the project is the implicit scope.
		return "tide: project complete", nil
	default:
		return "", fmt.Errorf("buildCommitMessage: unknown boundary %q (allowed: plan, phase, milestone, project)", boundary)
	}
}

// joinCSV joins paths with commas. Tide-push's --artifact-paths flag
// parses CSV; keep the join shape consistent with the parser there.
func joinCSV(paths []string) string {
	out := ""
	for i, p := range paths {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return out
}
