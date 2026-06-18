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

package v1alpha2

// ImportSourceRef declares the envelope salvage source for this Project.
// When set, ImportController drives the one-shot UID-rewrite import state machine
// before any planner dispatch fires (Phase 28 D-02).
type ImportSourceRef struct {
	// SeedManifestConfigMap names a ConfigMap in the project namespace
	// carrying the seed manifest JSON (FQ-name → oldUID + initial status).
	// +kubebuilder:validation:MinLength=1
	SeedManifestConfigMap string `json:"seedManifestConfigMap"`

	// SalvagedPVCSubPath is the sub-path within the shared tide-projects PVC
	// where the salvaged envelopes reside, e.g. "<oldProjectUID>/workspace".
	// +kubebuilder:validation:MinLength=1
	SalvagedPVCSubPath string `json:"salvagedPVCSubPath"`
}
