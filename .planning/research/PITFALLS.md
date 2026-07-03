# Domain Pitfalls — v1.0.7 First-Run Paper Cuts: Run Integrity & Operator Ergonomics

**Domain:** Adding first-external-run fixes to a shipped Go controller-runtime operator (TIDE v1.0.6)
**Feature areas covered:** (1) integration-miss gate / git integrate serialization, (2) GPG-signed bot commits via pure-Go openpgp, (3) `spec.git.baseRef` CRD field + CEL, (4) ConfigMap-based artifact persistence for the dashboard artifact view, (5) Claude 5 pricing-table rows, (6) Prometheus setup documentation, plus v1.0.6 tech-debt carry
**Researched:** 2026-07-03
**Confidence:** HIGH for codebase-grounded pitfalls (direct inspection of `pkg/git/integrate.go`, `internal/subagent/anthropic/pricing.go`, `cmd/tide-push/main.go`, `api/v1alpha2/`, `charts/`), HIGH for pricing facts (claude-api skill, verified 2026-07-03), MEDIUM for web-verified ecosystem claims (per classify-confidence seam: cross-verified websearch/webfetch)

**Binding constraints that pitfalls below must not violate:** CRD-`.status`-only persistence (no external DB); the Helm chart is a FIXED contract (binary catches up to chart; schema changes ride a chart version bump); wave-boundary failure semantics stay exact; planner/executor pools stay separately sized; waves are derived, never cached.

---

## Critical Pitfalls

### P1: Serializing the integrate race at the wrong layer (file locks on the PVC)

**What goes wrong:**
The observed race (first external run, 2026-07-03): two same-wave tasks both reached Succeeded, both worktree branches got their authored commit, but only one `tide: integrate` merge landed on the run branch — the other was silently lost, and the run shipped `Complete` missing a declared deliverable. `IntegrateTaskBranches` (`pkg/git/integrate.go`) shells out to `git merge --no-ff` inside a *single shared* integration worktree at `worktrees/run-<branch>/` on the RWO PVC. Two near-simultaneous integrations in the same worktree hit git's single-writer model: the loser either fails loudly on `index.lock` ("File exists") *or* — worse — silently loses work, because the index has one slot per path and operations that read HEAD+index (merge, checkout) don't know another process committed moments earlier. The idempotent worktree-provision check (`strings.Contains(msg, "already exists")` at integrate.go:76) can also mask a half-provisioned worktree from a concurrent racer.

The tempting fix — an `flock`/lockfile on the PVC guarding the integration worktree — trades one race for a worse operational failure: a pod OOM-killed or evicted mid-merge leaves a stale lockfile (a lockfile-existence protocol, unlike a kernel flock, does not release on process death), deadlocking every subsequent integration until a human deletes it. And any future move from RWO to RWX/NFS breaks `flock` semantics entirely.

**Why it happens:**
The integration step is invoked from tide-push Jobs (`cmd/tide-push --integrate-task-branches=...`, dispatched via `internal/controller/boundary_push.go` at two call sites) — Job-level concurrency is controlled by the K8s control plane, not by anything on the filesystem, so filesystem locking looks like the local fix.

**How to avoid:**
Serialize at the control plane, where TIDE already has a single writer: the controller. Options in order of preference:
1. **Single-dispatcher rule:** the controller never has two integrate-carrying push Jobs in flight for the same project. It already tracks Job state; gate dispatch of a new integrate Job on the previous one's terminal state (mirror the D3 in-flight `client.List` count-gate pattern from v1.0.6 Phase 32 — but note that pattern *parks* excess dispatches; here the parked integrate must be *queued and re-dispatched*, not dropped).
2. **Batch, don't race:** collect the wave's Succeeded task branches and pass them as one ordered `--integrate-task-branches` list to one Job (the loop in integrate.go is already sequential and ordered). This is the degenerate-case fix and preserves wave throughput completely — see P2.
3. If a filesystem lock is unavoidable, use kernel `flock(2)` (auto-released on process death), never a lockfile-existence protocol, and add a lease TTL anyway.

**Warning signs:** A plan that adds `os.OpenFile(lockpath, O_CREATE|O_EXCL)` or a `.lock` file next to the bare repo; any design that assumes "the PVC serializes access"; integrate logic that catches and ignores `index.lock` errors.

**Phase to address:** Integration-miss gate phase (the run-integrity headline). This is the core mechanism decision — make it explicitly at planning, not mid-execution.

---

### P2: Over-serialization — locking task execution instead of just the run-branch write

**What goes wrong:**
The serialization fix creeps upward: a mutex/gate ends up around task Job dispatch or wave advancement instead of around the integrate step alone. Wave parallelism — the whole point of derived waves — silently degrades to sequential execution. On a 6-task wave that's a 6× wall-clock regression, and it violates the spec's wave-boundary contract (same-wave siblings were declared independent and must run concurrently).

**Why it happens:**
"Serialize the integration" is ambiguous about scope. The safest-*looking* interpretation (serialize everything that touches git) includes the per-task worktree commits in `internal/harness/commit.go` — but those write to *per-task* worktrees/branches and don't race each other; only the shared run-branch integration worktree is contended.

**How to avoid:**
State the invariant precisely in the plan: **tasks execute and commit to their own worktree branches fully in parallel; only merges into the run branch serialize.** Integration is O(seconds) per task; serializing it costs nothing measurable. Add an integration-tier test with a 2-parallel-task wave (the todo already calls for this repro in the kind suite) and assert both task pods overlapped in time while both integrate commits landed.

