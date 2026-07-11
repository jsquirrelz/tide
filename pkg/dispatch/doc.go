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

// Package dispatch is the public Go contract for TIDE subagent image authors
// (D-A1). External images import "github.com/jsquirrelz/tide/pkg/dispatch" to
// decode the input envelope written by the orchestrator and to write the output
// envelope consumed by the controller on Job completion.
//
// The contract is versioned by the apiVersion / kind discriminator (D-A3):
// every envelope JSON carries explicit
// "apiVersion: dispatch.tideproject.k8s/v1alpha1" and
// "kind: TaskEnvelopeIn | TaskEnvelopeOut". Consumers MUST call
// [ValidateAPIVersionKind] before processing any field. Unknown apiVersion
// values return [*UnknownAPIVersionError]; unknown kind values return
// [*UnknownKindError].
//
// The group "dispatch.tideproject.k8s" is deliberately decoupled from the CRD
// group "tideproject.k8s" (D-08): this is a K8s-shaped document that is not
// itself a served API resource, so it gets its own subdomain group — the same
// pattern kubeadm uses for its own config-file API group (kubeadm.k8s.io),
// distinct from the core Kubernetes resource APIs it drives. Decoupling means
// a CRD version crank (v1alpha1 -> v1alpha2 -> v1alpha3 -> ...) can never
// collide with or accidentally bump the subagent-image envelope contract —
// the two lifecycles are now independent by construction.
//
// JSON tag stability is the public contract. Field names under v1alpha1 are
// frozen after this plan ships. Future breaking changes (e.g., new required
// fields) bump the dispatch group's OWN version component instead — they do
// not ride the CRD group's version cranks.
//
// Per SUB-01 / DAG-05-mirror, this package MUST NOT import:
//   - sigs.k8s.io/controller-runtime/* (manager/client/reconcile/...)
//   - github.com/anthropics/* (any LLM SDK)
//   - any internal/ package
//
// The package IS permitted to import k8s.io/apimachinery/pkg/runtime for
// runtime.RawExtension (Phase 3 D-A1: ChildCRDSpec.Spec uses RawExtension as
// the typed-but-deferred-decode escape hatch that keeps pkg/dispatch free of
// api/v1alpha1 imports, which would invert the dependency arrow). The
// required transitive closure of apimachinery (sigs.k8s.io/json,
// sigs.k8s.io/structured-merge-diff, k8s.io/kube-openapi, k8s.io/klog) rides
// along with that decision and is allowlisted.
//
// Enforced by the `make verify-dispatch-imports` Makefile target (SUB-01 /
// DAG-05 mirror), which uses `go list -deps ./pkg/dispatch/...` to check
// transitive imports and strips the allowlisted apimachinery closure before
// failing on any remaining k8s.io/sigs.k8s.io/anthropics import.
package dispatch
