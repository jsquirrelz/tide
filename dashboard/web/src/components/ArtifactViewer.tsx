/*
 * ArtifactViewer.tsx (plan 37-05, DASH-01 / D-03 / D-07 / R-04).
 *
 *   Renders one Planning-DAG node's artifacts fetched from
 *   GET /api/v1/nodes/{kind}/{name}/artifacts (plan 37-07 serves the same
 *   contract). A standalone component — plan 37-08 mounts it inside
 *   NodeDetailPanel and supplies `gateParked` from node status.
 *
 *   XSS lock (T-37-05-01): the markdown path passes GFM as its only remark
 *   plugin and NO raw-HTML plugin layer. LLM-authored HTML renders as
 *   escaped text and javascript:-scheme URLs are stripped by react-markdown's
 *   default URL transform. Do not add a raw-HTML plugin.
 *
 *   No truncation of any kind (D-03) — a large artifact scrolls.
 */
import { useCallback, useEffect, useRef, useState } from "react";
import Markdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { Loader2 } from "lucide-react";
import { fetchNodeArtifacts, type NodeArtifacts } from "../lib/api";

export type ArtifactViewerProps = {
  kind: string;
  name: string;
  project: string;
  namespace?: string;
  /** True when the node's status is AwaitingApproval — drives R-01's
   *  materializing display state + 10s polling window. */
  gateParked: boolean;
};

const POLL_INTERVAL_MS = 10_000;

/**
 * Markdown component overrides mapping the react-markdown output to the
 * UI-SPEC Typography ladder (scoped to this container only — h2=16px is the
 * one new size this phase). Anchors open in a new tab; the `href` is already
 * sanitized upstream by react-markdown's defaultUrlTransform.
 */
const MARKDOWN_COMPONENTS: Components = {
  h1: (props) => (
    <h1 style={{ fontSize: "18px", fontWeight: 600, lineHeight: 1.3 }} {...props} />
  ),
  h2: (props) => (
    <h2 style={{ fontSize: "16px", fontWeight: 600, lineHeight: 1.3 }} {...props} />
  ),
  h3: (props) => (
    <h3 style={{ fontSize: "14px", fontWeight: 600 }} {...props} />
  ),
  h4: (props) => (
    <h4 style={{ fontSize: "14px", fontWeight: 600 }} {...props} />
  ),
  h5: (props) => (
    <h5 style={{ fontSize: "14px", fontWeight: 600 }} {...props} />
  ),
  h6: (props) => (
    <h6 style={{ fontSize: "14px", fontWeight: 600 }} {...props} />
  ),
  code: (props) => (
    <code
      style={{
        fontFamily: "var(--font-mono)",
        fontSize: "13px",
        background: "var(--color-surface-overlay)",
        padding: "2px 4px",
        borderRadius: "4px",
      }}
      {...props}
    />
  ),
  pre: (props) => (
    <pre
      style={{
        fontFamily: "var(--font-mono)",
        fontSize: "13px",
        background: "var(--color-surface-overlay)",
        padding: "12px",
        borderRadius: "4px",
        overflowX: "auto",
      }}
      {...props}
    />
  ),
  th: (props) => (
    <th
      style={{
        fontSize: "11px",
        fontWeight: 600,
        textTransform: "uppercase",
        letterSpacing: "0.04em",
        color: "var(--color-text-muted)",
        textAlign: "left",
        padding: "6px 10px",
        borderBottom: "1px solid var(--color-border-subtle)",
      }}
      {...props}
    />
  ),
  td: (props) => (
    <td
      style={{
        padding: "6px 10px",
        borderBottom: "1px solid var(--color-border-subtle)",
      }}
      {...props}
    />
  ),
  a: (props) => <a target="_blank" rel="noreferrer" {...props} />,
};

/** A centered state placeholder card — the honest-copy surface for the four
 *  non-available states (R-04). Copy strings are LOCKED per UI-SPEC. */
function StatePanel({
  testId,
  spinner,
  heading,
  body,
  action,
}: {
  testId: string;
  spinner?: boolean;
  heading: string;
  body: string;
  action?: { label: string; onClick: () => void };
}) {
  return (
    <div
      data-testid={testId}
      className="flex h-full w-full flex-col items-center justify-center px-6 py-16 text-center"
      style={{ color: "var(--color-text-primary)" }}
    >
      {spinner && (
        <Loader2
          size={24}
          aria-label="Loading"
          className="animate-spin"
          style={{ color: "var(--color-text-muted)", marginBottom: "16px" }}
        />
      )}
      <h2 style={{ fontSize: "18px", fontWeight: 600 }}>{heading}</h2>
      <p
        style={{
          marginTop: "16px",
          fontSize: "14px",
          color: "var(--color-text-muted)",
          maxWidth: "440px",
        }}
      >
        {body}
      </p>
      {action && (
        <button
          type="button"
          onClick={action.onClick}
          className="rounded border px-3 py-2"
          style={{
            marginTop: "24px",
            fontFamily: "var(--font-mono)",
            fontSize: "13px",
            fontWeight: 600,
            borderColor: "var(--color-border-subtle)",
            color: "var(--color-text-primary)",
            background: "transparent",
          }}
        >
          {action.label}
        </button>
      )}
    </div>
  );
}

function prettyJSON(content: string): string {
  try {
    return JSON.stringify(JSON.parse(content), null, 2);
  } catch {
    // Malformed JSON still renders verbatim — never a library, never a throw.
    return content;
  }
}

