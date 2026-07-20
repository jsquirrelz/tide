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
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	pkggit "github.com/jsquirrelz/tide/pkg/git"
)

// buildTestOptions constructs a minimal BuildOptions for executor Kind tests.
func buildTestOptions() BuildOptions {
	task := &tidev1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "task-alpha",
			Namespace: "default",
			UID:       types.UID("task-uid-test"),
		},
		Spec: tidev1alpha3.TaskSpec{
			PlanRef:             "plan-alpha",
			FilesTouched:        []string{"foo.go"},
			DeclaredOutputPaths: []string{"out.json"},
			Caps: &tidev1alpha3.Caps{
				WallClockSeconds: 300,
			},
		},
	}
	project := &tidev1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "project-alpha",
			Namespace: "default",
			UID:       types.UID("project-uid-test"),
		},
		Spec: tidev1alpha3.ProjectSpec{SchemaRevision: "v1alpha3",
			TargetRepo:        "https://github.com/example/repo",
			ProviderSecretRef: "provider-secret-alpha",
		},
	}
	return BuildOptions{
		Kind:           JobKindExecutor,
		Task:           task,
		ParentObj:      task,
		Level:          "task",
		Project:        project,
		Attempt:        1,
		SignedToken:    "test-signed-token",
		EnvelopeInJSON: []byte(`{"apiVersion":"dispatch.tideproject.k8s/v1alpha1","kind":"TaskEnvelopeIn"}`),
		SubagentImage:  "ghcr.io/jsquirrelz/tide-stub-subagent:test",
		CredproxyImage: "ghcr.io/jsquirrelz/tide-credproxy:test",
		SecretUID:      "secret-uid-test",
		PVCName:        "tide-projects",
		ProjectUID:     "project-uid-test",
	}
}

// buildPlannerTestOptions constructs a minimal BuildOptions for planner Kind tests.
// Covers milestone-level dispatch (Phase 04.1 P1.2).
func buildPlannerTestOptions() BuildOptions {
	ms := &tidev1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "milestone-alpha",
			Namespace: "default",
			UID:       types.UID("milestone-uid-test"),
		},
	}
	project := &tidev1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "project-alpha",
			Namespace: "default",
			UID:       types.UID("project-uid-test"),
		},
		Spec: tidev1alpha3.ProjectSpec{SchemaRevision: "v1alpha3",
			TargetRepo:        "https://github.com/example/repo",
			ProviderSecretRef: "provider-secret-alpha",
		},
	}
	return BuildOptions{
		Kind:           JobKindPlanner,
		ParentObj:      ms,
		Level:          "milestone",
		Project:        project,
		Attempt:        1,
		SignedToken:    "test-planner-signed-token",
		EnvelopeInJSON: []byte(`{"apiVersion":"dispatch.tideproject.k8s/v1alpha1","kind":"TaskEnvelopeIn","role":"planner"}`),
		SubagentImage:  "ghcr.io/jsquirrelz/tide-stub-subagent:test",
		CredproxyImage: "ghcr.io/jsquirrelz/tide-credproxy:test",
		SecretUID:      "secret-uid-test",
		PVCName:        "tide-projects",
		ProjectUID:     "project-uid-test",
	}
}

// buildVerifierTestOptions constructs BuildOptions for JobKindVerifier
// dispatch tests (Phase 51 TASK-04/ESC-04) — same Task/Project fixture as
// the executor options, ReadOnly:true (the verifier's structural read-only
// contract), plus a GateCommand.
func buildVerifierTestOptions() BuildOptions {
	opts := buildTestOptions()
	opts.Kind = JobKindVerifier
	opts.ReadOnly = true
	opts.GateCommand = "make test-int"
	return opts
}

// buildNoSecretTestOptions constructs executor BuildOptions for a Project with NO
// ProviderSecretRef — the $0 acceptance / stub path (cascade-13). credproxy must be
// absent in this configuration.
func buildNoSecretTestOptions() BuildOptions {
	opts := buildTestOptions()
	// Clone the Project so we don't mutate the shared fixture, and clear the secret ref.
	project := *opts.Project
	project.Spec.ProviderSecretRef = ""
	opts.Project = &project
	opts.SecretUID = ""
	return opts
}

// validatePodSpecVolumeMountRefs asserts that every VolumeMount in every container and
// initContainer references a volume that is declared in spec.Volumes. A mount pointing at
// a missing volume is a hard K8s API validation error — this catches the cascade-13 failure
// mode where gating credproxy could orphan the cert-shared mount.
func validatePodSpecVolumeMountRefs(t *testing.T, spec corev1.PodSpec) {
	t.Helper()
	declared := map[string]bool{}
	for _, v := range spec.Volumes {
		declared[v.Name] = true
	}
	check := func(c corev1.Container) {
		for _, vm := range c.VolumeMounts {
			if !declared[vm.Name] {
				t.Errorf("container %q mounts volume %q which is not declared in spec.Volumes (invalid PodSpec)", c.Name, vm.Name)
			}
		}
	}
	for _, c := range spec.InitContainers {
		check(c)
	}
	for _, c := range spec.Containers {
		check(c)
	}
}

func TestBuildJobSpec_Name_FollowsDeterministicFormat(t *testing.T) {
	opts := buildTestOptions()
	job := BuildJobSpec(opts)
	want := JobName(opts.Task.UID, opts.Attempt)
	if job.Name != want {
		t.Errorf("job.Name = %q; want %q", job.Name, want)
	}
}

func TestBuildJobSpec_LabelsContainAllFour(t *testing.T) {
	opts := buildTestOptions()
	job := BuildJobSpec(opts)
	labels := job.Labels
	keys := []string{
		"tideproject.k8s/task-uid",
		"tideproject.k8s/attempt",
		"tideproject.k8s/provider-secret-uid",
		"tideproject.k8s/role",
	}
	for _, k := range keys {
		if _, ok := labels[k]; !ok {
			t.Errorf("job.Labels missing key %q", k)
		}
	}
}

func TestBuildJobSpec_HasTwoInitContainers_EnvelopeWriterAndCredproxy(t *testing.T) {
	opts := buildTestOptions()
	job := BuildJobSpec(opts)
	initContainers := job.Spec.Template.Spec.InitContainers
	if len(initContainers) != 2 {
		t.Fatalf("expected 2 initContainers, got %d", len(initContainers))
	}
	if initContainers[0].Name != ContainerNameEnvelopeWriter {
		t.Errorf("initContainers[0].Name = %q; want %q", initContainers[0].Name, ContainerNameEnvelopeWriter)
	}
	if initContainers[1].Name != ContainerNameCredproxy {
		t.Errorf("initContainers[1].Name = %q; want %q", initContainers[1].Name, ContainerNameCredproxy)
	}
}

// TestBuildJobSpec_CredproxySidecarHasRestartPolicyAlways is the LOAD-BEARING assertion
// that verifies K8s 1.33 native sidecar shape (RestartPolicy: Always on initContainer).
func TestBuildJobSpec_CredproxySidecarHasRestartPolicyAlways(t *testing.T) {
	opts := buildTestOptions()
	job := BuildJobSpec(opts)
	initContainers := job.Spec.Template.Spec.InitContainers
	if len(initContainers) < 2 {
		t.Fatalf("expected at least 2 initContainers, got %d", len(initContainers))
	}
	credproxy := initContainers[1]
	if credproxy.RestartPolicy == nil {
		t.Fatal("credproxy initContainer.RestartPolicy is nil; want ContainerRestartPolicyAlways")
	}
	if *credproxy.RestartPolicy != corev1.ContainerRestartPolicyAlways {
		t.Errorf("credproxy initContainer.RestartPolicy = %q; want ContainerRestartPolicyAlways", *credproxy.RestartPolicy)
	}
}