**Warning signs:** A semaphore acquired before `r.Create(job)` for task Jobs; wave N+1 tasks observably starting one-at-a-time; test assertions that pass with sequential execution.

**Phase to address:** Integration-miss gate phase — encode the invariant in the phase's success criteria.

---

### P3: A non-idempotent completeness gate — counting merge commits instead of asking git about reachability

**What goes wrong:**
The new gate ("every Succeeded task's worktree branch has a merge commit reachable from the run branch before boundary push / Complete") gets implemented as "count `tide: integrate` merge commits == count Succeeded tasks." That breaks three ways:
1. **Retry double-merge:** re-running `git merge --no-ff <already-merged-branch>` prints "Already up to date" and creates *no* commit — a retried integration after a partial failure yields fewer merge commits than tasks, permanently failing the gate even though every branch IS integrated.
2. **Empty-diff tasks:** a task can succeed with no file changes (its branch tip == the base). There is no merge commit to find; the gate must accept an explicit empty-diff marker (the todo already anticipates this).
3. **Restart re-derivation:** per the resumability constraint, a fresh controller must be able to re-evaluate the gate from git + the completed-task set alone — not from an in-memory "merges I performed" list, and not from anything cached in `.status` (waves are derived, never cached; the same discipline applies here).

**How to avoid:**
The idempotent predicate is `git merge-base --is-ancestor <task-branch-tip> <run-branch>` (exit 0 = integrated), evaluated per Succeeded task at gate time. It is true after a `--no-ff` merge, true after a retried no-op merge, true for a fast-forward, and rederivable after restart. Record the gate *outcome* as a status condition (with the failing task UIDs in the message), but always recompute from git.

**Warning signs:** Gate logic that greps `git log` for the `tide: integrate` message format; a `.status` field storing "integratedTaskUIDs" that the gate trusts without re-verification; gate failures on tasks whose diff was empty.

**Phase to address:** Integration-miss gate phase. Also stamp `status.git.lastPushedSHA` here (the lease bookkeeping shares the gap per the findings) — and note its update is a Project-status write racing the usage-rollup writers, so it needs the same `RetryOnConflict` + `MergeFromWithOptimisticLock` pattern as the W1 tech-debt item (see P12).

---

### P4: Signing the integrate merge commits — go-git can't do it, and the obvious workarounds silently break history

**What goes wrong:**
The signed-commits plan says "sign at all three commit sites via go-git `CommitOptions.SignKey`." Two of the three sites (harness `internal/harness/commit.go`, tide-push staging commits via `pkg/git/commit.go`) use go-git and can take `SignKey`. The third — the integrate merge commits in `pkg/git/integrate.go` — **shells out to the git CLI** precisely because go-git v5 only implements `FastForwardMerge` (`ErrUnsupportedMergeStrategy` for everything else; this is the documented D-01 rationale in the file header). The git CLI signs via an external `gpg` program — which the "no gpg binary in images" decision forbids. Naive workarounds each fail:
- `git merge --no-commit` + go-git `wt.Commit(...)` with SignKey: go-git's commit builds parents from HEAD only — it does not read `MERGE_HEAD` — so the "merge" commit gets one parent, silently flattening the wave-parallelism topology the `--no-ff` design exists to preserve (and breaking P3's ancestry predicate for the merged branch).
- Leaving integrate commits unsigned: on repos with branch protection requiring signed commits, the push is rejected outright — TIDE hard-blocks (the exact scenario the feature exists to fix); on ordinary repos the run branch shows a mix of Verified and Unverified commits.

**Why it happens:**
"Three commit sites" reads as three instances of the same change; they are two go-git sites and one CLI site with a fundamentally different signing path.

**How to avoid:**
Decide the mechanism at planning, as a first-class design choice. Viable options:
1. **Pure-Go `gpg.program` shim:** ship a tiny Go binary (the images already carry Go binaries) that implements the `gpg --status-fd=2 -bsau <key>` interface git invokes for signing, backed by ProtonMail/go-crypto; wire it via `git -c gpg.program=/usr/local/bin/tide-gpg-shim merge ...`. Keeps the CLI merge, keeps `--no-ff`, no gpg binary.
2. **Plumbing-level merge commit via go-git:** run `git merge --no-commit --no-ff`, read the resulting index tree and `MERGE_HEAD`, then construct the merge commit object *manually* (two `ParentHashes`, signed) via go-git's object storage. Correct but the most code; must be tested against conflict states.
3. **Explicitly scope v1.0.7 to sign only the two go-git sites** and document the integrate-commit gap — acceptable only if the docs say "require-signed-commits branch protection is not yet supported," which undercuts the feature's stated purpose. Prefer 1.

**Warning signs:** A PLAN.md that lists `pkg/git/integrate.go` in `files_modified` with only a `SignKey` plumb-through; a test that asserts signatures on task commits but never inspects an integrate merge commit; merge commits with one parent appearing on the run branch.

**Phase to address:** Signed-commits phase — flag it `research: true`; the shim-vs-plumbing choice deserves a spike.

---

### P5: Signed-but-Unverified — key/identity mismatches that only surface on the git host

