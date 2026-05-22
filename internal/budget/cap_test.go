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

// Package budget unit tests for cap.go: IsCapExceeded, IsBypassed, ConsumeBypass.
// Uses stdlib testing; constructs synthetic *tidev1alpha1.Project objects directly
// (no apiserver — these are pure-Go predicates).
package budget

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

// ---- IsCapExceeded ----

func TestIsCapExceeded(t *testing.T) {
	cases := []struct {
		name  string
		cap   int64
		spent int64
		want  bool
	}{
		{"under cap", 10000, 5000, false},
		{"at cap (not exceeded)", 10000, 10000, false},
		{"exceeded by one cent", 10000, 10001, true},
		{"zero cap = unlimited", 0, 999999, false},
		{"negative cap treated as unlimited", -1, 999999, false},
		{"nil project", 0, 0, false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "nil project" {
				got := IsCapExceeded(nil)
				if got != tc.want {
					t.Errorf("IsCapExceeded(nil) = %v; want %v", got, tc.want)
				}
				return
			}

			p := &tidev1alpha1.Project{
				Spec: tidev1alpha1.ProjectSpec{
					Budget: tidev1alpha1.BudgetConfig{AbsoluteCapCents: tc.cap},
				},
				Status: tidev1alpha1.ProjectStatus{
					Budget: tidev1alpha1.BudgetStatus{CostSpentCents: tc.spent},
				},
			}
			got := IsCapExceeded(p)
			if got != tc.want {
				t.Errorf("IsCapExceeded(%d cap, %d spent) = %v; want %v", tc.cap, tc.spent, got, tc.want)
			}
		})
	}
}

// TestIsCapExceeded_RollingCap verifies the rolling cap check added in Phase 04.1 P4.1.
// AbsoluteCapCents check is backward-compatible and unchanged.
func TestIsCapExceeded_RollingCap(t *testing.T) {
	cases := []struct {
		name        string
		absoluteCap int64
		rollingCap  int64
		spent       int64
		want        bool
	}{
		{
			name:        "absolute=0, rolling=100, spent=150 → exceeded by rolling",
			absoluteCap: 0,
			rollingCap:  100,
			spent:       150,
			want:        true,
		},
		{
			name:        "absolute=0, rolling=100, spent=50 → NOT exceeded",
			absoluteCap: 0,
			rollingCap:  100,
			spent:       50,
			want:        false,
		},
		{
			name:        "absolute=0, rolling=100, spent=100 → at cap, not exceeded",
			absoluteCap: 0,
			rollingCap:  100,
			spent:       100,
			want:        false,
		},
		{
			name:        "absolute=100, rolling=0, spent=150 → exceeded by absolute (existing behavior preserved)",
			absoluteCap: 100,
			rollingCap:  0,
			spent:       150,
			want:        true,
		},
		{
			name:        "absolute=100, rolling=200, both set, spent=150 → exceeded absolute not rolling",
			absoluteCap: 100,
			rollingCap:  200,
			spent:       150,
			want:        true,
		},
		{
			name:        "absolute=200, rolling=100, both set, spent=150 → exceeded rolling not absolute",
			absoluteCap: 200,
			rollingCap:  100,
			spent:       150,
			want:        true,
		},
		{
			name:        "both caps set, both exceeded, spent=300 → exceeded",
			absoluteCap: 100,
			rollingCap:  100,
			spent:       300,
			want:        true,
		},
		{
			name:        "nil project → NOT exceeded",
			absoluteCap: 0,
			rollingCap:  0,
			spent:       0,
			want:        false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "nil project → NOT exceeded" {
				got := IsCapExceeded(nil)
				if got != tc.want {
					t.Errorf("IsCapExceeded(nil) = %v; want %v", got, tc.want)
				}
				return
			}

			p := &tidev1alpha1.Project{
				Spec: tidev1alpha1.ProjectSpec{
					Budget: tidev1alpha1.BudgetConfig{
						AbsoluteCapCents:      tc.absoluteCap,
						RollingWindowCapCents: tc.rollingCap,
					},
				},
				Status: tidev1alpha1.ProjectStatus{
					Budget: tidev1alpha1.BudgetStatus{CostSpentCents: tc.spent},
				},
			}
			got := IsCapExceeded(p)
			if got != tc.want {
				t.Errorf("IsCapExceeded(absolute=%d, rolling=%d, spent=%d) = %v; want %v",
					tc.absoluteCap, tc.rollingCap, tc.spent, got, tc.want)
			}
		})
	}
}