func TestBuildJobSpec_BackoffLimitZero(t *testing.T) {
	opts := buildTestOptions()
	job := BuildJobSpec(opts)
	if job.Spec.BackoffLimit == nil {
		t.Fatal("job.Spec.BackoffLimit is nil; want *0")
	}
	if *job.Spec.BackoffLimit != 0 {
		t.Errorf("job.Spec.BackoffLimit = %d; want 0", *job.Spec.BackoffLimit)
	}
}

func TestBuildJobSpec_ActiveDeadlineSeconds_HasGrace(t *testing.T) {
	opts := buildTestOptions()
	job := BuildJobSpec(opts)
	want := int64(opts.Task.Spec.Caps.WallClockSeconds) + int64(DefaultWallClockGraceSeconds)
	if job.Spec.ActiveDeadlineSeconds == nil {
		t.Fatal("job.Spec.ActiveDeadlineSeconds is nil")
	}
	if *job.Spec.ActiveDeadlineSeconds != want {
		t.Errorf("job.Spec.ActiveDeadlineSeconds = %d; want %d", *job.Spec.ActiveDeadlineSeconds, want)
	}
}

func TestBuildJobSpec_FsGroup1000_RunAsUser1000_OnAllContainers(t *testing.T) {
	opts := buildTestOptions()
	job := BuildJobSpec(opts)
	spec := job.Spec.Template.Spec

	// Pod-level fsGroup
	if spec.SecurityContext == nil || spec.SecurityContext.FSGroup == nil {
		t.Fatal("pod SecurityContext.FSGroup is nil; want 1000")
	}
	if *spec.SecurityContext.FSGroup != 1000 {
		t.Errorf("pod FSGroup = %d; want 1000", *spec.SecurityContext.FSGroup)
	}

	// Pod-level RunAsGroup pins the primary gid to 1000 so executor-authored
	// files on the shared PVC are group-owned 1000 (not gid 0), letting the
	// tide-push integration/push Job (uid 65532, group 1000) write them.
	if spec.SecurityContext.RunAsGroup == nil {
		t.Fatal("pod SecurityContext.RunAsGroup is nil; want 1000")
	}
	if *spec.SecurityContext.RunAsGroup != 1000 {
		t.Errorf("pod RunAsGroup = %d; want 1000", *spec.SecurityContext.RunAsGroup)
	}
	// RunAsUser MUST accompany RunAsGroup at the pod level or the docker/CRI
	// runtime rejects the sandbox ("runAsGroup is specified without a runAsUser").
	if spec.SecurityContext.RunAsUser == nil || *spec.SecurityContext.RunAsUser != 1000 {
		t.Errorf("pod RunAsUser = %v; want 1000 (required alongside RunAsGroup)", spec.SecurityContext.RunAsUser)
	}

	// Check all containers have runAsUser=1000 AND runAsGroup=1000. RunAsGroup
	// must be set at the CONTAINER level too (not only pod level): empirically,
	// a container that sets RunAsUser without RunAsGroup created files as gid 0
	// (root) — breaking cross-uid writes for the tide-push Job. The
	// envelope-writer init container that creates /workspace/envelopes needs gid
	// 1000 so the push Job (uid 65532, group 1000) can write under it.
	containers := append(spec.InitContainers, spec.Containers...)
	for _, c := range containers {
		if c.SecurityContext == nil || c.SecurityContext.RunAsUser == nil {
			t.Errorf("container %q has nil SecurityContext.RunAsUser; want 1000", c.Name)
			continue
		}
		if *c.SecurityContext.RunAsUser != 1000 {
			t.Errorf("container %q RunAsUser = %d; want 1000", c.Name, *c.SecurityContext.RunAsUser)
		}
		if c.SecurityContext.RunAsGroup == nil || *c.SecurityContext.RunAsGroup != 1000 {
			t.Errorf("container %q RunAsGroup = %v; want 1000 (gid-0 files break cross-uid PVC writes)", c.Name, c.SecurityContext.RunAsGroup)
		}
	}
}

func TestBuildJobSpec_SubagentEnvHasAnthropicBaseURL_Pointing_127001_8443(t *testing.T) {
	opts := buildTestOptions()
	job := BuildJobSpec(opts)
	containers := job.Spec.Template.Spec.Containers
	if len(containers) == 0 {
		t.Fatal("no containers in pod spec")
	}
	subagent := containers[0]
	var found bool
	for _, e := range subagent.Env {
		if e.Name == "ANTHROPIC_BASE_URL" {
			found = true
			want := "https://127.0.0.1:8443"
			if e.Value != want {
				t.Errorf("ANTHROPIC_BASE_URL = %q; want %q", e.Value, want)
			}
		}
	}
	if !found {
		t.Error("subagent container is missing ANTHROPIC_BASE_URL env var")
	}
}

// TestBuildJobSpec_SubagentDoesNotReceiveProviderSecret_envFrom is the D-C4
// LOAD-BEARING security gate — verifies the subagent container's EnvFrom does NOT
// contain the providerSecretRef. Only the sidecar gets it.
func TestBuildJobSpec_SubagentDoesNotReceiveProviderSecret_envFrom(t *testing.T) {
	opts := buildTestOptions()
	job := BuildJobSpec(opts)
	containers := job.Spec.Template.Spec.Containers
	if len(containers) == 0 {
		t.Fatal("no containers in pod spec")
	}
	subagent := containers[0]
	for _, ef := range subagent.EnvFrom {
		if ef.SecretRef != nil && ef.SecretRef.Name == opts.Project.Spec.ProviderSecretRef {
			t.Errorf("subagent container EnvFrom contains providerSecretRef %q; D-C4 violation — only sidecar should have it",
				opts.Project.Spec.ProviderSecretRef)
		}
	}
}

// TestBuildJobSpec_CredproxySidecarHasEnvFromTwoSecrets verifies that the credproxy
// initContainer's EnvFrom references both tide-signing-key AND the Project's
// providerSecretRef (D-C4).
func TestBuildJobSpec_CredproxySidecarHasEnvFromTwoSecrets(t *testing.T) {
	opts := buildTestOptions()
	job := BuildJobSpec(opts)
	initContainers := job.Spec.Template.Spec.InitContainers
	if len(initContainers) < 2 {
		t.Fatalf("expected at least 2 initContainers, got %d", len(initContainers))
	}
	credproxy := initContainers[1]

	var hasSigningKey, hasProviderSecret bool
	for _, ef := range credproxy.EnvFrom {
		if ef.SecretRef == nil {
			continue
		}
		if ef.SecretRef.Name == "tide-signing-key" {
			hasSigningKey = true
		}
		if ef.SecretRef.Name == opts.Project.Spec.ProviderSecretRef {
			hasProviderSecret = true
		}
	}
	if !hasSigningKey {
		t.Error("credproxy EnvFrom missing tide-signing-key secret ref")
	}
	if !hasProviderSecret {
		t.Errorf("credproxy EnvFrom missing providerSecretRef %q", opts.Project.Spec.ProviderSecretRef)
	}
}

