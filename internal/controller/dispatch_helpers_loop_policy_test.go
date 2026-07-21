/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"os"
	"regexp"
	"strings"
	"testing"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// ---------- resolveVerifierModel tests (Phase 53 D-02, pure function — no envtest) ----------

// TestResolveVerifierModel pins D-02's two-real-tier precedence (chart
// default > borrow the level executor's model) at all three verifier
// dispatch levels this phase wires it into — the must_haves truth "When
// subagent.verify.model is set, all three verifier dispatch tiers use it
// instead of borrowing the level executor's model; when empty, behavior is
// byte-identical to today."
func TestResolveVerifierModel(t *testing.T) {
	t.Run("chart Model set -> chart value wins over the borrowed executor model", func(t *testing.T) {
		for _, level := range []string{"task", "plan", "phase"} {
			t.Run(level, func(t *testing.T) {
				project := &tideprojectv1alpha3.Project{Spec: tideprojectv1alpha3.ProjectSpec{
					Subagent: tideprojectv1alpha3.SubagentConfig{Model: "claude-sonnet-4-6"},
				}}
				chart := VerifyDefaults{Model: "claude-opus-4-7"}
				got := resolveVerifierModel(project, level, chart, ProviderDefaults{})
				if got != "claude-opus-4-7" {
					t.Errorf("resolveVerifierModel() = %q, want %q (chart tier wins)", got, "claude-opus-4-7")
				}
			})
		}
	})

	t.Run("chart Model empty -> byte-identical to the pre-Phase-53 ResolveProvider(...).Model borrow", func(t *testing.T) {
		project := &tideprojectv1alpha3.Project{Spec: tideprojectv1alpha3.ProjectSpec{
			Subagent: tideprojectv1alpha3.SubagentConfig{Model: "claude-sonnet-4-6"},
		}}
		want := ResolveProvider(project, "task", ProviderDefaults{}).Model
		got := resolveVerifierModel(project, "task", VerifyDefaults{}, ProviderDefaults{})
		if got != want {
			t.Errorf("resolveVerifierModel() = %q, want %q (byte-identical borrow when chart is unset)", got, want)
		}
		if got != "claude-sonnet-4-6" {
			t.Errorf("resolveVerifierModel() = %q, want %q", got, "claude-sonnet-4-6")
		}
	})

	t.Run("chart Model empty, no project model either -> falls through to the Helm default", func(t *testing.T) {
		helm := ProviderDefaults{Models: map[string]string{"task": "claude-haiku-4-5"}}
		got := resolveVerifierModel(nil, "task", VerifyDefaults{}, helm)
		if got != "claude-haiku-4-5" {
			t.Errorf("resolveVerifierModel() = %q, want %q (compiled/Helm fallback tier)", got, "claude-haiku-4-5")
		}
	})
}

// ---------- ResolveLoopPolicy / ResolveVerificationSpec tests (pure function — no envtest) ----------
//
// Phase 52 SC3: ResolveLoopPolicy is THE resolver — one function keyed on
// level, not CRD kind. Each subtest below covers one of the 8 behavior cases
// named in 52-04-PLAN.md Task 1.

func lockedVerification(gateCommand string, maxIterations int32, onExhaustion string) tideprojectv1alpha3.VerificationSpec {
	return tideprojectv1alpha3.VerificationSpec{
		Phase:         "Locked",
		GateCommand:   gateCommand,
		MaxIterations: maxIterations,
		OnExhaustion:  onExhaustion,
	}
}

