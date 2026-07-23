---
phase: quick-260722-ufe
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - examples/projects/demo-calculator/namespace.yaml
  - examples/projects/demo-calculator/calculator-remote-pvc.yaml
  - examples/projects/demo-calculator/seed-remote-job.yaml
  - examples/projects/demo-calculator/git-http-server.yaml
  - examples/projects/demo-calculator/per-namespace-resources.yaml
  - examples/projects/demo-calculator/project.yaml
  - examples/projects/demo-calculator/README.md
  - README.md
autonomous: true
requirements:
  - QUICK-260722-ufe-A  # examples/projects/demo-calculator/ sample dir (7 files) modeled on medium
  - QUICK-260722-ufe-B  # root README.md samples table gains a demo-calculator row
user_setup: []

must_haves:
  truths:
    - "examples/projects/demo-calculator/ contains exactly 7 files: namespace.yaml, calculator-remote-pvc.yaml, seed-remote-job.yaml, git-http-server.yaml, per-namespace-resources.yaml, project.yaml, README.md"
    - "project.yaml pins ghcr.io/jsquirrelz/tide-claude-subagent:1.0.9 (== chart appVersion), model claude-sonnet-5, budget 1000/1000/24h, all gates auto, planAdmission strict, maxAttemptsPerTask 3, schemaRevision v1alpha3"
    - "targetRepo and git.repoURL both read http://git-http-server.tide-demo-calculator.svc.cluster.local/calculator.git"
    - "seed-remote-job.yaml seeds a NON-empty bare repo (initial commit with README.md) and exits early if calculator.git already exists (idempotent-by-refusal)"
    - "Root README.md samples table carries a demo-calculator row (about $10, web calculator, Anthropic API key) between the medium and large rows"
    - "go test ./test/integration/kind/ -run TestExamplesSubagentImagePinsMatchChartAppVersion passes (walks examples/projects/ YAML+MD; any subagent tag != 1.0.9 fails)"
    - "charts/ untouched; the ONLY pre-existing file modified is root README.md"
  artifacts:
    - path: "examples/projects/demo-calculator/project.yaml"
      provides: "Project CRD demo-calculator in tide-demo-calculator with web-calculator outcomePrompt (index.html + style.css + app.js, four ops, keyboard, no deps, ONE Phase / ONE Plan / 3-5 tasks)"
      contains: "tide-claude-subagent:1.0.9"
      contains_token: "claude-sonnet-5"
      contains_token: "absoluteCapCents: 1000"
    - path: "examples/projects/demo-calculator/seed-remote-job.yaml"
      provides: "calculator-remote-init Job (image tide-git-http-server:1.0.0) seeding /workspace/calculator.git with an initial README commit"
      contains_token: "http.receivepack"
      contains_token: "calculator.git"
    - path: "examples/projects/demo-calculator/git-http-server.yaml"
      provides: "Deployment + ClusterIP Service serving calculator.git from calculator-remote-pvc at /srv/git"
      contains_token: "calculator-remote-pvc"
    - path: "examples/projects/demo-calculator/per-namespace-resources.yaml"
      provides: "tide-projects RWX PVC + tide-subagent SA + tide-push SA/Role/RoleBinding + tide-reporter SA/Role/RoleBinding in tide-demo-calculator"
      contains_token: "tide-push"
      contains_token: "tide-reporter"
    - path: "examples/projects/demo-calculator/README.md"
      provides: "Apply-sequence README mirroring medium's structure (Audience/Status/Overview/Cost/Wall time/Architecture/Apply Sequence/Payoff/Cleanup)"
      contains: "tide-claude-subagent:1.0.9"
    - path: "README.md"
      provides: "demo-calculator row in the Examples samples table"
      contains: "demo-calculator"
  key_links:
    - from: "project.yaml targetRepo + git.repoURL"
      to: "git-http-server.yaml Service (namespace tide-demo-calculator)"
      via: "Service DNS http://git-http-server.tide-demo-calculator.svc.cluster.local/calculator.git"
      pattern: "calculator\\.git"
    - from: "seed-remote-job.yaml (/workspace/calculator.git)"
      to: "git-http-server.yaml (/srv/git mount)"
      via: "same calculator-remote-pvc, different mount paths — repo served as /srv/git/calculator.git"
      pattern: "calculator-remote-pvc"
    - from: "project.yaml + README.md subagent image pins"
      to: "charts/tide/Chart.yaml appVersion 1.0.9"
      via: "TestExamplesSubagentImagePinsMatchChartAppVersion (test/integration/kind/examples_image_pin_test.go)"
      pattern: "tide-claude-subagent:1\\.0\\.9"
    - from: "per-namespace-resources.yaml tide-push SA + Role + RoleBinding"
      to: "controller clone/push Jobs in tide-demo-calculator"
      via: "missing tide-push SA makes clone/push Jobs fail FailedCreate — git-enabled project hard requirement"
      pattern: "tide-push"
