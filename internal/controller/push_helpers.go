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
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/internal/owner"
	pkggit "github.com/jsquirrelz/tide/pkg/git"
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

	// IntegrateTaskBranches is the ordered list of per-task branch names to merge
	// into the run branch before staging planner artifacts (D-04/D-02). The push job
	// receives these as --integrate-task-branches=<CSV>.
	// Empty means no integration step is run (milestone/phase boundaries).
	IntegrateTaskBranches []string

	// IntegrationOnly marks a per-wave integration Job: tide-push merges +
	// verifies the branches into the LOCAL run branch and exits without
	// committing or pushing (--integration-only). Boundary pushes leave this
	// false — they carry the same cumulative branch set but MUST push the
	// run branch to the remote.
	IntegrationOnly bool
	// StageEnvelopes is the cumulative <uid>:<destPrefix> map of planner-completed
	// levels whose planning *.md + children/*.json are staged into
	// .tide/planning/<destPrefix>/ on the run branch (37-06 / DASH-02). Rendered as
	// a single --stage-envelopes=<CSV> arg (parsed by cmd/tide-push, plan 37-02) and
	// appended only when non-empty. EVERY push — boundary or artifact-triggered —
	// carries the full cumulative map (R-05): one push writer class, no second
	// force-with-lease anchor path.
	StageEnvelopes []string

	// AgentName / AgentEmail are the resolved committer/author identity
	// (SIGN-01 / D-03) injected as TIDE_AGENT_NAME/TIDE_AGENT_EMAIL on the push
	// container. Both in-pod commit sites read them: IntegrateTaskBranches merge
	// commits and the tide-push boundary-commit signature (one injection covers
	// both). The controller stamps resolved values here (never empty — compiled
	// default backstops), so the Env injection is unconditional.
	AgentName  string
	AgentEmail string
}

// CloneOptions carries the clone-mode arguments. Clone is a one-time op
// at Project creation; no Branch/SHA, no commit message — the push Job
// binary handles cloning into <workspace>/repo.git.
type CloneOptions struct {
	// TidePushImage is the image ref for the tide-push container (same
	// image as push-mode — the binary dispatches on --mode).
	TidePushImage string

	// RunBranch is the per-run branch name (D-B6 format `tide/run-<project>-<unix>`).
	// When non-empty, the clone Job passes --run-branch to tide-push so it
	// calls EnsureRunBranch + provisions the run worktree after the bare clone.
	// Empty means no run branch is provisioned (backward-compat).
	RunBranch string

	// BaseRef is the optional operator-selected ref the run branch is created
	// from (Phase 35 D-01: branch, tag, full 40-hex SHA, or refs/-qualified
	// path). When non-empty the clone Job passes --base-ref to tide-push, which
	// resolves it inside EnsureRunBranch — the single resolution site (D-04);
	// there is no controller-side ls-remote preflight. Empty means the remote
	// default branch (HEAD), unchanged behavior.
	BaseRef string
}

// pushEnvelopeReasonMergeConflict is the tide-push PushResult envelope's
// Reason value for a genuine content merge conflict (cmd/tide-push/main.go's
// exitMergeConflict path). Shared across boundary-push and wave-integration
// envelope handling.
const pushEnvelopeReasonMergeConflict = "merge-conflict"

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

