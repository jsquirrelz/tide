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

package podjob

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/internal/owner"
	pkggit "github.com/jsquirrelz/tide/pkg/git"
)

// Note: JobKind type + JobKindExecutor/JobKindPlanner constants are defined in
// caps.go (Phase 04.1 P1.3) alongside DefaultCaps. This file consumes them.

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

// BuildOptions carries all inputs required to build a Job spec.
// Supports both executor (task) and planner (milestone/phase/plan) Kinds.
// Phase 04.1 P1.2: extended with Kind, ParentObj, Level, and Caps so planner
// Jobs share the full Phase 2 dispatch contract.
type BuildOptions struct {
	// Kind discriminates planner from executor Jobs. Required.
	// JobKindExecutor: task dispatch (Task reconciler).
	// JobKindPlanner: planner dispatch (Milestone/Phase/Plan reconcilers).
	Kind JobKind

	// Task is the Task CRD being dispatched (executor Kind only).
	Task *tidev1alpha3.Task

	// ParentObj is the parent CRD for both Kinds. For JobKindExecutor this is
	// the Task; for JobKindPlanner this is the Milestone, Phase, or Plan.
	// Used for Job name + label derivation (Phase 04.1 P1.2).
	ParentObj metav1.Object

	// Level is the dispatch level string: "project"|"milestone"|"phase"|"plan"|"task".
	// Drives the planner Job name format + level label (Phase 04.1 P1.2). "project"
	// predates this phase (project-level planner dispatch that authors
	// MILESTONE.md) and is documented here rather than left out-of-spec; the D-02
	// subagent.levels rename (internal/controller/dispatch_helpers.go
	// levelOverrideKey) remaps which Levels.<X> override slot each of these five
	// values resolves against — this Level string itself is unchanged by that
	// rename.
	Level string

	// Project is the owning Project (for namespace + ProviderSecretRef).
	Project *tidev1alpha3.Project

	// Attempt is the nth dispatch attempt counter (D-B5).
	Attempt int

	// SignedToken is the HMAC-signed token minted by the controller at Job-create
	// time. Passed to the subagent container as ANTHROPIC_API_KEY and
	// ANTHROPIC_AUTH_TOKEN (D-C1). The proxy validates the token before forwarding.
	SignedToken string

	// EnvelopeInJSON is the marshalled EnvelopeIn that the envelope-writer init
	// container will write to /workspace/envelopes/{parentUID}/in.json.
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

	// Caps applied via DefaultCaps (Phase 04.1 P1.3 — single source of truth
	// for the wall-clock floor; both executor and planner Kinds route through
	// the same helper, preventing token-validity vs Job-deadline drift).
	// nil → DefaultCaps applies the Kind-appropriate floor automatically.
	Caps *tidev1alpha3.Caps

	// PricingOverridesJSON is the D-02 transport: raw validated JSON forwarded
	// opaquely to the subagent container as TIDE_PRICING_OVERRIDES_JSON. The
	// manager does not interpret prices — it passes the validated string through.
	// Non-empty → env var stamped on the container. Empty → env var absent.
	// Populated at the controller call site in Plan 14-05 (not in this plan).
	PricingOverridesJSON string

	// TraceParent is the W3C traceparent string for this level's own subagent
	// dispatch Job, sourced from the IMMEDIATE PARENT's persisted span ID
	// (Phase 43 PROP-01). Empty when there is genuinely no parent span yet
	// available (Project's own dispatch is the sole such case — FormatTraceparent
	// already returns "" for a zero/invalid parent, so no special-case branch
	// is needed at the call site).
	TraceParent string

	// EstimatedCostCents is the D-05 pre-charge estimate in cents. Stamped as
	// label tideproject.k8s/estimated-cost on executor Jobs so that
	// budget.RederiveReservations can restore the in-process ReservationStore
	// after a manager restart (same restart-rederivation pattern as
	// tideproject.k8s/provider-secret-uid for rate-limiter buckets). Omitted
	// when 0 (pre-Phase-14-compatible; RederiveReservations treats absence as
	// 0 reserved — Pitfall 5). Only stamped on JobKindExecutor (executor
	// sessions are the run-1 overshoot source; planner Jobs are gated via
	// the BudgetBlocked condition).
	EstimatedCostCents int64

	// AgentName / AgentEmail are the committer/author identity the in-Job git
	// commit sites stamp on every commit (SIGN-01 / D-03). The controller stamps
	// RESOLVED values here (Project spec → chart value → compiled default) via
	// resolveAgentIdentity — the values are never empty (the compiled default
	// backstops), so injection into the subagent env is UNCONDITIONAL, unlike the
	// operator-gated PricingOverridesJSON transport above.
	AgentName  string
	AgentEmail string
}

