//go:build live_e2e

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

package e2e

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// TestLiveE2E is the Ginkgo entry point for the live Claude nightly E2E suite.
//
// Gated by `//go:build live_e2e` so it is the SOLE RunSpecs caller compiled into
// the live_e2e test binary. It was relocated out of suite_test.go (which stays
// un-tagged as the no-tag package anchor) so that the kind_e2e build — which
// compiles TestKindE2E's own RunSpecs — does NOT also pull in a second RunSpecs
// caller. Two RunSpecs invocations in one binary trips Ginkgo's "Rerunning Suite"
// guard and fails the package even when every spec passes (Failure 7).
//
// When `-tags=live_e2e` is active, live_claude_test.go contributes its Describe
// block to this same `package e2e`; the Describe registers a spec which this
// RunSpecs invocation picks up. Distinct from the kubebuilder `TestE2E` in
// e2e_suite_test.go (gated by `//go:build e2e`).
//
// RegisterFailHandler is from gomega, Fail is from ginkgo — both dot-imported,
// mirroring TestKindE2E in kind_setup_test.go.
func TestLiveE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Live Claude E2E Suite (TEST-03)")
}