func TestResolveLoopPolicy(t *testing.T) {
	t.Run("task level, authored MaxIterations 3, onExhaustion unset -> escalate, passthrough", func(t *testing.T) {
		task := &tideprojectv1alpha3.Task{Spec: tideprojectv1alpha3.TaskSpec{
			Verification: lockedVerification("go test ./...", 3, ""),
		}}
		policy := ResolveLoopPolicy(nil, nil, task, "task", VerifyDefaults{})
		if policy.Level != tideprojectv1alpha3.LoopLevelTask {
			t.Errorf("Level = %q, want %q", policy.Level, tideprojectv1alpha3.LoopLevelTask)
		}
		if policy.MaxIterations != 3 {
			t.Errorf("MaxIterations = %d, want 3 (authored passthrough)", policy.MaxIterations)
		}
		if policy.EscalationPolicy != tideprojectv1alpha3.EscalationEscalate {
			t.Errorf("EscalationPolicy = %q, want %q (behavior-preserving default)", policy.EscalationPolicy, tideprojectv1alpha3.EscalationEscalate)
		}
	})

	t.Run("plan level, authored contract, MaxIterations unset -> defaults to 1, escalate", func(t *testing.T) {
		plan := &tideprojectv1alpha3.Plan{Spec: tideprojectv1alpha3.PlanSpec{
			Verification: lockedVerification("make plan-check", 0, ""),
		}}
		policy := ResolveLoopPolicy(nil, plan, nil, "plan", VerifyDefaults{})
		if policy.Level != tideprojectv1alpha3.LoopLevelPlan {
			t.Errorf("Level = %q, want %q", policy.Level, tideprojectv1alpha3.LoopLevelPlan)
		}
		if policy.MaxIterations != 1 {
			t.Errorf("MaxIterations = %d, want 1 (plan default)", policy.MaxIterations)
		}
		if policy.EscalationPolicy != tideprojectv1alpha3.EscalationEscalate {
			t.Errorf("EscalationPolicy = %q, want %q", policy.EscalationPolicy, tideprojectv1alpha3.EscalationEscalate)
		}
	})

	t.Run("phase level, only Project default authored, onExhaustion unset -> MaxIterations 0, requireApproval", func(t *testing.T) {
		project := &tideprojectv1alpha3.Project{Spec: tideprojectv1alpha3.ProjectSpec{
			Verification: tideprojectv1alpha3.VerificationDefaults{
				Phase: func() *tideprojectv1alpha3.VerificationSpec {
					v := lockedVerification("make e2e", 0, "")
					return &v
				}(),
			},
		}}
		policy := ResolveLoopPolicy(project, nil, nil, "phase", VerifyDefaults{})
		if policy.Level != tideprojectv1alpha3.LoopLevelPhase {
			t.Errorf("Level = %q, want %q", policy.Level, tideprojectv1alpha3.LoopLevelPhase)
		}
		if policy.MaxIterations != 0 {
			t.Errorf("MaxIterations = %d, want 0", policy.MaxIterations)
		}
		if policy.EscalationPolicy != tideprojectv1alpha3.EscalationRequireApproval {
			t.Errorf("EscalationPolicy = %q, want %q", policy.EscalationPolicy, tideprojectv1alpha3.EscalationRequireApproval)
		}
	})

	t.Run("phase level, Project default authors MaxIterations 3 -> clamped to 0", func(t *testing.T) {
		project := &tideprojectv1alpha3.Project{Spec: tideprojectv1alpha3.ProjectSpec{
			Verification: tideprojectv1alpha3.VerificationDefaults{
				Phase: func() *tideprojectv1alpha3.VerificationSpec {
					v := lockedVerification("make e2e", 3, "")
					return &v
				}(),
			},
		}}
		policy := ResolveLoopPolicy(project, nil, nil, "phase", VerifyDefaults{})
		if policy.MaxIterations != 0 {
			t.Errorf("MaxIterations = %d, want 0 (D-07: no repair branch at phase level — unconditional clamp)", policy.MaxIterations)
		}
	})

	t.Run("milestone/project levels, authored onExhaustion escalate -> authored value wins over requireApproval default", func(t *testing.T) {
		for _, level := range []string{"milestone", "project"} {
			t.Run(level, func(t *testing.T) {
				var project *tideprojectv1alpha3.Project
				v := lockedVerification("make verify", 0, "escalate")
				switch level {
				case "milestone":
					project = &tideprojectv1alpha3.Project{Spec: tideprojectv1alpha3.ProjectSpec{
						Verification: tideprojectv1alpha3.VerificationDefaults{Milestone: &v},
					}}
				case "project":
					project = &tideprojectv1alpha3.Project{Spec: tideprojectv1alpha3.ProjectSpec{
						Verification: tideprojectv1alpha3.VerificationDefaults{Project: &v},
					}}
				}
				policy := ResolveLoopPolicy(project, nil, nil, level, VerifyDefaults{})
				if policy.EscalationPolicy != tideprojectv1alpha3.EscalationEscalate {
					t.Errorf("EscalationPolicy = %q, want %q (authored value wins)", policy.EscalationPolicy, tideprojectv1alpha3.EscalationEscalate)
				}
				if policy.MaxIterations != 0 {
					t.Errorf("MaxIterations = %d, want 0", policy.MaxIterations)
				}
			})
		}
	})

	t.Run("task level, task spec empty, Project Task default authored -> Project default applies", func(t *testing.T) {
		project := &tideprojectv1alpha3.Project{Spec: tideprojectv1alpha3.ProjectSpec{
			Verification: tideprojectv1alpha3.VerificationDefaults{
				Task: func() *tideprojectv1alpha3.VerificationSpec {
					v := lockedVerification("go vet ./...", 5, "")
					return &v
				}(),
			},
		}}
		task := &tideprojectv1alpha3.Task{} // empty Verification — falls through
		policy := ResolveLoopPolicy(project, nil, task, "task", VerifyDefaults{})
		if policy.MaxIterations != 5 {
			t.Errorf("MaxIterations = %d, want 5 (Project Task default applies)", policy.MaxIterations)
		}
		spec := ResolveVerificationSpec(project, nil, task, "task")
		if spec.GateCommand != "go vet ./..." {
			t.Errorf("GateCommand = %q, want the Project default's command", spec.GateCommand)
		}
	})

	t.Run("no authored contract anywhere -> empty GateCommand (stage off), Level still stamped", func(t *testing.T) {
		policy := ResolveLoopPolicy(nil, nil, nil, "phase", VerifyDefaults{})
		if policy.Level != tideprojectv1alpha3.LoopLevelPhase {
			t.Errorf("Level = %q, want %q (stamped even with no contract)", policy.Level, tideprojectv1alpha3.LoopLevelPhase)
		}
		spec := ResolveVerificationSpec(nil, nil, nil, "phase")
		if spec.GateCommand != "" {
			t.Errorf("GateCommand = %q, want empty (no contract anywhere = stage off)", spec.GateCommand)
		}
	})

	t.Run("plan level: plan.Spec.Verification wins over Project.Spec.Verification.Plan when both authored", func(t *testing.T) {
		project := &tideprojectv1alpha3.Project{Spec: tideprojectv1alpha3.ProjectSpec{
			Verification: tideprojectv1alpha3.VerificationDefaults{
				Plan: func() *tideprojectv1alpha3.VerificationSpec {
					v := lockedVerification("project-default-command", 0, "")
					return &v
				}(),
			},
		}}
		plan := &tideprojectv1alpha3.Plan{Spec: tideprojectv1alpha3.PlanSpec{
			Verification: lockedVerification("plan-own-command", 0, ""),
		}}
		spec := ResolveVerificationSpec(project, plan, nil, "plan")
		if spec.GateCommand != "plan-own-command" {
			t.Errorf("GateCommand = %q, want %q (plan's own spec wins over Project default)", spec.GateCommand, "plan-own-command")
		}
	})

	// ---------- Phase 53 D-04/CFG-02 chart-tier layering (53-06-PLAN.md Task 2) ----------

	t.Run("(a) task level, spec MaxIterations 0, chart task.MaxIterations 3 -> policy 3", func(t *testing.T) {
		task := &tideprojectv1alpha3.Task{Spec: tideprojectv1alpha3.TaskSpec{
			Verification: lockedVerification("go test ./...", 0, ""),
		}}
		chart := VerifyDefaults{Levels: map[string]pkgdispatch.LevelVerifyDefault{
			"task": {MaxIterations: 3},
		}}
		policy := ResolveLoopPolicy(nil, nil, task, "task", chart)
		if policy.MaxIterations != 3 {
			t.Errorf("MaxIterations = %d, want 3 (chart default feeds an unset authored value)", policy.MaxIterations)
		}
	})

	t.Run("(b) task level, authored MaxIterations 2, chart task.MaxIterations 5 -> policy 2 (authored wins)", func(t *testing.T) {
		task := &tideprojectv1alpha3.Task{Spec: tideprojectv1alpha3.TaskSpec{
			Verification: lockedVerification("go test ./...", 2, ""),
		}}
		chart := VerifyDefaults{Levels: map[string]pkgdispatch.LevelVerifyDefault{
			"task": {MaxIterations: 5},
		}}
		policy := ResolveLoopPolicy(nil, nil, task, "task", chart)
		if policy.MaxIterations != 2 {
			t.Errorf("MaxIterations = %d, want 2 (an authored value always wins over the chart default)", policy.MaxIterations)
		}
	})

	t.Run("(c) phase/milestone/project levels: chart MaxIterations 5 never reopens the D-07 clamp -> policy 0", func(t *testing.T) {
		for _, level := range []string{"phase", "milestone", "project"} {
			t.Run(level, func(t *testing.T) {
				v := lockedVerification("make e2e", 0, "")
				project := &tideprojectv1alpha3.Project{Spec: tideprojectv1alpha3.ProjectSpec{Verification: tideprojectv1alpha3.VerificationDefaults{}}}
				switch level {
				case "phase":
					project.Spec.Verification.Phase = &v
				case "milestone":
					project.Spec.Verification.Milestone = &v
				case "project":
					project.Spec.Verification.Project = &v
				}
				chart := VerifyDefaults{Levels: map[string]pkgdispatch.LevelVerifyDefault{
					level: {MaxIterations: 5},
				}}
				policy := ResolveLoopPolicy(project, nil, nil, level, chart)
				if policy.MaxIterations != 0 {
					t.Errorf("MaxIterations = %d, want 0 (the chart must not be able to re-open D-07's clamp)", policy.MaxIterations)
				}
			})
		}
	})

	t.Run("(d) plan level, spec OnExhaustion unset, chart plan.OnExhaustion requireApproval -> requireApproval", func(t *testing.T) {
		plan := &tideprojectv1alpha3.Plan{Spec: tideprojectv1alpha3.PlanSpec{
			Verification: lockedVerification("make plan-check", 0, ""),
		}}
		chart := VerifyDefaults{Levels: map[string]pkgdispatch.LevelVerifyDefault{
			"plan": {OnExhaustion: "requireApproval"},
		}}
		policy := ResolveLoopPolicy(nil, plan, nil, "plan", chart)
		if policy.EscalationPolicy != tideprojectv1alpha3.EscalationRequireApproval {
			t.Errorf("EscalationPolicy = %q, want %q (chart default feeds an unset authored onExhaustion)", policy.EscalationPolicy, tideprojectv1alpha3.EscalationRequireApproval)
		}
	})

	t.Run("(e) no chart entry for the level -> prior subtests' behavior unchanged (regression)", func(t *testing.T) {
		task := &tideprojectv1alpha3.Task{Spec: tideprojectv1alpha3.TaskSpec{
			Verification: lockedVerification("go test ./...", 3, ""),
		}}
		chart := VerifyDefaults{Levels: map[string]pkgdispatch.LevelVerifyDefault{
			"plan": {MaxIterations: 9}, // present for a DIFFERENT level only
		}}
		policy := ResolveLoopPolicy(nil, nil, task, "task", chart)
		if policy.MaxIterations != 3 {
			t.Errorf("MaxIterations = %d, want 3 (no chart entry for \"task\" — authored value unaffected)", policy.MaxIterations)
		}
		if policy.EscalationPolicy != tideprojectv1alpha3.EscalationEscalate {
			t.Errorf("EscalationPolicy = %q, want %q (no chart entry for \"task\" — compiled default unaffected)", policy.EscalationPolicy, tideprojectv1alpha3.EscalationEscalate)
		}
	})
}