// BuildJobSpec returns a complete *batchv1.Job for executor or planner dispatch
// at the given attempt number, ready for client.Create. The caller is responsible
// for invoking internal/owner.EnsureOwnerRef on the returned Job AFTER
// BuildJobSpec returns, per the Phase 1 helper-package-usage rule in PATTERNS.md.
//
// Key contracts:
//   - Job name: deterministic per Kind (executor: JobName, planner: PlannerJobName).
//   - backoffLimit: 0 — retries are controller-side via attempt counter (Pitfall 9).
//   - Two init containers: envelope-writer (standard), tide-credproxy (native sidecar).
//   - Subagent container receives only the signed token, never the raw provider secret (D-C4).
//   - PVC subPath: {project-uid}/workspace enforces per-Project isolation (Blocker #2/#3).
//
// Phase 04.1 P1.2: opts.Kind discriminates planner from executor Kinds. The
// Kind-invariant PodSpec (envelope-writer + credproxy sidecar + subagent container
// with signed-token env + PVC subPath + 3 SecurityContexts + ActiveDeadline via
// DefaultCaps) is shared; per-Kind divergence is Job name + label set only.
func BuildJobSpec(opts BuildOptions) *batchv1.Job {
	// 1. Compute the active deadline.
	//    Phase 04.1 P1.3 fix: route through DefaultCaps so that nil/zero caps
	//    apply the Kind-appropriate floor (executor=300s, planner=600s) —
	//    matches the controller's token mint validity and prevents the 60s
	//    active deadline + 360s token validity drift documented in audit P1.3.
	//    Phase 04.1 P1.2: use opts.Caps + opts.Kind so planner Kinds apply
	//    the 600s planner floor via the Kind-discriminated DefaultCaps API.
	kind := opts.Kind
	if kind == "" {
		kind = JobKindExecutor // backward compat: pre-P1.2 callers that omit Kind
	}
	var capsInput *tidev1alpha3.Caps
	switch kind {
	case JobKindExecutor:
		if opts.Task != nil {
			capsInput = opts.Task.Spec.Caps
		}
	default:
		capsInput = opts.Caps
	}
	caps := DefaultCaps(capsInput, kind)
	wallClockSeconds := int64(caps.WallClockSeconds)
	activeDeadline := wallClockSeconds + DefaultWallClockGraceSeconds

	// 2. Build labels + Job name — the only per-Kind divergence (Phase 04.1 P1.2).
	//    Shared label: project-uid, attempt, provider-secret-uid.
	//    Kind-specific: role, level, <level>-uid, task-uid (planner) or task-uid (executor).
	//    Planner pods carry BOTH tideproject.k8s/<level>-uid AND tideproject.k8s/task-uid
	//    (set to parentUID) so that PodStatusEnvelopeReader.ReadOut, which queries by
	//    task-uid, finds planner pods without a separate label key per level.
	var jobName string
	var parentUID string
	labels := map[string]string{
		owner.LabelAttempt:                    fmt.Sprintf("%d", opts.Attempt),
		"tideproject.k8s/provider-secret-uid": opts.SecretUID,
	}
	switch kind {
	case JobKindPlanner:
		if opts.ParentObj != nil {
			parentUID = string(opts.ParentObj.GetUID())
		}
		jobName = PlannerJobName(opts.Level, parentUID, opts.Attempt)
		labels["tideproject.k8s/role"] = "planner"
		labels["tideproject.k8s/level"] = opts.Level
		if opts.Level != "" && parentUID != "" {
			labels[fmt.Sprintf("tideproject.k8s/%s-uid", opts.Level)] = parentUID
			// PodStatusEnvelopeReader queries by task-uid; planner pods also carry
			// this label (keyed by parentUID) so the shared reader finds them.
			labels["tideproject.k8s/task-uid"] = parentUID
		}
	default: // JobKindExecutor (and legacy callers with Kind=="")
		if opts.Task != nil {
			parentUID = string(opts.Task.UID)
		}
		jobName = JobName(opts.Task.UID, opts.Attempt)
		labels["tideproject.k8s/task-uid"] = string(opts.Task.UID)
		labels["tideproject.k8s/role"] = "executor"
		// D-05: stamp estimated-cost label for restart rederivation via
		// budget.RederiveReservations. Omit when 0 (pre-Phase-14-compatible;
		// absence treated as 0 reserved — Pitfall 5).
		if opts.EstimatedCostCents > 0 {
			labels["tideproject.k8s/estimated-cost"] = strconv.FormatInt(opts.EstimatedCostCents, 10)
		}
	}

	// 3. Compute the PVC subPath for per-Project isolation (Blocker #2/#3).
	// subPath must be a relative path (K8s rejects absolute). When ProjectUID is empty
	// (planner Job created before Project resolves), use parentUID as the path prefix
	// so the path is still relative and unique. In production ProjectUID is always set.
	subPathPrefix := opts.ProjectUID
	if subPathPrefix == "" {
		subPathPrefix = parentUID
	}
	subPath := subPathPrefix + "/workspace"

	// 4. Determine the parent UID to use in the envelope-writer env var.
	//    For executors this is task UID; for planners it is the parent CRD UID.
	//    The env var drives the in.json path inside the PVC slice.
	envelopeUID := parentUID

	// 5. Build envelope-writer init container (standard init — NOT a sidecar).
	// Writes EnvelopeIn JSON to /workspace/envelopes/{uid}/in.json via base64-decode.
	envelopeB64 := base64.StdEncoding.EncodeToString(opts.EnvelopeInJSON)
	envelopeWriter := corev1.Container{
		Name:  ContainerNameEnvelopeWriter,
		Image: "busybox:stable",
		Command: []string{"sh", "-c",
			`mkdir -p /workspace/envelopes/${TIDE_TASK_UID} && echo "$ENVELOPE_IN_B64" | base64 -d > /workspace/envelopes/${TIDE_TASK_UID}/in.json`,
		},
		Env: []corev1.EnvVar{
			{Name: "TIDE_TASK_UID", Value: envelopeUID},
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
			RunAsUser:                new(int64(1000)),
			RunAsGroup:               new(int64(1000)),
			RunAsNonRoot:             new(true),
			AllowPrivilegeEscalation: new(false),
			ReadOnlyRootFilesystem:   new(true),
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		},
	}

	// 6. Build tide-credproxy init container with RestartPolicy: Always (K8s 1.33 native sidecar).
	// The sidecar MUST have RestartPolicy Always to mark it as a native sidecar — this is the
	// K8s 1.33 marker that causes the Job to complete when the main container exits, while
	// kubelet terminates the sidecar in reverse spec order (RESEARCH.md Pattern 2).
	//
	// cascade-13: credproxy injection is gated on a provider secret. The proxy's job is to
	// firewall + authenticate real provider calls (D-C1); with no ProviderSecretRef there is
	// no upstream to proxy and credproxy's requireEnv("ANTHROPIC_API_KEY") would crash the pod
	// (Init:CrashLoopBackOff). When absent, we skip credproxy AND the cert-shared volume/mount
	// + the subagent's cert env so the PodSpec stays valid (no mount → missing-volume API error)
	// and coherent (no dangling SSL_CERT_FILE pointing at an empty emptyDir). The $0 stub
	// subagent makes no provider call, so dropping the localhost base-url plumbing is harmless.
	credproxyEnabled := opts.Project != nil && opts.Project.Spec.ProviderSecretRef != ""

	credproxy := corev1.Container{
		Name:  ContainerNameCredproxy,
		Image: opts.CredproxyImage,
		// RestartPolicy: Always is the K8s 1.33 native-sidecar marker.
		RestartPolicy: ptr.To(corev1.ContainerRestartPolicyAlways),
		Env: []corev1.EnvVar{
			{Name: "TIDE_TASK_UID", Value: envelopeUID},
			{Name: "TIDE_PROXY_LISTEN", Value: "0.0.0.0:8443"},
			// Phase 04.1 P4.2: inject operator-extended allowlist as JSON array.
			// marshalAllowedRoutes returns "[]" when Providers is empty or has no
			// AllowedRoutes — credproxy treats this as baseline-only mode.
			{Name: "TIDE_ALLOWED_ROUTES", Value: func() string {
				if opts.Project != nil {
					return marshalAllowedRoutes(opts.Project.Spec.Providers)
				}
				return "[]"
			}()},
		},
		EnvFrom: func() []corev1.EnvFromSource {
			// Always include the signing-key secret.
			srcs := []corev1.EnvFromSource{
				{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "tide-signing-key"}}},
			}
			// Only add the provider secret when a name is set — K8s rejects empty
			// SecretRef.Name (Required value validation).
			// Guard against nil Project (e.g. planner Jobs created before Project resolves).
			if opts.Project != nil && opts.Project.Spec.ProviderSecretRef != "" {
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
			RunAsUser:                new(int64(1000)),
			RunAsGroup:               new(int64(1000)),
			RunAsNonRoot:             new(true),
			AllowPrivilegeEscalation: new(false),
			ReadOnlyRootFilesystem:   new(true),
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		},
	}

	// 7. Build the main subagent container. The subagent receives only the signed token
	// (not the raw provider secret) — D-C4 secret isolation contract.
	subagentEnv := []corev1.EnvVar{
		{Name: "TIDE_TASK_UID", Value: envelopeUID},
		// D-C1: signed token replaces the real API key in the subagent process.
		{Name: "ANTHROPIC_API_KEY", Value: opts.SignedToken},
		{Name: "ANTHROPIC_AUTH_TOKEN", Value: opts.SignedToken},
		// SIGN-01 / D-03: stamp the resolved agent identity so the in-Job harness
		// commit reads it via pkggit.AgentIdentity(). Unconditional — the controller
		// resolves a non-empty value (compiled default backstops); planner Kinds
		// carry it too (harmless, uniform injection beats kind-discrimination).
		{Name: pkggit.EnvAgentName, Value: opts.AgentName},
		{Name: pkggit.EnvAgentEmail, Value: opts.AgentEmail},
	}
	subagentMounts := []corev1.VolumeMount{
		{
			Name:      VolumeProjectWorkspace,
			MountPath: "/workspace",
			SubPath:   subPath,
		},
	}
	// D-02: stamp TIDE_PRICING_OVERRIDES_JSON only when the operator has configured
	// pricing overrides. Prices are public list rates (T-14-03: accept, not secret);
	// the env var is absent when not configured to keep the PodSpec clean.
	if opts.PricingOverridesJSON != "" {
		subagentEnv = append(subagentEnv, corev1.EnvVar{
			Name:  "TIDE_PRICING_OVERRIDES_JSON",
			Value: opts.PricingOverridesJSON,
		})
	}
	// Phase 43 PROP-01: stamp TRACEPARENT only when the immediate parent's span ID
	// is available. Empty when there is genuinely no parent span (Project's own
	// dispatch is the sole such case). TRACEPARENT is the standard OTel
	// autoinstrumentation env var — the runtime-neutral contract per PROJECT.md.
	if opts.TraceParent != "" {
		subagentEnv = append(subagentEnv, corev1.EnvVar{
			Name:  "TRACEPARENT",
			Value: opts.TraceParent,
		})
	}
	// cascade-13: only wire the localhost-credproxy plumbing (base-url + cert trust + cert
	// mount) when credproxy is actually injected. Without it, SSL_CERT_FILE would point at an
	// empty emptyDir and the cert mount would reference a removed volume (hard API error).
	if credproxyEnabled {
		subagentEnv = append(subagentEnv,
			// D-C1: subagent points to the localhost credproxy — never reaches Anthropic directly.
			corev1.EnvVar{Name: "ANTHROPIC_BASE_URL", Value: "https://127.0.0.1:8443"},
			// Trust the sidecar's self-signed cert for both Node.js and Go runtimes.
			corev1.EnvVar{Name: "SSL_CERT_FILE", Value: "/etc/tide/proxy/ca.crt"},
			corev1.EnvVar{Name: "NODE_EXTRA_CA_CERTS", Value: "/etc/tide/proxy/ca.crt"},
		)
		subagentMounts = append(subagentMounts, corev1.VolumeMount{
			Name:      VolumeCertShared,
			MountPath: "/etc/tide/proxy",
			ReadOnly:  true,
		})
	}
	subagent := corev1.Container{
		Name:         ContainerNameSubagent,
		Image:        opts.SubagentImage,
		WorkingDir:   "/workspace",
		Env:          subagentEnv,
		VolumeMounts: subagentMounts,
		// D-A4: subagent has zero K8s verbs — no EnvFrom from K8s Secrets.
		// D-C4: raw API key is NOT in subagent's env or EnvFrom.
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:                new(int64(1000)),
			RunAsGroup:               new(int64(1000)),
			RunAsNonRoot:             new(true),
			AllowPrivilegeEscalation: new(false),
			// Note: ReadOnlyRootFilesystem is false for subagent — it writes to /workspace.
			ReadOnlyRootFilesystem: new(false),
			Capabilities:           &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		},
	}

	// 8. Compose the PodSpec.
	// cascade-13: credproxy (native sidecar) and the cert-shared emptyDir it produces are
	// included together or skipped together. credproxy is the sole producer of the cert
	// volume; gating both keeps the PodSpec valid (no mount references a removed volume).
	initContainers := []corev1.Container{envelopeWriter}
	volumes := []corev1.Volume{
		{
			Name: VolumeProjectWorkspace,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: opts.PVCName,
				},
			},
		},
	}
	if credproxyEnabled {
		initContainers = append(initContainers, credproxy)
		volumes = append(volumes, corev1.Volume{
			Name: VolumeCertShared,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	}
	podSpec := corev1.PodSpec{
		ServiceAccountName: ServiceAccountSubagent,
		SecurityContext: &corev1.PodSecurityContext{
			FSGroup: new(int64(1000)),
			// RunAsUser MUST accompany RunAsGroup at the pod level — the docker/CRI
			// runtime rejects the sandbox ("runAsGroup is specified without a
			// runAsUser") otherwise. Matches the per-container RunAsUser=1000.
			RunAsUser: new(int64(1000)),
			// RunAsGroup pins the primary gid to 1000 (matching fsGroup) so files
			// the executor authors on the shared PVC are group-owned 1000, not gid
			// 0. Without it, RunAsUser=1000 alone leaves the primary group at root,
			// and the tide-push integration/push Job (uid 65532, member of group
			// 1000 via fsGroup) cannot write the executor's gid-0 files
			// (repo.git objects, /workspace/envelopes). Cross-uid PVC sharing needs
			// a shared PRIMARY group, which fsGroup (supplemental only) does not give.
			RunAsGroup: new(int64(1000)),
		},
		InitContainers: initContainers,
		Containers:     []corev1.Container{subagent},
		Volumes:        volumes,
		RestartPolicy:  corev1.RestartPolicyNever,
	}

	// 9. Compose the Job.
	// Derive namespace: use Project.Namespace when present; otherwise fall back
	// to ParentObj.GetNamespace() (planner Jobs with no Project resolved yet).
	jobNamespace := ""
	if opts.Project != nil {
		jobNamespace = opts.Project.Namespace
	} else if opts.ParentObj != nil {
		jobNamespace = opts.ParentObj.GetNamespace()
	}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: jobNamespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            new(int32(0)),
			TTLSecondsAfterFinished: new(int32(DefaultTTLSecondsAfterFinished)),
			ActiveDeadlineSeconds:   new(activeDeadline),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: podSpec,
			},
		},
	}
}

