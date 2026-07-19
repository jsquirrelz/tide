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
	"strings"

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

	// ReporterOTLPHeadersSecretName is the fixed name of the per-project-namespace
	// Secret the reporter Job's OTEL_EXPORTER_OTLP_HEADERS env references via
	// secretKeyRef. Mirrors the fixed-name tide-signing-key cross-namespace
	// mirror convention (internal/dispatch/podjob/jobspec.go:345) — operators
	// mirror the tide-system tide-otlp-headers Secret into each Project
	// namespace under this same name (docs/INSTALL.md § Enable tracing).
	ReporterOTLPHeadersSecretName = "tide-otlp-headers"

	// ReporterOTLPHeadersSecretKey is the data key inside
	// ReporterOTLPHeadersSecretName, matching the OTEL_EXPORTER_OTLP_HEADERS
	// env var name the tide-system Secret already uses.
	ReporterOTLPHeadersSecretKey = "OTEL_EXPORTER_OTLP_HEADERS"
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

	// OTLPEndpoint is the manager's own OTEL_EXPORTER_OTLP_ENDPOINT value,
	// forwarded so the reporter's otelinit.NewTracerProvider resolves the
	// SAME collector the manager uses (Phase 44 TRACE-03/D-06). Unlike
	// TraceParent this IS carried as Env (not Args) — it targets the
	// reporter's own TracerProvider bootstrap, mirroring how the manager
	// itself reads it via os.Getenv, not a CLI flag. When empty, no Env is
	// set at all and the reporter's otelinit falls back to its no-op
	// provider (materialization-mode posture, D-06).
	OTLPEndpoint string

	// OTLPHeadersSecret carries the per-project-namespace headers-Secret
	// NAME to reference (never the decoded value) so the reporter Job's own
	// TracerProvider authenticates to the SAME auth-enabled collector the
	// manager uses (Phase 47 PHX-02/D-08). Emitted as a
	// valueFrom.secretKeyRef against the fixed-name
	// ReporterOTLPHeadersSecretName Secret (key ReporterOTLPHeadersSecretKey)
	// mirrored into the project namespace — the same fixed-name convention
	// as tide-signing-key. The pinned otlptracegrpc v1.43.0 honors
	// OTEL_EXPORTER_OTLP_HEADERS automatically once resolved (no
	// reporter-binary change needed). Only emitted when OTLPEndpoint is also
	// non-empty (headers without an endpoint are meaningless). The
	// secretKeyRef is Optional: the Job spec exposes only the Secret NAME —
	// reading the token requires `get` on Secrets in that namespace, so
	// Job-read RBAC alone never sees the credential (corrects the T-47-02
	// rationale, which claimed RBAC equivalence with a strictly larger
	// literal-value exposure). A missing mirror degrades the reporter to an
	// unauthenticated export (dark traces) instead of blocking child-CRD
	// materialization with CreateContainerConfigError, matching the Phase
	// 44 D-10 exit-0/dark-pipe posture.
	OTLPHeadersSecret string

	// TraceOnly selects the Phase 44 trace-only Job shape (spawned by plan
	// 44-05 for completed Task dispatch Jobs) instead of the materialization
	// reporter shape: a distinct name keyed on TraceOnlyJobKey and a minimal
	// Args set with no parent-CR flags (trace-only mode makes no K8s API
	// calls, so the least-privilege tide-reporter SA is reused unwidened —
	// T-44-06). Zero value (false) is the existing materialization shape,
	// byte-identical to pre-Phase-44 behavior.
	TraceOnly bool

	// TraceOnlyJobKey is the completed dispatch Job's UID, keying the
	// trace-only Job's deterministic name ("tide-reporter-trace-<key>") so
	// it can never collide with the materialization reporter's
	// "tide-reporter-<parentUID>" name — a failed-then-retried planner Job's
	// trace-only spawn can never block a later materialization spawn. Only
	// consulted when TraceOnly is true.
	TraceOnlyJobKey string

	// SkipMessageSpans (ADAPT-01/Phase 45): set true when the manager's
	// pkgdispatch.SelfInstruments(vendor) lookup reports the dispatching
	// vendor emits OpenInference spans natively — the reporter skips
	// tracesynth.go's events.jsonl-based synthesis entirely. Zero value
	// (false) is the existing behavior, byte-identical pre-Phase-45: every
	// vendor resolves false today (D-03 default-safe).
	SkipMessageSpans bool

	// SessionID (46 D-05/OBS-02) is TIDE's own run identity — the Project
	// UID — stamped on every reporter-emitted LLM span via otelai.SessionID.
	// MUST be byte-identical to what the same level's AGENT span carries:
	// Phoenix's ProjectSession groups on an exact session.id string match.
	// Carried as an Arg (--session-id=), not Env, matching TraceParent's
	// precedent (Pitfall 3 — this file is 100% Args-based).
	SessionID string

	// MetadataJSON (46 D-05/OBS-03) is the manager-pre-JSON-encoded metadata
	// map (level kind, gate profile, failure-halt state, etc.) stamped
	// opaquely on every reporter-emitted LLM span via otelai.MetadataJSON —
	// this file never marshals it; the manager owns the encoding. Empty
	// string omits the Arg entirely (no attribute stamped, never a
	// fabricated empty value).
	MetadataJSON string

	// Tags (46 D-05/OBS-03) are low-cardinality categorical values (e.g.
	// level/gate/profile enums — no commas possible) rendered as a single
	// comma-joined Arg (--tags=) and split on comma by the reporter. Stamped
	// as a native attribute.STRINGSLICE via otelai.Tags, never JSON-encoded
	// (Pitfall 4). Empty slice omits the Arg entirely.
	Tags []string

	// AttemptID (50 D-01/D-05) is the Execution-loop attempt identity
	// ({taskUID}-{attempt}, matching podjob.JobName's tuple) — stamped as
	// loop.run_id context on every reporter-emitted LLM span so Phoenix
	// groups each tool/action iteration under its attempt. Carried as an
	// --attempt-id Arg, not Env, per this file's 100% Args-based convention
	// (Pitfall 3). Empty string omits the Arg entirely (no attribute
	// stamped, never a fabricated empty value).
	AttemptID string

	// LoopRunID (50 D-01/D-05) is the parent Task-loop run (taskUID), stable
	// across all repair attempts of one Task — accepted for signature
	// symmetry with AttemptID and future use, not yet stamped onto any
	// per-call LLM span attribute this phase. Carried as a --loop-run-id
	// Arg, not Env, per this file's 100% Args-based convention. Empty
	// string omits the Arg entirely.
	LoopRunID string
}

