---
phase: 47
slug: self-hosted-phoenix-install-end-to-end-proof
status: verified
threats_open: 0
asvs_level: 1
created: 2026-07-17
---

# Phase 47 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.
> Verified by the gsd-security-auditor against implemented code — every declared
> mitigation was confirmed in source (grep + read + passing tests), not accepted on
> documentation or intent. State-B create run: this file did not pre-exist.

---

## Trust Boundaries

Union of the ten plans' declared boundaries (deduplicated).

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| manager pod env (tide-system) → reporter Job spec (project ns) | The manager decides WHETHER OTLP headers are configured and threads only the fixed Secret NAME into reporter Job specs in Project namespaces | Secret NAME (never the decoded bearer value) |
| chart values → pod env (manager + dashboard) | Secret material must cross only as a Secret reference, never a rendered literal | OTLP auth header, Secret-sourced |
| helm install/upgrade output → operator terminal | NOTES.txt guidance; must not echo secret values | Operator-facing text |
| docs → operator copy-paste | Doc examples become real cluster state; a weak default or literal secret in a doc becomes a deployed weakness | Install recipes, Secret-creation commands |
| operator LAN / browser → Phoenix UI + OTLP | Exposure surface for (redacted-but-substantial) prompt/completion history; authenticated UI session during evidence capture | Trace/span history, admin session |
| TIDE pods → auth-ON Phoenix OTLP :4317 | Bearer-authenticated span export over the cluster network | LLM/agent spans (redacted) |
| dev VM → real Anthropic API | Real credential + real spend ($5-capped) during the live proof | API key, billed tokens |
| runlog / evidence artifacts → git repo | Operational evidence (runlog, EVIDENCE.md, screenshots) enters git permanently and must carry zero secret material | Trace IDs, queries, screenshots |
| manager (tide-system) → project-namespace Job/Pod specs | Job/Pod specs in Project namespaces are readable by any principal with Job-read RBAC there — a weaker audience than Secret-read | Secret NAME on secretKeyRef |
| controller → CRD .status subresource | New reporter-spawn marker fields writable only by the manager SA under existing status-subresource RBAC | Per-attempt spawn markers (Job UIDs) |
| tide-push Job → git remote | Authenticated force-with-lease push to the per-run branch; D-B6 protects external/manual work from overwrite | Run-branch commits, PAT |
| controller → tide-push Job | `--last-pushed-sha` arg + termination-message envelope contract | Lease anchor, exit/reason envelope |
| test suite → envtest apiserver | Test-only; no production trust boundary crossed | N/A |

---

## Threat Register