func TestBuildJobSpec_VolumesIncludeProjectWorkspaceAndCertShared(t *testing.T) {
	opts := buildTestOptions()
	job := BuildJobSpec(opts)
	volumes := job.Spec.Template.Spec.Volumes

	var foundWorkspace, foundCert bool
	for _, v := range volumes {
		if v.Name == VolumeProjectWorkspace {
			foundWorkspace = true
			if v.PersistentVolumeClaim == nil {
				t.Errorf("volume %q has no PVC source", VolumeProjectWorkspace)
			} else if v.PersistentVolumeClaim.ClaimName != opts.PVCName {
				t.Errorf("volume %q claimName = %q; want %q", VolumeProjectWorkspace, v.PersistentVolumeClaim.ClaimName, opts.PVCName)
			}
		}
		if v.Name == VolumeCertShared {
			foundCert = true
			if v.EmptyDir == nil {
				t.Errorf("volume %q is not an emptyDir", VolumeCertShared)
			}
		}
	}
	if !foundWorkspace {
		t.Errorf("volumes missing %q", VolumeProjectWorkspace)
	}
	if !foundCert {
		t.Errorf("volumes missing %q", VolumeCertShared)
	}

	// Verify subagent container's project-workspace volumeMount has correct subPath
	containers := job.Spec.Template.Spec.Containers
	if len(containers) == 0 {
		t.Fatal("no containers in pod spec")
	}
	subagent := containers[0]
	wantSubPath := opts.ProjectUID + "/workspace"
	var foundMount bool
	for _, vm := range subagent.VolumeMounts {
		if vm.Name == VolumeProjectWorkspace {
			foundMount = true
			if vm.SubPath != wantSubPath {
				t.Errorf("subagent volumeMount %q SubPath = %q; want %q", VolumeProjectWorkspace, vm.SubPath, wantSubPath)
			}
		}
	}
	if !foundMount {
		t.Errorf("subagent container missing volumeMount %q", VolumeProjectWorkspace)
	}

	// Verify envelope-writer initContainer also has correct subPath (writes to same slice)
	initContainers := job.Spec.Template.Spec.InitContainers
	if len(initContainers) == 0 {
		t.Fatal("no initContainers in pod spec")
	}
	envelopeWriter := initContainers[0]
	var foundInitMount bool
	for _, vm := range envelopeWriter.VolumeMounts {
		if vm.Name == VolumeProjectWorkspace {
			foundInitMount = true
			if vm.SubPath != wantSubPath {
				t.Errorf("envelope-writer volumeMount %q SubPath = %q; want %q", VolumeProjectWorkspace, vm.SubPath, wantSubPath)
			}
		}
	}
	if !foundInitMount {
		t.Errorf("envelope-writer initContainer missing volumeMount %q", VolumeProjectWorkspace)
	}
}

func TestBuildJobSpec_SubagentWorkingDirIsWorkspace(t *testing.T) {
	opts := buildTestOptions()
	job := BuildJobSpec(opts)
	containers := job.Spec.Template.Spec.Containers
	if len(containers) == 0 {
		t.Fatal("no containers in pod spec")
	}
	subagent := containers[0]
	if subagent.WorkingDir != "/workspace" {
		t.Fatalf("subagent WorkingDir = %q, want /workspace", subagent.WorkingDir)
	}
}

func TestBuildJobSpec_EnvelopeWriterCommand_DecodesB64ToInJson(t *testing.T) {
	opts := buildTestOptions()
	job := BuildJobSpec(opts)
	initContainers := job.Spec.Template.Spec.InitContainers
	if len(initContainers) == 0 {
		t.Fatal("no initContainers")
	}
	ew := initContainers[0]

	// Command should contain base64 -d
	found := false
	for _, arg := range ew.Command {
		if strings.Contains(arg, "base64") {
			found = true
		}
	}
	for _, arg := range ew.Args {
		if strings.Contains(arg, "base64") {
			found = true
		}
	}
	if !found {
		t.Error("envelope-writer command does not contain 'base64'")
	}

	// Should have ENVELOPE_IN_B64 env set to a valid base64 string
	var envB64 string
	for _, e := range ew.Env {
		if e.Name == "ENVELOPE_IN_B64" {
			envB64 = e.Value
			break
		}
	}
	if envB64 == "" {
		t.Fatal("envelope-writer missing ENVELOPE_IN_B64 env var")
	}
	decoded, err := base64.StdEncoding.DecodeString(envB64)
	if err != nil {
		t.Errorf("ENVELOPE_IN_B64 is not valid base64: %v", err)
	}
	if string(decoded) != string(opts.EnvelopeInJSON) {
		t.Errorf("decoded ENVELOPE_IN_B64 = %q; want %q", decoded, opts.EnvelopeInJSON)
	}
}

// ---- Phase 04.1 P1.2: JobKindPlanner tests ----

// TestBuildJobSpec_Planner_NameFollowsPlannerFormat verifies the deterministic
// Job name for a planner dispatch: tide-<level>-<parentUID>-<attempt>.
func TestBuildJobSpec_Planner_NameFollowsPlannerFormat(t *testing.T) {
	opts := buildPlannerTestOptions()
	job := BuildJobSpec(opts)
	want := PlannerJobName("milestone", "milestone-uid-test", 1)
	if job.Name != want {
		t.Errorf("planner job.Name = %q; want %q", job.Name, want)
	}
}

// TestBuildJobSpec_Planner_LabelsContainRolePlannerAndLevel verifies the planner
// label set: role=planner, level=milestone, milestone-uid=<uid>, task-uid=<uid>.
// Planner pods carry task-uid (equal to parentUID) so that PodStatusEnvelopeReader,
// which queries by task-uid, can find planner pods using the same code path as executor
// pods. Both the level-specific label and task-uid are required.
func TestBuildJobSpec_Planner_LabelsContainRolePlannerAndLevel(t *testing.T) {
	opts := buildPlannerTestOptions()
	job := BuildJobSpec(opts)
	labels := job.Labels
	if labels["tideproject.k8s/role"] != "planner" {
		t.Errorf("planner job label role = %q; want \"planner\"", labels["tideproject.k8s/role"])
	}
	if labels["tideproject.k8s/level"] != "milestone" {
		t.Errorf("planner job label level = %q; want \"milestone\"", labels["tideproject.k8s/level"])
	}
	if labels["tideproject.k8s/milestone-uid"] != "milestone-uid-test" {
		t.Errorf("planner job label milestone-uid = %q; want \"milestone-uid-test\"", labels["tideproject.k8s/milestone-uid"])
	}
	// Planner pods MUST also carry task-uid (=parentUID) so PodStatusEnvelopeReader
	// finds them via the shared label query. This dual-label is intentional.
	if labels["tideproject.k8s/task-uid"] != "milestone-uid-test" {
		t.Errorf("planner job label task-uid = %q; want \"milestone-uid-test\" (parentUID for shared reader lookup)", labels["tideproject.k8s/task-uid"])
	}
}

// TestBuildJobSpec_Planner_HasTwoInitContainers_EnvelopeWriterAndCredproxy
// verifies that the Kind-invariant init container topology applies to planner Jobs.
func TestBuildJobSpec_Planner_HasTwoInitContainers_EnvelopeWriterAndCredproxy(t *testing.T) {
	opts := buildPlannerTestOptions()
	job := BuildJobSpec(opts)
	initContainers := job.Spec.Template.Spec.InitContainers
	if len(initContainers) != 2 {
		t.Fatalf("planner job: expected 2 initContainers, got %d", len(initContainers))
	}
	if initContainers[0].Name != ContainerNameEnvelopeWriter {
		t.Errorf("initContainers[0].Name = %q; want %q", initContainers[0].Name, ContainerNameEnvelopeWriter)
	}
	if initContainers[1].Name != ContainerNameCredproxy {
		t.Errorf("initContainers[1].Name = %q; want %q", initContainers[1].Name, ContainerNameCredproxy)
	}
}

// TestBuildJobSpec_Planner_CredproxySidecarHasRestartPolicyAlways verifies that
// the native-sidecar marker applies to planner Jobs as well as executor Jobs.
func TestBuildJobSpec_Planner_CredproxySidecarHasRestartPolicyAlways(t *testing.T) {
	opts := buildPlannerTestOptions()
	job := BuildJobSpec(opts)
	initContainers := job.Spec.Template.Spec.InitContainers
	if len(initContainers) < 2 {
		t.Fatalf("planner job: expected at least 2 initContainers, got %d", len(initContainers))
	}
	credproxy := initContainers[1]
	if credproxy.RestartPolicy == nil {
		t.Fatal("planner credproxy initContainer.RestartPolicy is nil; want ContainerRestartPolicyAlways")
	}
	if *credproxy.RestartPolicy != corev1.ContainerRestartPolicyAlways {
		t.Errorf("planner credproxy RestartPolicy = %q; want ContainerRestartPolicyAlways", *credproxy.RestartPolicy)
	}
}

