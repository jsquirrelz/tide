# TIDE Security Policy

**Audience:** Security researchers and operators reporting vulnerabilities in TIDE.

**Status:** v1.0. Supply-chain provenance signing (cosign) is deferred to v1.x per `.goreleaser.yaml` — v1.0 chart and binary artifacts ship unsigned and SECURITY.md disclosures that depend on signed-artifact verification are not yet a supported recovery path.

**Scope of this doc:**

- How to report a vulnerability
- Expected response time
- In-scope components
- Out-of-scope components

## Reporting a vulnerability

The preferred channel is the [GitHub Security Advisory](https://github.com/jsquirrelz/tide/security/advisories/new) form. Advisories are private until you and the maintainers coordinate disclosure, which avoids the "fix-before-attackers" race that public Issues create.

If GitHub Security Advisories are unreachable from your environment, mail a placeholder address `security@tide.example` — this address is a stub until the project provisions a real one. Until then, prefer GitHub Security Advisory. The maintainers will replace this placeholder once a real mailbox is in place.

Do not file vulnerabilities as public GitHub Issues, GitHub Discussions, or pull requests. Doing so discloses them publicly before a fix is available.

## Expected response time

Acknowledgement within 48h of receipt (best effort by the solo maintainer; v1.0 is pre-team posture). The acknowledgement confirms the report is in triage; it is not a commitment that the vulnerability is confirmed or that a fix has been scoped.

Resolution timeline depends on severity:

- **Critical / exploitable in default install** — patch released as soon as the fix is verified; coordinated advisory + GitHub Security Advisory disclosure on the same day.
- **High** — patch within 7 days; advisory published with the patch release.
- **Medium / Low** — rolled into the next scheduled release; advisory published at the same cadence.

Coordinated disclosure is the default: advisory text and patch release are scheduled with the reporter so that downstream operators receive the fix and the disclosure together.

## In scope

The following components are in-scope for vulnerability reports against the v1.0 release:

- **TIDE controller** — `cmd/manager/` + `internal/controller/` reconcilers, webhooks, finalizer logic, owner-ref cascade.
- **TIDE dashboard** — `cmd/dashboard/` backend + `dashboard/web/` SPA bundle, including the SSE event stream and apiserver-proxy auth surface.
- **CRDs** — `api/v1alpha1/*` schemas, CEL validators, conversion webhook (no-op for v1).
- **RBAC** — `charts/tide/templates/*-rbac.yaml`, per-Kind ClusterRoles, ServiceAccount bindings, per-namespace RoleBinding template.
- **Helm chart templates** — `charts/tide/templates/` and `charts/tide-crds/templates/`, including default values, webhook configuration, and the dashboard ClusterRole verb minimization.
- **CLI** — `cmd/tide/` binary, including credential handling and apiserver authentication flow.
- **credproxy** — `cmd/credproxy/` (the subagent-side credential-proxy that mediates ANTHROPIC_API_KEY access from executor Jobs).

## Out of scope

The following are not in-scope for vulnerability reports against the v1.0 release:

- **Third-party LLM provider key compromise** — `ANTHROPIC_API_KEY` (and any future provider credentials) are operator-supplied via K8s Secrets. A leaked operator key is a credential-management incident on the operator's side, not a TIDE vulnerability. Route those to your secret-management provider.
- **Host Kubernetes cluster compromise** — node-level container escapes, kubelet RCE, etcd exposure, and similar K8s-platform vulnerabilities are the cluster operator's responsibility. Route those to your K8s distribution vendor.
- **Chart provenance signing** — v1.0 ships unsigned Helm charts and unsigned manager binaries. cosign keyless signing + SLSA provenance attestation are v1.x scope (see the `Supply-chain note:` footer in `.goreleaser.yaml`). Reports of "your chart is not cosign-signed" describe a known limitation, not a vulnerability.
- **Third-party Go dependency vulnerabilities** — `controller-runtime`, `client-go`, `chi`, `zap`, `prometheus/client_golang`, etc. Report those upstream. We accept reports of TIDE's *use* of a vulnerable dep (e.g. unsafe surface exposed because of how we call the lib) but not the dep's own bugs.
