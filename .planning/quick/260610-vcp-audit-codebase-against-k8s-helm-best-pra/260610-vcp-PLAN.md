---
phase: quick-260610-vcp
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - docs/audit/README.md
  - docs/audit/operator.md
  - docs/audit/helm-and-supply-chain.md
autonomous: true
requirements: [QUICK-260610-VCP]

must_haves:
  truths:
    - "Every checklist item from 260610-vcp-RESEARCH.md sections 1-9 has a recorded finding classified PASS / DRIFT / DEVIATION"
    - "Every finding cites concrete evidence (file:line, template name, or grep result) — no unverified claims"
    - "All 13 rows of the research's deliberate-deviations table are classified DEVIATION, never DRIFT"
    - "Recommendations are split into ship-blockers vs nice-to-haves, usable as a post-1.0 hardening backlog"
    - "No Go source, chart template, config manifest, or CI workflow was modified — only docs/ gained files"
  artifacts:
    - path: "docs/audit/README.md"
      provides: "Audit index: methodology, classification scheme, summary table, operator capability level claim, prioritized hardening backlog"
      min_lines: 60
    - path: "docs/audit/operator.md"
      provides: "Findings for checklist sections 1 (CRD design), 2 (reconciler patterns), 3 (RBAC), 4 (workload security), 7 (webhooks), 8 (observability)"
      min_lines: 150
    - path: "docs/audit/helm-and-supply-chain.md"
      provides: "Findings for checklist sections 5 (Helm conventions) and 6 (image/build supply chain)"
      min_lines: 80
  key_links:
    - from: "docs/audit/*.md"
      to: ".planning/quick/260610-vcp-audit-codebase-against-k8s-helm-best-pra/260610-vcp-RESEARCH.md"
      via: "each finding maps to a numbered checklist item"
      pattern: "PASS|DRIFT|DEVIATION"
    - from: "docs/audit/README.md"
      to: "docs/audit/operator.md, docs/audit/helm-and-supply-chain.md"
      via: "relative markdown links in the index"
      pattern: "\\]\\((operator|helm-and-supply-chain)\\.md"
---

<objective>
Audit the TIDE codebase against the Kubernetes-operator and Helm-chart best-practices checklist produced in 260610-vcp-RESEARCH.md, and record every finding, recommendation, anti-pattern, and implementation quirk as documents under docs/audit/.

Purpose: v1.0.0 just shipped. This audit becomes the post-1.0 hardening backlog — a citable, evidence-backed inventory of where TIDE aligns with upstream practice, where it drifted accidentally, and where it deviates deliberately.
Output: docs/audit/README.md (index + backlog), docs/audit/operator.md, docs/audit/helm-and-supply-chain.md.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/quick/260610-vcp-audit-codebase-against-k8s-helm-best-pra/260610-vcp-RESEARCH.md
@CLAUDE.md

Audit surfaces (read-only — NEVER edit any of these):
- api/v1alpha1/ — CRD types, markers, CEL validations, conversion, shared_types.go condition vocabulary
- internal/controller/ — reconcilers; internal/webhook/ — admission; internal/{dispatch,pool,reporter,subagent,metrics,otelinit,finalizer,owner,budget,gates,credproxy,gitleaks,harness,config}/
- cmd/manager/ — metrics auth options, health probes, leader election; other cmd/ binaries as referenced
- config/ — kubebuilder manifests (rbac/, webhook/, manager/, default/, prometheus/, network-policy/, certmanager/, crd/, samples/)
- charts/tide/ + charts/tide-crds/ — templates (full list includes deployment.yaml, metrics-*-rbac.yaml, per-namespace-rolebinding.yaml, serviceaccount-subagent.yaml, reporter-rbac.yaml, push-rbac.yaml, dashboard-*, projects-pvc.yaml, selfsigned-issuer.yaml, _helpers.tpl, NOTES.txt if present), Chart.yaml, values.yaml, values.schema.json if present
- Dockerfile, Dockerfile.dashboard, .goreleaser.yaml, .github/workflows/{ci.yaml,release.yaml,dry-run.yaml,lint.yml,test.yml,nightly-integration.yml,live-e2e.yml}
</context>