// TestBuildJobSpec_Planner_PVCMountWithSubPathIsolation verifies that planner
// Jobs mount the PVC with the same {project-uid}/workspace subPath as executor Jobs.
func TestBuildJobSpec_Planner_PVCMountWithSubPathIsolation(t *testing.T) {
	opts := buildPlannerTestOptions()
	job := BuildJobSpec(opts)
	volumes := job.Spec.Template.Spec.Volumes

	var foundWorkspace bool
	for _, v := range volumes {
		if v.Name == VolumeProjectWorkspace {
			foundWorkspace = true
			if v.PersistentVolumeClaim == nil {
				t.Errorf("planner volume %q has no PVC source", VolumeProjectWorkspace)
			} else if v.PersistentVolumeClaim.ClaimName != opts.PVCName {
				t.Errorf("planner PVC claimName = %q; want %q", v.PersistentVolumeClaim.ClaimName, opts.PVCName)
			}
		}
	}
	if !foundWorkspace {
		t.Errorf("planner volumes missing %q", VolumeProjectWorkspace)
	}

	// Verify subagent container uses subPath {project-uid}/workspace
	wantSubPath := opts.ProjectUID + "/workspace"
	containers := job.Spec.Template.Spec.Containers
	if len(containers) == 0 {
		t.Fatal("planner job: no containers")
	}
	subagent := containers[0]
	var foundMount bool
	for _, vm := range subagent.VolumeMounts {
		if vm.Name == VolumeProjectWorkspace {
			foundMount = true
			if vm.SubPath != wantSubPath {
				t.Errorf("planner subagent volumeMount SubPath = %q; want %q", vm.SubPath, wantSubPath)
			}
		}
	}
	if !foundMount {
		t.Errorf("planner subagent container missing volumeMount %q", VolumeProjectWorkspace)
	}
}

// TestBuildJobSpec_Planner_SubagentHasSignedTokenEnv verifies that the planner
// Job's subagent container receives the signed token env (D-C1 contract).
func TestBuildJobSpec_Planner_SubagentHasSignedTokenEnv(t *testing.T) {
	opts := buildPlannerTestOptions()
	job := BuildJobSpec(opts)
	containers := job.Spec.Template.Spec.Containers
	if len(containers) == 0 {
		t.Fatal("planner job: no containers")
	}
	subagent := containers[0]
	var foundToken bool
	for _, e := range subagent.Env {
		if e.Name == "ANTHROPIC_API_KEY" && e.Value != "" {
			foundToken = true
		}
	}
	if !foundToken {
		t.Error("planner subagent container missing ANTHROPIC_API_KEY env var (signed token)")
	}
}

// TestBuildJobSpec_Planner_ActiveDeadlineUsesPlanner600sFloor verifies that when
// no caps are passed the planner floor (600s + grace) is applied via DefaultCaps.
func TestBuildJobSpec_Planner_ActiveDeadlineUsesPlanner600sFloor(t *testing.T) {
	opts := buildPlannerTestOptions()
	opts.Caps = nil // explicitly nil → planner floor applies
	job := BuildJobSpec(opts)
	want := int64(plannerCapsFloorSeconds) + DefaultWallClockGraceSeconds
	if job.Spec.ActiveDeadlineSeconds == nil {
		t.Fatal("planner job.Spec.ActiveDeadlineSeconds is nil")
	}
	if *job.Spec.ActiveDeadlineSeconds != want {
		t.Errorf("planner ActiveDeadlineSeconds = %d; want %d (600s floor + %ds grace)",
			*job.Spec.ActiveDeadlineSeconds, want, DefaultWallClockGraceSeconds)
	}
}

// TestBuildJobSpec_Planner_ContainerTopologyMatchesExecutor verifies that the
// planner Job has the same container count (2 main containers) and init container
// count (2) as the executor Job — the Kind-invariant PodSpec contract.
func TestBuildJobSpec_Planner_ContainerTopologyMatchesExecutor(t *testing.T) {
	plannerJob := BuildJobSpec(buildPlannerTestOptions())
	executorJob := BuildJobSpec(buildTestOptions())

	plannerInits := len(plannerJob.Spec.Template.Spec.InitContainers)
	executorInits := len(executorJob.Spec.Template.Spec.InitContainers)
	if plannerInits != executorInits {
		t.Errorf("init container count mismatch: planner=%d executor=%d (should match Kind-invariant PodSpec)", plannerInits, executorInits)
	}

	plannerContainers := len(plannerJob.Spec.Template.Spec.Containers)
	executorContainers := len(executorJob.Spec.Template.Spec.Containers)
	if plannerContainers != executorContainers {
		t.Errorf("main container count mismatch: planner=%d executor=%d (should match Kind-invariant PodSpec)", plannerContainers, executorContainers)
	}
}

// ---- cascade-13: credproxy injection gated on ProviderSecretRef ----

// TestBuildJobSpec_Credproxy_PresentWhenProviderSecretRefSet verifies that the
// credproxy native sidecar IS injected when the Project carries a ProviderSecretRef,
// and that the cert-shared volume + subagent cert plumbing accompany it.
func TestBuildJobSpec_Credproxy_PresentWhenProviderSecretRefSet(t *testing.T) {
	opts := buildTestOptions() // has ProviderSecretRef set
	if opts.Project.Spec.ProviderSecretRef == "" {
		t.Fatal("test fixture invariant broken: expected a ProviderSecretRef")
	}
	job := BuildJobSpec(opts)
	spec := job.Spec.Template.Spec

	// credproxy initContainer present.
	var hasCredproxy bool
	for _, c := range spec.InitContainers {
		if c.Name == ContainerNameCredproxy {
			hasCredproxy = true
		}
	}
	if !hasCredproxy {
		t.Error("credproxy initContainer should be present when ProviderSecretRef is set")
	}

	// cert-shared volume present.
	var hasCertVolume bool
	for _, v := range spec.Volumes {
		if v.Name == VolumeCertShared {
			hasCertVolume = true
		}
	}
	if !hasCertVolume {
		t.Errorf("volume %q should be present when credproxy is injected", VolumeCertShared)
	}

	// subagent cert mount + ANTHROPIC_BASE_URL present.
	subagent := spec.Containers[0]
	var hasCertMount bool
	for _, vm := range subagent.VolumeMounts {
		if vm.Name == VolumeCertShared {
			hasCertMount = true
		}
	}
	if !hasCertMount {
		t.Errorf("subagent should mount %q when credproxy is injected", VolumeCertShared)
	}
	var hasBaseURL bool
	for _, e := range subagent.Env {
		if e.Name == "ANTHROPIC_BASE_URL" {
			hasBaseURL = true
		}
	}
	if !hasBaseURL {
		t.Error("subagent should have ANTHROPIC_BASE_URL when credproxy is injected")
	}

	validatePodSpecVolumeMountRefs(t, spec)
}

