/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// artifacts.go — GET /api/v1/nodes/{kind}/{name}/artifacts (plan 37-07, DASH-01).
//
// Serves the planning artifacts THIS node's planner produced, read live from
// the `.tide/planning/<kind>/<name>/` subtree at the tip of the Project's run
// branch (via the gitfetch Store, plan 37-03). Every response carries an R-04
// state discriminator — available | absent | no-git | error — so the UI never
// has to disambiguate an empty list.
//
// DASH-05 zero-mutation contract: this handler is HTTP GET only; the router's
// TestZeroMutationRoutes walks the route tree and fails the build on any
// non-GET registration.
package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/cmd/dashboard/gitfetch"
)

// gitPATKey is the Secret data key carrying the HTTPS PAT — the exact key
// tide-push reads (cmd/tide-push/main.go GIT_PAT convention, GitConfig.CredsSecretRef doc).
const gitPATKey = "GIT_PAT"

// artifactKinds is the closed allowlist of node kinds a client may request.
// Any other value is a 400 — the name/kind never touch a filesystem path, but
// the allowlist keeps the prefix filter well-formed and rejects garbage early
// (T-37-07-04 path-traversal mitigation).
var artifactKinds = map[string]bool{
	"project":   true,
	"milestone": true,
	"phase":     true,
	"plan":      true,
	"task":      true,
}

// ArtifactsHandler serves GET /api/v1/nodes/{kind}/{name}/artifacts. Unlike the
// other read handlers it needs three deps: the read-only controller-runtime
// Client (CR Gets, informer-cache-backed), a typed Clientset (the Secret read —
// see the fetch-time creds note in Get), and the gitfetch Store (the cached git
// read path).
type ArtifactsHandler struct {
	Client    client.Client
	Clientset kubernetes.Interface
	Store     *gitfetch.Store
	Log       logr.Logger
}

// artifactFile is one planning artifact blob. Content is the FULL file body as
// a string (no caps, no truncation — D-03); SizeBytes is its byte length.
// Mirrors the TS ArtifactFile type (dashboard/web/src/lib/api.ts, plan 37-05).
type artifactFile struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Content   string `json:"content"`
	SizeBytes int64  `json:"sizeBytes"`
}

// nodeArtifacts is the R-04 state-discriminated response. State is always
// present; Branch/CommitSHA are set only in the available state; Error only in
// the error state; Files is ALWAYS a (possibly empty) array, never null.
// Mirrors the TS NodeArtifacts type (plan 37-05).
type nodeArtifacts struct {
	State     string         `json:"state"`
	Branch    string         `json:"branch,omitempty"`
	CommitSHA string         `json:"commitSHA,omitempty"`
	Files     []artifactFile `json:"files"`
	Error     string         `json:"error,omitempty"`
}

// emptyState builds a nodeArtifacts in a non-available state with an
// always-serialized empty Files array (empty-array-not-null contract).
func emptyState(state string) nodeArtifacts {
	return nodeArtifacts{State: state, Files: make([]artifactFile, 0)}
}

