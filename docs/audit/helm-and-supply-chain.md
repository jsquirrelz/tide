# Helm Chart and Supply Chain Audit Findings — TIDE v1.0.0

Audited 2026-06-10 against [260610-vcp-RESEARCH.md](../../.planning/quick/260610-vcp-audit-codebase-against-k8s-helm-best-pra/260610-vcp-RESEARCH.md).
Classification: **PASS** (meets practice) / **DRIFT** (accidental gap) / **DEVIATION** (deliberate, documented).

---

## Section 5: Helm Chart Conventions

### HELM-01: Chart.yaml complete

**Classification**: DRIFT

**Evidence:** `charts/tide/Chart.yaml` contains `apiVersion: v2`, `description`, `type: application`, `version: 1.0.0`, `appVersion: "1.0.0"`. Missing fields compared to the checklist:
- `kubeVersion` constraint — absent (TIDE requires ≥ 1.29 for CEL CRD validation; `docs/INSTALL.md:18` states the prerequisite but the chart doesn't enforce it via `kubeVersion`)
- `home` URL — absent
- `sources` — absent
- `maintainers` — absent

`charts/tide-crds/Chart.yaml` has the same four fields present, same four missing.

**Recommendation [NICE-TO-HAVE]:** Add to both `Chart.yaml` files:
```yaml
kubeVersion: ">=1.29.0-0"
home: https://github.com/jsquirrelz/tide
sources:
  - https://github.com/jsquirrelz/tide
maintainers:
  - name: TIDE Authors
    url: https://github.com/jsquirrelz/tide
```
The `kubeVersion` field is the most operationally important — it produces a clear error on `helm install` against an incompatible cluster instead of a confusing CRD-validation-failure.

---

### HELM-02: `values.schema.json` present

**Classification**: DRIFT

**Evidence:** `ls charts/tide/values.schema.json charts/tide-crds/values.schema.json` returns nothing — neither schema file exists. Users can supply any value shape at install/upgrade time without Helm-level validation; typos in `controllerManager.replicas` or `images.tideReporter.tag` are silently accepted.

**Recommendation [NICE-TO-HAVE]:** Generate `values.schema.json` from the existing `values.yaml` structure (tools: `helm-values-schema-gen` or a one-off `yq` + `jsonschema` pass). At minimum, constrain top-level required fields (`images.tideController.repository`, `signingKey.secretName`) and numeric ranges (`controllerManager.replicas >= 1`).

---

### HELM-03: `_helpers.tpl` naming convention

**Classification**: PASS

**Evidence:** `charts/tide/templates/_helpers.tpl` defines:
- `tide.name` (line 5)
- `tide.fullname` (line 13)
- `tide.chart` (line 27)
- `tide.labels` (line 33)
- `tide.selectorLabels` (line 43)
- `tide.serviceAccountName` (line 50)

All helpers are namespaced as `tide.<name>`. Chart name is lowercase-dash compliant (`tide`, `tide-crds`).

---

### HELM-04: Standard labels on every object

**Classification**: DRIFT

**Evidence:** `_helpers.tpl:33-44` provides `tide.labels` (includes `helm.sh/chart`, `app.kubernetes.io/version`, `app.kubernetes.io/managed-by`) and `tide.selectorLabels` (`app.kubernetes.io/name`, `app.kubernetes.io/instance`). However, several templates apply labels inconsistently:
- `charts/tide/templates/deployment.yaml:5-8`: uses `{{- include "tide.labels" . | nindent 4 }}` — correct
- `charts/tide/templates/deployment.yaml:14-18` (selector): uses `{{- include "tide.selectorLabels" . | nindent 6 }}` — correct
- Selector labels are frozen to name+instance — correct for immutability

One gap: the `per-namespace-rolebinding.yaml` template applies `{{- include "tide.labels" $ | nindent 4 }}` + one extra `app.kubernetes.io/component` label — this is fine, the base label set is present.

**Notes:** The `helm.sh/chart` label is present in `tide.labels`, which includes the chart version. The full standard label set (`app.kubernetes.io/name`, `instance`, `version`, `managed-by`, `helm.sh/chart`) is covered. No missing required labels found after spot-check.

**Recommendation [NICE-TO-HAVE]:** Add `app.kubernetes.io/part-of: tide` to `tide.labels` for multi-chart ecosystems.

---

### HELM-05: CRD handling — upgrade story documented

**Classification**: DEVIATION (with residual DRIFT on uninstall documentation)

**Evidence:** `charts/tide-crds/templates/` contains the six CRD templates as standard Kubernetes manifests (`project-crd.yaml`, `plan-crd.yaml`, etc.) — NOT in a `crds/` directory. This means:
- `helm upgrade tide-crds ...` WILL upgrade CRDs (unlike the `crds/` dir which is install-once)
- `helm uninstall tide-crds` WILL delete the CRD objects, which cascades to deleting all CRs (Projects, Plans, etc.)

**Documented:** `charts/tide-crds/Chart.yaml` description: "Splitting the CRDs into their own chart lets `helm upgrade tide ...` roll the controller without reinstalling or annotating CRDs." `docs/INSTALL.md:99`: "The chart pair is intentionally NOT wired as a Helm dependency (per D-E1's upgrade safety rationale)."

**Residual DRIFT:** `docs/INSTALL.md` has no `## Upgrade` or `## Uninstall` section. The uninstall danger — that `helm uninstall tide-crds` deletes all CRs — is not documented. Users following the "Quickstart" path have no warning that `helm uninstall tide-crds` is destructive.

**Deviation cites:** Deviations table row 11: "Helm chart pair (controller chart + CRDs subchart via pinned helmify v0.4.17)" — CLAUDE.md constraint.

**Recommendation [NICE-TO-HAVE]:** Add an `## Upgrade` section to `docs/INSTALL.md` documenting:
1. `helm upgrade tide-crds` is safe (CRDs are templates, they upgrade)
2. `helm uninstall tide-crds` DELETES all TIDE CRs — always drain/back-up Projects first
3. The recommended upgrade order (tide-crds first, then tide)

---

### HELM-06: No `latest` image tags; defaults to AppVersion

**Classification**: PASS

**Evidence:** `charts/tide/values.yaml` sets `tag: ""` for all images (`tide-controller` at line 39, `tide-stub-subagent` at 140, `tide-credproxy` at 144, `tide-push` at 155, `tide-claude-subagent` at 165, `tide-reporter` at 175, `tide-dashboard` at 254). The deployment template resolves empty tag to `.Chart.AppVersion`:
```
image: {{ .Values.controllerManager.manager.image.repository }}:{{ .Values.controllerManager.manager.image.tag | default .Chart.AppVersion }}
```
The `busybox` image is pinned to `tag: "1.36"` (not latest — a named tag, but mutable). No `latest` tags anywhere.

**Notes:** `values.yaml:135-136` documents digest-pinning override: "In production, prefer pinning by @sha256 digest." No dedicated `image.digest` field exists per image — digest is supported by overriding the `repository` field to include `@sha256:...`. This is a workable pattern but non-standard.

**Recommendation [NICE-TO-HAVE]:** Add a `digest: ""` field alongside each `repository`/`tag` pair in values, and template the image reference as `{{ if .digest }}{{ .repository }}@{{ .digest }}{{ else }}{{ .repository }}:{{ .tag | default $.Chart.AppVersion }}{{ end }}`. This makes digest-pinning a first-class helm parameter.

---

### HELM-07: `NOTES.txt` present

**Classification**: DRIFT

**Evidence:** `ls charts/tide/templates/NOTES.txt` returns nothing — the file does not exist. After `helm install tide ...`, users see no post-install instructions about how to apply a Project, access the dashboard, or check status.

**Recommendation [NICE-TO-HAVE]:** Add `charts/tide/templates/NOTES.txt` with at minimum:
- kubectl port-forward command for the dashboard
- `kubectl apply -f <sample project>` pointer
- Link to `docs/INSTALL.md`

---

### HELM-08: `helm lint` clean; chart-testing in CI

**Classification**: DRIFT

**Evidence:** `.github/workflows/ci.yaml:170-174`: `helm lint charts/tide` and `helm lint charts/tide-crds` run in CI. However, `ct lint` (chart-testing) and `ct install` (integration install against a kind cluster) are NOT present in any CI workflow. `kubeconform` against the pinned `kubeVersion` range is also absent.

**Recommendation [NICE-TO-HAVE]:** Add `ct lint` and `ct install` jobs to CI (using `helm/chart-testing-action`). `ct install` requires a kind cluster — this fits the existing dry-run infrastructure. Additionally add `kubeconform` to validate rendered templates against the target Kubernetes version schema.

---

### HELM-09: `helm template` passes kubeconform

**Classification**: DRIFT (assumed — not confirmed in CI)

**Evidence:** No `kubeconform` or server-side dry-run step found in `.github/workflows/ci.yaml`, `lint.yml`, `test.yml`. `helm lint` runs but it does not validate the rendered YAML against the Kubernetes API schema.

**Recommendation [NICE-TO-HAVE]:** Add a kubeconform step:
```yaml
- run: helm template charts/tide | kubeconform -strict -kubernetes-version 1.33.0
```

---

### HELM-10: Optional integrations guarded behind flags

**Classification**: DEVIATION

**Evidence:** `charts/tide/templates/servicemonitor.yaml:1`: `{{- if .Values.prometheus.serviceMonitor.enabled }}`. Default in `values.yaml`: `prometheus.serviceMonitor.enabled: false`. cert-manager is a documented hard prerequisite (not optional) — `docs/INSTALL.md:101` documents it as a required pre-install step.

**Deviation cites:** Deviations table row 3: "`prometheus.enabled=false` chart default" — CLAUDE.md constraint.

---

### HELM-11: Helm test hooks

**Classification**: DRIFT

**Evidence:** `ls charts/tide/templates/tests/` — directory does not exist. No Helm test hooks present in either chart. Post-install smoke check is not available via `helm test tide`.

**Recommendation [NICE-TO-HAVE]:** Add `charts/tide/templates/tests/test-connection.yaml` — a minimal test Job that checks the manager health endpoint (`/healthz`) is reachable. A passing `helm test tide` gives installers confidence the chart deployed correctly.

---

## Section 6: Image / Build Supply Chain

### SUPPLY-01: Multi-stage builds

**Classification**: PASS

**Evidence:** `Dockerfile:3`: `FROM --platform=$BUILDPLATFORM golang:1.26 AS builder`. `Dockerfile:28`: `FROM gcr.io/distroless/static:nonroot` (runtime stage). `CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build` at `Dockerfile:24` produces a static binary. `Dockerfile.dashboard` follows the identical pattern (`Dockerfile.dashboard:17,31`).

---

### SUPPLY-02: Distroless/nonroot base

**Classification**: PASS

**Evidence:**
- `Dockerfile:28`: `FROM gcr.io/distroless/static:nonroot`
- `Dockerfile.dashboard:31`: `FROM gcr.io/distroless/static:nonroot`
- `USER 65532:65532` in both (`Dockerfile:31`, `Dockerfile.dashboard:34`)

No shell, no package manager in runtime images. Debug variant not present (no `:debug` tag).

---

### SUPPLY-03: Numeric non-root USER

**Classification**: PASS

**Evidence:** `Dockerfile:31`: `USER 65532:65532`. `Dockerfile.dashboard:34`: `USER 65532:65532`. This matches distroless `:nonroot` UID and satisfies `runAsNonRoot: true` verification without runtime UID lookup.

---

### SUPPLY-04: Base images pinned by digest

**Classification**: DRIFT

**Evidence:** `Dockerfile:3`: `FROM --platform=$BUILDPLATFORM golang:1.26 AS builder` — tag-pinned, NOT digest-pinned. `Dockerfile:28`: `FROM gcr.io/distroless/static:nonroot` — tag-pinned, NOT digest-pinned. Neither Dockerfile pins base images with `@sha256:` digests. A mutable `:nonroot` tag could change between builds, making builds non-reproducible and enabling supply-chain substitution.

**Recommendation [NICE-TO-HAVE]:** Pin base images by digest:
```dockerfile
FROM --platform=$BUILDPLATFORM golang:1.26@sha256:<pinned-digest> AS builder
FROM gcr.io/distroless/static:nonroot@sha256:<pinned-digest>
```
Use `docker buildx imagetools inspect` to obtain digests. Refresh on scheduled cadence (monthly or on CVE notification).

---

### SUPPLY-05: SBOM generated per image

**Classification**: DRIFT

**Evidence:** `.goreleaser.yaml:16`: comment states "SLSA provenance + cosign signatures are deferred to v1.x hardening." No `sboms:` section in `.goreleaser.yaml`. No syft/SBOM generation step in `.github/workflows/release.yaml`. Images ship without SPDX or CycloneDX attestations.

**Recommendation [NICE-TO-HAVE]:** Add goreleaser `sboms:` stanza or a post-build `syft` step in the release workflow. GHCR supports OCI artifact attestation via `docker buildx imagetools create`. This is explicitly deferred to v1.x — recording as backlog.

---

### SUPPLY-06: Images signed with cosign

**Classification**: DRIFT

**Evidence:** `.goreleaser.yaml:117-118`: "v1.0 ships unsigned. SLSA provenance + cosign signatures are deferred to v1.x." No `signs:` section in `.goreleaser.yaml`. No `cosign sign` step in `.github/workflows/release.yaml`.

**Recommendation [NICE-TO-HAVE]:** Add keyless cosign signing to the release workflow after push:
```yaml
- uses: sigstore/cosign-installer@v3
- run: cosign sign --yes ghcr.io/jsquirrelz/${{ matrix.component }}:${{ env.IMAGE_TAG }}
```
Keyless signing uses the GitHub Actions OIDC token — no long-lived key required. This is explicitly deferred to v1.x.

---

### SUPPLY-07: Multi-arch manifests

**Classification**: PASS

**Evidence:** `.github/workflows/release.yaml:242,316`: "Multi-arch: linux/amd64 + linux/arm64 via buildx + QEMU" with `platforms: linux/amd64,linux/arm64`. `Dockerfile:3,5`: `FROM --platform=$BUILDPLATFORM golang:1.26 AS builder` + `ARG TARGETARCH` — correctly uses cross-compilation rather than QEMU for the Go build stage. QEMU is used only for the node runtime stage in `tide-claude-subagent`.

---

### SUPPLY-08: Vulnerability scan in CI

**Classification**: DRIFT

**Evidence:** No `trivy`, `grype`, or equivalent image scan step found in `.github/workflows/ci.yaml`, `release.yaml`, `dry-run.yaml`, `lint.yml`, or `test.yml`. Images ship without a vulnerability scan gate.

**Recommendation [NICE-TO-HAVE]:** Add a Trivy scan step in the release workflow (or a nightly scan against the published images):
```yaml
- uses: aquasecurity/trivy-action@master
  with:
    image-ref: ghcr.io/jsquirrelz/${{ matrix.component }}:${{ env.IMAGE_TAG }}
    severity: HIGH,CRITICAL
    exit-code: '1'
```
Gate on HIGH/CRITICAL to avoid blocking on LOW/MEDIUM noise.

---

### SUPPLY-09: Image inventory completeness

**Classification**: PASS (v1.0.0 post-fix)

**Evidence:** Chart `values.yaml` references these 7 GHCR images:
1. `ghcr.io/jsquirrelz/tide-controller` (line 38)
2. `ghcr.io/jsquirrelz/tide-stub-subagent` (line 139)
3. `ghcr.io/jsquirrelz/tide-credproxy` (line 143)
4. `ghcr.io/jsquirrelz/tide-push` (line 154)
5. `ghcr.io/jsquirrelz/tide-claude-subagent` (line 164)
6. `ghcr.io/jsquirrelz/tide-reporter` (line 174)
7. `ghcr.io/jsquirrelz/tide-dashboard` (line 253)

Release workflow matrix (`.github/workflows/release.yaml:260-279`) publishes:
1. `tide-controller`
2. `tide-dashboard`
3. `tide-stub-subagent`
4. `tide-credproxy`
5. `tide-push`
6. `tide-claude-subagent`
7. `tide-reporter`

Both lists are 7 images and match exactly. `tide-reporter` was the ship-blocker in the original v1.0.0 push (missing from the 6-image inventory prior to commit `aefd203`). The fix landed before the rc.6 green run.

**Notes:** `busybox:1.36` (envelope-writer init container, `values.yaml:147-148`) is a Docker Hub image, not a GHCR-published TIDE image — it is referenced in values but not in the GHCR publish matrix, which is correct (it's a third-party base image, not a TIDE artifact). This is expected and acceptable.