// TestBuildJobSpec_Credproxy_AbsentWhenNoProviderSecretRef verifies the cascade-13 fix:
// with no ProviderSecretRef ($0 stub path), credproxy and the cert-shared volume/mount +
// subagent cert env are all skipped, and the PodSpec remains valid (no orphaned mount).
func TestBuildJobSpec_Credproxy_AbsentWhenNoProviderSecretRef(t *testing.T) {
	opts := buildNoSecretTestOptions()
	if opts.Project.Spec.ProviderSecretRef != "" {
		t.Fatal("test fixture invariant broken: expected empty ProviderSecretRef")
	}
	job := BuildJobSpec(opts)
	spec := job.Spec.Template.Spec

	// Only envelope-writer should remain as an initContainer.
	if len(spec.InitContainers) != 1 {
		t.Fatalf("expected exactly 1 initContainer (envelope-writer only), got %d", len(spec.InitContainers))
	}
	if spec.InitContainers[0].Name != ContainerNameEnvelopeWriter {
		t.Errorf("sole initContainer = %q; want %q", spec.InitContainers[0].Name, ContainerNameEnvelopeWriter)
	}
	for _, c := range spec.InitContainers {
		if c.Name == ContainerNameCredproxy {
			t.Error("credproxy initContainer must be ABSENT when ProviderSecretRef is empty")
		}
	}

	// cert-shared volume must be absent.
	for _, v := range spec.Volumes {
		if v.Name == VolumeCertShared {
			t.Errorf("volume %q must be ABSENT when credproxy is skipped", VolumeCertShared)
		}
	}

	// subagent must NOT mount cert-shared and must NOT carry cert/base-url env.
	subagent := spec.Containers[0]
	for _, vm := range subagent.VolumeMounts {
		if vm.Name == VolumeCertShared {
			t.Errorf("subagent must not mount %q when credproxy is skipped (orphaned mount → invalid PodSpec)", VolumeCertShared)
		}
	}
	for _, e := range subagent.Env {
		if e.Name == "ANTHROPIC_BASE_URL" || e.Name == "SSL_CERT_FILE" || e.Name == "NODE_EXTRA_CA_CERTS" {
			t.Errorf("subagent must not carry %q when credproxy is skipped", e.Name)
		}
	}
	// The signed token env must still be present (subagent identity is unchanged).
	var hasToken bool
	for _, e := range subagent.Env {
		if e.Name == "ANTHROPIC_API_KEY" && e.Value == opts.SignedToken {
			hasToken = true
		}
	}
	if !hasToken {
		t.Error("subagent should still carry ANTHROPIC_API_KEY (signed token) even without credproxy")
	}

	// PodSpec must be valid: no mount references a missing volume.
	validatePodSpecVolumeMountRefs(t, spec)

	// envelope-writer (the surviving init container) must still mount the workspace.
	var hasWorkspaceMount bool
	for _, vm := range spec.InitContainers[0].VolumeMounts {
		if vm.Name == VolumeProjectWorkspace {
			hasWorkspaceMount = true
		}
	}
	if !hasWorkspaceMount {
		t.Errorf("envelope-writer must still mount %q in the no-secret path", VolumeProjectWorkspace)
	}
}

// TestBuildJobSpec_PodSpecValid_BothSecretConfigurations is a belt-and-suspenders check
// that the rendered PodSpec validates (no mount→missing-volume) WITH and WITHOUT a
// ProviderSecretRef, for both executor and planner Kinds.
func TestBuildJobSpec_PodSpecValid_BothSecretConfigurations(t *testing.T) {
	cases := map[string]BuildOptions{
		"executor-with-secret":    buildTestOptions(),
		"executor-without-secret": buildNoSecretTestOptions(),
		"planner-with-secret":     buildPlannerTestOptions(),
		"planner-without-secret": func() BuildOptions {
			o := buildPlannerTestOptions()
			p := *o.Project
			p.Spec.ProviderSecretRef = ""
			o.Project = &p
			return o
		}(),
	}
	for name, opts := range cases {
		t.Run(name, func(t *testing.T) {
			job := BuildJobSpec(opts)
			validatePodSpecVolumeMountRefs(t, job.Spec.Template.Spec)
		})
	}
}

// TestPlannerJobName_Format verifies the tide-<level>-<uid>-<attempt> format.
func TestPlannerJobName_Format(t *testing.T) {
	tests := []struct {
		level    string
		uid      string
		attempt  int
		expected string
	}{
		{"milestone", "abc-123", 1, "tide-milestone-abc-123-1"},
		{"phase", "def-456", 2, "tide-phase-def-456-2"},
		{"plan", "ghi-789", 1, "tide-plan-ghi-789-1"},
	}
	for _, tt := range tests {
		got := PlannerJobName(tt.level, tt.uid, tt.attempt)
		if got != tt.expected {
			t.Errorf("PlannerJobName(%q, %q, %d) = %q; want %q", tt.level, tt.uid, tt.attempt, got, tt.expected)
		}
	}
}

// ---- Phase 14 D-02: TIDE_PRICING_OVERRIDES_JSON env transport ----

// TestBuildJobSpec_PricingOverridesJSON_PresentWhenSet verifies that a non-empty
// BuildOptions.PricingOverridesJSON stamps TIDE_PRICING_OVERRIDES_JSON on both
// JobKindExecutor and JobKindPlanner subagent containers (D-02 transport).
func TestBuildJobSpec_PricingOverridesJSON_PresentWhenSet(t *testing.T) {
	const pricingJSON = `{"claude-haiku-4-5":{"inputCentsPerMTok":200,"outputCentsPerMTok":1000}}`

	cases := map[string]BuildOptions{
		"executor": func() BuildOptions {
			o := buildTestOptions()
			o.PricingOverridesJSON = pricingJSON
			return o
		}(),
		"planner": func() BuildOptions {
			o := buildPlannerTestOptions()
			o.PricingOverridesJSON = pricingJSON
			return o
		}(),
	}

	for name, opts := range cases {
		t.Run(name, func(t *testing.T) {
			job := BuildJobSpec(opts)
			containers := job.Spec.Template.Spec.Containers
			if len(containers) == 0 {
				t.Fatal("no containers in pod spec")
			}
			subagent := containers[0]
			var found bool
			var gotVal string
			for _, e := range subagent.Env {
				if e.Name == "TIDE_PRICING_OVERRIDES_JSON" {
					found = true
					gotVal = e.Value
				}
			}
			if !found {
				t.Errorf("%s: subagent missing TIDE_PRICING_OVERRIDES_JSON env var when PricingOverridesJSON is set", name)
			}
			if gotVal != pricingJSON {
				t.Errorf("%s: TIDE_PRICING_OVERRIDES_JSON = %q; want %q", name, gotVal, pricingJSON)
			}
		})
	}
}

// ---- Phase 36 SIGN-01 (D-03): agent-identity env transport ----

// TestBuildJobSpec_AgentIdentityEnv verifies that BuildOptions.AgentName/AgentEmail
// stamp TIDE_AGENT_NAME/TIDE_AGENT_EMAIL (via pkggit.EnvAgentName/EnvAgentEmail)
// unconditionally on BOTH executor and planner subagent containers, with the exact
// controller-resolved values. Non-default values are used deliberately — the compiled
// default backstop silently masks a missed injection surface (Pitfall 3).
func TestBuildJobSpec_AgentIdentityEnv(t *testing.T) {
	const (
		wantName  = "Custom Agent"
		wantEmail = "custom@example.com"
	)

	cases := map[string]BuildOptions{
		"executor": func() BuildOptions {
			o := buildTestOptions()
			o.AgentName = wantName
			o.AgentEmail = wantEmail
			return o
		}(),
		"planner": func() BuildOptions {
			o := buildPlannerTestOptions()
			o.AgentName = wantName
			o.AgentEmail = wantEmail
			return o
		}(),
	}

	for name, opts := range cases {
		t.Run(name, func(t *testing.T) {
			job := BuildJobSpec(opts)
			containers := job.Spec.Template.Spec.Containers
			if len(containers) == 0 {
				t.Fatal("no containers in pod spec")
			}
			env := map[string]string{}
			for _, e := range containers[0].Env {
				env[e.Name] = e.Value
			}
			if got := env[pkggit.EnvAgentName]; got != wantName {
				t.Errorf("%s: %s = %q; want %q", name, pkggit.EnvAgentName, got, wantName)
			}
			if got := env[pkggit.EnvAgentEmail]; got != wantEmail {
				t.Errorf("%s: %s = %q; want %q", name, pkggit.EnvAgentEmail, got, wantEmail)
			}
		})
	}
}

