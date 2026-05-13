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

// buildTestOptions constructs a minimal BuildOptions for use in tests.
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
		Task:           task,
		Project:        project,
		Attempt:        1,
		SignedToken:     "test-signed-token",
		EnvelopeInJSON:  []byte(`{"apiVersion":"tideproject.k8s/v1alpha1","kind":"TaskEnvelopeIn"}`),
		SubagentImage:   "ghcr.io/jsquirrelz/tide-stub-subagent:test",
		CredproxyImage:  "ghcr.io/jsquirrelz/tide-credproxy:test",
		SecretUID:       "secret-uid-test",
		PVCName:         "tide-projects",
		ProjectUID:      "project-uid-test",
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