<constraints>
- READ-ONLY over source: the ONLY writes permitted are new files under docs/audit/. Do NOT modify Go code, chart templates, config manifests, Dockerfiles, or CI workflows — even to fix a defect the audit finds. Defects become backlog entries, not edits.
- Evidence discipline (Observe First): every finding cites file:line or template name obtained by actually grepping/reading the artifact. Never assert "TIDE does X" from memory or from CLAUDE.md alone — confirm in source. Prefer targeted grep per checklist item over wholesale file reads (e.g. `grep -rn "WithAuthenticationAndAuthorization" cmd/ internal/` rather than reading all of cmd/manager/main.go).
- Classification scheme is exactly the research's: PASS (meets the practice) / DRIFT (accidental gap) / DEVIATION (deliberate, must match a row in the research's 13-row deviations table or another documented decision — cite the rationale of record). When evidence is ambiguous, record the finding as DRIFT with a note, never silently skip.
- Recommendations must never contradict the Project Constraints block in the research (e.g. do not recommend slog over zap, a DB over CRD-status, or webhooks where CEL suffices).
- Finding ID format: `<SECTION>-<NN>` (e.g. CRD-03, RBAC-01, HELM-05, SUPPLY-02, WEBHOOK-04, OBS-01, RECON-06, PODSEC-02, MATURITY-01) so the backlog can reference findings stably.
</constraints>

<tasks>

<task type="auto">
  <name>Task 1: Audit operator surfaces and write docs/audit/operator.md</name>
  <files>docs/audit/operator.md</files>
  <action>
Walk checklist sections 1 (CRD design), 2 (controller/reconciler patterns), 3 (RBAC), 4 (pod/workload security), 7 (webhooks), and 8 (observability) from 260610-vcp-RESEARCH.md against the operator-side surfaces: api/v1alpha1/, internal/, cmd/manager/, config/, and the chart's RBAC/deployment templates where they carry the runtime posture (deployment.yaml securityContext, metrics-auth-rbac.yaml, per-namespace-rolebinding.yaml, serviceaccount-subagent.yaml).

For each checklist item, gather evidence by grep first (examples: `grep -rn "subresource:status" api/v1alpha1/`; `grep -rn "observedGeneration" api/ internal/controller/`; `grep -rn "XValidation" api/v1alpha1/`; `grep -rn "GenerationChangedPredicate\|WithEventFilter" internal/controller/`; `grep -rn "LeaderElection" cmd/manager/`; `grep -rn "WithAuthenticationAndAuthorization" cmd/ internal/`; `grep -rn "RequeueAfter" internal/controller/`; `grep -rn "automountServiceAccountToken" charts/ config/`; `grep -rn "failurePolicy\|sideEffects\|timeoutSeconds" config/webhook/ charts/`; `grep -rn "activeDeadlineSeconds\|ttlSecondsAfterFinished\|backoffLimit" internal/dispatch/`), then read only the specific line ranges needed to confirm context. Resolve the research's two Open Questions: (1) metrics auth posture in cmd/manager (confirm WithAuthenticationAndAuthorization survived the Phase 02.2 --metrics-bind-address flag work), (2) is covered in Task 2.