// ---- Phase 14 D-05: EstimatedCostCents label tests ----

// TestBuildJobSpec_EstimatedCostCents_PresentOnExecutorWhenSet verifies that
// BuildOptions.EstimatedCostCents > 0 stamps tideproject.k8s/estimated-cost on the
// executor Job's labels. This label enables budget.RederiveReservations to restore
// the in-process ReservationStore after a manager restart (D-05 restart rederivation).
func TestBuildJobSpec_EstimatedCostCents_PresentOnExecutorWhenSet(t *testing.T) {
	opts := buildTestOptions()
	opts.EstimatedCostCents = 250
	job := BuildJobSpec(opts)
	got, ok := job.Labels["tideproject.k8s/estimated-cost"]
	if !ok {
		t.Fatal("executor Job labels missing tideproject.k8s/estimated-cost when EstimatedCostCents=250")
	}
	if got != "250" {
		t.Errorf("tideproject.k8s/estimated-cost = %q; want \"250\"", got)
	}
}

// TestBuildJobSpec_EstimatedCostCents_AbsentWhenZero verifies that
// BuildOptions.EstimatedCostCents == 0 does NOT stamp the label (pre-Phase-14
// compatibility — RederiveReservations treats label absence as 0 reserved).
func TestBuildJobSpec_EstimatedCostCents_AbsentWhenZero(t *testing.T) {
	opts := buildTestOptions()
	opts.EstimatedCostCents = 0 // default
	job := BuildJobSpec(opts)
	if _, ok := job.Labels["tideproject.k8s/estimated-cost"]; ok {
		t.Error("executor Job labels must NOT carry tideproject.k8s/estimated-cost when EstimatedCostCents=0")
	}
}

// TestBuildJobSpec_EstimatedCostCents_PresentOnVerifierWhenSet verifies the
// verifier Job also carries tideproject.k8s/estimated-cost so a manager restart
// while the verifier is in-flight can rederive its reservation (TASK-05/ESC-04):
// the verifier shares the executor's per-task reservation key, but the
// terminated executor Job is skipped on rederive, so an unlabeled verifier Job
// would silently drop the reservation.
func TestBuildJobSpec_EstimatedCostCents_PresentOnVerifierWhenSet(t *testing.T) {
	opts := buildVerifierTestOptions()
	opts.EstimatedCostCents = 250
	job := BuildJobSpec(opts)
	got, ok := job.Labels["tideproject.k8s/estimated-cost"]
	if !ok {
		t.Fatal("verifier Job labels missing tideproject.k8s/estimated-cost when EstimatedCostCents=250")
	}
	if got != "250" {
		t.Errorf("tideproject.k8s/estimated-cost = %q; want \"250\"", got)
	}
}

// TestBuildJobSpec_PricingOverridesJSON_AbsentWhenEmpty verifies that an empty
// BuildOptions.PricingOverridesJSON does NOT stamp TIDE_PRICING_OVERRIDES_JSON
// on the subagent container (D-02 transport — clean PodSpec when not configured).
func TestBuildJobSpec_PricingOverridesJSON_AbsentWhenEmpty(t *testing.T) {
	cases := map[string]BuildOptions{
		"executor": buildTestOptions(),        // PricingOverridesJSON is zero value = ""
		"planner":  buildPlannerTestOptions(), // same
	}

	for name, opts := range cases {
		t.Run(name, func(t *testing.T) {
			if opts.PricingOverridesJSON != "" {
				t.Fatalf("%s: test fixture invariant broken: expected empty PricingOverridesJSON", name)
			}
			job := BuildJobSpec(opts)
			containers := job.Spec.Template.Spec.Containers
			if len(containers) == 0 {
				t.Fatal("no containers in pod spec")
			}
			subagent := containers[0]
			for _, e := range subagent.Env {
				if e.Name == "TIDE_PRICING_OVERRIDES_JSON" {
					t.Errorf("%s: subagent should NOT carry TIDE_PRICING_OVERRIDES_JSON when PricingOverridesJSON is empty", name)
				}
			}
		})
	}
}

// ---- Phase 43 PROP-01: TRACEPARENT env transport ----

// TestBuildJobSpec_TraceparentEnvPresentWhenSet verifies that BuildOptions.TraceParent,
// when non-empty, stamps TRACEPARENT on the subagent container with the exact value —
// on both executor and planner Kinds (D-05: all five levels' dispatch Jobs).
func TestBuildJobSpec_TraceparentEnvPresentWhenSet(t *testing.T) {
	const traceParent = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"

	cases := map[string]BuildOptions{
		"executor": func() BuildOptions {
			o := buildTestOptions()
			o.TraceParent = traceParent
			return o
		}(),
		"planner": func() BuildOptions {
			o := buildPlannerTestOptions()
			o.TraceParent = traceParent
			return o
		}(),
	}

	for name, opts := range cases {
		t.Run(name, func(t *testing.T) {
			job := BuildJobSpec(opts)
			containers := job.Spec.Template.Spec.Containers
			if len(containers) == 0 {
				t.Fatal("no containers in pod spec")
			}
			subagent := containers[0]
			var found bool
			var gotVal string
			for _, e := range subagent.Env {
				if e.Name == "TRACEPARENT" {
					found = true
					gotVal = e.Value
				}
			}
			if !found {
				t.Errorf("%s: subagent missing TRACEPARENT env var when TraceParent is set", name)
			}
			if gotVal != traceParent {
				t.Errorf("%s: TRACEPARENT = %q; want %q", name, gotVal, traceParent)
			}
		})
	}
}

// ---- Phase 51 TASK-04/ESC-04: JobKindVerifier dispatch wiring ----

// TestBuildJobSpec_Verifier_NameUsesVerifierJobName verifies that
// Kind=JobKindVerifier produces the deterministic tide-verifier-<uid>-<attempt>
// name (VerifierJobName), grep-distinct from the executor's
// tide-task-<uid>-<attempt> form — Plan 06 only needs to set opts.Kind for
// this to take effect (caller-ready).
func TestBuildJobSpec_Verifier_NameUsesVerifierJobName(t *testing.T) {
	opts := buildVerifierTestOptions()
	job := BuildJobSpec(opts)
	want := VerifierJobName(opts.Level, string(opts.ParentObj.GetUID()), opts.Attempt)
	if job.Name != want {
		t.Errorf("verifier job.Name = %q; want %q", job.Name, want)
	}
	if job.Name == JobName(opts.Task.UID, opts.Attempt) {
		t.Error("verifier job.Name collides with the executor's JobName form")
	}
}

// TestBuildJobSpec_Verifier_NonTaskParentObj_NoPanic proves the Pitfall-1
// nil-panic regression fix (RESEARCH.md Pitfall 1): a JobKindVerifier
// dispatch for a non-Task level (Task: nil, ParentObj: a Plan) must build
// successfully — not panic on a nil opts.Task dereference — and stamp
// tideproject.k8s/level with the dispatched level.
func TestBuildJobSpec_Verifier_NonTaskParentObj_NoPanic(t *testing.T) {
	opts := buildVerifierTestOptions()
	plan := &tidev1alpha3.Plan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "plan-alpha",
			Namespace: "default",
			UID:       types.UID("plan-uid-test"),
		},
	}
	opts.Task = nil
	opts.ParentObj = plan
	opts.Level = "plan"

	job := BuildJobSpec(opts) // must not panic

	wantName := VerifierJobName("plan", string(plan.UID), opts.Attempt)
	if job.Name != wantName {
		t.Errorf("verifier job.Name = %q; want %q", job.Name, wantName)
	}
	if got := job.Labels["tideproject.k8s/level"]; got != "plan" {
		t.Errorf("verifier job label level = %q; want \"plan\"", got)
	}
	if got := job.Labels["tideproject.k8s/plan-uid"]; got != string(plan.UID) {
		t.Errorf("verifier job label plan-uid = %q; want %q", got, plan.UID)
	}
}