// ---------- SC3 static guard ----------

// TestNoDirectVerificationPolicyReads is the SC3 static guard (T-52-11): no
// controller may read Spec.Verification.MaxIterations/.OnExhaustion directly
// — every read MUST route through ResolveLoopPolicy so gate policy resolves
// from ONE place, keyed on level.
//
// Walks every internal/controller/*.go source file (excluding _test.go files
// and dispatch_helpers.go itself, the resolver's own home), strips
// line-comment content (drop everything after "//" on each line) so doc
// prose referencing the forbidden field names cannot self-invalidate the
// guard, then asserts zero matches of the forbidden regex.
//
// Phase 52-06: task_controller.go's repairOrHalt now reads MaxIterations
// exclusively through ResolveLoopPolicy — the guard covers the whole
// package except dispatch_helpers.go (the resolver's own home).
func TestNoDirectVerificationPolicyReads(t *testing.T) {
	dir := "."
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read internal/controller dir: %v", err)
	}

	forbidden := regexp.MustCompile(`Spec\.Verification\.(MaxIterations|OnExhaustion)`)
	excluded := map[string]bool{
		"dispatch_helpers.go": true, // the resolver's own home
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if excluded[name] {
			continue
		}
		data, rErr := os.ReadFile(name)
		if rErr != nil {
			t.Fatalf("read %s: %v", name, rErr)
		}
		stripped := stripGoLineComments(string(data))
		if forbidden.MatchString(stripped) {
			t.Errorf("SC3 violation: %s reads Spec.Verification.MaxIterations/OnExhaustion directly — "+
				"route through ResolveLoopPolicy instead (dispatch_helpers.go)", name)
		}
	}
}

// stripGoLineComments drops everything after "//" on each line (best-effort
// hygiene strip, not a full Go parser — sufficient to keep doc-comment prose
// from tripping the SC3 static guard).
func stripGoLineComments(src string) string {
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		if before, _, found := strings.Cut(line, "//"); found {
			lines[i] = before
		}
	}
	return strings.Join(lines, "\n")
}