---

<objective>
Add `examples/projects/demo-calculator/` — a ~$10 real-Claude web-calculator demo sample for TIDE v1.0.9, modeled file-for-file on the medium sample, plus a `demo-calculator` row in the root README's samples table.

Purpose: The existing samples exercise TIDE on Go-fixture repos. This sample gives operators a visually satisfying payoff (open index.html, use a working calculator) built by real Claude via an in-cluster git remote — a self-contained demo with zero external repo dependency, same architecture as medium (seed Job → git-http-server → Project CRD) but with a fresh bare repo seeded inline instead of the tide-demo-init fixture image.

Output: 7 new files under examples/projects/demo-calculator/ + 1 row added to root README.md. No other pre-existing file changes; charts/ is a fixed contract and stays untouched.

All design choices below were locked by the user in brainstorming — implement exactly, do not revisit.
</objective>

<execution_context>
@$HOME/.claude/gsd-core/workflows/execute-plan.md
@$HOME/.claude/gsd-core/templates/summary.md
</execution_context>

<context>
@./CLAUDE.md
@examples/projects/medium/namespace.yaml
@examples/projects/medium/demo-remote-pvc.yaml
@examples/projects/medium/per-namespace-resources.yaml
@examples/projects/medium/git-http-server-deployment.yaml
@examples/projects/medium/project.yaml
@examples/projects/medium/README.md

