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

// Package eval is the quality and cost gate for TIDE's prompt template changes.
//
// The deterministic tests in this package (render_test.go, protocol_test.go,
// cost_replay_test.go) run under `make test` (go test ./...) as part of the
// standard unit tier — zero-network, no build tag, no new Makefile target
// required. Every PR that modifies a compiled-in prompt template in
// internal/subagent/common/templates/ is gated automatically by these tests.
//
// The online surface is cmd/tide-eval, invoked by `make eval`. It calls the
// Anthropic count_tokens API to measure the actual billed token count for each
// rendered template against a fixed fixture. `make eval` requires
// TIDE_PROXY_ENDPOINT + TIDE_SIGNED_TOKEN and incurs a small API cost; it is
// separate from `make test` so normal CI does not require credentials.
//
// Offline ratchet proxy: the render_test.go byte ratchets (testdata/ratchets/)
// use len(rendered) as an offline proxy for token count (D-01a). A PR that
// grows any template's rendered byte count beyond the committed per-template
// ceiling fails `make test` automatically, without an API call. Phase 18 freezes
// the ceilings at the un-trimmed v1.0.1 byte counts; a later phase ratchets
// them down after template trimming lands.
//
// Import boundary — this package MUST NOT import:
//   - internal/controller  (import cycle: controller → subagent → eval)
//   - internal/budget      (import cycle: budget → metrics → eval)
//   - internal/metrics     (import cycle: metrics → eval)
//   - api/v1alpha1         (import cycle: api → controller → eval)
//
// The only allowed project-package imports are internal/subagent/common
// (LoadPromptTemplate) and pkg/dispatch (EnvelopeIn fixture type).
package eval