// ---- IsBypassed ----

func TestIsBypassed(t *testing.T) {
	future := time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339)
	past := time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)

	cases := []struct {
		name        string
		annotations map[string]string
		want        bool
	}{
		{
			name:        "no annotation",
			annotations: nil,
			want:        false,
		},
		{
			name:        "bypass-budget=true",
			annotations: map[string]string{"tideproject.k8s/bypass-budget": "true"},
			want:        true,
		},
		{
			name:        "bypass-budget=false (explicit false)",
			annotations: map[string]string{"tideproject.k8s/bypass-budget": "false"},
			want:        false,
		},
		{
			name:        "bypass-budget-until=future",
			annotations: map[string]string{"tideproject.k8s/bypass-budget-until": future},
			want:        true,
		},
		{
			name:        "bypass-budget-until=past",
			annotations: map[string]string{"tideproject.k8s/bypass-budget-until": past},
			want:        false,
		},
		{
			name:        "bypass-budget-until=invalid timestamp",
			annotations: map[string]string{"tideproject.k8s/bypass-budget-until": "not-a-date"},
			want:        false,
		},
		{
			name: "both annotations set — TTL future wins",
			annotations: map[string]string{
				"tideproject.k8s/bypass-budget":       "true",
				"tideproject.k8s/bypass-budget-until": future,
			},
			want: true,
		},
	}

	now := time.Now()
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			p := &tidev1alpha1.Project{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: tc.annotations,
				},
			}
			got := IsBypassed(p, now)
			if got != tc.want {
				t.Errorf("IsBypassed(%v) = %v; want %v", tc.annotations, got, tc.want)
			}
		})
	}
}

// TestIsBypassed_NilProject ensures nil receiver is handled gracefully.
func TestIsBypassed_NilProject(t *testing.T) {
	if IsBypassed(nil, time.Now()) {
		t.Errorf("IsBypassed(nil) should return false")
	}
}

// ---- ConsumeBypass ----

func TestConsumeBypass(t *testing.T) {
	t.Run("removes bypass-budget annotation", func(t *testing.T) {
		p := &tidev1alpha1.Project{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"tideproject.k8s/bypass-budget": "true",
					"other-key":                     "other-value",
				},
			},
		}
		out := ConsumeBypass(p)
		if _, ok := out["tideproject.k8s/bypass-budget"]; ok {
			t.Errorf("ConsumeBypass: bypass-budget key should be removed")
		}
		if out["other-key"] != "other-value" {
			t.Errorf("ConsumeBypass: other-key should be preserved; got %q", out["other-key"])
		}
	})

	t.Run("does not remove bypass-budget-until", func(t *testing.T) {
		until := time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339)
		p := &tidev1alpha1.Project{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"tideproject.k8s/bypass-budget":       "true",
					"tideproject.k8s/bypass-budget-until": until,
				},
			},
		}
		out := ConsumeBypass(p)
		if _, ok := out["tideproject.k8s/bypass-budget"]; ok {
			t.Errorf("bypass-budget key should be removed")
		}
		if got := out["tideproject.k8s/bypass-budget-until"]; got != until {
			t.Errorf("bypass-budget-until should be preserved; got %q", got)
		}
	})

	t.Run("nil project returns nil", func(t *testing.T) {
		if ConsumeBypass(nil) != nil {
			t.Errorf("ConsumeBypass(nil) should return nil")
		}
	})

	t.Run("empty annotations returns empty map", func(t *testing.T) {
		p := &tidev1alpha1.Project{}
		out := ConsumeBypass(p)
		if out == nil {
			t.Errorf("ConsumeBypass on project with nil annotations should return non-nil empty map")
		}
		if len(out) != 0 {
			t.Errorf("expected empty map; got len=%d", len(out))
		}
	})
}
