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

package controller

import (
	"testing"

	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

func TestIsPlannerFailure(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		out        pkgdispatch.EnvelopeOut
		envReadOK  bool
		wantResult bool
	}{
		{
			name:       "false-leaf: exitCode!=0, childCount==0, envReadOK — guard fires",
			out:        pkgdispatch.EnvelopeOut{ExitCode: 1, ChildCount: 0},
			envReadOK:  true,
			wantResult: true,
		},
		{
			name:       "genuine leaf: exitCode==0, childCount==0, envReadOK — PLANFAIL-03 non-regression",
			out:        pkgdispatch.EnvelopeOut{ExitCode: 0, ChildCount: 0},
			envReadOK:  true,
			wantResult: false,
		},
		{
			name:       "envelope unreadable: exitCode!=0, childCount==0, envReadOK=false — guard does not fire",
			out:        pkgdispatch.EnvelopeOut{ExitCode: 1, ChildCount: 0},
			envReadOK:  false,
			wantResult: false,
		},
		{
			name:       "children present: exitCode!=0, childCount==3, envReadOK — guard does not fire",
			out:        pkgdispatch.EnvelopeOut{ExitCode: 1, ChildCount: 3},
			envReadOK:  true,
			wantResult: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isPlannerFailure(tc.out, tc.envReadOK)
			if got != tc.wantResult {
				t.Errorf("isPlannerFailure(%+v, %v) = %v; want %v",
					tc.out, tc.envReadOK, got, tc.wantResult)
			}
		})
	}
}
