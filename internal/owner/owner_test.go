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

// Package owner unit tests verifying same-namespace enforcement
// (Pitfall 23 prevention) and controller-owner-ref shape (CRD-02).
package owner

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
)

// newScheme builds a runtime.Scheme with core/v1 types registered. The
// tests use corev1.ConfigMap as a stand-in for any TIDE CRD parent/child
// pair — EnsureOwnerRef is type-agnostic and the controllerutil.SetControllerReference
// call below only needs the parent's GVK to be discoverable from the scheme.
func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	return s
}

func TestEnsureOwnerRef_SameNamespace(t *testing.T) {
	s := newScheme(t)
	parent := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "parent-cm",
			Namespace: "ns-a",
			UID:       "parent-uid",
		},
	}
	child := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "child-cm",
			Namespace: "ns-a",
		},
	}
	if err := EnsureOwnerRef(child, parent, s); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	refs := child.GetOwnerReferences()
	if len(refs) != 1 {
		t.Fatalf("expected 1 owner ref, got %d", len(refs))
	}
	if refs[0].UID != "parent-uid" {
		t.Errorf("UID = %s, want parent-uid", refs[0].UID)
	}
	if refs[0].BlockOwnerDeletion == nil || !*refs[0].BlockOwnerDeletion {
		t.Errorf("BlockOwnerDeletion = false, want true (CRD-02)")
	}
	if refs[0].Controller == nil || !*refs[0].Controller {
		t.Errorf("Controller = false, want true")
	}
	if refs[0].Name != "parent-cm" {
		t.Errorf("Name = %s, want parent-cm", refs[0].Name)
	}
}

func TestEnsureOwnerRef_CrossNamespace(t *testing.T) {
	s := newScheme(t)
	parent := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "parent", Namespace: "ns-a"},
	}
	child := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: "ns-b"},
	}
	err := EnsureOwnerRef(child, parent, s)
	if err == nil {
		t.Fatalf("expected cross-namespace error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "cross-namespace") {
		t.Errorf("error message %q missing 'cross-namespace' marker", msg)
	}
	if !strings.Contains(msg, "Pitfall 23") {
		t.Errorf("error message %q missing 'Pitfall 23' marker", msg)
	}
	if !strings.Contains(msg, "ns-a") || !strings.Contains(msg, "ns-b") {
		t.Errorf("error message %q should mention both namespaces (ns-a, ns-b)", msg)
	}
	// Child must not have received an owner ref on the failure path.
	if len(child.GetOwnerReferences()) != 0 {
		t.Errorf("child should have 0 owner refs on cross-namespace failure, got %d",
			len(child.GetOwnerReferences()))
	}
}

func TestEnsureOwnerRef_NilParent(t *testing.T) {
	s := newScheme(t)
	child := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "ns"}}
	if err := EnsureOwnerRef(child, nil, s); err == nil {
		t.Fatalf("expected error for nil parent, got nil")
	}
}

func TestEnsureOwnerRef_NilChild(t *testing.T) {
	s := newScheme(t)
	parent := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "ns"}}
	if err := EnsureOwnerRef(nil, parent, s); err == nil {
		t.Fatalf("expected error for nil child, got nil")
	}
}