// routeJSON is the wire format for a single allowlist entry serialized into
// the TIDE_ALLOWED_ROUTES env var (Phase 04.1 P4.2). The field names must
// match the credproxy.RouteSpec JSON tags — the two types are parallel by
// design (import-firewall: jobspec.go may import api/v1alpha3; credproxy
// may not).
type routeJSON struct {
	Method     string `json:"method"`
	PathPrefix string `json:"pathPrefix"`
}

// marshalAllowedRoutes serializes the flattened AllowedRoutes from all
// Project.Spec.Providers entries into a JSON array for the TIDE_ALLOWED_ROUTES
// env var consumed by the credproxy sidecar (Phase 04.1 P4.2).
//
// Returns "[]" on empty input — credproxy unmarshals this into an empty slice
// and only the hardcoded baseline allowlist applies.
func marshalAllowedRoutes(providers []tidev1alpha3.ProviderConfig) string {
	var routes []routeJSON
	for _, p := range providers {
		for _, r := range p.AllowedRoutes {
			routes = append(routes, routeJSON{Method: r.Method, PathPrefix: r.PathPrefix})
		}
	}
	if len(routes) == 0 {
		return "[]"
	}
	b, err := json.Marshal(routes)
	if err != nil {
		return "[]" // safe default — credproxy treats as empty
	}
	return string(b)
}
