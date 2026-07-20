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
	"testing"

	"k8s.io/apimachinery/pkg/types"
)

func TestJobName(t *testing.T) {
	cases := []struct {
		name    string
		uid     types.UID
		attempt int
		want    string
	}{
		{
			name:    "basic uid and attempt 1",
			uid:     types.UID("task-uid-abc"),
			attempt: 1,
			want:    "tide-task-task-uid-abc-1",
		},
		{
			name:    "uid xyz and attempt 7",
			uid:     types.UID("xyz"),
			attempt: 7,
			want:    "tide-task-xyz-7",
		},
		{
			name:    "zero attempt",
			uid:     types.UID("task-uid-zero"),
			attempt: 0,
			want:    "tide-task-task-uid-zero-0",
		},
		{
			name:    "large attempt",
			uid:     types.UID("task-uid-large"),
			attempt: 999,
			want:    "tide-task-task-uid-large-999",
		},
		{
			name:    "empty UID degenerate case",
			uid:     types.UID(""),
			attempt: 1,
			want:    "tide-task--1",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := JobName(c.uid, c.attempt)
			if got != c.want {
				t.Errorf("JobName(%q, %d) = %q; want %q", c.uid, c.attempt, got, c.want)
			}
		})
	}
}

// TestVerifierJobName verifies the deterministic
// tide-verifier-{level}-{parentUID}-{attempt} format (Phase 51 TASK-04/
// ESC-04, generalized level-generic in Phase 52 P02 — mirrors
// PlannerJobName's (level, parentUID, attempt) signature) — the dedup key +
// role=verifier label-selector target Plan 06's verifierInFlightCount
// counts against.
func TestVerifierJobName(t *testing.T) {
	cases := []struct {
		name      string
		level     string
		parentUID string
		attempt   int
		want      string
	}{
		{
			name:      "task level, basic uid and attempt 1",
			level:     "task",
			parentUID: "task-uid-abc",
			attempt:   1,
			want:      "tide-verifier-task-task-uid-abc-1",
		},
		{
			name:      "task level, uid xyz and attempt 7",
			level:     "task",
			parentUID: "xyz",
			attempt:   7,
			want:      "tide-verifier-task-xyz-7",
		},
		{
			name:      "task level, zero attempt",
			level:     "task",
			parentUID: "task-uid-zero",
			attempt:   0,
			want:      "tide-verifier-task-task-uid-zero-0",
		},
		{
			name:      "task level, large attempt",
			level:     "task",
			parentUID: "task-uid-large",
			attempt:   999,
			want:      "tide-verifier-task-task-uid-large-999",
		},
		{
			name:      "non-task level (plan)",
			level:     "plan",
			parentUID: "plan-uid-1",
			attempt:   2,
			want:      "tide-verifier-plan-plan-uid-1-2",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := VerifierJobName(c.level, c.parentUID, c.attempt)
			if got != c.want {
				t.Errorf("VerifierJobName(%q, %q, %d) = %q; want %q", c.level, c.parentUID, c.attempt, got, c.want)
			}
		})
	}
}

// TestVerifierJobName_DistinctFromJobName verifies the verifier and executor
// Job names never collide for the same taskUID+attempt tuple — TASK-04's
// "logically independent process" requirement depends on the two Jobs
// having distinct identities even when dispatched for the same Task/attempt.
func TestVerifierJobName_DistinctFromJobName(t *testing.T) {
	uid := types.UID("task-uid-shared")
	attempt := 1
	if VerifierJobName("task", string(uid), attempt) == JobName(uid, attempt) {
		t.Errorf("VerifierJobName(\"task\", %q, %d) collides with JobName(%q, %d)", uid, attempt, uid, attempt)
	}
}