Key facts verified during planning:
- charts/tide/Chart.yaml appVersion is "1.0.9" — every `tide-claude-subagent:` reference in the new YAML AND the new README must pin exactly 1.0.9 (test/integration/kind/examples_image_pin_test.go walks examples/projects/ .yaml/.yml/.md and requires tag == appVersion; dogfood/ is the only exclusion).
- `go test ./test/integration/kind/ -run TestExamplesSubagentImagePinsMatchChartAppVersion -count=1` runs cluster-free (no TestMain in the package; the -run filter skips the Ginkgo entry funcs) — baseline passes in ~0.5s.
- images/tide-git-http-server/ exists (Dockerfile, entrypoint.sh, nginx.conf); the image runs as USER 1000 and carries sh + git — it doubles as the seed-job image (the medium sample's tide-demo-init image is NOT used here).
- `yq` is available on this host for YAML syntax validation.
</context>

<tasks>

<task type="auto">
  <name>Task 1: Infra manifests — namespace, PVC, seed Job, git-http-server, per-namespace resources</name>
  <files>examples/projects/demo-calculator/namespace.yaml, examples/projects/demo-calculator/calculator-remote-pvc.yaml, examples/projects/demo-calculator/seed-remote-job.yaml, examples/projects/demo-calculator/git-http-server.yaml, examples/projects/demo-calculator/per-namespace-resources.yaml</name>
  <action>
Create five manifests adapted from the medium sample. Match medium's comment density — heavily commented YAML explaining WHY, referencing the same failure modes (RWO ordering, FailedCreate on missing tide-push SA, kind RWX deadlock). Every resource carries labels `app.kubernetes.io/name: tide`, `app.kubernetes.io/managed-by: tide-sample`, `tideproject.k8s/sample: demo-calculator`. Namespace everywhere is `tide-demo-calculator`.

1. **namespace.yaml** — Namespace `tide-demo-calculator`. Adapt medium's namespace.yaml header comment (Pitfall 9 note about the distinct `tide-samples` kubebuilder fixture namespace still applies; this sample uses the `tide-demo-` prefix rather than `tide-sample-` because it is a demo payoff sample, note that deliberately).

2. **calculator-remote-pvc.yaml** — PVC `calculator-remote-pvc`, `accessModes: [ReadWriteOnce]`, 100Mi, plus `app.kubernetes.io/component: calculator-remote` label. Mirror demo-remote-pvc.yaml's comment rationale (100Mi ample for a seed README + per-run-branch commits; RWO sufficient because seed Job and server never mount simultaneously — apply order enforces it).

3. **seed-remote-job.yaml** — Job `calculator-remote-init` in `tide-demo-calculator`. Container image `ghcr.io/jsquirrelz/tide-git-http-server:1.0.0` with `imagePullPolicy: IfNotPresent` (image is unpublished — locally built + minikube-loaded per README) and a command override (`command: ["sh", "-c", ...]`) running an inline shell script. The image carries sh + git and runs as UID 1000, which is why it doubles as the seed image (no separate tide-demo-init fixture needed). Script requirements, in order:
   - `set -eu`.
   - Idempotent-by-refusal: if `/workspace/calculator.git` already exists, log and `exit 0` WITHOUT touching it.
   - `git init --bare --initial-branch=main /workspace/calculator.git`.
   - `git -C /workspace/calculator.git config http.receivepack true` and `git -C /workspace/calculator.git config core.sharedRepository group`.
   - Seed an initial commit — the repo must NOT be left empty (a zero-commit clone is an edge case for the controller's clone Job): clone `/workspace/calculator.git` to a mktemp dir, `git checkout -b main` (clone of an empty repo lands on an unborn branch — normalize the name), write a small README.md describing the repo as the TIDE demo-calculator seed, `git add`, commit using inline identity flags (`git -c user.name=... -c user.email=... commit`) so no writable global config is needed, then `git push origin main` (file-path transport internal to this pod — the ONLY place file-path git is used, mirroring medium's demo-remote-init rationale).
   - Mount `calculator-remote-pvc` at `/workspace`. Pod securityContext: `runAsUser: 1000`, `runAsNonRoot: true` (matches the image's USER 1000). `restartPolicy: Never`, `backoffLimit: 2`.
   - Header comment: explain the image reuse choice, the non-empty-repo requirement, and the RWO ordering contract (this Job must reach Complete and release the PVC before git-http-server.yaml is applied).

4. **git-http-server.yaml** — Deployment + ClusterIP Service, adapted from medium's git-http-server-deployment.yaml with namespace/PVC/repo swapped: mounts `calculator-remote-pvc` at `/srv/git` (repo seeded at /workspace/calculator.git is served as /srv/git/calculator.git — same PVC, different mount path, matching GIT_PROJECT_ROOT in nginx.conf), image `ghcr.io/jsquirrelz/tide-git-http-server:1.0.0` with `imagePullPolicy: IfNotPresent`. Preserve medium's threat-model comments and settings verbatim in adapted form: ClusterIP only (T-08-05-01), containerPort 8080 with Service 80→8080 (nonroot cannot bind 80), runAsUser 1000 + runAsNonRoot true, readOnlyRootFilesystem false with the nginx pid/tmp/fcgi-socket rationale. Preserve the RWO-ordering header comment (seed Job Complete before this Deployment).

5. **per-namespace-resources.yaml** — Adapt medium's per-namespace-resources.yaml wholesale with namespace + sample-label swap: tide-projects PVC (ReadWriteMany 1Gi — keep the production-default rationale and the kind/RWO-provisioner warning comment pointing at the README's override step), tide-subagent SA, tide-push SA + Role (get on secrets) + RoleBinding, tide-reporter SA + Role (create/get/list on the five child CRD kinds + get on projects, keeping the T-09-07 least-privilege comment) + RoleBinding. This is a git-enabled project — tide-push is REQUIRED or the clone/push Jobs fail FailedCreate ("serviceaccount tide-push not found"); keep that comment. Keep the signing-key-mirror instructions comment block (the heredoc showing how to copy tide-signing-key from tide-system), namespace-swapped to tide-demo-calculator.
  </action>
  <verify>
    <automated>for f in examples/projects/demo-calculator/namespace.yaml examples/projects/demo-calculator/calculator-remote-pvc.yaml examples/projects/demo-calculator/seed-remote-job.yaml examples/projects/demo-calculator/git-http-server.yaml examples/projects/demo-calculator/per-namespace-resources.yaml; do yq eval-all '.' "$f" > /dev/null || exit 1; done && [ "$(grep -rlv '^#' examples/projects/demo-calculator/*.yaml | xargs grep -l 'namespace: tide-demo-calculator' | wc -l | tr -d ' ')" -ge 4 ] && grep -q 'initial-branch=main' examples/projects/demo-calculator/seed-remote-job.yaml && grep -q 'http.receivepack' examples/projects/demo-calculator/seed-remote-job.yaml</automated>
  </verify>
  <done>Five YAML files exist, all parse under yq, all namespaced resources target tide-demo-calculator with the three sample labels, the seed Job script is idempotent-by-refusal and produces a non-empty main branch, and per-namespace-resources.yaml carries tide-push + tide-reporter RBAC plus the signing-key-mirror comment block.</done>
</task>

<task type="auto">
  <name>Task 2: project.yaml + demo-calculator README.md</name>
  <files>examples/projects/demo-calculator/project.yaml, examples/projects/demo-calculator/README.md</files>
  <action>
**project.yaml** — Project CRD modeled on medium's project.yaml (keep the apply-order header comment style and per-field WHY comments):
- `apiVersion: tideproject.k8s/v1alpha3`, `kind: Project`, name `demo-calculator`, namespace `tide-demo-calculator`, the three sample labels.
- `spec.schemaRevision: v1alpha3` (required — controller fail-closes without the explicit opt-in; keep medium's comment).
- `targetRepo: http://git-http-server.tide-demo-calculator.svc.cluster.local/calculator.git` and `git.repoURL` set to the same URL; `git.credsSecretRef: tide-secrets`; `providerSecretRef: tide-secrets`. Keep medium's comments on the scheme-conditional empty-GIT_PAT allowance for anonymous http:// and the pure-Go go-git transport.
- `subagent.image: ghcr.io/jsquirrelz/tide-claude-subagent:1.0.9` — MUST be exactly 1.0.9; comment WHY (tag must equal chart appVersion or pkg/dispatch envelope apiVersion validation rejects every dispatch — enforced by TestExamplesSubagentImagePinsMatchChartAppVersion). `subagent.model: claude-sonnet-5` (calculator UI quality warrants Sonnet over medium's Haiku; comment the cost tradeoff).
- `budget: { absoluteCapCents: 1000, rollingWindowCapCents: 1000, rollingWindowDuration: 24h }` — explicit rollingWindowDuration per Pitfall 7; $10 cap is the safety net, not the operating budget.
- `gates:` milestone/phase/plan/task all `auto`, `pauseBetweenWaves: false`. `planAdmission.fileTouchMode: strict`. `maxAttemptsPerTask: 3`.
- `outcomePrompt` (literal block, same structure as medium's): build a single-page web calculator — exactly three files: index.html, style.css, app.js. Four operations (add, subtract, multiply, divide), clear and equals buttons, keyboard support. NO external dependencies, frameworks, or build step — plain HTML/CSS/JS opened directly in a browser. Tight scope as the cost-control mechanism: ONE Phase, ONE Plan, target 3-5 tasks. Pass criterion: opening index.html in a browser yields a working calculator.

**README.md** — mirror medium README's structure and section order: title line (`# demo-calculator — ~$10 real Claude (web-calculator payoff)` shape), **Audience** (operators wanting a visually satisfying real-Claude demo), **Status**, **Overview** (what TIDE builds and how the in-cluster remote works — fresh bare repo seeded by the init Job instead of a Go fixture), **Cost** (~$10 worst-case, absoluteCapCents 1000), **Wall time** (~10–20 minutes, dominated by Claude round-trips), **Architecture** (three components: (a) calculator-remote-init seed Job, (b) git-http-server Deployment, (c) controller Jobs via pure-Go go-git HTTP — controller Jobs never mount calculator-remote-pvc), **Prerequisites**, **Apply Sequence** (numbered bash block, order matters), **Payoff**, **Budget cap behavior**, **Cleanup** (`kubectl delete namespace tide-demo-calculator`).

Apply Sequence steps, in this exact order:
1. Build + load the git-http-server image: `docker build -t ghcr.io/jsquirrelz/tide-git-http-server:1.0.0 -f images/tide-git-http-server/Dockerfile .` then `minikube image load ghcr.io/jsquirrelz/tide-git-http-server:1.0.0` (image is unpublished; include medium's minikube stale-tag warning — `minikube image load` is not idempotent, `minikube ssh -- docker rmi -f` before reloading a rebuild; kind users substitute `kind load docker-image`).
2. `kubectl apply -f` namespace.yaml.
3. calculator-remote-pvc.yaml.
4. per-namespace-resources.yaml — with a kind/RWO-only-provisioner sub-step mirroring medium's step 3b: delete + recreate tide-projects as ReadWriteOnce (accessModes immutable, cannot patch). Note that RWX binds fine on minikube.
5. Mirror the tide-signing-key Secret from tide-system (medium's step-4 heredoc, namespace-swapped).
6. seed-remote-job.yaml + `kubectl wait --for=condition=Complete job/calculator-remote-init -n tide-demo-calculator --timeout=2m` (RWO ordering — WHY comment).
7. git-http-server.yaml + `kubectl wait --for=condition=Available deployment/git-http-server -n tide-demo-calculator --timeout=2m`.
8. `kubectl create secret generic tide-secrets --from-literal=ANTHROPIC_API_KEY="$(tr -d '\n\r' < /path/to/your/anthropic-key)" --from-literal=GIT_PAT="" -n tide-demo-calculator` — newline-stripped key (a trailing newline in the Secret corrupts the auth header); empty GIT_PAT accepted for anonymous http://.
9. `kubectl apply -f` project.yaml.
10. Watch: `tide watch demo-calculator -n tide-demo-calculator` + dashboard port-forward.

**Payoff section**: port-forward git-http-server (`kubectl port-forward svc/git-http-server 8081:80 -n tide-demo-calculator`), `git clone http://localhost:8081/calculator.git`, check out the `tide/run-demo-calculator-*` branch, open index.html in a browser — working calculator.

Any `tide-claude-subagent:` reference in this README must pin exactly 1.0.9 (the image-pin test walks .md files too). Cross-link the Related section to ../README.md, ../medium/README.md, images/tide-git-http-server/, docs/project-authoring.md. Match the repo's tight, declarative, em-dash-heavy doc voice.
  </action>
  <verify>
    <automated>yq eval-all '.' examples/projects/demo-calculator/project.yaml > /dev/null && [ "$(grep -v '^#' examples/projects/demo-calculator/project.yaml | grep -c 'tide-claude-subagent:1.0.9')" -eq 1 ] && grep -q 'claude-sonnet-5' examples/projects/demo-calculator/project.yaml && grep -q 'absoluteCapCents: 1000' examples/projects/demo-calculator/project.yaml && grep -q 'schemaRevision: v1alpha3' examples/projects/demo-calculator/project.yaml && [ "$(grep -v '^#' examples/projects/demo-calculator/project.yaml | grep -c 'git-http-server.tide-demo-calculator.svc.cluster.local/calculator.git')" -eq 2 ] && grep -q 'calculator-remote-init' examples/projects/demo-calculator/README.md && grep -q 'tr -d' examples/projects/demo-calculator/README.md</automated>
  </verify>
  <done>project.yaml parses, pins subagent 1.0.9 + claude-sonnet-5, carries the $10 budget with explicit 24h window, all-auto gates, strict fileTouchMode, and the calculator outcomePrompt; targetRepo and git.repoURL both point at the calculator.git Service DNS. README.md mirrors medium's section structure with the 10-step apply sequence, newline-stripped Secret creation, kind RWO override note, and the clone-and-open-index.html payoff.</done>
</task>

<task type="auto">
  <name>Task 3: Root README samples-table row + repo-wide pin verification</name>
  <files>README.md</files>
  <action>
Edit the root README.md Examples samples table (near line 167) ONLY — add one row for demo-calculator between the medium and large rows (cost-ascending order), matching the existing row shape exactly:

`| [demo-calculator](examples/projects/demo-calculator/) | about $10 | Real Claude builds a web calculator via in-cluster git remote | Anthropic API key |`

Do not modify any other part of README.md, and do not touch charts/ (fixed contract).

Then run the repo-wide pin gate as the closing verification for the whole quick task: TestExamplesSubagentImagePinsMatchChartAppVersion walks every .yaml/.yml/.md under examples/projects/ and fails if any tide-{stub,claude}-subagent tag differs from chart appVersion 1.0.9 — this mechanically covers both new files from Tasks 1-2 and this row.
  </action>
  <verify>
    <automated>grep -c 'demo-calculator](examples/projects/demo-calculator/)' README.md | grep -qx '1' && go test ./test/integration/kind/ -run TestExamplesSubagentImagePinsMatchChartAppVersion -count=1 && [ "$(git diff --name-only HEAD -- charts/ | wc -l | tr -d ' ')" -eq 0 ]</automated>
  </verify>
  <done>Root README samples table has exactly one demo-calculator row positioned between medium and large; the image-pin test passes over the full examples/projects/ tree including the 7 new files; charts/ shows zero diff.</done>
</task>

</tasks>

<verification>
1. `ls examples/projects/demo-calculator/` lists exactly 7 files (5 YAML + project.yaml + README.md).
2. All 6 YAML files parse under `yq eval-all`.
3. `go test ./test/integration/kind/ -run TestExamplesSubagentImagePinsMatchChartAppVersion -count=1` → ok (cluster-free, ~0.5s).
4. `git status --porcelain` shows only the 7 new files + root README.md modified — no charts/ changes, no other pre-existing file touched.
5. Seed job script review: refuses when calculator.git exists; seeds main with one README commit via file-path push internal to the pod.
</verification>

<success_criteria>
- A fresh operator can follow examples/projects/demo-calculator/README.md top-to-bottom on minikube: build+load image → namespace → PVC → per-namespace resources → signing-key mirror → seed job Complete → server Available → tide-secrets → project.yaml → watch, then clone the run branch and open a working calculator.
- The sample survives the release gate: every subagent pin equals chart appVersion 1.0.9 (image-pin test green).
- Root README advertises the sample in cost order (small $0 → medium $5 → demo-calculator $10 → large $25).
</success_criteria>

<output>
Create `.planning/quick/260722-ufe-add-examples-projects-demo-calculator-we/260722-ufe-SUMMARY.md` when done.
</output>
