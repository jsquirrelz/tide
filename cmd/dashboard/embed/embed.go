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

// Package embed holds the Vite-built React SPA bundle that the dashboard
// backend serves as a static file system (Phase 4 D-D5 / D-X2).
//
// At build time, `make dashboard-frontend` invokes Vite under `dashboard/web/`
// and copies the produced `dist/` tree into `cmd/dashboard/embed/dist/`.
// The `//go:embed all:dist` directive then bakes those assets into the
// `tide-dashboard` binary so v1.0 ships as a single image with no separate
// static-asset CDN dependency.
//
// Until plan 04-12/04-13 lands the real Vite output, this package embeds
// only a placeholder `index.html` so the dashboard backend compiles and
// the embed.FS is non-empty (the `all:` prefix retains files whose name
// starts with `.` or `_` — none exist here yet, but it future-proofs the
// React build's chunk-hashed sub-directories).
package embed

import "embed"

// Dist is the embedded SPA bundle. Plan 04-10 ships a placeholder
// `dist/index.html`; plans 04-12 + 04-13 overwrite it with the real
// Vite output via `make dashboard-frontend`.
//
//go:embed all:dist
var Dist embed.FS
