// Package finalizer unit tests for the bounded-deadline deletion recipe
// (Pitfall 21 prevention, CTRL-05). Uses controller-runtime's fake client
// to verify finalizer state transitions without an envtest cluster.
package finalizer

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	testFinalizer = "tideproject.k8s/test-cleanup"
	testTimeout   = 50 * time.Millisecond
)

func newFakeClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	return fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()
}

func TestHandleDeletion_NoFinalizer(t *testing.T) {
	obj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "ns"},
	}
	c := newFakeClient(t, obj)
	cleanupCalled := false
	cleanup := func(ctx context.Context) error {
		cleanupCalled = true
		return nil
	}
	res, err := HandleDeletion(context.Background(), c, obj, testFinalizer, cleanup, testTimeout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Requeue {
		t.Errorf("expected no requeue, got Requeue=true")
	}
	if cleanupCalled {
		t.Errorf("cleanup should not have been called when finalizer absent")
	}
}

func TestHandleDeletion_SuccessfulCleanup(t *testing.T) {
	obj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "cm",
			Namespace:  "ns",
			Finalizers: []string{testFinalizer},
		},
	}
	c := newFakeClient(t, obj)
	cleanupCalled := false
	cleanup := func(ctx context.Context) error {
		cleanupCalled = true
		return nil
	}
	res, err := HandleDeletion(context.Background(), c, obj, testFinalizer, cleanup, testTimeout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Requeue {
		t.Errorf("expected no requeue, got Requeue=true")
	}
	if !cleanupCalled {
		t.Errorf("cleanup should have been called")
	}
	if controllerutil.ContainsFinalizer(obj, testFinalizer) {
		t.Errorf("finalizer should have been removed from local obj")
	}
	// Verify the persisted object also lost the finalizer.
	var refetched corev1.ConfigMap
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(obj), &refetched); err != nil {
		t.Fatalf("Get after HandleDeletion: %v", err)
	}
	if controllerutil.ContainsFinalizer(&refetched, testFinalizer) {
		t.Errorf("finalizer should have been removed via client.Update")
	}
}

func TestHandleDeletion_DeadlineExceeded(t *testing.T) {
	obj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "cm",
			Namespace:  "ns",
			Finalizers: []string{testFinalizer},
		},
	}
	c := newFakeClient(t, obj)
	cleanup := func(ctx context.Context) error {
		// Block until the bounded context is cancelled, then surface its err.
		<-ctx.Done()
		return ctx.Err()
	}
	res, err := HandleDeletion(context.Background(), c, obj, testFinalizer, cleanup, testTimeout)
	if err != nil {
		t.Fatalf("expected nil error on forcible removal, got: %v", err)
	}
	if res.Requeue {
		t.Errorf("expected no requeue on forcible removal, got Requeue=true")
	}
	if controllerutil.ContainsFinalizer(obj, testFinalizer) {
		t.Errorf("finalizer should have been FORCIBLY removed on deadline-exceeded (Pitfall 21 prevention)")
	}
	var refetched corev1.ConfigMap
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(obj), &refetched); err != nil {
		t.Fatalf("Get after HandleDeletion: %v", err)
	}
	if controllerutil.ContainsFinalizer(&refetched, testFinalizer) {
		t.Errorf("persisted obj should have finalizer removed after forcible cleanup")
	}
}

func TestHandleDeletion_NonTimeoutError(t *testing.T) {
	obj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "cm",
			Namespace:  "ns",
			Finalizers: []string{testFinalizer},
		},
	}
	c := newFakeClient(t, obj)
	boom := errors.New("boom")
	cleanup := func(ctx context.Context) error {
		return boom
	}
	res, err := HandleDeletion(context.Background(), c, obj, testFinalizer, cleanup, testTimeout)
	if err == nil || !errors.Is(err, boom) {
		t.Fatalf("expected boom error to be returned for requeue, got: %v", err)
	}
	if !res.Requeue {
		t.Errorf("expected Requeue=true on non-timeout error")
	}
	if !controllerutil.ContainsFinalizer(obj, testFinalizer) {
		t.Errorf("finalizer should remain on non-timeout error (transient retry)")
	}
}

func TestHandleDeletion_IdempotentRemoval(t *testing.T) {
	// Object already had its finalizer removed in a previous reconcile pass —
	// HandleDeletion should be a no-op and not call cleanup again.
	obj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "ns"},
	}
	c := newFakeClient(t, obj)
	cleanupCalled := false
	cleanup := func(ctx context.Context) error {
		cleanupCalled = true
		return nil
	}
	res, err := HandleDeletion(context.Background(), c, obj, testFinalizer, cleanup, testTimeout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Requeue {
		t.Errorf("expected no requeue on idempotent re-run")
	}
	if cleanupCalled {
		t.Errorf("cleanup should not run when finalizer already removed")
	}
}