Write docs/audit/operator.md with one `### <ID>: <checklist item title>` block per item containing: **Classification** (PASS/DRIFT/DEVIATION), **Evidence** (file:line citations with a one-line quote or summary of what was found), **Notes/quirks** (odd-but-working implementation details worth recording, e.g. the verify-rbac-marker-discipline grep scope, the three-pass reconcile pattern, PodStatusEnvelopeReader termination-message channel), and for DRIFT items a **Recommendation** marked either `[SHIP-BLOCKER]` or `[NICE-TO-HAVE]`. DEVIATION items must cite the matching deviations-table row or decision of record. Match the spec doc's tight declarative voice.
  </action>
  <verify>
    <automated>test -f docs/audit/operator.md && grep -cE '^\*\*Classification\*\*: (PASS|DRIFT|DEVIATION)' docs/audit/operator.md | awk '{exit ($1 < 30) ? 1 : 0}' && git status --porcelain | grep -v '^??' | grep -vE 'docs/audit/|\.planning/' | wc -l | awk '{exit ($1 != 0) ? 1 : 0}'</automated>
  </verify>
  <done>docs/audit/operator.md exists with ≥30 classified findings (checklist sections 1,2,3,4,7,8 total ~40 items), every finding has file:line evidence, no tracked source file modified.</done>
</task>

<task type="auto">
  <name>Task 2: Audit Helm charts and supply chain, write docs/audit/helm-and-supply-chain.md</name>
  <files>docs/audit/helm-and-supply-chain.md</files>
  <action>
Walk checklist sections 5 (Helm conventions) and 6 (image/build supply chain) against charts/tide/, charts/tide-crds/, Dockerfile, Dockerfile.dashboard, .goreleaser.yaml, and .github/workflows/ (ci.yaml, release.yaml, dry-run.yaml, lint.yml).

Evidence gathering, grep-first (examples: `cat charts/tide/Chart.yaml charts/tide-crds/Chart.yaml`; `ls charts/tide/values.schema.json charts/tide/templates/NOTES.txt charts/tide/templates/tests/ 2>/dev/null`; `grep -rn "app.kubernetes.io" charts/tide/templates/_helpers.tpl`; `grep -rn "latest\|AppVersion" charts/tide/values.yaml charts/tide/templates/`; `grep -n "FROM" Dockerfile Dockerfile.dashboard` (check digest pinning and distroless/nonroot base); `grep -n "sboms\|signs\|cosign" .goreleaser.yaml`; `grep -rn "trivy\|grype\|ct lint\|kubeconform\|helm lint" .github/workflows/`). Resolve research Open Question 1: determine whether charts/tide-crds ships CRDs as templates (upgradeable, but uninstall deletes CRs) or via a crds/ dir (never upgraded), and whether docs/INSTALL.md documents the upgrade/uninstall semantics — cite what INSTALL.md says or doesn't.

Verify image inventory completeness (checklist 6, final item): enumerate every image reference in chart templates and values (`grep -rhoE 'tide-[a-z-]+' charts/tide/values.yaml charts/tide/templates/ | sort -u`) and diff against the publish matrix in .github/workflows/release.yaml and .goreleaser.yaml — the tide-reporter omission was a real v1.0.0 ship-blocker, so this check gets its own finding with both lists quoted.

Write docs/audit/helm-and-supply-chain.md in the same per-finding format as Task 1 (Classification / Evidence / Notes / Recommendation with [SHIP-BLOCKER] or [NICE-TO-HAVE]). Remember the chart-is-fixed-contract deviation: any recommendation touching charts/tide/values.yaml must be framed as a backlog item routed through the chart-first process, not a code-side workaround.
  </action>
  <verify>
    <automated>test -f docs/audit/helm-and-supply-chain.md && grep -cE '^\*\*Classification\*\*: (PASS|DRIFT|DEVIATION)' docs/audit/helm-and-supply-chain.md | awk '{exit ($1 < 15) ? 1 : 0}'</automated>
  </verify>
  <done>docs/audit/helm-and-supply-chain.md exists with ≥15 classified findings covering all of checklist sections 5 and 6, the image-inventory diff finding quotes both lists, and the CRD-chart upgrade-story open question is answered with evidence.</done>
</task>

<task type="auto">
  <name>Task 3: Synthesize index, capability-level claim, and hardening backlog in docs/audit/README.md</name>
  <files>docs/audit/README.md</files>
  <action>
Write docs/audit/README.md as the audit's front door:

1. **Methodology** — one short section: audited 2026-06-10 against the checklist in .planning/quick/260610-vcp-audit-codebase-against-k8s-helm-best-pra/260610-vcp-RESEARCH.md (link it); classification scheme PASS/DRIFT/DEVIATION defined in one line each; read-only audit, v1.0.0 baseline (cite the v1.0.0 tag commit from `git log --oneline -1 v1.0.0` or current HEAD if tag resolution is ambiguous).
2. **Summary table** — counts of PASS/DRIFT/DEVIATION per checklist section (9 rows), each row linking to the section in operator.md or helm-and-supply-chain.md.
3. **Deliberate deviations register** — reproduce the research's 13-row deviations table with a fourth column citing where the audit confirmed each in source (file:line). Checklist section 9's "know your level and document it" item lands here too: state TIDE's claimed Operator Capability Level with one-line justification per level claimed/not-claimed, as a MATURITY-NN finding.
4. **Post-1.0 hardening backlog** — every DRIFT recommendation from both docs, grouped `## Ship-blockers` then `## Nice-to-haves`, each entry referencing its finding ID and one-line evidence pointer. Order ship-blockers by blast radius (install-breaking > security > correctness > polish).
5. **Quirks appendix** — the odd-but-working implementation details collected in tasks 1-2 (things a new contributor would trip on), each with a file:line pointer.

Cross-check the must_haves self-audit: confirm every checklist item from research sections 1-9 appears as a finding ID somewhere across the three docs (build the ID list with `grep -rhoE '^### [A-Z]+-[0-9]+' docs/audit/ | sort -u` and compare against the section walk). Any missed item gets added to the appropriate doc before finishing.
  </action>
  <verify>
    <automated>test -f docs/audit/README.md && grep -qE '\]\(operator\.md' docs/audit/README.md && grep -qE '\]\(helm-and-supply-chain\.md' docs/audit/README.md && grep -qiE 'ship.blocker' docs/audit/README.md && git status --porcelain | grep -v '^??' | grep -vE 'docs/audit/|\.planning/' | wc -l | awk '{exit ($1 != 0) ? 1 : 0}'</automated>
  </verify>
  <done>README.md links both detail docs, contains the 9-section summary table, the 13-row deviations register with source confirmations, a capability-level claim, a backlog split ship-blockers/nice-to-haves, and a quirks appendix. Working tree shows no modifications outside docs/audit/ and .planning/.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| none introduced | Plan is read-only over source; only output is markdown under docs/audit/ |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-Q260610-01 | Tampering | source tree during audit | mitigate | per-task verify gate: `git status --porcelain` must show zero modified tracked files outside docs/audit/ and .planning/ |
| T-Q260610-02 | Information Disclosure | docs/audit/*.md (public repo) | mitigate | findings cite file:line only; never quote secret values, tokens, or key material into audit docs (gitleaks conventions apply) |
</threat_model>

<verification>
- All three docs exist under docs/audit/ and render as valid markdown.
- `grep -rhoE '(PASS|DRIFT|DEVIATION)' docs/audit/operator.md docs/audit/helm-and-supply-chain.md | sort | uniq -c` shows all three classifications in use (an audit with zero DRIFT findings is suspicious — re-check before accepting).
- All 13 deviations-table rows appear in README.md classified DEVIATION.
- `git status --porcelain | grep -v '^??' | grep -vE 'docs/audit/|\.planning/'` is empty — read-only constraint held.
- Research Open Questions 1 (CRD subchart upgrade story) and 2 (metrics auth posture) are both answered with evidence.
</verification>

<success_criteria>
- Every checklist item from RESEARCH.md sections 1-9 has a finding with ID, classification, and file:line evidence.
- The backlog distinguishes ship-blockers from nice-to-haves and is actionable as post-1.0 hardening work.
- No source file, chart template, config manifest, or workflow modified.
</success_criteria>

<output>
Create `.planning/quick/260610-vcp-audit-codebase-against-k8s-helm-best-pra/260610-vcp-SUMMARY.md` when done.
</output>