// Get implements GET /api/v1/nodes/{kind}/{name}/artifacts?project=<name>[&namespace=foo].
//
// Flow: validate kind against the closed allowlist → resolve the Project CR →
// short-circuit no-git / pre-first-push (absent) → resolve fetch-time git creds
// from the per-project Secret via the TYPED clientset → gitfetch the run-branch
// tip → filter to this node's .tide/planning/<kind>/<name>/ subtree → serve full
// content. Fetch failures return 200 with state:"error" (most failure surfaces
// arrive as data, not HTTP errors); only apiserver/CR errors 4xx/5xx.
func (h *ArtifactsHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	kind := chi.URLParam(r, "kind")
	name := chi.URLParam(r, "name")

	if !artifactKinds[kind] {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid kind %q (must be one of project, milestone, phase, plan, task)", kind))
		return
	}

	projectName := r.URL.Query().Get("project")
	if projectName == "" {
		writeError(w, http.StatusBadRequest, "missing required query parameter: project")
		return
	}
	namespace := r.URL.Query().Get("namespace")
	if namespace == "" {
		namespace = "default"
	}

	var proj tidev1alpha3.Project
	if err := h.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: projectName}, &proj); err != nil {
		if apierrors.IsNotFound(err) {
			writeError(w, http.StatusNotFound, fmt.Sprintf("project %s not found", projectName))
			return
		}
		h.Log.Error(err, "get project failed", "project", projectName, "namespace", namespace)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get project: %s", err.Error()))
		return
	}

	// no-git: a Project without a git remote has no run branch to read.
	if proj.Spec.Git == nil || proj.Spec.Git.RepoURL == "" {
		writeJSON(w, http.StatusOK, emptyState("no-git"))
		return
	}

	// absent (pre-first-push): the run branch is fixed at Project creation, but
	// until the first push lands there is nothing to fetch.
	branch := proj.Status.Git.BranchName
	if branch == "" {
		writeJSON(w, http.StatusOK, emptyState("absent"))
		return
	}

	// Fetch-time credentials via the TYPED clientset. This is deliberate and
	// load-bearing: a controller-runtime cached client.Get on a Secret would
	// silently start a cluster-wide Secret informer (RESEARCH Pitfall 4 — the
	// dashboard would begin caching every Secret in every namespace, a massive
	// memory + privilege blowup). The typed clientset issues a one-shot GET with
	// no informer. The PAT lives only in this call frame and the gitfetch call
	// frame — never cached, never logged (T-37-03-01 / T-37-07-03).
	auth, credErr := h.resolveAuth(ctx, namespace, proj.Spec.Git.CredsSecretRef, proj.Spec.Git.RepoURL)
	if credErr != "" {
		na := emptyState("error")
		na.Error = credErr
		writeJSON(w, http.StatusOK, na)
		return
	}

	sha, files, err := h.Store.Artifacts(ctx, proj.Spec.Git.RepoURL, branch, auth)
	if err != nil {
		// gitfetch guarantees its error strings never embed the PAT
		// (T-37-03-01, tested in gitfetch). Surface as data, not an HTTP error.
		h.Log.Error(err, "gitfetch artifacts failed", "project", projectName, "kind", kind, "name", name)
		na := emptyState("error")
		na.Error = err.Error()
		writeJSON(w, http.StatusOK, na)
		return
	}

	// Filter to this node's subtree — Pitfall 9 semantic lock: a node serves the
	// artifacts ITS planner produced under .tide/planning/<kind>/<name>/, never a
	// parent's. Same prefix rule as plans 37-02 / 37-06.
	prefix := fmt.Sprintf(".tide/planning/%s/%s/", kind, name)
	matched := make([]artifactFile, 0, len(files))
	for _, f := range files {
		if !strings.HasPrefix(f.Path, prefix) {
			continue
		}
		matched = append(matched, artifactFile{
			Name:      f.Name,
			Path:      f.Path,
			Content:   string(f.Content),
			SizeBytes: int64(len(f.Content)),
		})
	}

	if len(matched) == 0 {
		writeJSON(w, http.StatusOK, emptyState("absent"))
		return
	}

	writeJSON(w, http.StatusOK, nodeArtifacts{
		State:     "available",
		Branch:    branch,
		CommitSHA: sha,
		Files:     matched,
	})
}

// resolveAuth reads the per-project git creds Secret via the typed clientset and
// returns the pass-through Auth. An empty credsSecretRef means anonymous access
// (public / in-cluster http:// remote) → (nil, "").
//
// A missing/empty GIT_PAT key is scheme-conditional (Gap 37-G1): it mirrors
// cmd/tide-push/main.go resolveGitAuth's requirePAT rule — https:// and git@
// remotes REQUIRE the PAT (missing → creds error), while anonymous http://
// remotes proceed anonymously with nil Auth. This keeps the dashboard read path
// in lockstep with the push path: an in-cluster http:// remote that pushed fine
// without a PAT also renders its artifacts without one.
//
// A missing Secret (a set credsSecretRef pointing at a non-existent Secret)
// stays a loud error for every scheme. The returned error MESSAGE never echoes
// any secret value — the caller renders it as state:"error".
func (h *ArtifactsHandler) resolveAuth(ctx context.Context, namespace, credsSecretRef, repoURL string) (*gitfetch.Auth, string) {
	if credsSecretRef == "" {
		return nil, ""
	}
	sec, err := h.Clientset.CoreV1().Secrets(namespace).Get(ctx, credsSecretRef, metav1.GetOptions{})
	if err != nil {
		// The apiserver error carries the Secret NAME and reason, never its
		// contents — safe to surface. Shape it as a creds message.
		return nil, fmt.Sprintf("failed to read git credentials secret %q: %s", credsSecretRef, err.Error())
	}
	pat, ok := sec.Data[gitPATKey]
	if !ok || len(pat) == 0 {
		// Same guard as tide-push resolveGitAuth: only https:// and git@ remotes
		// require the PAT. Anonymous http:// remotes fetch without credentials.
		requirePAT := strings.HasPrefix(repoURL, "https://") || strings.HasPrefix(repoURL, "git@")
		if !requirePAT {
			return nil, ""
		}
		return nil, fmt.Sprintf("git credentials secret %q is missing data key %s", credsSecretRef, gitPATKey)
	}
	// Username defaults to the x-access-token convention inside gitfetch.basicAuth.
	return &gitfetch.Auth{Password: string(pat)}, ""
}