export default function ArtifactViewer({
  kind,
  name,
  project,
  namespace,
  gateParked,
}: ArtifactViewerProps) {
  const [data, setData] = useState<NodeArtifacts | null>(null);
  const [selected, setSelected] = useState(0);
  const mountedRef = useRef(true);

  const load = useCallback(async () => {
    try {
      const result = await fetchNodeArtifacts(kind, name, project, namespace);
      if (!mountedRef.current) return;
      setData(result);
    } catch (err) {
      if (!mountedRef.current) return;
      setData({
        state: "error",
        files: [],
        error: err instanceof Error ? err.message : String(err),
      });
    }
  }, [kind, name, project, namespace]);

  useEffect(() => {
    mountedRef.current = true;
    void load();
    return () => {
      mountedRef.current = false;
    };
  }, [load]);

  // Reset the selected tab to the first *.md whenever a new available
  // payload arrives (Pitfall 9 semantic lock owns which files these are).
  useEffect(() => {
    if (data?.state === "available") {
      const firstMd = data.files.findIndex((f) => f.name.endsWith(".md"));
      setSelected(firstMd >= 0 ? firstMd : 0);
    }
  }, [data]);

  // Bounded polling (Open Q2): only while gate-parked AND the last wire state
  // is absent (the R-01 eventual-consistency window). Stops on available and
  // on unmount — the effect cleanup clears the interval on both.
  useEffect(() => {
    const materializing = gateParked && data?.state === "absent";
    if (!materializing) return;
    const id = setInterval(() => {
      void load();
    }, POLL_INTERVAL_MS);
    return () => clearInterval(id);
  }, [gateParked, data?.state, load]);

  if (data === null) {
    return (
      <div
        data-testid="artifact-state-loading"
        className="flex h-full w-full items-center justify-center py-16"
      >
        <Loader2
          size={24}
          aria-label="Loading"
          className="animate-spin"
          style={{ color: "var(--color-text-muted)" }}
        />
      </div>
    );
  }

  if (data.state === "error") {
    return (
      <StatePanel
        testId="artifact-state-error"
        heading="Couldn't fetch artifacts"
        body="The dashboard could not read the run branch. Check the dashboard logs: kubectl logs deploy/tide-dashboard"
        action={{ label: "Retry", onClick: () => void load() }}
      />
    );
  }

  if (data.state === "no-git") {
    return (
      <StatePanel
        testId="artifact-state-no-git"
        heading="No git remote configured"
        body="Artifacts are stored on the run branch — this project has no git remote, so there is nothing to fetch."
      />
    );
  }

  if (data.state === "absent") {
    if (gateParked) {
      return (
        <StatePanel
          testId="artifact-state-materializing"
          spinner
          heading="Artifacts materializing"
          body="Committing planning artifacts to the run branch — this usually takes under a minute."
        />
      );
    }
    return (
      <StatePanel
        testId="artifact-state-absent"
        heading="Artifacts not available for this run"
        body="This node's planner has not committed artifacts to the run branch. Runs started before v1.0.7 have no in-git artifacts."
        action={{ label: "Check again", onClick: () => void load() }}
      />
    );
  }

  // state === "available"
  const files = data.files ?? [];
  const active = files[Math.min(selected, files.length - 1)];

  return (
    <div
      data-testid="artifact-state-available"
      className="flex h-full w-full flex-col"
    >
      {files.length > 1 && (
        <div
          role="tablist"
          aria-label="Artifact files"
          className="flex flex-shrink-0 flex-wrap gap-1 border-b"
          style={{ borderColor: "var(--color-border-subtle)" }}
          onKeyDown={(e) => {
            if (e.key !== "ArrowRight" && e.key !== "ArrowLeft") return;
            e.preventDefault();
            const delta = e.key === "ArrowRight" ? 1 : -1;
            const next = (selected + delta + files.length) % files.length;
            setSelected(next);
            const nextTab = e.currentTarget.querySelectorAll<HTMLButtonElement>(
              '[role="tab"]',
            )[next];
            nextTab?.focus();
          }}
        >
          {files.map((f, i) => {
            const isSelected = i === selected;
            return (
              <button
                key={f.path}
                type="button"
                role="tab"
                aria-selected={isSelected}
                tabIndex={isSelected ? 0 : -1}
                onClick={() => setSelected(i)}
                className="px-3 py-2"
                style={{
                  fontFamily: "var(--font-mono)",
                  fontSize: "12px",
                  fontWeight: 600,
                  background: "transparent",
                  color: isSelected
                    ? "var(--color-text-primary)"
                    : "var(--color-text-muted)",
                  borderBottom: isSelected
                    ? "2px solid var(--color-border-strong)"
                    : "2px solid transparent",
                }}
              >
                {f.name}
              </button>
            );
          })}
        </div>
      )}

      <div
        data-testid="artifact-content"
        className="min-h-0 flex-1 overflow-y-auto px-4 py-3"
        style={{ fontSize: "14px", lineHeight: 1.5 }}
      >
        {active === undefined || active.content === "" || active.sizeBytes === 0 ? (
          <p style={{ color: "var(--color-text-muted)", fontSize: "14px" }}>
            This artifact is empty.
          </p>
        ) : active.name.endsWith(".json") ? (
          <pre
            data-testid="artifact-json"
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: "13px",
              background: "var(--color-surface-overlay)",
              padding: "12px",
              borderRadius: "4px",
              overflowX: "auto",
              whiteSpace: "pre",
            }}
          >
            {prettyJSON(active.content)}
          </pre>
        ) : (
          <Markdown remarkPlugins={[remarkGfm]} components={MARKDOWN_COMPONENTS}>
            {active.content}
          </Markdown>
        )}
      </div>
    </div>
  );
}