// BuildReporterJob constructs the K8s batchv1.Job that runs the in-namespace
// reporter process in the project namespace. Supports two shapes, selected by
// opts.TraceOnly:
//
//   - Materialization shape (opts.TraceOnly == false, the default): reads
//     out.json from the PVC and materializes child CRDs — this is the
//     original (Phase 09) shape, unchanged unless opts.OTLPEndpoint is set.
//
//   - Trace-only shape (opts.TraceOnly == true, added Phase 44): spawned by
//     plan 44-05 for a completed Task dispatch Job to synthesize LLM
//     message-array spans from events.jsonl. Makes no K8s API calls (no
//     parent CRD to materialize against), so its Args omit all parent-CR
//     flags and its Job name is keyed on opts.TraceOnlyJobKey (the completed
//     dispatch Job's UID) rather than the parent's UID — this keeps it from
//     ever colliding with a materialization reporter Job for the same parent
//     (a failed-then-retried planner Job's trace-only spawn can never block
//     a later materialization spawn).
//
// Both shapes share: SA, PVC volume/subPath, SecurityContext, role labels,
// BackoffLimit/TTL, and ownerRef via owner.EnsureOwnerRef.
//
// Identity (materialization shape):
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
//     five TIDE CRD Kinds — plan 09-04 SA+Role+RoleBinding). Reused unwidened
//     for the trace-only shape (T-44-06 — trace-only mode never builds a K8s
//     client, so no RBAC widening is needed).
//
//   - PVC subPath: <project.UID>/workspace → /workspace (same layout the dispatch
//     Job wrote out.json into — the load-bearing same-namespace fix).
//
//   - Args (materialization): --workspace, --project-uid, --task-uid (envelope
//     key), --parent-name, --parent-namespace, --parent-kind. Mirrors plan
//     09-05 reporter flag set. Args (trace-only): --trace-only, --workspace,
//     --task-uid only — no parent-CR flags.
//
//   - Env: empty unless opts.OTLPEndpoint is set (Phase 44 D-06/T-44-04), in
//     which case OTEL_EXPORTER_OTLP_ENDPOINT + OTEL_BSP_MAX_EXPORT_BATCH_SIZE=6
//     are added, plus a secretKeyRef-sourced OTEL_EXPORTER_OTLP_HEADERS when
//     opts.OTLPHeadersSecret is also set (Phase 47 PHX-02/D-08) — see the
//     ReporterOptions.OTLPEndpoint and OTLPHeadersSecret doc comments.
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
	// Phase 44: trace-only shape omits all parent-CR flags (--project-uid/
	// --parent-name/--parent-namespace/--parent-kind) — trace-only mode makes
	// no K8s API calls, so the least-privilege tide-reporter SA is reused
	// unwidened (T-44-06). The materialization shape is unchanged.
	var args []string
	if opts.TraceOnly {
		args = []string{
			"--trace-only",
			"--workspace=/workspace",
			"--task-uid=" + taskUID,
		}
	} else {
		args = []string{
			"--workspace=/workspace",
			"--project-uid=" + string(project.UID),
			"--task-uid=" + taskUID,
			"--parent-name=" + parent.GetName(),
			"--parent-namespace=" + parent.GetNamespace(),
			"--parent-kind=" + parentKind,
		}
	}
	// Phase 43 PROP-01: --traceparent Arg, not Env (Pitfall 3 — this file is
	// 100% Args-based via stdlib flag; zero Env entries on the reporter container).
	if opts.TraceParent != "" {
		args = append(args, "--traceparent="+opts.TraceParent)
	}
	// Phase 45 ADAPT-01/D-04: bareword flag (not "=value" — matches
	// --trace-only's shape), appended only when true so absence resolves to
	// synthesize (D-03). Placed after the shape-selecting branch above, so
	// it rides both the materialization and trace-only shapes uniformly
	// (D-05).
	if opts.SkipMessageSpans {
		args = append(args, "--skip-message-spans")
	}
	// 46 D-05: session/metadata/tags ride the same uniform-across-both-shapes
	// placement as SkipMessageSpans above. All three are per-span attribute
	// values, so they follow TraceParent's Args precedent, never Env.
	if opts.SessionID != "" {
		args = append(args, "--session-id="+opts.SessionID)
	}
	if opts.MetadataJSON != "" {
		// Pre-JSON-encoded by the manager — this file never marshals it.
		args = append(args, "--metadata="+opts.MetadataJSON)
	}
	if len(opts.Tags) > 0 {
		// Reporter splits on comma; tag values are level/gate/profile enums
		// — no commas possible in practice.
		args = append(args, "--tags="+strings.Join(opts.Tags, ","))
	}
	// 50 D-01/D-05: attempt-id/loop-run-id ride the same Args-only precedent
	// as session/metadata/tags above — absent when empty, never a
	// fabricated empty value.
	if opts.AttemptID != "" {
		args = append(args, "--attempt-id="+opts.AttemptID)
	}
	if opts.LoopRunID != "" {
		args = append(args, "--loop-run-id="+opts.LoopRunID)
	}

	// Phase 44 D-06/T-44-04: reporter container Env — empty unless the
	// manager has an OTLP endpoint configured. OTEL_EXPORTER_OTLP_ENDPOINT
	// forwards the SAME collector value the manager's own otelinit reads, so
	// the reporter's TracerProvider resolves it identically.
	// OTEL_BSP_MAX_EXPORT_BATCH_SIZE=6 is a hardcoded literal (NOT a Helm
	// value — values.yaml is a FIXED contract): 6 spans x 512 KiB whole-span
	// cap = 3 MiB per export batch, ~25% headroom under the 4 MiB OTLP gRPC
	// ceiling (RESEARCH Size-Boundary Model). The pinned OTel SDK's
	// NewBatchSpanProcessor reads this env automatically — zero otelinit
	// changes required.
	var env []corev1.EnvVar
	if opts.OTLPEndpoint != "" {
		env = []corev1.EnvVar{
			{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: opts.OTLPEndpoint},
			{Name: "OTEL_BSP_MAX_EXPORT_BATCH_SIZE", Value: "6"},
		}
		if opts.OTLPHeadersSecret != "" {
			// Optional=true: a missing per-namespace mirror degrades the
			// reporter to an unauthenticated export (dark traces) rather
			// than blocking child-CRD materialization with
			// CreateContainerConfigError (T-47-06-02).
			optionalTrue := true
			env = append(env, corev1.EnvVar{
				Name: ReporterOTLPHeadersSecretKey,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: opts.OTLPHeadersSecret},
						Key:                  ReporterOTLPHeadersSecretKey,
						Optional:             &optionalTrue,
					},
				},
			})
		}
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

	// jobName: the materialization shape keys on the parent UID (deterministic
	// per parent, AlreadyExists on Create is idempotent success, D-B5). The
	// trace-only shape keys on TraceOnlyJobKey (the completed dispatch Job's
	// UID) instead, in the "tide-reporter-trace-" namespace so it can never
	// collide with a materialization reporter's name for the same parent.
	jobName := fmt.Sprintf("tide-reporter-%s", parent.GetUID())
	if opts.TraceOnly {
		jobName = "tide-reporter-trace-" + opts.TraceOnlyJobKey
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
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
							Env:   env,
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
