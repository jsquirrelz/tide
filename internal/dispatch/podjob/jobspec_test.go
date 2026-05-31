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

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

// buildTestOptions constructs a minimal BuildOptions for executor Kind tests.
func buildTestOptions() BuildOptions {
	task := &tidev1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "task-alpha",
			Namespace: "default",
			UID:       types.UID("task-uid-test"),
		},
		Spec: tidev1alpha1.TaskSpec{
			PlanRef:             "plan-alpha",
			FilesTouched:        []string{"foo.go"},
			DeclaredOutputPaths: []string{"out.json"},
			Caps: &tidev1alpha1.Caps{
				WallClockSeconds: 300,
			},
		},
	}
	project := &tidev1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "project-alpha",
			Namespace: "default",
			UID:       types.UID("project-uid-test"),
		},
		Spec: tidev1alpha1.ProjectSpec{
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
		EnvelopeInJSON: []byte(`{"apiVersion":"tideproject.k8s/v1alpha1","kind":"TaskEnvelopeIn"}`),
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
	ms := &tidev1alpha1.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "milestone-alpha",
			Namespace: "default",
			UID:       types.UID("milestone-uid-test"),
		},
	}
	project := &tidev1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "project-alpha",
			Namespace: "default",
			UID:       types.UID("project-uid-test"),
		},
		Spec: tidev1alpha1.ProjectSpec{
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
		EnvelopeInJSON: []byte(`{"apiVersion":"tideproject.k8s/v1alpha1","kind":"TaskEnvelopeIn","role":"planner"}`),
		SubagentImage:  "ghcr.io/jsquirrelz/tide-stub-subagent:test",
		CredproxyImage: "ghcr.io/jsquirrelz/tide-credproxy:test",
		SecretUID:      "secret-uid-test",
		PVCName:        "tide-projects",
		ProjectUID:     "project-uid-test",
	}
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

	// Check all containers have runAsUser=1000
	containers := append(spec.InitContainers, spec.Containers...)
	for _, c := range containers {
		if c.SecurityContext == nil || c.SecurityContext.RunAsUser == nil {
			t.Errorf("container %q has nil SecurityContext.RunAsUser; want 1000", c.Name)
			continue
		}
		if *c.SecurityContext.RunAsUser != 1000 {
			t.Errorf("container %q RunAsUser = %d; want 1000", c.Name, *c.SecurityContext.RunAsUser)
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
