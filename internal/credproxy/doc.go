// Package credproxy implements the TIDE signed-token credential proxy
// (HARN-03 / D-C1, D-C2, D-C3).
//
// # Architecture
//
// The credproxy runs as a Kubernetes 1.33 native sidecar (initContainer with
// restartPolicy: Always — D-C1) inside every Task Job Pod. The subagent
// container sees only a short-lived HMAC-signed token as its
// ANTHROPIC_API_KEY; the sidecar holds the real key via envFrom and injects
// it on every outbound request after verifying the token.
//
// This eliminates Pitfall 18 (secret leakage) at the environment layer:
// a token leaked from the subagent process is useless without the sidecar's
// signing key, and cross-Pod replay is defeated by TIDE_TASK_UID binding.
//
// Components
//
//   - token.go — HMAC-SHA256 Sign/Verify with sentinel-error types.
//   - cert.go  — MintSelfSignedCert; ECDSA P-256, 1-day validity, SANs for localhost.
//   - server.go — Proxy struct; HTTPS reverse-proxy; validates bearer tokens, injects real key.
//
// Plan lineage
//
//   - Plan 05 (this package) — core library + Dockerfile.
//   - Plan 08 (PodJobBackend) — wires sidecar into every Task Job PodSpec.
//   - Plan 09 (TaskReconciler) — signs per-Task tokens at Job-create time using tide-signing-key.
//
// The public dispatch.Subagent contract (pkg/dispatch.Subagent — Plan 01) is the
// interface that the harness in Plan 06 implements; this package is the network-layer
// security wrapper around the subagent's outbound HTTP traffic.
package credproxy
