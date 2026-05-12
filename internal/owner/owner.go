// Package owner provides a helper for setting controller-style owner references
// with same-namespace enforcement.
//
// Kubernetes silently ignores cross-namespace owner refs — the API server
// accepts them at admission time but the garbage collector refuses to honor
// them, leading to orphaned resources on parent deletion. This is documented
// in .planning/research/PITFALLS.md as Pitfall 23.
//
// Every TIDE reconciler creating a child resource calls EnsureOwnerRef
// instead of controllerutil.SetControllerReference directly so the
// same-namespace invariant is enforced uniformly across the codebase.
//
// Per CRD-02 from .planning/REQUIREMENTS.md.
package owner

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// EnsureOwnerRef sets a controller-style owner reference on child pointing
// to parent. The reference has Controller=true and BlockOwnerDeletion=true
// so the K8s garbage collector cascades parent deletion to child (CRD-02).
//
// Returns an error if:
//   - parent is nil
//   - child is nil
//   - parent and child are in different namespaces (Pitfall 23 prevention)
//
// On the cross-namespace failure path, the child is NOT mutated.
func EnsureOwnerRef(child, parent metav1.Object, scheme *runtime.Scheme) error {
	if parent == nil {
		return fmt.Errorf("owner: parent is nil")
	}
	if child == nil {
		return fmt.Errorf("owner: child is nil")
	}
	if child.GetNamespace() != parent.GetNamespace() {
		return fmt.Errorf("owner: cross-namespace owner ref forbidden (Pitfall 23): parent=%s/%s child=%s/%s",
			parent.GetNamespace(), parent.GetName(),
			child.GetNamespace(), child.GetName())
	}
	return controllerutil.SetControllerReference(parent, child, scheme,
		controllerutil.WithBlockOwnerDeletion(true))
}
