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

// Phase 4 Plan 03 Task 3: assert cmd/manager/main.go wires
// internal/otelinit at boot — both the import line and the constructor +
// deferred Shutdown call. The wire-up shape is asserted via static
// source-grep on main.go (same pattern as TestMetricsBlankImportPresent
// from plan 04-01) so this test fails loudly if a future refactor
// removes any of the three required call sites.
//
// A complementary runtime check (TestManagerOtelInit) constructs a
// no-op TracerProvider via the same internal/otelinit package and asserts
// otel.GetTracerProvider() returns it — confirming the global state
// hand-off works in the production code path's shape.
package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"

	"github.com/jsquirrelz/tide/internal/otelinit"
)

// TestOtelInitWiredInMain asserts the three required wire-up substrings
// are present in cmd/manager/main.go:
//  1. the internal/otelinit import line,
//  2. the constructor call site,
//  3. the deferred Shutdown.
//
// All three are required for the dispatch-chain spans to flush before
// the process exits.
func TestOtelInitWiredInMain(t *testing.T) {
	root := findRepoRootFromOtelTest(t)
	data, err := os.ReadFile(filepath.Join(root, "cmd", "manager", "main.go"))
	if err != nil {
		t.Fatalf("read cmd/manager/main.go: %v", err)
	}
	src := string(data)

	required := []string{
		`"github.com/jsquirrelz/tide/internal/otelinit"`,
		`otelinit.NewTracerProvider(`,
		`otelShutdown(`,
	}
	for _, want := range required {
		if !strings.Contains(src, want) {
			t.Errorf("cmd/manager/main.go missing required Phase 4 Plan 03 wire-up: %q", want)
		}
	}
}

// TestManagerOtelInit asserts the runtime contract behind the wire-up: a
// NewTracerProvider call with no env-set endpoint installs a no-op
// TracerProvider as the otel global. This proves the production code path
// (which calls NewTracerProvider in the same way) results in a usable
// global tracer that does not panic and degrades to no-op.
func TestManagerOtelInit(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	ctx := context.Background()
	tp, shutdown, err := otelinit.NewTracerProvider(ctx)
	if err != nil {
		t.Fatalf("NewTracerProvider err = %v, want nil", err)
	}
	defer func() { _ = shutdown(context.Background()) }()

	if tp == nil {
		t.Fatal("NewTracerProvider returned nil tp")
	}
	if otel.GetTracerProvider() != tp {
		t.Error("otel.GetTracerProvider() != tp; production wire-up requires the global handle be set")
	}

	got := reflect.TypeOf(otel.GetTracerProvider()).String()
	if got != "noop.TracerProvider" {
		t.Errorf("global TracerProvider type = %q, want %q (no-op path)", got, "noop.TracerProvider")
	}
}

// findRepoRootFromOtelTest walks up from cwd until it finds go.mod.
// Named uniquely to avoid collision with cmd/manager/metrics_test.go's
// findRepoRootFromCmdManager (both files are in `package main`).
func findRepoRootFromOtelTest(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := cwd
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			return root
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatalf("go.mod not found from %s; cannot locate repo root", cwd)
		}
		root = parent
	}
}
