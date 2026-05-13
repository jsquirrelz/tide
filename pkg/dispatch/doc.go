// Package dispatch is the public Go contract for TIDE subagent image authors
// (D-A1). External images import "github.com/jsquirrelz/tide/pkg/dispatch" to
// decode the input envelope written by the orchestrator and to write the output
// envelope consumed by the controller on Job completion.
//
// The contract is versioned by the apiVersion / kind discriminator (D-A3):
// every envelope JSON carries explicit "apiVersion: tideproject.k8s/v1alpha1"
// and "kind: TaskEnvelopeIn | TaskEnvelopeOut". Consumers MUST call
// [ValidateAPIVersionKind] before processing any field. Unknown apiVersion
// values return [*UnknownAPIVersionError]; unknown kind values return
// [*UnknownKindError].
//
// JSON tag stability is the public contract. Field names under v1alpha1 are
// frozen after this plan ships. Future breaking changes (e.g., new required
// fields) ride a v1beta1 apiVersion bump via the same hub/spoke conversion
// path the CRDs use (CRD-05 scaffold).
//
// Per SUB-01 / DAG-05-mirror, this package MUST NOT import:
//   - k8s.io/*            (any)
//   - sigs.k8s.io/*       (any)
//   - github.com/anthropics/* (any)
//   - any internal/ package
//
// Enforced by the `make verify-dispatch-imports` Makefile target (SUB-01 /
// DAG-05 mirror), which uses `go list -deps ./pkg/dispatch/...` to check
// transitive imports.
package dispatch
