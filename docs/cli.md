# `tide` CLI

The `tide` CLI is the operator-facing entry point to a TIDE control plane.
It manages `Project` CRDs, streams Task pod logs, advances human-gated
checkpoints, and inspects budget + wave state — without ever requiring a
separate auth flow (it reuses the same kubeconfig as `kubectl`).

Phase 4 ships ten read/write verbs + `completion`. Plan 04-09 (this plan)
lands the release pipeline; the first tagged release (`v0.1.0`) is cut by
Phase 5 work.

---

## 1. Install

Three install paths. Pick whichever matches your toolchain.

### 1.1 Krew (pending index submission)

[Krew](https://krew.sigs.k8s.io/) is the standard kubectl plugin distribution
channel. TIDE is **not yet in the krew index** — `kubectl krew install tide`
will not resolve until the submission is accepted. The manifest template lives
at `krew-plugins/tide.yaml`; once accepted, Krew will download the platform
tarball from the GitHub Release, verify its `sha256` against the manifest, and
place the binary at `~/.krew/bin/kubectl-tide`. Until then, use §1.2 or §1.3.
(`kubectl` discovers any binary named `kubectl-<verb>` on `$PATH` as a plugin,
so §1.2 + a rename gives you `kubectl tide ...` today.)

### 1.2 Standalone binary from GitHub Releases

If you do not use kubectl plugins, download the archive directly:

```bash
# Pick the right archive for your OS/arch. Release assets use the bare
# version (no v prefix) in the filename; the tag path keeps it.
TAG=v1.0.0
VER=${TAG#v}
OS=linux            # linux | darwin | windows
ARCH=amd64          # amd64 | arm64 (windows: amd64 only)
EXT=tar.gz          # tar.gz for linux/darwin; zip for windows

curl -LO "https://github.com/jsquirrelz/tide/releases/download/${TAG}/tide_${VER}_${OS}_${ARCH}.${EXT}"
tar -xzf "tide_${VER}_${OS}_${ARCH}.tar.gz"     # or `unzip` on windows
sudo mv tide /usr/local/bin/

tide --help
```

The archive ships with `tide` (or `tide.exe`) + `README.md`. Verify the
checksum against `checksums.txt` from the same Release before installing.

### 1.3 From source (Go toolchain required)

```bash
go install github.com/jsquirrelz/tide/cmd/tide@latest
tide --help
```

Pin to a tag (`@v0.1.0`) for reproducible builds. Note: `go install`
binaries report `tide version dev` (no ldflag injection); the GitHub
Release binaries report the actual version.

---

## 2. Invocation forms — `tide` vs `kubectl tide`

The same binary supports both invocation forms; cobra's root command
resolves its `Use:` from `filepath.Base(os.Args[0])`, so help text always
matches the invocation name.

| Form                           | When                                                            |
| ------------------------------ | --------------------------------------------------------------- |
| `tide <verb>`                  | Direct invocation — standalone install (1.2 or 1.3)             |
| `kubectl tide <verb>`          | Plugin invocation — Krew install (1.1) renames binary to `kubectl-tide` |

**Pitfall 27 — completion script name matching.** Cobra's completion
generator uses `os.Args[0]` as the leading binary name in the script. So:

```bash
tide completion bash         # wires `_tide`             — for the `tide` binary
kubectl tide completion bash # wires `__complete kubectl-tide` — for the Krew binary
```

Run completion from the invocation form you actually use, not the other. If
`kubectl tide` completions stop working after install, check
`kubectl plugin list | grep tide` and re-run `kubectl tide completion bash`.

---

## 3. Verb reference

Every verb honours the kubectl-aligned persistent flag surface
(`--kubeconfig`, `--context`, `--namespace`, `-n`, `--output|-o`, plus the
full `--as`, `--as-group`, `--cluster`, `--insecure-skip-tls-verify` set via
`k8s.io/cli-runtime/pkg/genericclioptions`).

`--output|-o` accepts `human` (default) or `json`.

### 3.1 `apply` — server-side apply a TIDE manifest

```bash
tide apply -f project.yaml
```

Reads `project.yaml`, unmarshals into an `unstructured.Unstructured`, and
patches it server-side with `FieldManager: tide-cli, Force: true`. On
schema-validation failures, walks the `StatusError.ErrStatus.Details.Causes`
slice and prints each violation field-by-field — operators see
`spec.targetRepo: required value` instead of a wall of JSON.

### 3.2 `watch` — live Project status

```bash
tide watch my-project
tide watch my-project -n tide-system
```

Polls `Project.Status.Phase` + active-Milestone count every second, printing
a one-line update when state changes:

```
default/my-project phase=Running active_milestones=2
default/my-project phase=Running active_milestones=1
default/my-project phase=Complete active_milestones=0
```

Ctrl-C terminates within `pollInterval` (1s) via `signal.NotifyContext`
propagation into `cmd.Context()`.

### 3.3 `inspect-wave` — tabular wave view

```bash
tide inspect-wave my-project --wave 0
tide inspect-wave my-project --wave 0 -o json
```

Lists Tasks in the resolved namespace filtered by the canonical label
vocabulary (`tideproject.k8s/project` + `tideproject.k8s/wave-index`) and
sorts by `(wave, name)`.

Human form: 5-column `text/tabwriter` block (`NAME`, `STATUS`, `AGE`,
`ATTEMPT`, `SCHEDULED-IN-WAVE`).

JSON form: array of `{name, status, age, attempt, wave}` records.

Empty waves print `No tasks in wave N for project P.` to **stderr** and
exit 0 (informational, not an error — the wave might not yet be stamped by
PlanReconciler).

### 3.4 `describe-budget` — budget status (dashboard grammar)

```bash
tide describe-budget my-project
tide describe-budget my-project -o json
```

Reads `Project.Status.Budget.CostSpentCents` against
`Project.Spec.Budget.AbsoluteCapCents` (USD cents, int64) and renders:

```
Project: my-project
Absolute cap:    $50.00 (5000 cents)
Current spend:   $12.34 (1234 cents)
Tokens spent:    1500000
Window start:    2026-05-19T15:00:00Z
Utilization:     24.7%
Status:          within budget
```

When `CostSpentCents > AbsoluteCapCents`, the status line reads
`OVER BUDGET` (uppercase). T-04-C3 mitigation by construction — the
renderer reads ONLY `Status.Budget` + `Spec.Budget.AbsoluteCapCents`; it
never touches `Spec.SecretRefs` or `ProviderSecretRef`.

### 3.5 `artifact-get` — read a TIDE artifact (dry-run in v1.0)

```bash
tide artifact-get default/my-project/envelopes/abc/out.json
```

Parses the `<ns>/<project>/<path>` ref (path may contain `/` — `SplitN`
with `n=3`). v1.0 ships dry-run only — prints the inspector-pod spec the
real apiserver pod-exec proxy will create:

```
tide artifact-get (dry-run; real apiserver pod-exec proxy lands in plan 04-14)
  namespace: default
  project: my-project
  path: envelopes/abc/out.json
  inspector pod:
    image: busybox:1.36
    command: ["sh", "-c", "cat /workspace/artifacts/envelopes/abc/out.json"]
    volumeMounts:
      - name: workspace
        mountPath: /workspace
    volumes:
      - name: workspace
        persistentVolumeClaim:
          claimName: tide-projects
    ttlSecondsAfterFinished: 30
```

Real implementation lands with the kind harness in plan 04-14.

### 3.6 `tail` — stream Task pod logs (plan 04-08)

```bash
tide tail my-task
tide tail my-task --follow
```

Streams the Task's executor pod logs to stdout. Honours `ctx.Done()` so
Ctrl-C terminates the stream cleanly. v1.0 implementation lands in plan
04-08.

### 3.7 `approve` — advance a human-gated checkpoint (plan 04-08)

```bash
tide approve my-project --level milestone
tide approve my-project --wave 3
```

Writes the `tideproject.k8s/approve-wave-N` (or level-specific) annotation
that the gates reconciler watches for. v1.0 implementation lands in plan
04-08.

### 3.8 `reject` — halt a Project at the next reconcile (plan 04-08)

```bash
tide reject my-project --reason "scope changed"
```

Writes the `tideproject.k8s/reject` annotation; reconcilers honour it by
refusing to dispatch new Tasks. Halts gracefully; does not delete Pods. v1.0
implementation lands in plan 04-08.

### 3.9 `cancel` — destructively cancel a Project (plan 04-08)

```bash
tide cancel my-project --force
```

Cascade-deletes the Project and reclaims its PVC. Requires `--force` to
prevent accidental destruction. v1.0 implementation lands in plan 04-08.

### 3.10 `resume` — clear a reject annotation (plan 04-08)

```bash
tide resume my-project
```

Removes the `tideproject.k8s/reject` annotation; reconciliation resumes on
the next reconcile loop. v1.0 implementation lands in plan 04-08.

### 3.11 `completion` — generate shell completion scripts

```bash
tide completion bash > /etc/bash_completion.d/tide
tide completion zsh  > "${fpath[1]}/_tide"
tide completion fish > ~/.config/fish/completions/tide.fish
tide completion powershell > $PROFILE/tide-completion.ps1
```

Provided by cobra's built-in `completion` subcommand. See
§4 below for the Krew-renamed-binary form.

---

## 4. Completion

Match the script-binding to the invocation form you actually use (Pitfall
27). The completion script's first line resolves to `os.Args[0]`.

### 4.1 Bash

Standalone (`tide`):
```bash
sudo tide completion bash > /etc/bash_completion.d/tide
```

Krew (`kubectl tide`):
```bash
sudo kubectl tide completion bash > /etc/bash_completion.d/kubectl-tide
```

### 4.2 Zsh

Standalone:
```bash
tide completion zsh > "${fpath[1]}/_tide"
```

Krew:
```bash
kubectl tide completion zsh > "${fpath[1]}/_kubectl-tide"
```

### 4.3 Fish

Standalone:
```bash
tide completion fish > ~/.config/fish/completions/tide.fish
```

Krew:
```bash
kubectl tide completion fish > ~/.config/fish/completions/kubectl-tide.fish
```

---

## 5. Troubleshooting

**`kubectl tide` not found after Krew install.**
```bash
kubectl plugin list | grep tide
ls -la ~/.krew/bin/kubectl-tide
echo $PATH | tr ':' '\n' | grep krew
```
Ensure `~/.krew/bin` is on `$PATH` (Krew adds this during init —
`kubectl krew` itself fails without it).

**Help text shows `Usage: tide ...` after `kubectl tide --help`.**
Pre-Pitfall-27 build; verify `tide version` reports v0.1.0 or later. The
fix sets `Use: filepath.Base(os.Args[0])` so the help banner reflects the
actual invocation name.

**`kubectl tide completion bash` produces a script that doesn't tab-complete.**
The script's leading binary name must match how kubectl invokes it. Re-run
under the form you use (`kubectl tide ...`, not `tide ...`).

**Wrong cluster / namespace.**
TIDE never invents its own auth — it reuses `kubectl`'s persistent flags.
Confirm you're pointed where you think:
```bash
tide --context my-cluster --namespace tide-system inspect-wave my-project
```
or `kubectl config current-context && kubectl config view --minify`.

**Verifying a release archive's signature.**
v1.0 ships unsigned (SLSA provenance + cosign are v1.x). Verify the
sha256 against `checksums.txt` from the Release:
```bash
curl -LO https://github.com/jsquirrelz/tide/releases/download/v0.1.0/checksums.txt
sha256sum -c checksums.txt --ignore-missing
```

---

## 6. Security

v1.0 supply-chain posture:

- **Binary signing:** none — v1.x roadmap item per
  `.planning/phases/04-.../04-RESEARCH.md` §A4.
- **SLSA provenance:** none in v1.0; deferred per RESEARCH §A4.
- **sha256 integrity:** Krew install verifies sha256 from
  `krew-plugins/tide.yaml` against the downloaded archive; mismatches fail
  install. `checksums.txt` is published in every GitHub Release.
- **Build symbol stripping:** `-s -w` ldflags strip debug + DWARF; no
  source paths in shipped binaries (T-04-Release-leak mitigation).
- **No baked secrets:** `ANTHROPIC_API_KEY` is runtime-only per CLAUDE.md;
  goreleaser hooks (`go mod tidy`, `make tide-lint`) never read secrets.

Operators concerned about supply-chain hardening should track v1.x
release notes for cosign signature + SLSA provenance landing.