// stagedEnvelopesAnnotation stamps the CSV of staged <uid>:<kind>/<name> entries
// this push Job carried at create time (Defect E / DASH-02 follow-up). A later
// boundary reconcile reads it back and compares against a fresh
// collectStageEnvelopes call: if the current cumulative map is a STRICT SUPERSET
// of what this Job staged, the Job is a stale artifact push (an early D-B5/R-05
// single-flight winner that snapshotted a partial map) and must be superseded
// rather than accepted as terminal. A Job with NO stamp is unknown provenance and
// must NEVER be second-guessed — this keeps pre-fix / bare push Jobs behaving as
// before.
const stagedEnvelopesAnnotation = "tideproject.k8s/staged-envelopes"

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
func buildPushJob(project *tideprojectv1alpha3.Project, pvcName string, opts PushOptions, scheme *runtime.Scheme) *batchv1.Job {
	args := []string{
		"--mode=push",
		"--branch=" + opts.Branch,
		"--last-pushed-sha=" + opts.LastPushedSHA,
		"--project-uid=" + string(project.UID),
		"--workspace=/workspace",
		"--commit-message=" + opts.CommitMessage,
	}
	if len(opts.ArtifactPaths) > 0 {
		// WR-04 fix: use strings.Join (stdlib O(n)) instead of the
		// custom joinCSV which built the CSV via O(n²) string concat.
		args = append(args, "--artifact-paths="+strings.Join(opts.ArtifactPaths, ","))
	}
	if len(opts.IntegrateTaskBranches) > 0 {
		// D-04: pass --integrate-task-branches=<CSV> so tide-push merges
		// per-task branches into the run branch before staging planner artifacts.
		args = append(args, "--integrate-task-branches="+strings.Join(opts.IntegrateTaskBranches, ","))
	}
	if opts.IntegrationOnly {
		// D-02 per-wave integration Job: merge+verify locally, no commit/push.
		args = append(args, "--integration-only")
	}
	if len(opts.StageEnvelopes) > 0 {
		// 37-06 / DASH-02: cumulative planner-artifact staging map. cmd/tide-push
		// (plan 37-02) parses <uid>:<destPrefix> CSV and stages each level's *.md +
		// children/*.json into .tide/planning/<destPrefix>/. Byte-identical restages
		// are no-ops via the clean-tree skip, so re-emitting the full map is safe.
		args = append(args, "--stage-envelopes="+strings.Join(opts.StageEnvelopes, ","))
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
			// Phase 34 D-02: every Job that merges into or pushes the run
			// branch carries these two labels so gitWriterInFlightCount can
			// List in-flight writers project-wide before dispatching another
			// (the single-flight gate). triggerWaveIntegrationJob reuses this
			// builder and only overrides Name/OwnerReferences, so wave-
			// integration Jobs inherit the same labels automatically.
			Labels: map[string]string{
				gitWriterRoleLabelKey:    gitWriterRoleLabelValue,
				gitWriterProjectLabelKey: project.Name,
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            new(int32(2)),
			TTLSecondsAfterFinished: new(int32(300)),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: pushSAName,
					SecurityContext: &corev1.PodSecurityContext{
						FSGroup: new(int64(1000)),
						// RunAsUser MUST accompany RunAsGroup — the docker/CRI runtime
						// rejects the sandbox ("runAsGroup is specified without a
						// runAsUser") otherwise. 65532 is the tide-push image's nonroot
						// user (D-G3); set explicitly here so the pod-level RunAsGroup
						// is valid.
						RunAsUser: new(int64(65532)),
						// RunAsGroup pins the primary gid to 1000 so files this Job
						// writes on the shared PVC are group-owned 1000, matching the
						// executor (which also runs RunAsGroup=1000). Cross-uid PVC
						// sharing between the clone/push Job (uid 65532) and the
						// executor (uid 1000) requires a shared PRIMARY group; fsGroup
						// is supplemental-only and does not set the gid of created files.
						RunAsGroup: new(int64(1000)),
					},
					Volumes: volumes,
					Containers: []corev1.Container{
						{
							Name:  pushContainerName,
							Image: opts.TidePushImage,
							Args:  args,
							// SIGN-01 / D-03: stamp the resolved agent identity so the
							// in-pod integrate merge commits and the tide-push boundary
							// commit read it via pkggit.AgentIdentity(). Unconditional —
							// the controller resolves a non-empty value (compiled default
							// backstops). Placed before EnvFrom; the creds SecretRef path
							// (below) is untouched.
							Env: []corev1.EnvVar{
								{Name: pkggit.EnvAgentName, Value: opts.AgentName},
								{Name: pkggit.EnvAgentEmail, Value: opts.AgentEmail},
							},
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
							// Phase 4 W-1: tide-push writes its push-result envelope
							// to /dev/termination-log (K8s terminationMessagePath
							// default surface). The ProjectReconciler reads this via
							// the Pod's Status.ContainerStatuses[0].State.Terminated
							// .Message to classify exit-10 (leak-detected) vs
							// exit-11 (lease-rejected) without mounting the PVC.
							// FallbackToLogsOnError handles the edge case where the
							// container exited before writing the file.
							TerminationMessagePath:   "/dev/termination-log",
							TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
						},
					},
				},
			},
		},
	}

	// Defect E / DASH-02: stamp the cumulative staged-envelope set onto the Job so
	// a later boundary reconcile can detect a stale (subset) artifact push and
	// supersede it. Gated on the SAME condition as the --stage-envelopes arg above
	// so the annotation and the arg are always stamped together or not at all — a
	// Job with no arg has no map to compare, and an absent stamp reads as unknown
	// provenance (never stale) in isStaleArtifactPush.
	if len(opts.StageEnvelopes) > 0 {
		job.Annotations = map[string]string{
			stagedEnvelopesAnnotation: strings.Join(opts.StageEnvelopes, ","),
		}
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
func buildCloneJob(project *tideprojectv1alpha3.Project, pvcName string, opts CloneOptions, scheme *runtime.Scheme) *batchv1.Job {
	args := []string{
		"--mode=clone",
		"--repo-url=" + project.Spec.Git.RepoURL,
		"--workspace=/workspace",
		// Phase 35 D-05: --project-uid keys the clone envelope's PVC path
		// (<workspace>/envelopes/clone/<uid>.json). Passed unconditionally so
		// tide-push always writes the provenance copy that carries baseSHA back.
		"--project-uid=" + string(project.UID),
	}
	if opts.RunBranch != "" {
		// B6: pass --run-branch so tide-push calls EnsureRunBranch + provisions
		// the run worktree during the clone phase (B5). This ensures the run
		// worktree exists before any executor wave is dispatched.
		args = append(args, "--run-branch="+opts.RunBranch)
	}
	if opts.BaseRef != "" {
		// Phase 35 D-01/D-04: base the run branch off this ref; tide-push
		// resolves it inside EnsureRunBranch (the single resolution site). An
		// unresolvable value surfaces as an exit-2 baseref-unresolvable envelope.
		args = append(args, "--base-ref="+opts.BaseRef)
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("tide-clone-%s", project.UID),
			Namespace: project.Namespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            new(int32(2)),
			TTLSecondsAfterFinished: new(int32(300)),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: pushSAName,
					SecurityContext: &corev1.PodSecurityContext{
						FSGroup: new(int64(1000)),
						// RunAsUser MUST accompany RunAsGroup — the docker/CRI runtime
						// rejects the sandbox ("runAsGroup is specified without a
						// runAsUser") otherwise. 65532 is the tide-push image's nonroot
						// user (D-G3); set explicitly here so the pod-level RunAsGroup
						// is valid.
						RunAsUser: new(int64(65532)),
						// RunAsGroup pins the primary gid to 1000 so files this Job
						// writes on the shared PVC are group-owned 1000, matching the
						// executor (which also runs RunAsGroup=1000). Cross-uid PVC
						// sharing between the clone/push Job (uid 65532) and the
						// executor (uid 1000) requires a shared PRIMARY group; fsGroup
						// is supplemental-only and does not set the gid of created files.
						RunAsGroup: new(int64(1000)),
					},
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
							// Phase 35 D-05: mirror buildPushJob's termination-message
							// wiring (push_helpers.go:284-285). tide-push writes the
							// CloneResult envelope to /dev/termination-log; the
							// ProjectReconciler reads it off the pod's
							// Terminated.Message to classify baseref-unresolvable and
							// stamp baseSHA — without this the envelope never reaches
							// the controller. FallbackToLogsOnError covers a container
							// that died before writing the file.
							TerminationMessagePath:   "/dev/termination-log",
							TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
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
//     with "+ executed"
//     suffix (Tasks
//     have already
//     executed by
//     the time Plan
//     boundary fires).
//   - "phase":     "tide: phase <name> authored"
//   - "milestone": "tide: milestone <name> authored"
//   - "project":   "tide: project complete"                 ← no name suffix;
//     final commit.
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

// WR-04 fix: joinCSV deleted in favor of strings.Join at the single call
// site above. The original helper rebuilt a CSV via O(n²) string concat;
// strings.Join is O(n) and shipping in the stdlib.
