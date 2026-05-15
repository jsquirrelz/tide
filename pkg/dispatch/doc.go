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
