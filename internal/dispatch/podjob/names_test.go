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