| Threat ID | Category | Component | Disposition | Mitigation | Status |
|-----------|----------|-----------|-------------|------------|--------|
| T-47-01 | Information Disclosure | OTLP headers env on manager + dashboard Deployments | mitigate | `valueFrom: secretKeyRef` only, `{{- if }}`-guarded, no literal `value:` — `deployment.yaml:98-107`, `dashboard-deployment.yaml:57-65`. Render gate `assert-otlp-headers-env.py` checks BOTH containers and fails on any literal `value`; `make helm-telemetry-assert` exits 0 with both containers PASS. | closed |
| T-47-02 | Information Disclosure | reporter Job spec env (literal forward into project ns) | mitigate (superseded from accept) | Stale "accept" rationale rejected; root-fixed in 47-06. `reporter_jobspec.go:324-340` builds `ValueFrom.SecretKeyRef`; no literal-value construction survives (comment-filtered grep = 0); stale T-47-02 rationale removed from live comments (grep = 0). | closed |
| T-47-03a | Spoofing | headersSecretRef default key fallback | accept | `default "OTEL_EXPORTER_OTLP_HEADERS"` (`values.yaml:426-428`, `deployment.yaml:105`) is a key NAME convention, not credential material. Logged in Accepted Risks. | closed |
| T-47-03b | Elevation of Privilege | weak Phoenix defaults (admin / postgres) | mitigate | Docs name both as values to OVERRIDE, not accept — `observability.md:238-242` (`PHOENIX_POSTGRES_PASSWORD`=`postgres`, `admin` initial password), `INSTALL.md:245` ("change it at first login"). | closed |
| T-47-04 | Information Disclosure | Phoenix reachable beyond loopback | mitigate | Recipe pins `service.type=ClusterIP` + `ingress.enabled=false` + `auth.enableAuth=true` (`INSTALL.md:231-233`); port-forward-only framing, "never NodePort/LoadBalancer" (`observability.md:260-265`). | closed |
| T-47-05 | Information Disclosure | literal creds in docs / runlog / screenshots / evidence | mitigate | Bare `host:port` OTLP endpoints (`INSTALL.md:278`, `observability.md:196`); `--from-literal` with `${PHOENIX_API_KEY}` shell var (`INSTALL.md:252,265`); `sk-ant` grep = 0 on runlog + evidence; only ellipsis placeholders in docs. EVIDENCE §4: key-prefix search over 392 spans returned 0 hits. | closed |
| T-47-06 | Denial of Service | VM OOM from concurrent heavy workloads | mitigate | Operational control documented in runlog: `tide-dogfood` deleted first (2.65 GiB freed, `47-PROOF-RUNLOG.md:74-84`), only `tide-phoenix-proof` created; one cluster / one heavy run. | closed |
| T-47-07 | Repudiation | evidence claims without observable backing | mitigate | `47-EVIDENCE.md` records a full 32-hex trace ID (`e912...86fd`), the exact Phoenix DSL query (`metadata['level'] == 'phase'`), a 4-PNG index (all present on disk), and a Known-limitations §5 honesty block. | closed |
| T-47-06-01 | Information Disclosure | reporter Job PodSpec env | mitigate | Literal `EnvVar.Value` replaced with `valueFrom.secretKeyRef` to per-namespace `tide-otlp-headers` — `reporter_jobspec.go:324-340`; all 5 spawn sites thread `OTLPHeadersSecret` (Secret NAME only); manager threads name not value (`main.go:294-295`). Unit test green. | closed |
| T-47-06-02 | Denial of Service | reporter Job start (missing mirror Secret) | mitigate | `Optional: &optionalTrue` on the secretKeyRef (`reporter_jobspec.go:329,336`) — a missing mirror degrades to unauthenticated export, not CreateContainerConfigError. | closed |
| T-47-06-03 | Tampering (operator trust) | docs/observability.md false-equivalence claim | mitigate | "already implies access" claim removed (grep = 0); `observability.md:170-190` now states the true NAME-vs-value exposure delta, Optional degrade behavior, and the mirror requirement. | closed |
| T-47-07-01 | DoS / telemetry integrity | reporter spawn sites (duplicate Jobs after TTL-GC) | mitigate | Durable per-attempt `.status` marker gates Create at ALL 5 sites; planner-tier (milestone/phase/plan/project) stamp via `RetryOnConflict` + `MergeFromWithOptimisticLock` and return error on exhaustion; task path non-fatal (gate stays open). CR-01 re-fix handles nil-Job re-entry. | closed |
| T-47-07-02 | Tampering | new .status marker fields | accept | Status subresource writable only by controller SA under existing RBAC; a tampered marker degrades to skipped/duplicate telemetry only — no dispatch/spend surface. Logged in Accepted Risks. | closed |
| T-47-07-03 | Repudiation | reporter spawn observability | mitigate | "spawned reporter Job" (Info) + "already exists; skipping spawn" (V(1)) + "ReporterImage not configured" (V(1)) log lines retained — `dispatch_helpers.go:114,134,139`. | closed |
| T-47-08-01 | Tampering | run-branch lease refresh weakening D-B6 into blind force push | mitigate | `deriveEffectiveLease` (`cmd/tide-push/main.go:984-1044`) refreshes ONLY when the observed remote tip `IsAncestor` of local HEAD; missing-object and non-ancestor fail closed with exit 11 / `lease-rejected`; still force-with-leases against the just-observed tip. Both regression tests green. | closed |
| T-47-08-02 | Information Disclosure | new stderr/error paths in tide-push | mitigate | New error text passes through `redactPAT` (`main.go:991`); `RemoteBranchTip` wraps errors with `%w` on branch/repo only — the PAT is never embedded (`pkg/git/remote.go:42-72`). | closed |
| T-47-08-03 | Spoofing | remote ref listing | accept | `RemoteBranchTip` uses the same `x-access-token` BasicAuth transport as Push (`remote.go:55-60`) — no new trust granted. Logged in Accepted Risks. | closed |
| T-47-09-01 | DoS (data loss on reuse) | `kubectl delete pvc tide-projects` doc step | mitigate | Step 3b positioned immediately after first creation (claim Pending, unmounted); scoped kind/RWO-only; WHY note states accessModes immutability and "nothing has mounted the claim yet" — `examples/projects/medium/README.md:124-134`. Shipped YAML default unchanged. | closed |
| T-47-10-01 | Repudiation (regression invisibility) | CR-01 defect class | mitigate | `reporter_spawn_idempotency_test.go` (746 lines) — 5 heavy specs, 5 `Consistently` non-recreation assertions, 19 marker assertions, three spawn-site shapes + two planner-Job TTL-GC re-entry specs pinning the CR-01 re-fix. | closed |
| T-47-SC | Tampering (supply chain) | package installs / Phoenix chart provenance | accept | No new package-manager installs (go.mod last touched Phase 42; no phase-47 commit touches go.mod/go.sum). Phoenix chart version-pinned `10.0.0`/appVersion `18.0.0`, audit-Approved via two live channels (OCI digest + GitHub raw Chart.yaml) — `47-RESEARCH.md:118-124`. Logged in Accepted Risks. | closed |

*Status: open · closed*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-47-01 | T-47-03a | The `headersSecretRef.key` fallback `default "OTEL_EXPORTER_OTLP_HEADERS"` is a data-KEY NAME convention, not credential material. The operator controls the Secret contents; the rendered chart never carries a token value. No exposure. | gsd-security-auditor | 2026-07-17 |
| AR-47-02 | T-47-07-02 | The five new `*ReporterSpawnedUID` `.status` marker fields are on the status subresource, writable only by the manager's ServiceAccount under existing RBAC. A tampered marker can only cause skipped or duplicate telemetry — it grants no dispatch, spend, or data-integrity surface. | gsd-security-auditor | 2026-07-17 |
| AR-47-03 | T-47-08-03 | `RemoteBranchTip` lists remote refs over the identical `x-access-token` authenticated transport `Push` already uses. A remote able to lie to `List` can equally lie to `Push`; the read grants no new trust beyond what the push already assumes. | gsd-security-auditor | 2026-07-17 |
| AR-47-04 | T-47-SC | No `go get` / `npm` / `pip` / `cargo` installs occurred in this phase (go.mod/go.sum untouched since Phase 42; verified via git log). The only external artifact is the Phoenix Helm chart, version-pinned to `10.0.0` (appVersion `18.0.0`) and provenance-Approved via two independent live channels (OCI registry pull digest `sha256:fa3beb12…ead33` + GitHub `main` raw `Chart.yaml`), both agreeing on version/appVersion/maintainer. Every doc example pins `--version 10.0.0`. | gsd-security-auditor | 2026-07-17 |

*Accepted risks do not resurface in future audit runs.*

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-07-17 | 20 | 20 | 0 | gsd-security-auditor |

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-07-17
