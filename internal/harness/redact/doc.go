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

// Package redact implements a streaming io.Writer that redacts known secret
// patterns from bytes before forwarding them to an underlying writer.
//
// Security context (HARN-04):
//
//   - Prevents known secret patterns (Anthropic API keys, GitHub PATs, AWS
//     access keys, Slack tokens, JWTs) from leaking through subagent stdout /
//     stderr into Pod logs or result artifacts. Defends against Pitfall 18
//     (secret leakage via unredacted subagent output).
//
// Pitfall A defense (split-token buffering):
//
//   - A regex pattern may straddle two consecutive Write calls — the first
//     Write carries the token prefix, the second carries the suffix. Without
//     a tail-keep buffer both halves are flushed unredacted. RedactingWriter
//     retains the last [maxPatternLen] bytes from each Write as a tail-keep
//     buffer, prepends it to the next Write, runs the full pattern set on the
//     combined buffer, and only flushes bytes beyond the new tail-keep window.
//     Close() drains the final tail-keep through one last redaction pass.
//
// Pattern set lives in [patterns.go] as [SecretPatterns].
package redact