// TestBuildJobSpec_Verifier_RoleLabelIsVerifier verifies ESC-04: a verifier
// Job carries tideproject.k8s/role=verifier (distinct from executor/planner),
// the label-selector target Plan 06's verifierInFlightCount counts against.
func TestBuildJobSpec_Verifier_RoleLabelIsVerifier(t *testing.T) {
	opts := buildVerifierTestOptions()
	job := BuildJobSpec(opts)
	if got := job.Labels["tideproject.k8s/role"]; got != "verifier" {
		t.Errorf("verifier job label role = %q; want \"verifier\"", got)
	}
	if got := job.Labels["tideproject.k8s/task-uid"]; got != string(opts.Task.UID) {
		t.Errorf("verifier job label task-uid = %q; want %q", got, opts.Task.UID)
	}
}

// TestBuildJobSpec_Verifier_UsesVerifyCapsFloor verifies that a verifier Job
// with nil/zero caps applies verifierCapsFloorSeconds (shorter than the
// executor floor per TASK-04), not the executor's 1200s floor.
func TestBuildJobSpec_Verifier_UsesVerifyCapsFloor(t *testing.T) {
	opts := buildVerifierTestOptions()
	opts.Caps = nil
	job := BuildJobSpec(opts)
	want := int64(verifierCapsFloorSeconds) + DefaultWallClockGraceSeconds
	if job.Spec.ActiveDeadlineSeconds == nil {
		t.Fatal("verifier job.Spec.ActiveDeadlineSeconds is nil")
	}
	if *job.Spec.ActiveDeadlineSeconds != want {
		t.Errorf("verifier ActiveDeadlineSeconds = %d; want %d (verify floor + grace)",
			*job.Spec.ActiveDeadlineSeconds, want)
	}
}

// ---- Phase 51 TASK-04: TIDE_GATE_COMMAND env transport ----

// TestBuildJobSpec_GateCommandEnvPresentWhenSet verifies that a non-empty
// BuildOptions.GateCommand stamps TIDE_GATE_COMMAND on the subagent
// container with the exact value (mirrors the PricingOverridesJSON/
// TraceParent conditional-append shape). GateCommand carries ONLY the
// canonical single gateCommand for the LLM's run_gate_command tool — the
// full pass-criteria list travels via the envelope (VerifyContext.Commands),
// not this env var.
func TestBuildJobSpec_GateCommandEnvPresentWhenSet(t *testing.T) {
	opts := buildVerifierTestOptions()
	job := BuildJobSpec(opts)
	containers := job.Spec.Template.Spec.Containers
	if len(containers) == 0 {
		t.Fatal("no containers in pod spec")
	}
	subagent := containers[0]
	var found bool
	var gotVal string
	for _, e := range subagent.Env {
		if e.Name == "TIDE_GATE_COMMAND" {
			found = true
			gotVal = e.Value
		}
	}
	if !found {
		t.Fatal("subagent missing TIDE_GATE_COMMAND env var when GateCommand is set")
	}
	if gotVal != opts.GateCommand {
		t.Errorf("TIDE_GATE_COMMAND = %q; want %q", gotVal, opts.GateCommand)
	}
}

// TestBuildJobSpec_GateCommandEnvAbsentWhenEmpty verifies that an empty
// BuildOptions.GateCommand does NOT stamp TIDE_GATE_COMMAND — non-verifier
// dispatches never carry it.
func TestBuildJobSpec_GateCommandEnvAbsentWhenEmpty(t *testing.T) {
	opts := buildTestOptions() // GateCommand is zero value = ""
	if opts.GateCommand != "" {
		t.Fatal("test fixture invariant broken: expected empty GateCommand")
	}
	job := BuildJobSpec(opts)
	for _, e := range job.Spec.Template.Spec.Containers[0].Env {
		if e.Name == "TIDE_GATE_COMMAND" {
			t.Error("subagent should NOT carry TIDE_GATE_COMMAND when GateCommand is empty")
		}
	}
}

// ---- Phase 51 TASK-04: RW envelopes/ subPath mount (out.json write-back) ----

// TestBuildJobSpec_Verifier_RWEnvelopesSubPathMount verifies the jobspec.go
// :199-204 forward-note resolution: under the ReadOnly verifier variant, the
// subagent gets a SECOND VolumeMount of the SAME project-workspace volume,
// scoped via subPath to envelopes/<uid>/ and read-write, so out.json can be
// written even though /workspace itself stays ReadOnly.
func TestBuildJobSpec_Verifier_RWEnvelopesSubPathMount(t *testing.T) {
	opts := buildVerifierTestOptions()
	job := BuildJobSpec(opts)
	subagent := job.Spec.Template.Spec.Containers[0]

	wantMountPath := "/workspace/envelopes/" + string(opts.Task.UID)
	wantSubPath := opts.ProjectUID + "/workspace/envelopes/" + string(opts.Task.UID)

	var found bool
	for _, vm := range subagent.VolumeMounts {
		if vm.Name == VolumeProjectWorkspace && vm.MountPath == wantMountPath {
			found = true
			if vm.ReadOnly {
				t.Error("envelopes RW mount ReadOnly = true; want false (out.json write-back)")
			}
			if vm.SubPath != wantSubPath {
				t.Errorf("envelopes RW mount SubPath = %q; want %q", vm.SubPath, wantSubPath)
			}
		}
	}
	if !found {
		t.Fatalf("subagent missing RW envelopes/ mount at %q", wantMountPath)
	}

	// The primary /workspace mount must still be ReadOnly (RO worktree preserved).
	var primaryFound bool
	for _, vm := range subagent.VolumeMounts {
		if vm.Name == VolumeProjectWorkspace && vm.MountPath == "/workspace" {
			primaryFound = true
			if !vm.ReadOnly {
				t.Error("primary /workspace mount ReadOnly = false; want true (RO worktree preserved)")
			}
		}
	}
	if !primaryFound {
		t.Fatal("subagent missing primary /workspace mount")
	}

	validatePodSpecVolumeMountRefs(t, job.Spec.Template.Spec)
}

// TestBuildJobSpec_Verifier_RWEnvelopesMountAbsentWhenNotReadOnly verifies
// the RW envelopes/ mount is scoped to the ReadOnly variant only — the
// normal RW executor path already has full /workspace write access and
// does not need a second overlapping mount.
func TestBuildJobSpec_Verifier_RWEnvelopesMountAbsentWhenNotReadOnly(t *testing.T) {
	opts := buildTestOptions() // ReadOnly unset == false
	job := BuildJobSpec(opts)
	subagent := job.Spec.Template.Spec.Containers[0]
	wantMountPath := "/workspace/envelopes/" + string(opts.Task.UID)
	for _, vm := range subagent.VolumeMounts {
		if vm.MountPath == wantMountPath {
			t.Errorf("unexpected envelopes RW mount %q present when ReadOnly=false", wantMountPath)
		}
	}
}

// TestBuildJobSpec_TraceparentEnvAbsentWhenEmpty verifies that an empty
// BuildOptions.TraceParent does NOT stamp TRACEPARENT on any container — zero
// behavior change for the sole genuinely-parentless case (Project's own dispatch).
func TestBuildJobSpec_TraceparentEnvAbsentWhenEmpty(t *testing.T) {
	cases := map[string]BuildOptions{
		"executor": buildTestOptions(),        // TraceParent is zero value = ""
		"planner":  buildPlannerTestOptions(), // same
	}

	for name, opts := range cases {
		t.Run(name, func(t *testing.T) {
			if opts.TraceParent != "" {
				t.Fatalf("%s: test fixture invariant broken: expected empty TraceParent", name)
			}
			job := BuildJobSpec(opts)
			for _, c := range job.Spec.Template.Spec.Containers {
				for _, e := range c.Env {
					if e.Name == "TRACEPARENT" {
						t.Errorf("%s: container %q should NOT carry TRACEPARENT when TraceParent is empty", name, c.Name)
					}
				}
			}
			for _, c := range job.Spec.Template.Spec.InitContainers {
				for _, e := range c.Env {
					if e.Name == "TRACEPARENT" {
						t.Errorf("%s: init container %q should NOT carry TRACEPARENT when TraceParent is empty", name, c.Name)
					}
				}
			}
		})
	}
}

