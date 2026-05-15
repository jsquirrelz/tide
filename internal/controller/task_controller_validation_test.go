package controller

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

func TestValidateControllerOutputPathsSkipsMissingWorkspaceRoot(t *testing.T) {
	missingRoot := filepath.Join(t.TempDir(), "missing-workspace")

	violations, skipped, err := validateControllerOutputPaths(
		missingRoot,
		time.Now().Add(-time.Second),
		[]string{"alpha.go"},
	)
	if err != nil {
		t.Fatalf("validateControllerOutputPaths() error = %v", err)
	}
	if !skipped {
		t.Fatal("validateControllerOutputPaths() skipped = false, want true")
	}
	if len(violations) != 0 {
		t.Fatalf("validateControllerOutputPaths() violations = %v, want none", violations)
	}
}

func TestValidateControllerOutputPathsReportsVisibleViolation(t *testing.T) {
	root := t.TempDir()
	runStart := time.Now().Add(-time.Second)
	leakPath := filepath.Join(root, "escape.txt")
	if err := os.WriteFile(leakPath, []byte("outside declared paths"), 0o644); err != nil {
		t.Fatalf("write leak fixture: %v", err)
	}

	violations, skipped, err := validateControllerOutputPaths(root, runStart, []string{"alpha.go"})
	if err != nil {
		t.Fatalf("validateControllerOutputPaths() error = %v", err)
	}
	if skipped {
		t.Fatal("validateControllerOutputPaths() skipped = true, want false")
	}
	if len(violations) != 1 {
		t.Fatalf("validateControllerOutputPaths() violations = %v, want one violation", violations)
	}
}

func TestConditionReasonFromEnvelopeResultSanitizesKubernetesConditionReason(t *testing.T) {
	validReason := regexp.MustCompile(`^[A-Za-z]([A-Za-z0-9_,:]*[A-Za-z0-9_])?$`)
	tests := []struct {
		name     string
		result   string
		exitCode int
		want     string
	}{
		{name: "output paths violation", result: "output-paths-violation", exitCode: 1, want: "OutputPathsViolation"},
		{name: "forced failure", result: "forced-failure", exitCode: 1, want: "ForcedFailure"},
		{name: "cap hit", result: "cap-hit", exitCode: 1, want: "CapHit"},
		{name: "empty nonzero", result: "", exitCode: 1, want: "NonZeroExitCode"},
		{name: "leading digit", result: "9bad-result!", exitCode: 1, want: "Result9badResult"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := conditionReasonFromEnvelopeResult(tt.result, tt.exitCode)
			if got != tt.want {
				t.Fatalf("conditionReasonFromEnvelopeResult(%q, %d) = %q, want %q", tt.result, tt.exitCode, got, tt.want)
			}
			if !validReason.MatchString(got) {
				t.Fatalf("conditionReasonFromEnvelopeResult(%q, %d) = %q, not a valid metav1.Condition reason", tt.result, tt.exitCode, got)
			}
		})
	}
}
