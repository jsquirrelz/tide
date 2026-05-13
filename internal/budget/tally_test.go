// Package budget unit tests for tally.go: RollUpUsage.
// Uses controller-runtime's fake client with WithStatusSubresource so Status
// patches actually persist.
package budget

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// newTallyFakeClient builds a fake client that honors Status subresource patches
// for Project objects.
func newTallyFakeClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	if err := tidev1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme tidev1alpha1: %v", err)
	}
	return fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(objs...).
		WithStatusSubresource(&tidev1alpha1.Project{}).
		Build()
}

func makeProject(name string) *tidev1alpha1.Project {
	return &tidev1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: tidev1alpha1.ProjectSpec{
			TargetRepo: "https://github.com/example/repo",
		},
	}
}

// TestRollUpUsage_AccumulatesAcrossCalls verifies that two successive RollUpUsage
// calls accumulate TokensSpent and CostSpentCents additively.
func TestRollUpUsage_AccumulatesAcrossCalls(t *testing.T) {
	p := makeProject("test-project")
	c := newTallyFakeClient(t, p)

	usage1 := pkgdispatch.Usage{InputTokens: 1000, OutputTokens: 500, EstimatedCostCents: 25}
	usage2 := pkgdispatch.Usage{InputTokens: 800, OutputTokens: 200, EstimatedCostCents: 15}

	if err := RollUpUsage(context.Background(), c, p, usage1); err != nil {
		t.Fatalf("first RollUpUsage: %v", err)
	}

	// Fetch updated project after first patch.
	updated := &tidev1alpha1.Project{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(p), updated); err != nil {
		t.Fatalf("Get after first RollUpUsage: %v", err)
	}

	if err := RollUpUsage(context.Background(), c, updated, usage2); err != nil {
		t.Fatalf("second RollUpUsage: %v", err)
	}

	// Read final state.
	final := &tidev1alpha1.Project{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(p), final); err != nil {
		t.Fatalf("Get after second RollUpUsage: %v", err)
	}

	wantTokens := int64(1000 + 500 + 800 + 200) // 2500
	wantCents := int64(25 + 15)                 // 40

	if final.Status.Budget.TokensSpent != wantTokens {
		t.Errorf("TokensSpent: got %d; want %d", final.Status.Budget.TokensSpent, wantTokens)
	}
	if final.Status.Budget.CostSpentCents != wantCents {
		t.Errorf("CostSpentCents: got %d; want %d", final.Status.Budget.CostSpentCents, wantCents)
	}
}

// TestRollUpUsage_SetsWindowStartOnFirstCall verifies that WindowStart is set on
// the first RollUpUsage call when it was previously nil.
func TestRollUpUsage_SetsWindowStartOnFirstCall(t *testing.T) {
	p := makeProject("test-project-ws")
	c := newTallyFakeClient(t, p)

	if p.Status.Budget.WindowStart != nil {
		t.Fatal("expected nil WindowStart before first call")
	}

	usage := pkgdispatch.Usage{InputTokens: 100, OutputTokens: 50, EstimatedCostCents: 5}
	if err := RollUpUsage(context.Background(), c, p, usage); err != nil {
		t.Fatalf("RollUpUsage: %v", err)
	}

	updated := &tidev1alpha1.Project{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(p), updated); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Status.Budget.WindowStart == nil {
		t.Errorf("WindowStart should be set after first RollUpUsage call; got nil")
	}
}

// TestRollUpUsage_PreservesExistingWindowStart verifies that a pre-set WindowStart
// is not overwritten by subsequent RollUpUsage calls.
func TestRollUpUsage_PreservesExistingWindowStart(t *testing.T) {
	p := makeProject("test-project-pws")

	// Set a WindowStart before creating the client.
	existingTime := metav1.Now()
	p.Status.Budget.WindowStart = &existingTime
	c := newTallyFakeClient(t, p)

	usage := pkgdispatch.Usage{InputTokens: 100, OutputTokens: 50, EstimatedCostCents: 5}
	if err := RollUpUsage(context.Background(), c, p, usage); err != nil {
		t.Fatalf("RollUpUsage: %v", err)
	}

	updated := &tidev1alpha1.Project{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(p), updated); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Status.Budget.WindowStart == nil {
		t.Errorf("WindowStart should be preserved; got nil")
	}
	// Compare with second-level truncation since metav1.Time is serialized to
	// JSON as RFC3339 (second precision) and the fake client round-trips through
	// JSON encoding, stripping sub-second precision and the monotonic clock reading.
	// WR-08: time.Second is the self-documenting form of the previous magic
	// `Truncate(1000000000)` (1e9 ns).
	if !updated.Status.Budget.WindowStart.Time.Truncate(time.Second).Equal(existingTime.Time.Truncate(time.Second)) {
		t.Errorf("WindowStart changed: got %v; want %v", updated.Status.Budget.WindowStart, existingTime)
	}
}