**What goes wrong:**
Everything signs cleanly in-cluster, but GitHub/GitLab/Gitea still show **Unverified**. The signature is cryptographically fine; the *verification policy* fails. Verified (MEDIUM confidence, GitHub docs): the **committer** email (not author) must match an email in the signing key's UID identities *and* be a verified email on the account that uploaded the public key. Failure modes specific to this codebase:
- `cmd/tide-push/main.go:134` hardcodes `tide-bot@tideproject.k8s` — a non-routable domain no account can verify. Until bot identity is uniformly configurable across all three sites (the todo's second half), signing produces Unverified badges by construction.
- The three sites read identity from different places today (env in harness/integrate, hardcoded in tide-push); if one site's email drifts from the key's UID, only that site's commits show Unverified — a confusing partial state.
- Expired keys or expired signing subkeys: ProtonMail/go-crypto's signing-key selection returns no usable key → hard error at commit time, deep inside a Job, hours into a run.
- Container clock skew: a signature timestamped before the key's creation time (or after expiry) verifies as invalid. VM-based nodes (minikube on macOS — the actual first-run host) drift.

**How to avoid:**
- Make identity + key a single configuration unit: one Secret ref, one bot name/email pair, plumbed identically to all three sites; validate at Project admission (or first reconcile) that the configured email appears in the key's UIDs — fail early with a condition, not at commit #47.
- Document the machine-account recipe (the todo's `docs/project-authoring.md` item): create machine user → verify email → upload public key → the committer email TIDE must be configured with. GitHub noreply addresses work and avoid needing a routable domain.
- Reject expired keys and passphrase-protected keys at load time with explicit errors (see P6).
- Verification test must go end-to-end: `git verify-commit` in-cluster proves the signature; only a live push to a real host proves the *badge*. Include one manual verify step against a real GitHub repo in UAT.

**Warning signs:** UAT criteria that say "commits are signed" without "commits show Verified on the host"; different `TIDE_BOT_EMAIL` defaults across the three binaries after the change.

**Phase to address:** Signed-commits phase (identity unification is a prerequisite task, not a follow-up).

---

### P6: Passphrase-protected and multi-entity armored keys — go-git won't decrypt, and picking `entities[0]` blindly

**What goes wrong:**
go-git's `SignKey` contract (HIGH confidence, pkg.go.dev): the private key "must be present and already decrypted." An operator exports with `gpg --export-secret-keys --armor` — which preserves the passphrase protection — and TIDE fails at signing time with an opaque go-crypto error. Separately, `openpgp.ReadArmoredKeyRing` returns an `EntityList`; keyrings routinely contain multiple entities, and an entity may have a certify-only primary with a signing subkey. Grabbing `entities[0]` and its primary private key signs with the wrong key material or fails on a sign-incapable primary.

**How to avoid:**
- v1.0.7 scope decision: **reject passphrase-protected keys with a clear condition message** ("export an unprotected key: `gpg --export-secret-keys` after removing the passphrase, or generate a dedicated no-passphrase signing key for the machine account"). Supporting a passphrase means a second Secret key (`GIT_SIGNING_KEY_PASSPHRASE`) and calling `entity.PrivateKey.Decrypt()` *plus every signing subkey's* `Decrypt()` — fine as a follow-up, silent-failure-prone as an unplanned add-on.
- Select the signing entity/key deliberately: iterate the EntityList, use go-crypto's signing-key selection (validity at current time, sign-capable), and error if zero or >1 candidates unless configured.
- Load and validate the key **once at reconcile/admission**, not per-commit — a bad key should surface as a Project condition before any subagent spends tokens.

**Warning signs:** `keyring[0]` in the diff; no test fixture with (a) passphrase-protected key, (b) expired key, (c) two-entity keyring; signing errors that only appear in Job pod logs (which GC — see P14).

**Phase to address:** Signed-commits phase.

---

### P7: Signing key exfiltration — the harness commit site runs inside pods executing LLM-authored code

**What goes wrong:**
"Sign at all three commit sites" mounts the armored private key (via the Secret ref) into the **task executor pod** — the same pod where a Claude-driven subagent runs arbitrary tool commands against the workspace. A prompt-injected or simply misbehaving agent can read the key material (`cat` the mounted Secret, `env`) and exfiltrate it in a commit, artifact, or network call. The key then signs *anything* as the trusted bot identity — the feature meant to increase trust becomes a signing oracle.

**Why it happens:**
The three commit sites have different trust levels: integrate and tide-push run TIDE-controlled binaries in TIDE-controlled Jobs; the harness commit happens in the subagent pod *after* untrusted code executed in the same filesystem/process tree.

**How to avoid:**
Options, decide at planning:
1. **Sign only at integrate + tide-push** (controller-trust sites). Task worktree commits stay unsigned; every commit *reachable from the pushed run branch tip via first-parent* is a signed merge/staging commit. Caveat: "require signed commits" branch protection checks *all* commits on the branch, so this still fails hard-protected repos — pair with option 2 if that matters.
2. Restructure the harness so the commit happens in a separate step/container after the agent process exits (init/sidecar split or a post-step in the Job), so the key is never mounted while agent code runs.
3. Accept and document the risk explicitly (single-operator clusters, key scoped to a machine account with access to only the target repo). Minimum bar: the docs must say the signing key's blast radius is the machine account.
Whichever option: mount the Secret only into containers that need it, never into the subagent container's env.

**Warning signs:** The chart mounting `git.signingKeySecretRef` into the subagent container spec; docs recipe that suggests reusing a personal GPG key.

**Phase to address:** Signed-commits phase — this is a scope-defining decision point (ASK-FIRST per operating notes).

---

### P8: `baseRef` silently pruned by a stale CRD schema — the chart-skew trap

**What goes wrong:**
CRDs ship in a **separate chart** (`charts/tide-crds`) from the manager (`charts/tide`). An operator upgrades the app chart/image to 1.0.7 but not the CRD chart (or Helm's crds-handling means the schema never updated). The 1.0.7 CLI/controller sets `spec.git.baseRef`; the API server's structural-schema pruning **silently drops the unknown field** — no error, no warning. The run branches from HEAD, and the operator only notices when the run's diff is against the wrong base. Same trap for any new optional field this milestone adds.

**Why it happens:**
Pruning is by design for structural schemas; unknown fields are removed, not rejected. Two-chart distribution makes version skew an ordinary operator mistake.

**How to avoid:**
- INSTALL/upgrade docs: CRD chart upgrades **first**, always, with the matching version (this is the operational meaning of "binary catches up to chart").
- Cheap runtime guard: when the controller resolves a Project and `spec.git.baseRef` semantics matter, it reads the object the API server stored — if the CLI is the setter, have `tide` CLI read back the applied object and warn when the field it sent is absent. Alternatively (better): the controller can check the CRD's served schema for the field at startup and set a degraded condition.
- The kind-suite regression: apply a 1.0.6-schema CRD + 1.0.7 Project manifest and assert the failure mode is *loud*.

**Warning signs:** `kubectl get project -o yaml` missing a field the manifest set; support reports of "baseRef ignored".

**Phase to address:** baseRef phase (and note it generically for promptFile if that lands as a spec field).

---

### P9: `baseRef` dropped in the v1alpha1⇄v1alpha2 conversion round-trip

**What goes wrong:**
`v1alpha2` is the storage version (`+kubebuilder:storageversion` across `api/v1alpha2/*_types.go`) and `v1alpha1` is still served with conversion in place. Adding `BaseRef` only to `v1alpha2.GitConfig` means any client interaction through the v1alpha1 endpoint (an old CLI, stored automation manifests, `kubectl get project.v1alpha1...` + re-apply) round-trips storage→v1alpha1→storage and **drops the field** unless the conversion carries it. Silent, intermittent, and dependent on which apiVersion each client speaks — the nastiest kind of drift.

**How to avoid:**
Add the field to **both** versions and both conversion directions (the mechanical option, preferred over annotation-preservation gymnastics for a simple string field). Add a round-trip fuzz/unit test for GitConfig conversion — kubebuilder's conversion test scaffolding covers exactly this. Grep the conversion functions for every new field this milestone adds before closing the phase.

**Warning signs:** New field present in only one `api/v1alphaN/project_types.go`; conversion functions untouched in the diff that adds a spec field.

**Phase to address:** baseRef phase.

---

### P10: CEL/defaulting on the new optional field breaking existing stored objects and SSA ownership

**What goes wrong:**
Three related traps when adding validated optional fields to a served CRD under upgrade:
1. **CEL must guard absence:** a rule like `self.baseRef.matches(...)` errors on objects without the field; every rule touching an optional field needs `!has(self.git.baseRef) || ...`. Kubebuilder markers make this easy to forget because the rule sits on the parent struct.
2. **Ratcheting doesn't cover transition rules:** CRD validation ratcheting is GA in 1.33 (TIDE's floor), so a new rule that stored objects violate won't block untouched-field updates — *except* rules referencing `oldSelf`, which are never ratcheted (MEDIUM confidence, KEP-4008). An immutability rule (`self == oldSelf`) on baseRef would be reasonable (changing the base mid-run is nonsense) but will fire on adopted/imported objects the moment any writer touches the field — including a defaulting mutation.
3. **Defaulting surprises under SSA:** giving baseRef a `+kubebuilder:default` (e.g. `""`) materializes the field on every object at next write, changes "absent means HEAD" into "empty-string means HEAD" (two states, one meaning — CEL and controller logic must now handle both), and under server-side apply the API server's defaulted value competes for field ownership with whatever manager applied the spec. Keep **absent** as the only "use HEAD" encoding: no default marker, `omitempty`, nil-safe controller reads.

**How to avoid:** As above per trap; plus validate against a cluster upgraded *from* 1.0.6 state in the kind suite (apply 1.0.6 Projects, upgrade CRDs, exercise updates). Prefer rejecting unresolvable refs at **reconcile with a clear condition** (the todo's stated design) over trying to validate ref-existence in CEL — CEL cannot see the remote repo, and pushing that check into admission would just fail with a worse message.

**Warning signs:** `kubectl apply` on a pre-existing Project failing after the CRD upgrade; `metadata.managedFields` showing two managers owning `spec.git.baseRef`; CEL cost-budget errors on the Project CRD (rules on large structs multiply).

**Phase to address:** baseRef phase.

---

### P11: ConfigMap artifact persistence — etcd caps, informer-cache blow-up, and ownership/GC mistakes

**What goes wrong:**
If the dashboard artifact view lands on the "reporter writes each artifact into a ConfigMap" transport (one of the three options in the todo), four traps stack:
1. **Size:** etcd's ~1.5 MiB limit is on the whole *request* (object + envelope); practical ConfigMap payload ceiling is ~1 MiB. `PLAN.md` and `children/*.json` grow with plan complexity — a 44-plan import-scale run would produce artifacts that flirt with the cap. Un-capped writes fail with `etcdserver: request is too large` *from the reporter Job*, failing a run over a UI feature.
2. **Manager cache:** the first `client.Get` of a ConfigMap through the manager's default (cached) client starts an informer that lists-and-watches **all ConfigMaps in the watch namespace(s)** — now including every large artifact CM — ballooning manager memory. Use `cache.Options.ByObject[&corev1.ConfigMap{}] = {Label: tideArtifactSelector}` or read via `mgr.GetAPIReader()` (uncached) on demand; the dashboard's chi server can also just use the uncached reader since it's a low-QPS human surface.
3. **Ownership/GC:** owner references must point at a **same-namespace** namespaced owner; a cross-namespace ownerRef makes GC treat the owner as absent and deletes the CM. Own the artifact CM by the in-namespace CR (Phase/Plan) whose envelope produced it, so the existing owner-cascade cleans up; label with the CR UID for the dashboard query (UIDs fit the 63-char label-value limit).
4. **Truth drift (PERSIST-02 smell):** ConfigMaps are a *display cache* of PVC artifacts. The moment resume/import logic reads a CM instead of the PVC artifact, the artifacts-as-source-of-truth constraint is violated. The dashboard reads CMs; the orchestrator never does.

**How to avoid:** Cap at a fixed budget (e.g. 512 KiB per artifact), truncate tail-first with an explicit `--- truncated: full artifact at <pvc path> ---` marker the UI renders (see UX table), configure the cache selector in the same PR that adds the first ConfigMap read, and write an envtest asserting a >1 MiB artifact produces a truncated CM rather than a failed reporter Job.

**Warning signs:** Manager RSS jump after the dashboard phase merges; reporter Job failures mentioning "request is too large"; `kubectl get cm -A | wc -l` growth without corresponding cleanup after milestone archive.

**Phase to address:** Dashboard artifact-view phase — transport choice (reader-Job vs ConfigMap vs git) is the phase's opening decision; if ConfigMap wins, these four are the acceptance checklist.

---

### P12: Pricing rows that fix the overcount but introduce an *undercount* on cache writes — and a table that quietly re-rots

**What goes wrong:**
The headline fix is mechanical: `claude-sonnet-5` is missing from `priceTable`, so every Sonnet-5 dispatch bills at the conservative fable-5 tier ($10/$50) — the observed 2.8× overcount ($10.86 tallied vs $3.84 actual). But three subtler traps ride along (pricing facts HIGH confidence, claude-api skill 2026-07-03):
1. **Cache-write TTL multiplier:** the table hard-codes `cacheWrite = 1.25× input` — that's the **5-minute** TTL rate; **1-hour TTL writes cost 2×**. The stream parser (`internal/subagent/anthropic/stream_parser.go`) reads only the total `cache_creation_input_tokens` and has no `ephemeral_5m`/`ephemeral_1h` breakdown. If the `claude` CLI dispatch surface writes 1h-TTL cache entries, TIDE *undercounts* cache-write spend by 1.6× — the first undercount in a system whose stated invariant is "never silently under-report spend" (T-09-01). Verify empirically which TTL the CLI uses (the credproxy tee from the CACHE-01 spike is the ready-made instrument), or price cache writes at 2× as the conservative bound.
2. **Sonnet 5 intro pricing:** the sticker is $3/$15/MTok, but an introductory **$2/$10 applies through 2026-08-31**. A correct sticker-priced table still overcounts ~1.5× vs the console until September. Encode sticker prices (conservative direction is correct for cap enforcement) but *document the expected console delta* in the row comment — otherwise the next tally-vs-console comparison re-files this as a bug.
3. **Fallback visibility:** the `pricing: unknown model` warning goes to the subagent pod's stderr — pods that Job-TTL GC (the exact blind spot the log-drawer fix addresses). The first run's operator only found it by luck. Surface table misses as a Prometheus counter and/or a Project condition, not just stderr; the conservative fallback should be loud at the *project* level.
Also: the table exact-matches the resolved model string. Dated IDs (`claude-haiku-4-5-20251001`) or provider-prefixed IDs (`anthropic.claude-…` on Bedrock, when that backend lands) miss the table and silently hit the fallback.

**How to avoid:** Add rows `claude-sonnet-5` ($3/$15), keep `claude-fable-5` ($10/$50) and `claude-opus-4-8` ($5/$25) — both already present and correct — with cache read 0.10× / write 1.25× *after* verifying the CLI's cache TTL; extend `hack/check-pricing-drift.sh` (per D-03 it drift-checks weekly) to cover the new rows and the intro-pricing expiry date; add the unknown-model metric.

**Warning signs:** `grep 'pricing: unknown model'` non-empty in any run's subagent logs; `BudgetStatus.CostSpentCents` diverging >2× from console; cache-heavy runs (post-CACHE-F1) tallying *below* console.

**Phase to address:** Pricing-table phase (small, but pair it with the telemetry surface so the fallback is observable).

---

### P13: Prometheus docs that produce a ServiceMonitor nobody scrapes

**What goes wrong:**
The operator follows the new INSTALL.md telemetry step: installs kube-prometheus-stack, sets `prometheus.enabled=true`, the ServiceMonitor renders — and every dashboard panel stays empty. kube-prometheus-stack defaults `serviceMonitorSelectorNilUsesHelmValues: true`, meaning Prometheus selects **only** ServiceMonitors labeled `release: <prometheus-helm-release-name>` (MEDIUM confidence, prometheus-community #1631 et al.). TIDE's ServiceMonitor carries TIDE's labels, not the Prometheus release's, so it is silently ignored. This is the single most-reported failure mode of the entire kube-prometheus-stack ecosystem, and a doc step that omits it ships a broken recipe.

Second-order trap: templating the ServiceMonitor behind `.Capabilities.APIVersions.Has "monitoring.coreos.com/v1"` instead of the current explicit `prometheus.enabled` values toggle — Capabilities is empty under `helm template`/`--dry-run` and under GitOps client-side rendering (Flux/Argo), making behavior differ between `helm install` and GitOps. Keep the explicit toggle (current design is right; don't "improve" it).

**How to avoid:**
The INSTALL.md step must include one of, explicitly: (a) a chart value to stamp extra labels on TIDE's ServiceMonitor (`prometheus.serviceMonitor.additionalLabels.release=<their-release>`) — add this value if it doesn't exist, it rides a chart version bump per the FIXED-contract rule; or (b) instruct setting `prometheusSpec.serviceMonitorSelectorNilUsesHelmValues=false` on their stack. End the doc step with a *verification command* — "Prometheus → Status → Targets shows `tide-manager`" or the equivalent `curl`/`promtool` check — so a silent non-scrape is caught in the setup flow, not weeks later. Pin the kube-prometheus-stack version the recipe was tested against; its values keys move across majors.

**Warning signs:** `kubectl get servicemonitor` shows the object but the Prometheus targets page doesn't list it; docs PR with no verification step; NOTES.txt warning only covering `enabled=false` and not the label-selector case.

**Phase to address:** Prometheus setup-step phase (docs + chart NOTES.txt + the dashboard banner in the same phase — see UX table).

---

## Technical Debt Patterns

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| Sign only the two go-git commit sites, defer integrate merges (P4 option 3) | Ships fast, no shim | Mixed Verified/Unverified history; hard-fails signed-commit branch protection — the feature's headline use case | Only with explicit docs caveat; prefer the gpg-shim |
| Reject passphrase-protected signing keys instead of supporting decryption | Avoids a second Secret key + subkey-decrypt plumbing | Operators with org key policies can't use the feature | Acceptable for v1.0.7 with a clear error message; revisit on demand |
| Completeness gate stores its verdict in `.status` and trusts it on resume | Skips a git subprocess per resume | Violates rederivability; stale verdict after manual git surgery (the first run was recovered via manual `format-patch`!) | Never — condition may cache the *last* verdict but gate must recompute |
| Price all cache writes at 1.25× without verifying CLI cache TTL | No spike needed | Silent undercount (1.6×) on cache-heavy runs if CLI uses 1h TTL — inverts the T-09-01 invariant | Never — one teed request or usage-breakdown check settles it |
| Hand-edit `charts/tide/values.yaml` configmap default 16→4 without a chart version bump | One-line fix | Breaks the FIXED-contract rule; installed releases keep the old rendered value anyway | Never — ride the 1.0.7 chart bump, note the changed default in upgrade notes |
| Copy heavy controller-envtest specs to a new tier by file move without a suite-count assertion | Quick tier split | Specs can silently run in *neither* tier (build tags / Ginkgo label filters miss) | Acceptable only with a "total spec count conserved" CI check |
| ConfigMap artifacts without a cache `ByObject` selector | One less config change | Manager memory scales with every ConfigMap in watched namespaces | Never — same PR |

## Integration Gotchas

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| git CLI ↔ go-git in one worktree | Assuming go-git commit after `git merge --no-commit` produces a merge commit | go-git builds parents from HEAD only (ignores MERGE_HEAD) — construct parents explicitly or sign via gpg-program shim (P4) |
| GitHub/GitLab/Gitea Verified badge | Testing `git verify-commit` locally and calling it done | The badge additionally requires committer-email ∈ key UIDs ∈ account verified emails; test against a real host once (P5) |
| ProtonMail/go-crypto keyring load | `ReadArmoredKeyRing(...)[0]` + primary private key | Deliberate signing-key selection (validity, sign-capable, subkeys); reject ambiguity (P6) |
| CRD upgrade via `tide-crds` chart | Upgrading app chart only | CRD chart first, always; new spec fields silently prune against old schemas (P8) |
| v1alpha1⇄v1alpha2 conversion | Adding the field to storage version only | Both versions + both conversion directions + round-trip test (P9) |
| controller-runtime cached client + ConfigMaps | `client.Get` a CM through the manager client "because it's there" | `ByObject` label-selected cache or `GetAPIReader()` for on-demand dashboard reads (P11) |
| kube-prometheus-stack | "ServiceMonitor exists ⇒ scraped" | `release:` label or `serviceMonitorSelectorNilUsesHelmValues=false`; verify on the Targets page (P13) |
| Claude CLI usage JSON | Reading only `cache_creation_input_tokens` total | Check the `cache_creation` 5m/1h breakdown (or tee one request) before choosing the cache-write multiplier (P12) |
| promptFile via ConfigMap ref (if that route wins) | Inlining the CM content into `spec.outcomePrompt` at CLI time and calling it a "ref" | Decide CLI-inline vs true CM-ref at planning; a true ref needs size cap + immutability expectations (an edited CM mid-run changes resumption semantics — artifacts-as-truth says snapshot it) |

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| Serializing task dispatch instead of run-branch merges | Wave wall-clock ≈ sum of task times | P2 invariant + overlap assertion in kind suite | Any wave with ≥2 tasks |
| Un-selectored ConfigMap informer | Manager RSS climbs with cluster CM count | `ByObject` label selector / uncached reader | Clusters with many/large CMs (cert bundles, Helm release CMs) |
| Label-selector LIST for artifact CMs per dashboard render | Dashboard latency on busy namespaces | Fine at TIDE scale (one human, one run); index by UID label; don't add a watch per render | ~thousands of CMs per namespace — not expected in v1 |
| Per-task integrate push Jobs (one Job per task branch) | Job churn, pod scheduling latency dominates integration | Batch the wave's branches into one ordered Job invocation (P1 option 2) | Waves ≥ ~4 tasks on small nodes (the 7.65 GiB constraint) |
| Reader-Job-per-click artifact transport (if that option wins) | Approve-gate review takes ~pod-startup-seconds per artifact | Cache last-read artifact; or prefer ConfigMap transport for planning artifacts | Every gate review — this is the surface's hot path |

## Security Mistakes

| Mistake | Risk | Prevention |
|---------|------|------------|
| Mounting the GPG private key into subagent (LLM-executing) pods | Prompt-injected agent exfiltrates the key; attacker signs as the trusted bot | Sign only in controller-trust Jobs, or isolate the harness commit step from the agent process; document blast radius (P7) |
| Reusing a personal GPG key for the bot | Compromise burns the human's identity | Docs mandate a dedicated machine account + dedicated key |
| Logging the armored key or its fingerprint-adjacent material on load errors | Key material in pod logs / log aggregation | Error messages name the Secret and the failure class, never key bytes; redact via existing `internal/harness/redact` patterns |
| `baseRef` accepted as arbitrary string passed to `git` argv | Argument-injection-shaped refs (`--upload-pack=...`) | Validate refname format (CEL `matches` on safe charset + controller-side `git check-ref-format` equivalent); resolve via explicit `refs/heads/`→tag→SHA fallback as the todo specifies |
| Artifact ConfigMaps readable cluster-wide | Planning artifacts can contain repo internals / prompt content | Keep CMs in the project namespace under existing namespace-scoped RBAC; no new ClusterRole rules for the dashboard read path |

## UX Pitfalls

| Pitfall | User Impact | Better Approach |
|---------|-------------|-----------------|
| Log drawer renders empty for GC'd pods | "Empty component indistinguishable from broken" (verbatim first-run finding) | Three explicit states: loading / streaming / "logs no longer available (pod garbage-collected)"; backend returns a distinguishable pod-gone response, not an empty 200 |
| Truncated artifact shown without a marker | Operator approves a gate having read half a PLAN.md | Visible truncation banner + PVC path + byte counts (P11) |
| Telemetry-disabled shown as empty panels | Operator assumes the dashboard is broken; telemetry stays dark (first-run finding) | Distinguish three states: `prometheus.enabled=false` → banner with the enable recipe; Prometheus unreachable → error state; reachable-but-no-data-yet → "waiting for first scrape (~30–60s)" |
| Budget tally silently priced at conservative fallback | Operator distrusts the whole budget meter (2.8× event) | Unknown-model condition/metric + a dashboard hint when fallback pricing is active (P12) |
| Gate failure message without task identity | Operator can't tell *which* task's work is missing | Completeness-gate condition lists failing task UIDs + branch names (P3) |

## "Looks Done But Isn't" Checklist

- [ ] **Integration serialization:** passes with 1 task — verify with a **2+ parallel-task wave in kind**, asserting task-pod time overlap AND both integrate commits reachable (P1/P2).
- [ ] **Completeness gate:** passes on fresh runs — verify it also passes after (a) a retried integration, (b) an empty-diff task, (c) a controller restart mid-boundary (P3).
- [ ] **`lastPushedSHA`:** stamped on the happy path — verify under a concurrent usage-rollup status write (RetryOnConflict + optimistic lock, W1 pattern).
- [ ] **Signed commits:** `git verify-commit` green in-cluster — verify the **Verified badge on a real git host**, including one integrate merge commit and one tide-push commit (P4/P5).
- [ ] **Signing key absent:** feature off = byte-identical behavior to 1.0.6 (unsigned, current identity defaults) — regression-test the nil path.
- [ ] **baseRef:** works via v1alpha2 — verify a v1alpha1 round-trip preserves it, and an unresolvable ref yields the documented condition, not a worktree-add stack trace (P9/P10).
- [ ] **CRD upgrade:** 1.0.6→1.0.7 upgrade path exercised in kind with pre-existing Projects (ratcheting + pruning behavior observed, not assumed) (P8/P10).
- [ ] **Pricing:** new rows present — verify the CLI cache-TTL question is answered with evidence (teed request or usage breakdown), and `hack/check-pricing-drift.sh` covers the new rows (P12).
- [ ] **Prometheus step:** doc followed on a clean cluster ends with TIDE visible on the **Targets page**, not just a rendered ServiceMonitor (P13).
- [ ] **Envtest tier split:** total executed spec count across tiers equals the pre-split count (no spec runs in zero tiers).
- [ ] **Chart values:** every new value (`git.signingKeySecretRef`, ServiceMonitor labels, plannerConcurrency default) landed via a chart version bump with the binary reading chart-first (FIXED contract).

## Recovery Strategies

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| Integration miss shipped again (gate bug) | MEDIUM | Same as first run: `git format-patch` from the PVC bare repo's task branch, apply to run branch, force-with-lease push; then treat as gate regression with the branch preserved as fixture |
| Stale lockfile deadlock (if P1 ignored) | LOW | Delete lock on PVC via reader pod; then replace lockfile with control-plane serialization |
| Unverified badges post-release | LOW | Config-only: fix bot email / upload key to machine account; already-pushed commits stay Unverified (history rewrite not sanctioned) — document, don't rewrite |
| Field pruned by stale CRD | LOW | Upgrade `tide-crds` chart, re-apply Project; no data loss (field was never stored) |
| Oversized artifact CM failing reporter Jobs | MEDIUM | Hotfix cap+truncate; delete failed CMs by label; artifacts remain intact on PVC (truth untouched) |
| Undercounted budget discovered mid-run | MEDIUM | `absoluteCapCents` is the backstop — lower it to compensate while the multiplier fix ships; reconcile against provider console |

## Pitfall-to-Phase Mapping

| Pitfall | Prevention Phase | Verification |
|---------|------------------|--------------|
| P1/P2/P3 (race, over-serialization, gate idempotency) | Integration-miss gate phase | 2-parallel-task kind repro: overlap + both merges + gate green after retry/restart/empty-diff |
| P4/P5/P6/P7 (merge signing, badge policy, key handling, key exposure) | Signed-commits phase — **flag `research: true`** (gpg-shim vs plumbing spike; key-exposure scope decision is ASK-FIRST) | Verified badge on real host incl. integrate commit; key-error conditions surface pre-spend; nil-key path unchanged |
| P8/P9/P10 (pruning, conversion, CEL/defaulting) | baseRef phase | kind upgrade-path test from 1.0.6 state; conversion round-trip test; unresolvable-ref condition |
| P11 (ConfigMap persistence) | Dashboard artifact-view phase (transport decision first) | >1 MiB artifact → truncated CM + marker; manager RSS flat; owner-cascade cleanup observed |
| P12 (pricing under/overcount, fallback visibility) | Pricing-table phase | Tally within expected delta of console on a live spot-check; unknown-model metric fires in a fixture |
| P13 (ServiceMonitor not scraped, empty-panel UX) | Prometheus setup-step phase | Clean-cluster doc walkthrough ends on Targets page; dashboard shows banner when disabled |
| Tech-debt carry (W1 RetryOnConflict, configmap default, envtest tiers) | Tech-debt phase | W1 marker test; chart bump renders 4; spec-count conservation check |

## Sources

- Direct code inspection (HIGH): `pkg/git/integrate.go`, `pkg/git/commit.go`, `cmd/tide-push/main.go`, `internal/controller/boundary_push.go`, `internal/controller/push_helpers.go`, `internal/subagent/anthropic/pricing.go`, `internal/subagent/anthropic/stream_parser.go`, `internal/harness/`, `api/v1alpha1|2/project_types.go`, `charts/tide` + `charts/tide-crds`
- First-run findings (HIGH): `.planning/todos/pending/2026-07-03-*.md` (integration miss, pricing, signed commits, baseRef, prometheus, dashboard), `.planning/PROJECT.md`
- Claude pricing & cache economics (HIGH): claude-api skill (cached 2026-06-24, read 2026-07-03) — Sonnet 5 $3/$15 sticker with $2/$10 intro through 2026-08-31; cache read ~0.1×, write 1.25× (5m TTL) / 2× (1h TTL)
- go-git `CommitOptions.SignKey`/`Signer` contract + merge limitation (MEDIUM per seam, official docs): [pkg.go.dev go-git v5](https://pkg.go.dev/github.com/go-git/go-git/v5)
- GitHub Verified requirements (MEDIUM per seam, official docs): [About commit signature verification](https://docs.github.com/en/authentication/managing-commit-signature-verification/about-commit-signature-verification), [Using a verified email address in your GPG key](https://docs.github.com/en/authentication/troubleshooting-commit-signature-verification/using-a-verified-email-address-in-your-gpg-key)
- CRD validation ratcheting (MEDIUM per seam): [KEP-4008 CRD ratcheting](https://github.com/kubernetes/enhancements/tree/master/keps/sig-api-machinery/4008-crd-ratcheting), [k8s CRD docs](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/)
- kube-prometheus-stack selector trap (MEDIUM per seam): [prometheus-community/helm-charts#1631](https://github.com/prometheus-community/helm-charts/issues/1631), [#2381](https://github.com/prometheus-community/helm-charts/issues/2381)
- git single-writer locking / parallel-agent worktree races (MEDIUM per seam): [git-worktree docs](https://git-scm.com/docs/git-worktree/2.33.0), [anthropics/claude-code#55724](https://github.com/anthropics/claude-code/issues/55724), [dev.to index.lock](https://dev.to/rijultp/fixing-common-git-lock-errors-understanding-and-recovering-from-gitindexlock-47ej)

---
*Pitfalls research for: TIDE v1.0.7 First-Run Paper Cuts*
*Researched: 2026-07-03*
