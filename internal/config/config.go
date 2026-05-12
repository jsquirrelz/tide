// Package config loads TIDE's runtime configuration from a YAML file
// mounted from a Helm-rendered ConfigMap at /etc/tide/config.yaml.
//
// Per CTRL-04: Config exposes the two parallelism budgets (plannerConcurrency,
// executorConcurrency) and the per-Kind MaxConcurrentReconciles map.
//
// Load applies documented defaults for any field omitted from the YAML
// (so a minimal config.yaml is valid) but rejects any field that is
// explicitly zero or negative (so a user can't accidentally disable a
// reconciler by typing `plannerConcurrency: 0`).
//
// Values surface decisions are pinned in 01-RESEARCH.md §"Configuration
// Plumbing (CTRL-04)" and §"Helm values surface".
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the top-level runtime configuration loaded from the operator's
// ConfigMap. Both parallelism budgets and the per-Kind reconciler concurrency
// map are exposed; cmd/manager/main.go (Plan 08) reads this struct to
// construct the planner/executor pools and pass MaxConcurrentReconciles into
// each Reconciler's controller.Options.
type Config struct {
	PlannerConcurrency      int                     `yaml:"plannerConcurrency"`
	ExecutorConcurrency     int                     `yaml:"executorConcurrency"`
	MaxConcurrentReconciles MaxConcurrentReconciles `yaml:"maxConcurrentReconciles"`
}

// MaxConcurrentReconciles is the per-Kind reconciler concurrency map. Each
// field is the value passed as controller.Options{MaxConcurrentReconciles}
// to the Manager when registering that Kind's Reconciler.
//
// Defaults (from 01-RESEARCH.md "Helm values surface"):
//
//	Project:   1   (rare, sequential bootstrap)
//	Milestone: 1   (one milestone in flight per project)
//	Phase:     2   (Phase work overlaps across two milestones in transition)
//	Plan:      4   (multiple plans per phase parallel)
//	Wave:      8   (waves fan out across plans)
//	Task:      16  (task reconciler is the hot path)
type MaxConcurrentReconciles struct {
	Project   int `yaml:"project"`
	Milestone int `yaml:"milestone"`
	Phase     int `yaml:"phase"`
	Plan      int `yaml:"plan"`
	Wave      int `yaml:"wave"`
	Task      int `yaml:"task"`
}

// rawConfig mirrors Config but with pointer-int fields so we can distinguish
// "field omitted" (nil → apply default) from "field explicitly zero or
// negative" (validation error). yaml.v3 leaves *int fields nil when the key
// is absent and sets them to the decoded value (including 0) when present.
type rawConfig struct {
	PlannerConcurrency      *int             `yaml:"plannerConcurrency"`
	ExecutorConcurrency     *int             `yaml:"executorConcurrency"`
	MaxConcurrentReconciles rawMaxConcurrent `yaml:"maxConcurrentReconciles"`
}

type rawMaxConcurrent struct {
	Project   *int `yaml:"project"`
	Milestone *int `yaml:"milestone"`
	Phase     *int `yaml:"phase"`
	Plan      *int `yaml:"plan"`
	Wave      *int `yaml:"wave"`
	Task      *int `yaml:"task"`
}

// Load reads path, parses it as YAML, applies defaults for omitted fields,
// validates that every concurrency value is >= 1, and returns the resolved
// Config. Returns a non-nil error on:
//   - file not found / read error
//   - malformed YAML
//   - any concurrency field explicitly set to zero or a negative integer
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	var raw rawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}

	cfg := &Config{}
	if err := applyAndValidate(&raw, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// applyAndValidate resolves each rawConfig pointer field by either taking
// its explicit value (validated >= 1) or applying the documented default.
func applyAndValidate(raw *rawConfig, out *Config) error {
	// Top-level pools.
	if err := resolveField("plannerConcurrency", raw.PlannerConcurrency, 16, &out.PlannerConcurrency); err != nil {
		return err
	}
	if err := resolveField("executorConcurrency", raw.ExecutorConcurrency, 4, &out.ExecutorConcurrency); err != nil {
		return err
	}
	// Per-Kind reconciler concurrencies.
	m := &raw.MaxConcurrentReconciles
	if err := resolveField("maxConcurrentReconciles.project", m.Project, 1, &out.MaxConcurrentReconciles.Project); err != nil {
		return err
	}
	if err := resolveField("maxConcurrentReconciles.milestone", m.Milestone, 1, &out.MaxConcurrentReconciles.Milestone); err != nil {
		return err
	}
	if err := resolveField("maxConcurrentReconciles.phase", m.Phase, 2, &out.MaxConcurrentReconciles.Phase); err != nil {
		return err
	}
	if err := resolveField("maxConcurrentReconciles.plan", m.Plan, 4, &out.MaxConcurrentReconciles.Plan); err != nil {
		return err
	}
	if err := resolveField("maxConcurrentReconciles.wave", m.Wave, 8, &out.MaxConcurrentReconciles.Wave); err != nil {
		return err
	}
	if err := resolveField("maxConcurrentReconciles.task", m.Task, 16, &out.MaxConcurrentReconciles.Task); err != nil {
		return err
	}
	return nil
}

// resolveField stores defaultVal in *out if raw is nil (field omitted from
// the YAML), otherwise validates raw >= 1 and stores it. Returns a
// descriptive error naming the field path on rejection.
func resolveField(field string, raw *int, defaultVal int, out *int) error {
	if raw == nil {
		*out = defaultVal
		return nil
	}
	if *raw < 1 {
		return fmt.Errorf("config: %s must be >= 1, got %d", field, *raw)
	}
	*out = *raw
	return nil
}
