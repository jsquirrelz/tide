// Package gitleaks is a thin wrapper around the
// github.com/zricethezav/gitleaks/v8/detect Go library, scoped to TIDE's
// push-Job-time defense-in-depth use case (D-B3 / ART-07 from Phase 3).
//
// Security context (D-B3):
//
//   - TIDE's push Job invokes ScanDiff on the staged diff BEFORE handing
//     the diff to go-git's Push call. A finding short-circuits the push
//     (push Job exits non-zero with reason="leak-detected" per the
//     03-RESEARCH §"Q5 exit-code map"); the secret never reaches the
//     remote git host.
//
//   - This is the SECOND line of defense against secret leakage. The FIRST
//     line is internal/harness/redact (Phase 2 D-F4) which strips known
//     secret patterns from subagent stdout / stderr before they reach
//     pod logs. Both must hold; one bypassed does not invalidate the other.
//
// Defense-in-depth role (ART-07):
//
//   - Default ruleset is embedded at BUILD time via go:embed from
//     default_rules.toml — vendored verbatim from gitleaks v8.30.1
//     upstream config/gitleaks.toml. The byte slice is exposed via
//     [DefaultRulesTOML] for inspection / audit; the scan hot-path goes
//     through detect.NewDetectorDefaultConfig() which uses an identical
//     copy embedded inside the gitleaks library itself.
//
//   - Per-Project rule-set OVERRIDES land via [LoadConfig] which accepts
//     a TOML path. The TOML file SHOULD declare `[extend] useDefault = true`
//     so embedded defaults compose with user rules. Without that directive
//     the loaded detector sees ONLY the user's rules — embedded defaults
//     are silently dropped (gitleaks v8 design; verified against
//     gitleaks v8.30.1 config/config.go:extendDefault).
//
// Layering / import firewall:
//
//   - This package is K8s-API-free by construction. The ConfigMap-mount
//     mechanics that surface a per-Project override TOML to LoadConfig
//     live on the push Job spec (plan 03-06); the scanner itself reads
//     a plain file path so it can be unit-tested without a K8s cluster.
//
//   - Consumed by cmd/tide-push (plan 03-06) only; no other internal
//     consumer in Phase 3. v1.x may also consume it from a harness-time
//     diff-scan, but that is out of scope for Phase 3.
//
// Plan-level traceability:
//
//   - D-B3 — gitleaks as Go library (not shell-out, not container).
//   - ART-07 — push-boundary secret-leak prevention + Prometheus
//     counter `tide_secret_leak_blocked_total` (the counter is wired by
//     the push Job's exit-code observer in plan 03-08, not here).
package gitleaks
