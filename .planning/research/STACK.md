# Stack Research — TIDE v1.0.7 First-Run Paper Cuts

**Domain:** Kubernetes operator additions — GPG-signed bot commits, Claude 5 pricing rows, promptFile, dashboard artifact views (Run Integrity & Operator Ergonomics)
**Researched:** 2026-07-03
**Confidence:** HIGH — every load-bearing claim below was verified against a primary source today (go-git `options.go`, TIDE's own `go.mod`, npm registry JSON, official Anthropic pricing docs fetched live). The generic web-provider confidence classifier tiers raw search results LOW; per-claim confidence is noted in Sources where it diverges.

**Scope note:** This milestone needs almost no new stack. The existing validated stack (Go 1.26, controller-runtime v0.24.x, go-git v5, React 18 + Tailwind v4 dashboard) covers the integration-miss gate, `spec.git.baseRef`, log-drawer states, and the Prometheus setup step with zero additions. New surface: **one Go dep promotion** (indirect → direct), **three npm packages**, and **one data-only pricing table**.

## Recommended Stack

### Core Technologies (new for v1.0.7)

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|-----------------|
| `github.com/ProtonMail/go-crypto/openpgp` | **v1.1.6** (promote indirect → direct) | Pure-Go GPG: parse armored private key from a K8s Secret, sign commits | Already in TIDE's dependency graph — pinned as an *indirect* dep of go-git v5.19.0 (`go.mod:43`). go-git's `CommitOptions.SignKey` is typed as `*openpgp.Entity` from **this fork specifically**, so it is the only compatible choice. Zero new supply-chain surface, no `gpg` binary in any image. |
| go-git v5 `CommitOptions.SignKey` | v5.19.0 (already pinned) | Sign at all three commit sites (harness, integrate, tide-push) | Verified against `options.go`: `SignKey *openpgp.Entity` — "nil means the commit will not be signed. The private key must be present and already decrypted." go-git produces the armored detached OpenPGP signature internally; the signing call site is a one-field change per `Commit()`. |
| `react-markdown` | **v10.1.0** | Render planning artifacts (MILESTONE.md, PLAN.md) in the dashboard artifact view | Verified on npm 2026-07-03; peerDeps `react >=18` — compatible with the dashboard's React 18.3.1. Renders markdown to real React elements with **no `dangerouslySetInnerHTML`** — XSS-safe by construction, which matters because artifacts are LLM-authored content. |
| `remark-gfm` | **v4.0.1** | GFM tables, task lists, strikethrough in artifacts | Verified on npm. Planner templates emit GFM tables (task tables, dependency tables); CommonMark-only rendering would break the approve-gate review surface. |
| `@tailwindcss/typography` | **v0.5.20** | `prose` styling for rendered markdown | Verified on npm; peerDep `tailwindcss >=3.0.0 \|\| >=4.0.0` — compatible with the dashboard's Tailwind ^4.3.0. Register in CSS (v4 style, no config file): `@plugin "@tailwindcss/typography";` |

### Pricing table rows (data, not a dependency)

Verified 2026-07-03 against `platform.claude.com/docs/en/pricing.md` (official, fetched live) and cross-checked against the models overview page. All USD per MTok; all values are exact integer cents at MTok granularity, so `CostSpentCents` math stays integral.

| Model ID | Input | Output | Cache read (0.1×) | 5m cache write (1.25×) | 1h cache write (2×) |
|----------|-------|--------|-------------------|------------------------|---------------------|
| `claude-fable-5` | $10.00 | $50.00 | $1.00 | $12.50 | $20.00 |
| `claude-opus-4-8` | $5.00 | $25.00 | $0.50 | $6.25 | $10.00 |
| `claude-opus-4-7` | $5.00 | $25.00 | $0.50 | $6.25 | $10.00 |
| `claude-opus-4-6` | $5.00 | $25.00 | $0.50 | $6.25 | $10.00 |
| `claude-sonnet-5` (standard, from 2026-09-01) | $3.00 | $15.00 | $0.30 | $3.75 | $6.00 |
| `claude-sonnet-5` (intro, through 2026-08-31) | $2.00 | $10.00 | $0.20 | $2.50 | $4.00 |
| `claude-sonnet-4-6` | $3.00 | $15.00 | $0.30 | $3.75 | $6.00 |
| `claude-haiku-4-5` | $1.00 | $5.00 | $0.10 | $1.25 | $2.00 |

**Notes for the implementation:**

- **Sonnet 5 is date-dependent.** Recommend encoding the **standard** ($3/$15) rate, not the intro rate: a tally that slightly overcounts during the intro window is conservative for `absoluteCapCents` enforcement and needs no code change on 2026-09-01. If exactness is wanted, add an effective-date column — but that's schema complexity for a two-month window.
- **Match exact IDs, not prefixes.** The 2.8× overcount came from unknown IDs falling back to the most-expensive tier. Current-generation IDs are dateless pinned snapshots (`claude-fable-5`, `claude-opus-4-8`, `claude-sonnet-5`), but **Haiku 4.5's full ID is dated**: `claude-haiku-4-5-20251001` — the API may report either the alias or the full ID depending on surface. Add both spellings (or normalize by stripping a trailing `-YYYYMMDD` before lookup).
- **Fallback behavior should warn, not silently price at max.** Keep a most-expensive fallback for cap safety, but emit a log line / condition when an unpriced model ID is encountered — that's the observable that would have caught this bug on day one.
- Cache multipliers (0.1× read, 1.25× 5m write, 2× 1h write) are uniform across models, so the table can store base input/output only and derive cache rates — fewer rows to keep current.

## Installation

```bash
# Go — promote existing indirect dep to direct (no version change; go-git v5.19.0 pins v1.1.6)
go get github.com/ProtonMail/go-crypto@v1.1.6 && go mod tidy

# Dashboard (dashboard/web/)
npm install react-markdown@10.1.0 remark-gfm@4.0.1
npm install -D @tailwindcss/typography@0.5.20
```

```css
/* dashboard/web CSS entry — Tailwind v4 plugin registration */
@import "tailwindcss";
@plugin "@tailwindcss/typography";
```

## Integration Notes

### GPG signing (pure-Go, no gpg binary)

```go
import "github.com/ProtonMail/go-crypto/openpgp"

entities, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(armoredKeyFromSecret))
// validate: len(entities) >= 1, entities[0].PrivateKey != nil
// if entities[0].PrivateKey.Encrypted → Decrypt(passphrase) (entity + signing subkeys), or reject with a clear condition
_, err = wt.Commit(msg, &git.CommitOptions{
    Author:  &object.Signature{Name: botName, Email: botEmail, When: time.Now()},
    SignKey: entities[0],
})
```

- **`SignKey` over `Signer`.** `CommitOptions` has both; `Signer` (an interface, takes precedence over `SignKey`) is the seam for non-OpenPGP schemes (SSH signing) later. For this milestone `SignKey` is the minimal change at all three commit sites; wrap key loading in one shared helper so a future `Signer` swap is one function. Neither field is deprecated (verified against `options.go`).
- **Key must be decrypted before use** — go-git will not prompt. Recommend documenting the Secret contract as an *unencrypted* armored private key (`gpg --export-secret-keys --armor`), with an optional `passphrase` Secret key handled via `PrivateKey.Decrypt` on the entity **and each signing subkey** if provided.
- **GitHub/GitLab "Verified" badge** requires (a) the committer email to match a UID on the key and (b) the public key uploaded to the bot account. The signing work is therefore coupled to the "bot identity uniformly configurable" fix — the hardcoded tide-push identity would break verification if it diverges from the key's UID. One config surface (name, email, secretRef) feeding all three sites.
- **Secret reach:** signing happens wherever `Commit()` runs. If the harness commit site executes in the subagent container (not the manager), the key Secret must be projected there too — same pattern as the existing git-push creds, but audit all three sites' pod specs.
- **Optionality:** nil `SignKey` = unsigned commit, so "no secretRef configured → unsigned" falls out naturally; no flag plumbing needed beyond the Secret resolution.

### promptFile (outcomePrompt from file) — both routes are zero-dependency

| Route | Mechanism | Costs |
|-------|-----------|-------|
| **CLI-side inlining (recommended)** | `tide apply --prompt-file ./prompt.md` reads the file and sets `spec.outcomePrompt` before create | None: no CRD change beyond docs, no controller change, no RBAC change, no drift semantics. `kubectl`-only users inline the text themselves (status quo). |
| ConfigMap keyRef | `spec.outcomePromptFrom.configMapKeyRef` (reuse `corev1.ConfigMapKeySelector` — already vendored via `k8s.io/api`); controller resolves via the existing client `Get` | New CRD field + CEL (`outcomePrompt` XOR `outcomePromptFrom`), controller Role needs ConfigMap `get` in watch namespaces, and drift semantics: prompt must be resolved **once** (snapshot into status or an annotation at Initialize) or a mid-run ConfigMap edit changes replanning behavior. |

Recommendation: ship CLI-side inlining now; it's the operator-ergonomics fix the first run actually surfaced (a human authoring a long prompt in an editor). Add the ConfigMap route only if a GitOps consumer asks — and note the size ceiling either way: CRD objects live under etcd's ~1.5 MiB request cap, ConfigMaps under a documented 1 MiB data cap, so any realistic prompt fits both.

### Dashboard artifact transport — ConfigMap persistence over reader Jobs

**Recommendation: controller writes each planning artifact into a per-artifact ConfigMap at the level boundary** (owner-ref'd to the level CRD, labeled for lookup), and the chi dashboard handler serves it via the manager's existing cached client.

- **Size:** ConfigMap data (data + binaryData combined) is capped at **1 MiB** (Kubernetes docs; etcd request cap ~1.5 MiB behind it). TIDE planning artifacts are typically 5–50 KiB — two orders of magnitude of headroom. Guard anyway: truncate at ~900 KiB with a "see git" marker rather than failing the boundary.
- **Why not a reader Job:** the artifact PVC is **RWO** — on a multi-node cluster a reader pod can hit volume-attach conflicts with a running task pod on another node (exactly the multi-node infra the next milestone targets); plus pod-startup latency (seconds) on every approve-gate view, plus Job GC and RBAC for log streaming. The three ad-hoc reader pods in the first run are the symptom this feature removes — don't automate the symptom.
- **Why this doesn't violate CRD-status-only persistence:** the ConfigMap is a *display cache*, not truth — git (pushed at level boundaries) remains the artifact source of truth, and the ConfigMap is rederivable/deletable. Do **not** put artifact bodies in CRD `.status` (etcd per-object budget is the reason that constraint exists).
- **Markdown rendering:** `react-markdown` + `remark-gfm` + `prose` classes (versions above). v10 removed the `className` prop — wrap: `<div className="prose prose-invert max-w-none"><ReactMarkdown remarkPlugins={[remarkGfm]}>{md}</ReactMarkdown></div>`. Do **not** add `rehype-raw`: it re-enables raw-HTML passthrough and reintroduces injection risk from model-authored artifacts.

### Features needing NO stack additions

| Feature | Covered by |
|---------|-----------|
| Integration-miss gate (serialize merges, reachability check, `lastPushedSHA`) | go-git v5.19.0: existing merge plumbing + `repo.ResolveRevision`, `object.Commit.IsAncestor` for reachability; serialization is a controller-side mutex/workqueue concern |
| `spec.git.baseRef` | `repo.ResolveRevision(plumbing.Revision(ref))` resolves branch/tag/SHA; reject unresolvable with a clear condition — no new dep |
| Log-drawer loading/streaming/pod-gone states | Existing SSE + client-go pod log API; pure frontend state-machine work |
| Prometheus setup step | Docs (INSTALL.md), chart NOTES.txt, dashboard banner — no code deps; keep `prometheus.enabled=false` default per existing anti-pattern |
| v1.0.6 tech-debt carry (RetryOnConflict, plannerConcurrency default, test-tier split) | `k8s.io/client-go/util/retry` already vendored |

## Alternatives Considered

| Recommended | Alternative | When to Use Alternative |
|-------------|-------------|-------------------------|
| `CommitOptions.SignKey` | `CommitOptions.Signer` interface | When adding a second signature scheme (SSH signing / KMS-held keys). Takes precedence over SignKey; same package. Wrap key loading in one helper now so this swap stays cheap. |
| ProtonMail/go-crypto v1.1.6 (go-git's pin) | Bumping go-crypto to latest (v1.3.x) | Only if go-git itself bumps it. Independent bumps risk type/behavior skew with go-git's internal signing path — same coupling rule as `k8s.io/*`. |
| ConfigMap artifact cache | In-namespace PVC reader Job | If artifacts routinely exceed ~1 MiB (they don't) or if raw task *diffs* (potentially large) later need serving — then a streaming reader is worth its latency. |
| ConfigMap artifact cache | go-git read of the bare repo from the manager | If the manager already has the project PVC mounted at dashboard-serve time; avoids the copy. Verify mount topology before choosing — RWO makes this fragile on multi-node. |
| CLI-side prompt inlining | `spec.outcomePromptFrom.configMapKeyRef` | GitOps pipelines that can't shell out to `tide`; costs a CRD field, RBAC, and resolve-once snapshot semantics. |
| react-markdown | `marked`/`markdown-it` + DOMPurify | Never for this dashboard — string-HTML pipelines need a sanitizer and `dangerouslySetInnerHTML`; react-markdown avoids the class of bug entirely for LLM-authored input. |
| Standard Sonnet 5 pricing row | Effective-dated intro pricing rows | Only if budget-tally exactness during Jul–Aug 2026 matters more than schema simplicity; intro window ends 2026-08-31. |

## What NOT to Use

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| `golang.org/x/crypto/openpgp` | Frozen/deprecated upstream **and** type-incompatible: go-git's `SignKey` is ProtonMail's `openpgp.Entity`, not x/crypto's | `github.com/ProtonMail/go-crypto/openpgp` |
| Shelling out to a `gpg` binary | Adds a binary + keyring state to every image; breaks the pure-Go constraint; keyring permissions in containers are a known pain | go-git `SignKey` (pure Go) |
| Artifact bodies in CRD `.status` | Blows the per-object etcd budget the CRD-status-only constraint depends on | Per-artifact ConfigMap display cache (git = truth) |
| `rehype-raw` / `dangerouslySetInnerHTML` in the artifact view | Re-enables HTML injection from model-authored markdown | react-markdown's default (raw HTML rendered as text) |
| Prefix/substring model-ID matching in the pricing table | The `-fast`, dated-snapshot, and Bedrock-prefixed variants make prefix matching wrong in both directions | Exact-ID lookup with a normalizer for trailing `-YYYYMMDD`, plus a logged most-expensive fallback |
| Hardcoding "$2/$10" for Sonnet 5 | Intro pricing expires 2026-08-31 → silent *under*count after that, the inverse of the bug being fixed | Standard $3/$15 row (conservative during intro) |

## Version Compatibility

| Package | Compatible With | Notes |
|---------|-----------------|-------|
| ProtonMail/go-crypto v1.1.6 | go-git v5.19.0 | go-git's `go.mod` dictates this pin; never bump independently (same rule as `k8s.io/*` ← controller-runtime) |
| react-markdown 10.1.0 | React ≥18 (dashboard: 18.3.1 ✓) | v10 removed `className` prop — style via a wrapper element |
| remark-gfm 4.0.1 | react-markdown 10.x (unified v11 ecosystem) | Both on `unified@^11` / `remark-parse@^11` — matched majors |
| @tailwindcss/typography 0.5.20 | tailwindcss ≥4 (dashboard: ^4.3.0 ✓) | v4 registration is CSS `@plugin`, not `tailwind.config.js` |

## Sources

- `go-git/go-git` `options.go` (master, fetched 2026-07-03) — `CommitOptions.SignKey` / `Signer` field declarations, decrypted-key requirement, precedence, no deprecation — HIGH (primary source)
- `/Users/justinsearles/Workspace/repos/tide/go.mod` — go-git v5.19.0 pinned; ProtonMail/go-crypto v1.1.6 already indirect — HIGH (local evidence)
- `platform.claude.com/docs/en/pricing.md` + `docs/en/about-claude/models/overview.md` (fetched live 2026-07-03) — full per-model input/output/cache-read/5m-write/1h-write table; Sonnet 5 intro pricing through 2026-08-31; cache multipliers 0.1×/1.25×/2× — HIGH (official docs, cross-checked against the claude-api skill's cached table dated 2026-06-24)
- npm registry (`registry.npmjs.org`, fetched 2026-07-03) — react-markdown 10.1.0 (peer react ≥18), remark-gfm 4.0.1, @tailwindcss/typography 0.5.20 (peer tailwindcss ≥4) — HIGH (primary source)
- [react-markdown changelog](https://github.com/remarkjs/react-markdown/blob/main/changelog.md) — v10 `className` removal, v9 React-18 floor — HIGH
- Kubernetes ConfigMap documentation (concepts/configuration/configmap) — 1 MiB data cap — HIGH (stable documented limit)
- [tailwindcss-typography](https://github.com/tailwindlabs/tailwindcss-typography) + [Tailwind v4 plugin discussion](https://github.com/tailwindlabs/tailwindcss/discussions/14551) — `@plugin` registration in v4 — MEDIUM (community-confirmed, matches npm peerDeps)
- Supporting search results: [go-git commit signature verification](https://darkowlzz.github.io/post/git-commit-signature-verification/), [react-markdown repo](https://github.com/remarkjs/react-markdown), [remark-gfm repo](https://github.com/remarkjs/remark-gfm)

---
*Stack research for: TIDE v1.0.7 First-Run Paper Cuts*
*Researched: 2026-07-03*