// ---- Phase 52 D-07: level-verify worktree-checkout init container ----

// TestBuildJobSpec_Verifier_WorktreeCheckout covers the three statics named
// in 52-05-PLAN.md Task 2 acceptance criteria: (a) present — fields set on a
// JobKindVerifier dispatch composes a second init container after
// envelope-writer, workspace mounted RW in it, main container workspace
// still ReadOnly; (b) credential-absence — no ANTHROPIC_API_KEY / secret
// refs on the checkout container; (c) absent — a task-shaped dispatch
// (fields empty) has no worktree-checkout container.
//
// buildVerifierTestOptions/buildTestOptions carry a ProviderSecretRef, so
// credproxy is ALSO present in these fixtures (pre-existing
// TestBuildJobSpec_HasTwoInitContainers_EnvelopeWriterAndCredproxy
// precedent) — assertions below check container identity/position, not a
// raw total count that would conflate the two orthogonal gates.
func TestBuildJobSpec_Verifier_WorktreeCheckout(t *testing.T) {
	t.Run("present: second init container after envelope-writer, RW workspace, main stays RO", func(t *testing.T) {
		opts := buildVerifierTestOptions()
		opts.WorktreeCheckoutImage = "ghcr.io/jsquirrelz/tide-push:test"
		opts.WorktreeBranch = "tide/run-alpha-1"
		job := BuildJobSpec(opts)

		initContainers := job.Spec.Template.Spec.InitContainers
		if len(initContainers) != 3 {
			t.Fatalf("initContainers count = %d; want 3 (envelope-writer, worktree-checkout, credproxy)", len(initContainers))
		}
		if initContainers[0].Name != ContainerNameEnvelopeWriter {
			t.Errorf("initContainers[0].Name = %q; want %q", initContainers[0].Name, ContainerNameEnvelopeWriter)
		}
		checkout := initContainers[1]
		if checkout.Name != "worktree-checkout" {
			t.Errorf("initContainers[1].Name = %q; want \"worktree-checkout\"", checkout.Name)
		}
		if checkout.Image != opts.WorktreeCheckoutImage {
			t.Errorf("checkout.Image = %q; want %q", checkout.Image, opts.WorktreeCheckoutImage)
		}

		argsJoined := strings.Join(checkout.Args, " ")
		wantParentUID := string(opts.Task.UID)
		for _, want := range []string{
			"--mode=worktree-checkout",
			"--uid=" + wantParentUID,
			"--branch=" + opts.WorktreeBranch,
		} {
			if !strings.Contains(argsJoined, want) {
				t.Errorf("checkout.Args = %q; missing %q", argsJoined, want)
			}
		}

		var wsMount *corev1.VolumeMount
		for i := range checkout.VolumeMounts {
			if checkout.VolumeMounts[i].Name == VolumeProjectWorkspace {
				wsMount = &checkout.VolumeMounts[i]
			}
		}
		if wsMount == nil {
			t.Fatal("worktree-checkout container missing project-workspace VolumeMount")
		}
		if wsMount.MountPath != "/workspace" {
			t.Errorf("checkout workspace MountPath = %q; want \"/workspace\"", wsMount.MountPath)
		}
		if wsMount.ReadOnly {
			t.Error("checkout workspace VolumeMount ReadOnly = true; want false (RW — git worktree add must write)")
		}

		// The verifier's own MAIN container /workspace mount stays ReadOnly —
		// unaffected by the new init container's RW mount of the same volume.
		subagent := job.Spec.Template.Spec.Containers[0]
		var mainWSMount *corev1.VolumeMount
		for i := range subagent.VolumeMounts {
			if subagent.VolumeMounts[i].Name == VolumeProjectWorkspace && subagent.VolumeMounts[i].MountPath == "/workspace" {
				mainWSMount = &subagent.VolumeMounts[i]
			}
		}
		if mainWSMount == nil {
			t.Fatal("subagent missing primary /workspace mount")
		}
		if !mainWSMount.ReadOnly {
			t.Error("subagent primary /workspace mount ReadOnly = false; want true (RO worktree preserved)")
		}

		validatePodSpecVolumeMountRefs(t, job.Spec.Template.Spec)
	})

	t.Run("credential-absence: no ANTHROPIC_API_KEY or secret refs on the checkout container", func(t *testing.T) {
		opts := buildVerifierTestOptions()
		opts.WorktreeCheckoutImage = "ghcr.io/jsquirrelz/tide-push:test"
		opts.WorktreeBranch = "tide/run-alpha-1"
		job := BuildJobSpec(opts)

		checkout := job.Spec.Template.Spec.InitContainers[1]
		for _, e := range checkout.Env {
			if e.Name == "ANTHROPIC_API_KEY" || e.Name == "ANTHROPIC_AUTH_TOKEN" {
				t.Errorf("worktree-checkout container carries credential env %q", e.Name)
			}
		}
		if len(checkout.EnvFrom) != 0 {
			t.Errorf("worktree-checkout container has %d EnvFrom source(s); want 0 (no secret refs)", len(checkout.EnvFrom))
		}
	})

	t.Run("absent: task-shaped dispatch (fields empty, no provider secret) has exactly one init container", func(t *testing.T) {
		opts := buildNoSecretTestOptions() // WorktreeCheckoutImage/WorktreeBranch zero value = ""; no credproxy either
		if opts.WorktreeCheckoutImage != "" || opts.WorktreeBranch != "" {
			t.Fatal("test fixture invariant broken: expected empty WorktreeCheckoutImage/WorktreeBranch")
		}
		job := BuildJobSpec(opts)
		initContainers := job.Spec.Template.Spec.InitContainers
		if len(initContainers) != 1 {
			t.Fatalf("initContainers count = %d; want 1 (envelope-writer only)", len(initContainers))
		}
		if initContainers[0].Name != ContainerNameEnvelopeWriter {
			t.Errorf("initContainers[0].Name = %q; want %q", initContainers[0].Name, ContainerNameEnvelopeWriter)
		}
	})

	t.Run("absent: JobKindVerifier with fields empty never composes a worktree-checkout container", func(t *testing.T) {
		opts := buildVerifierTestOptions() // WorktreeCheckoutImage/WorktreeBranch zero value = ""
		job := BuildJobSpec(opts)
		for _, c := range job.Spec.Template.Spec.InitContainers {
			if c.Name == "worktree-checkout" {
				t.Fatal("worktree-checkout container present despite empty WorktreeCheckoutImage/WorktreeBranch")
			}
		}
	})
}

// TestBuildJobSpec_WorktreeCheckoutAbsentOnPlannerEvenIfFieldsSet proves the
// gate is Kind==JobKindVerifier, not merely "fields set" — a planner
// dispatch never gets a worktree-checkout container even if a caller
// mistakenly populated the fields.
func TestBuildJobSpec_WorktreeCheckoutAbsentOnPlannerEvenIfFieldsSet(t *testing.T) {
	opts := buildPlannerTestOptions()
	opts.WorktreeCheckoutImage = "ghcr.io/jsquirrelz/tide-push:test"
	opts.WorktreeBranch = "tide/run-alpha-1"
	job := BuildJobSpec(opts)
	for _, c := range job.Spec.Template.Spec.InitContainers {
		if c.Name == "worktree-checkout" {
			t.Fatal("planner dispatch composed a worktree-checkout init container; want none (Kind must be JobKindVerifier)")
		}
	}
}
