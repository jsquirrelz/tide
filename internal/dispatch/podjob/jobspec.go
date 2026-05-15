package podjob

import (
	"encoding/base64"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

// Container and volume name constants exported so Plan 09 (TaskReconciler) and
// Plan 13 (integration tests) can reference them by name rather than by string literal.
const (
	// ContainerNameEnvelopeWriter is the name of the init container that writes
	// EnvelopeIn JSON to the per-Project PVC slice before the subagent starts.
	ContainerNameEnvelopeWriter = "envelope-writer"

	// ContainerNameCredproxy is the name of the K8s 1.33 native-sidecar init
	// container that runs the HTTPS credential proxy (Plan 05). Its RestartPolicy
	// is Always — this is the marker that makes it a native sidecar.
	ContainerNameCredproxy = "tide-credproxy"

	// ContainerNameSubagent is the name of the main subagent container.
	ContainerNameSubagent = "subagent"

	// VolumeProjectWorkspace is the volume name for the shared PVC mount.
	// The PVC "tide-projects" is chart-provisioned; per-Project isolation is
	// via subPath: {project-uid}/workspace (Blocker #2/#3 architecture).
	VolumeProjectWorkspace = "project-workspace"

	// VolumeCertShared is the emptyDir volume shared between the credproxy
	// sidecar and the subagent container for the self-signed TLS certificate
	// minted by the sidecar at pod startup (D-C2).
	VolumeCertShared = "cert-shared"

	// ServiceAccountSubagent is the name of the ServiceAccount with zero K8s
	// verbs used by subagent pods (D-A4). Plan 12's Helm chart creates the SA.
	ServiceAccountSubagent = "tide-subagent"

	// DefaultWallClockGraceSeconds is added to Task.Spec.Caps.WallClockSeconds
	// when setting Job.spec.activeDeadlineSeconds. This gives the harness's
	// internal SIGTERM a chance to fire and write the EnvelopeOut before K8s
	// kills the pod (per Claude's-Discretion in 02-RESEARCH.md Open Question #3).
	DefaultWallClockGraceSeconds = 60

	// DefaultTTLSecondsAfterFinished is the Job TTL after completion.
	DefaultTTLSecondsAfterFinished = 600
)

// BuildOptions carries all inputs required to build a Job spec for a Task dispatch.
type BuildOptions struct {
	// Task is the Task CRD being dispatched.
	Task *tidev1alpha1.Task

	// Project is the owning Project (for namespace + ProviderSecretRef).
	Project *tidev1alpha1.Project

	// Attempt is the nth dispatch attempt counter (D-B5).
	Attempt int

	// SignedToken is the HMAC-signed token minted by the controller at Job-create
	// time. Passed to the subagent container as ANTHROPIC_API_KEY and
	// ANTHROPIC_AUTH_TOKEN (D-C1). The proxy validates the token before forwarding.
	SignedToken string

	// EnvelopeInJSON is the marshalled EnvelopeIn that the envelope-writer init
	// container will write to /workspace/envelopes/{taskUID}/in.json.
	EnvelopeInJSON []byte

	// SubagentImage is the image ref for the subagent container (Plan 04 stub for
	// Phase 2; Plan 12's Helm values bind it to the production image in Phase 3+).
	SubagentImage string

	// CredproxyImage is the image ref for the tide-credproxy sidecar (Plan 05).
	CredproxyImage string

	// SecretUID is the K8s UID of the Project's providerSecretRef Secret.
	// Stamped as label tideproject.k8s/provider-secret-uid so Plan 07's PreCharge
	// can find active Jobs at Manager restart.
	SecretUID string

	// PVCName is the name of the shared PVC ("tide-projects" — chart-provisioned).
	// Single PVC for all Projects; per-Project isolation via subPath (Blocker #2/#3).
	PVCName string

	// ProjectUID is the UID of the owning Project. Used as the subPath prefix:
	// {project-uid}/workspace. Kubelet enforces subPath isolation across Projects.
	ProjectUID string
}

// BuildJobSpec returns a complete *batchv1.Job for the Task at the given attempt
// number, ready for client.Create. The caller is responsible for invoking
// internal/owner.EnsureOwnerRef on the returned Job AFTER BuildJobSpec returns,
// per the Phase 1 helper-package-usage rule in PATTERNS.md.
//
// Key contracts:
//   - Job name: JobName(opts.Task.UID, opts.Attempt) — the SUB-03 idempotency key.
//   - backoffLimit: 0 — retries are controller-side via attempt counter (Pitfall 9).
//   - Two init containers: envelope-writer (standard), tide-credproxy (native sidecar).
//   - Subagent container receives only the signed token, never the raw provider secret (D-C4).
//   - PVC subPath: {project-uid}/workspace enforces per-Project isolation (Blocker #2/#3).
func BuildJobSpec(opts BuildOptions) *batchv1.Job {
	// 1. Compute the active deadline.
	var wallClockSeconds int64
	if opts.Task.Spec.Caps != nil {
		wallClockSeconds = int64(opts.Task.Spec.Caps.WallClockSeconds)
	}
	activeDeadline := wallClockSeconds + DefaultWallClockGraceSeconds

	// 2. Build the four labels stamped on Job + propagated to Pod via Template.
	labels := map[string]string{
		"tideproject.k8s/task-uid":            string(opts.Task.UID),
		"tideproject.k8s/attempt":             fmt.Sprintf("%d", opts.Attempt),
		"tideproject.k8s/provider-secret-uid": opts.SecretUID,
		"tideproject.k8s/role":                "executor",
	}

	// 3. Compute the PVC subPath for per-Project isolation (Blocker #2/#3).
	subPath := opts.ProjectUID + "/workspace"

	// 4. Build envelope-writer init container (standard init — NOT a sidecar).
	// Writes EnvelopeIn JSON to /workspace/envelopes/{taskUID}/in.json via base64-decode.
	envelopeB64 := base64.StdEncoding.EncodeToString(opts.EnvelopeInJSON)
	envelopeWriter := corev1.Container{
		Name:  ContainerNameEnvelopeWriter,
		Image: "busybox:stable",
		Command: []string{"sh", "-c",
			`mkdir -p /workspace/envelopes/${TIDE_TASK_UID} && echo "$ENVELOPE_IN_B64" | base64 -d > /workspace/envelopes/${TIDE_TASK_UID}/in.json`,
		},
		Env: []corev1.EnvVar{
			{Name: "TIDE_TASK_UID", Value: string(opts.Task.UID)},
			{Name: "ENVELOPE_IN_B64", Value: envelopeB64},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      VolumeProjectWorkspace,
				MountPath: "/workspace",
				SubPath:   subPath,
			},
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:                ptr.To(int64(1000)),
			RunAsNonRoot:             ptr.To(true),
			AllowPrivilegeEscalation: ptr.To(false),
			ReadOnlyRootFilesystem:   ptr.To(true),
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		},
	}

	// 5. Build tide-credproxy init container with RestartPolicy: Always (K8s 1.33 native sidecar).
	// The sidecar MUST have RestartPolicy Always to mark it as a native sidecar — this is the
	// K8s 1.33 marker that causes the Job to complete when the main container exits, while
	// kubelet terminates the sidecar in reverse spec order (RESEARCH.md Pattern 2).
	credproxy := corev1.Container{
		Name:  ContainerNameCredproxy,
		Image: opts.CredproxyImage,
		// RestartPolicy: Always is the K8s 1.33 native-sidecar marker.
		RestartPolicy: ptr.To(corev1.ContainerRestartPolicyAlways),
		Env: []corev1.EnvVar{
			{Name: "TIDE_TASK_UID", Value: string(opts.Task.UID)},
			{Name: "TIDE_PROXY_LISTEN", Value: "0.0.0.0:8443"},
		},
		EnvFrom: func() []corev1.EnvFromSource {
			// Always include the signing-key secret.
			srcs := []corev1.EnvFromSource{
				{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "tide-signing-key"}}},
			}
			// Only add the provider secret when a name is set — K8s rejects empty
			// SecretRef.Name (Required value validation).
			if opts.Project.Spec.ProviderSecretRef != "" {
				srcs = append(srcs, corev1.EnvFromSource{
					SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: opts.Project.Spec.ProviderSecretRef}},
				})
			}
			return srcs
		}(),
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      VolumeCertShared,
				MountPath: "/etc/tide/proxy",
			},
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt32(8443),
				},
			},
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:                ptr.To(int64(1000)),
			RunAsNonRoot:             ptr.To(true),
			AllowPrivilegeEscalation: ptr.To(false),
			ReadOnlyRootFilesystem:   ptr.To(true),
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		},
	}

	// 6. Build the main subagent container. The subagent receives only the signed token
	// (not the raw provider secret) — D-C4 secret isolation contract.
	subagent := corev1.Container{
		Name:       ContainerNameSubagent,
		Image:      opts.SubagentImage,
		WorkingDir: "/workspace",
		Env: []corev1.EnvVar{
			{Name: "TIDE_TASK_UID", Value: string(opts.Task.UID)},
			// D-C1: subagent points to the localhost credproxy — never reaches Anthropic directly.
			{Name: "ANTHROPIC_BASE_URL", Value: "https://127.0.0.1:8443"},
			// D-C1: signed token replaces the real API key in the subagent process.
			{Name: "ANTHROPIC_API_KEY", Value: opts.SignedToken},
			{Name: "ANTHROPIC_AUTH_TOKEN", Value: opts.SignedToken},
			// Trust the sidecar's self-signed cert for both Node.js and Go runtimes.
			{Name: "SSL_CERT_FILE", Value: "/etc/tide/proxy/ca.crt"},
			{Name: "NODE_EXTRA_CA_CERTS", Value: "/etc/tide/proxy/ca.crt"},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      VolumeProjectWorkspace,
				MountPath: "/workspace",
				SubPath:   subPath,
			},
			{
				Name:      VolumeCertShared,
				MountPath: "/etc/tide/proxy",
				ReadOnly:  true,
			},
		},
		// D-A4: subagent has zero K8s verbs — no EnvFrom from K8s Secrets.
		// D-C4: raw API key is NOT in subagent's env or EnvFrom.
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:                ptr.To(int64(1000)),
			RunAsNonRoot:             ptr.To(true),
			AllowPrivilegeEscalation: ptr.To(false),
			// Note: ReadOnlyRootFilesystem is false for subagent — it writes to /workspace.
			ReadOnlyRootFilesystem: ptr.To(false),
			Capabilities:           &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		},
	}

	// 7. Compose the PodSpec.
	podSpec := corev1.PodSpec{
		ServiceAccountName: ServiceAccountSubagent,
		SecurityContext: &corev1.PodSecurityContext{
			FSGroup: ptr.To(int64(1000)),
		},
		InitContainers: []corev1.Container{envelopeWriter, credproxy},
		Containers:     []corev1.Container{subagent},
		Volumes: []corev1.Volume{
			{
				Name: VolumeProjectWorkspace,
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: opts.PVCName,
					},
				},
			},
			{
				Name: VolumeCertShared,
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			},
		},
		RestartPolicy: corev1.RestartPolicyNever,
	}

	// 8. Compose the Job.
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      JobName(opts.Task.UID, opts.Attempt),
			Namespace: opts.Project.Namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            ptr.To(int32(0)),
			TTLSecondsAfterFinished: ptr.To(int32(DefaultTTLSecondsAfterFinished)),
			ActiveDeadlineSeconds:   ptr.To(activeDeadline),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: podSpec,
			},
		},
	}
}
