import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ArrowLeft,
  Check,
  Copy,
  Download,
  File,
  FileCode,
  FileText,
  Globe,
  Image as ImageIcon,
  Pencil,
  Plus,
  RefreshCw,
  Trash2,
  Upload,
} from "lucide-react";
import { ApiError, fetchArtifactBytes, replaceArtifact, uploadArtifact } from "./api.js";
import { ago, itemNum, name as participantLabel, submitOnModEnter } from "./backlogFormat.js";
import { MemoMarkdown } from "./Markdown.jsx";
import { itemLink, spaLink } from "./nav.js";
import { useKeyShortcut } from "./shortcuts.jsx";
import Button from "./ui/Button.jsx";
import Dialog from "./ui/Dialog.jsx";
import Field from "./ui/Field.jsx";
import { Input, Select } from "./ui/Input.jsx";
import ScreenHead from "./ui/ScreenHead.jsx";

const MAX_ARTIFACT_BYTES = 10 << 20;

const FILTER_TABS = [
  { id: "all", label: "All" },
  { id: "docs", label: "Docs" },
  { id: "html", label: "HTML" },
  { id: "images", label: "Images" },
];

export function withArtifact(list, art) {
  const lower = art.name.toLowerCase();
  const rest = list.filter((x) => x.id !== art.id && x.name.toLowerCase() !== lower);
  return [art, ...rest].sort((a, b) => (b.updated_at || 0) - (a.updated_at || 0) || b.id - a.id);
}

export function formatArtifactSize(bytes) {
  const n = Number(bytes) || 0;
  if (n >= 1 << 20) return `${(n / (1 << 20)).toFixed(1)} MB`;
  if (n >= 1 << 10) return `${(n / (1 << 10)).toFixed(1)} KB`;
  return `${n} B`;
}

export function artifactKind(art) {
  const lower = String(art?.name || "").toLowerCase();
  const mime = String(art?.mime_type || "").toLowerCase();
  if (/\.(md|markdown|mdx)$/.test(lower) || mime.startsWith("text/markdown")) return "markdown";
  if (/\.(html|htm)$/.test(lower) || mime.startsWith("text/html")) return "html";
  if (/\.(png|jpe?g|gif|webp|svg|ico)$/.test(lower) || mime.startsWith("image/")) return "image";
  if (/\.(json|jsonl|geojson)$/.test(lower) || mime.startsWith("application/json")) return "json";
  if (
    mime.startsWith("text/") ||
    /\.(txt|csv|tsv|log|ya?ml|toml|ini|cfg|conf|xml|sql|css|scss|m?js|cjs|jsx|tsx?|go|py|rb|rs|c|cc|cpp|h|hpp|java|kt|swift|sh|bash|zsh|diff|patch)$/.test(
      lower,
    )
  ) {
    return "text";
  }
  return "binary";
}

export function ArtifactTypeIcon({ artifact }) {
  const kind = artifactKind(artifact);
  if (kind === "markdown") return <FileText aria-hidden="true" />;
  if (kind === "html") return <Globe aria-hidden="true" />;
  if (kind === "image") return <ImageIcon aria-hidden="true" />;
  if (kind === "json" || kind === "text") return <FileCode aria-hidden="true" />;
  return <File aria-hidden="true" />;
}

function actorDisplay(emoji, pname, id, participants) {
  if (pname) return emoji ? `${emoji} ${pname}` : pname;
  if (id) return participantLabel(participants, id);
  return "";
}

async function downloadArtifact(wsId, token, art, onError) {
  if (!token) {
    const a = document.createElement("a");
    a.href = art.raw_url;
    a.download = art.name;
    document.body.appendChild(a);
    a.click();
    a.remove();
    return;
  }
  try {
    const resp = await fetchArtifactBytes(wsId, token, art.name);
    const blob = await resp.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = art.name;
    document.body.appendChild(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  } catch (e) {
    onError?.(e.message);
  }
}

export default function Artifacts({
  artifacts,
  setArtifacts,
  artifactFile,
  items,
  participants,
  workspace,
  token,
  call,
  onError,
  head,
  error,
  navigate,
}) {
  if (artifactFile) {
    return (
      <ArtifactViewer
        key={artifactFile}
        file={artifactFile}
        artifacts={artifacts}
        setArtifacts={setArtifacts}
        items={items}
        participants={participants}
        workspace={workspace}
        token={token}
        call={call}
        onError={onError}
        head={head}
        error={error}
        navigate={navigate}
      />
    );
  }
  return (
    <ArtifactListScreen
      artifacts={artifacts}
      setArtifacts={setArtifacts}
      items={items}
      participants={participants}
      workspace={workspace}
      token={token}
      call={call}
      onError={onError}
      head={head}
      error={error}
    />
  );
}

function ArtifactListScreen({ artifacts, setArtifacts, items, participants, workspace, token, call, onError, head, error }) {
  const [filter, setFilter] = useState("all");
  const [uploading, setUploading] = useState(false);
  const [copiedName, setCopiedName] = useState("");
  const [replacingName, setReplacingName] = useState("");
  const replaceInputRef = useRef(null);
  const replaceTargetRef = useRef(null);

  useKeyShortcut(
    (e) => !e.shiftKey && (e.key === "c" || e.key === "C"),
    () => setUploading(true),
  );

  const tabCounts = useMemo(
    () => ({
      all: artifacts.length,
      docs: artifacts.filter((a) => {
        const k = artifactKind(a);
        return k === "markdown" || k === "text" || k === "json";
      }).length,
      html: artifacts.filter((a) => artifactKind(a) === "html").length,
      images: artifacts.filter((a) => artifactKind(a) === "image").length,
    }),
    [artifacts],
  );

  const visible = useMemo(() => {
    switch (filter) {
      case "docs":
        return artifacts.filter((a) => {
          const k = artifactKind(a);
          return k === "markdown" || k === "text" || k === "json";
        });
      case "html":
        return artifacts.filter((a) => artifactKind(a) === "html");
      case "images":
        return artifacts.filter((a) => artifactKind(a) === "image");
      default:
        return artifacts;
    }
  }, [artifacts, filter]);

  const handleCopyLink = (art) => {
    const full = `${window.location.origin}${art.url}`;
    navigator.clipboard?.writeText(full).catch(() => {});
    setCopiedName(art.name);
    setTimeout(() => setCopiedName((cur) => (cur === art.name ? "" : cur)), 1500);
  };

  const triggerReplace = (art) => {
    replaceTargetRef.current = art;
    replaceInputRef.current?.click();
  };

  const handleReplacePicked = async (e) => {
    const file = e.target.files?.[0];
    e.target.value = "";
    const target = replaceTargetRef.current;
    if (!file || !target) return;
    if (file.size > MAX_ARTIFACT_BYTES) {
      onError(`"${file.name}" is over 10 MB.`);
      return;
    }
    setReplacingName(target.name);
    try {
      const updated = await replaceArtifact(workspace.id, token, target.name, file);
      setArtifacts((list) => withArtifact(list, updated));
    } catch (err) {
      onError(err.message);
    } finally {
      setReplacingName("");
    }
  };

  const handleDelete = async (art) => {
    if (!window.confirm(`Delete artifact "${art.name}"?`)) return;
    try {
      await call("DELETE", `/artifacts/${encodeURIComponent(art.name)}`);
      setArtifacts((list) => list.filter((x) => x.id !== art.id && x.name.toLowerCase() !== art.name.toLowerCase()));
    } catch (err) {
      onError(err.message);
    }
  };

  const tools = (
    <div className="seg" role="group" aria-label="Artifact type">
      {FILTER_TABS.map(({ id, label }) => (
        <button key={id} type="button" aria-pressed={filter === id} onClick={() => setFilter(id)}>
          {label}
          {tabCounts[id] > 0 && <span className="todo-tab-count"> {tabCounts[id]}</span>}
        </button>
      ))}
    </div>
  );

  return (
    <section className="screen" aria-label="Artifacts">
      <ScreenHead title="Artifacts" tools={tools}>
        {head}
        <Button variant="primary" size="small" onClick={() => setUploading(true)} aria-haspopup="dialog" aria-label="Upload artifact">
          <Plus aria-hidden="true" />
          <span className="label">Upload artifact</span>
        </Button>
      </ScreenHead>
      {error}
      <div className="screen-body">
        {visible.length === 0 ? (
          <p className="recurring-empty">
            {artifacts.length === 0
              ? "No artifacts yet. Upload design docs, HTML prototypes, JSON reports, or screenshots."
              : "No matching artifacts."}
          </p>
        ) : (
          <div className="artifact-list">
            {visible.map((art) => {
              const linkedItem = art.item_id ? items.find((it) => it.id === art.item_id) || art.item : null;
              const linkedNum = art.item_number || itemNum(linkedItem) || art.item_id;
              const updater = actorDisplay(
                art.updater_emoji || art.uploader_emoji,
                art.updater_name || art.uploader_name,
                art.updated_by || art.uploaded_by,
                participants,
              );
              const hasCustomTitle = art.title && art.title !== art.name;
              return (
                <div key={art.id} className="artifact-row">
                  <div className="artifact-row-icon">
                    <ArtifactTypeIcon artifact={art} />
                  </div>
                  <div className="artifact-row-main">
                    <div className="artifact-row-head">
                      <a className="artifact-row-title" {...spaLink(art.url)}>
                        {hasCustomTitle ? art.title : art.name}
                      </a>
                      {hasCustomTitle && <code className="artifact-filename">{art.name}</code>}
                      <span className="chip">v{art.version}</span>
                      {art.item_id && (
                        <a
                          className="chip todo-link-chip"
                          {...itemLink(linkedNum)}
                          title={linkedItem?.title ? `#${linkedNum}: ${linkedItem.title}` : `Item #${linkedNum}`}
                        >
                          #{linkedNum}
                          {linkedItem?.title ? ` · ${linkedItem.title}` : ""}
                        </a>
                      )}
                    </div>
                    <div className="artifact-row-sub">
                      <span>{formatArtifactSize(art.size)}</span>
                      <span>·</span>
                      <span>
                        Updated {ago(art.updated_at)}
                        {updater ? ` by ${updater}` : ""}
                      </span>
                    </div>
                  </div>
                  <div className="artifact-row-actions">
                    <Button
                      size="small"
                      variant="ghost"
                      onClick={() => handleCopyLink(art)}
                      title="Copy permalink"
                      aria-label={`Copy link to ${art.name}`}
                    >
                      {copiedName === art.name ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
                      <span className="artifact-action-label">{copiedName === art.name ? "Copied" : "Copy link"}</span>
                    </Button>
                    <Button
                      size="small"
                      variant="ghost"
                      onClick={() => downloadArtifact(workspace.id, token, art, onError)}
                      title={`Download ${art.name}`}
                      aria-label={`Download ${art.name}`}
                    >
                      <Download aria-hidden="true" />
                    </Button>
                    <Button
                      size="small"
                      variant="ghost"
                      disabled={replacingName === art.name}
                      onClick={() => triggerReplace(art)}
                      title="Replace file in-place (keeps permalink)"
                      aria-label={`Replace ${art.name}`}
                    >
                      <RefreshCw aria-hidden="true" />
                      <span className="artifact-action-label">{replacingName === art.name ? "Replacing…" : "Replace"}</span>
                    </Button>
                    <Button
                      iconOnly
                      variant="ghost"
                      className="button-danger"
                      onClick={() => handleDelete(art)}
                      title={`Delete ${art.name}`}
                      aria-label={`Delete ${art.name}`}
                    >
                      <Trash2 aria-hidden="true" />
                    </Button>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>

      <input ref={replaceInputRef} type="file" hidden onChange={handleReplacePicked} />

      {uploading && (
        <UploadArtifactDialog
          workspaceId={workspace.id}
          token={token}
          items={items}
          onClose={() => setUploading(false)}
          onUploaded={(art) => setArtifacts((list) => withArtifact(list, art))}
        />
      )}
    </section>
  );
}

function ArtifactViewer({
  file,
  artifacts,
  setArtifacts,
  items,
  participants,
  workspace,
  token,
  call,
  onError,
  head,
  error,
  navigate,
}) {
  const [fetchedMeta, setFetchedMeta] = useState(null);
  const [notFound, setNotFound] = useState(false);
  const [content, setContent] = useState(null);
  const [loadingContent, setLoadingContent] = useState(false);
  const [blobUrl, setBlobUrl] = useState("");
  const [mode, setMode] = useState("preview");
  const [copied, setCopied] = useState(false);
  const [replacing, setReplacing] = useState(false);
  const [editingMeta, setEditingMeta] = useState(false);
  const replaceInputRef = useRef(null);
  const onErrorRef = useRef(onError);
  onErrorRef.current = onError;

  const metaFromList = useMemo(
    () => artifacts.find((a) => a.name.toLowerCase() === file.toLowerCase()) || null,
    [artifacts, file],
  );
  const artifact = metaFromList || fetchedMeta;
  const kind = useMemo(() => (artifact ? artifactKind(artifact) : "binary"), [artifact]);
  const isSvg = Boolean(artifact && (artifact.name.toLowerCase().endsWith(".svg") || artifact.mime_type === "image/svg+xml"));
  const needsText = Boolean(artifact && (kind === "markdown" || kind === "html" || kind === "json" || kind === "text" || isSvg));

  // Fetch metadata directly if not in the cached workspace list yet (or verify 404).
  useEffect(() => {
    if (metaFromList) {
      setNotFound(false);
      setFetchedMeta(metaFromList);
      return;
    }
    let active = true;
    call("GET", `/artifacts/${encodeURIComponent(file)}?meta=1`).then(
      (meta) => {
        if (!active) return;
        setNotFound(false);
        setFetchedMeta(meta);
      },
      (err) => {
        if (!active) return;
        if (err instanceof ApiError && err.status === 404) {
          setNotFound(true);
          setFetchedMeta(null);
        } else {
          onErrorRef.current(err.message);
        }
      },
    );
    return () => {
      active = false;
    };
  }, [call, file, metaFromList]);

  // Fetch text content initially and immediately whenever sha256 changes over SSE!
  useEffect(() => {
    if (!artifact || !needsText) return;
    const ctrl = new AbortController();
    let active = true;
    setLoadingContent(true);
    fetchArtifactBytes(workspace.id, token, artifact.name, { signal: ctrl.signal })
      .then((r) => r.text())
      .then((txt) => {
        if (active) setContent(txt);
      })
      .catch((err) => {
        if (active && err?.name !== "AbortError") onErrorRef.current(err.message);
      })
      .finally(() => {
        if (active) setLoadingContent(false);
      });
    return () => {
      active = false;
      ctrl.abort();
    };
  }, [workspace.id, token, artifact?.name, artifact?.sha256, needsText]);

  // For participant-token sessions (open dev mode without cookie), fetch image blob with Bearer header.
  useEffect(() => {
    if (!artifact || kind !== "image" || !token) {
      setBlobUrl("");
      return;
    }
    const ctrl = new AbortController();
    let active = true;
    let createdUrl = "";
    fetchArtifactBytes(workspace.id, token, artifact.name, { signal: ctrl.signal })
      .then((r) => r.blob())
      .then((b) => {
        if (!active) return;
        createdUrl = URL.createObjectURL(b);
        setBlobUrl(createdUrl);
      })
      .catch(() => {});
    return () => {
      active = false;
      ctrl.abort();
      if (createdUrl) URL.revokeObjectURL(createdUrl);
    };
  }, [workspace.id, token, artifact?.name, artifact?.sha256, kind]);

  const formattedJson = useMemo(() => {
    if (kind !== "json" || content === null) return "";
    if (mode === "source") return content;
    try {
      return JSON.stringify(JSON.parse(content), null, 2);
    } catch {
      return content;
    }
  }, [kind, content, mode]);

  const handleCopyLink = useCallback(() => {
    if (!artifact) return;
    navigator.clipboard?.writeText(`${window.location.origin}${artifact.url}`).catch(() => {});
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  }, [artifact]);

  const handleReplaceFile = async (e) => {
    const picked = e.target.files?.[0];
    e.target.value = "";
    if (!picked || !artifact) return;
    if (picked.size > MAX_ARTIFACT_BYTES) {
      onError(`"${picked.name}" is over 10 MB.`);
      return;
    }
    setReplacing(true);
    try {
      const updated = await replaceArtifact(workspace.id, token, artifact.name, picked);
      setFetchedMeta(updated);
      setArtifacts((list) => withArtifact(list, updated));
    } catch (err) {
      onError(err.message);
    } finally {
      setReplacing(false);
    }
  };

  const handleDelete = async () => {
    if (!artifact || !window.confirm(`Delete artifact "${artifact.name}"?`)) return;
    try {
      await call("DELETE", `/artifacts/${encodeURIComponent(artifact.name)}`);
      setArtifacts((list) => list.filter((x) => x.id !== artifact.id && x.name.toLowerCase() !== artifact.name.toLowerCase()));
      navigate(`/w/${workspace.id}/artifacts`);
    } catch (err) {
      onError(err.message);
    }
  };

  const handleSaveMeta = async (patch) => {
    if (!artifact) return false;
    try {
      const updated = await call("PATCH", `/artifacts/${encodeURIComponent(artifact.name)}`, patch);
      setFetchedMeta(updated);
      setArtifacts((list) => withArtifact(list, updated));
      return true;
    } catch (err) {
      onError(err.message);
      return false;
    }
  };

  const backHref = `/w/${workspace.id}/artifacts`;

  if (notFound) {
    return (
      <section className="screen" aria-label="Artifact not found">
        <ScreenHead
          title={
            <span className="artifact-viewer-heading">
              <a className="artifact-back" {...spaLink(backHref)}>
                <ArrowLeft aria-hidden="true" />
                Artifacts
              </a>
              <span className="muted">/</span>
              <span>{file}</span>
            </span>
          }
        >
          {head}
        </ScreenHead>
        {error}
        <div className="screen-body">
          <p className="recurring-empty">Artifact &ldquo;{file}&rdquo; was not found in this workspace.</p>
        </div>
      </section>
    );
  }

  if (!artifact) {
    return (
      <section className="screen" aria-label="Loading artifact">
        <ScreenHead
          title={
            <span className="artifact-viewer-heading">
              <a className="artifact-back" {...spaLink(backHref)}>
                <ArrowLeft aria-hidden="true" />
                Artifacts
              </a>
            </span>
          }
        >
          {head}
        </ScreenHead>
        {error}
        <div className="screen-body">
          <p className="recurring-empty">Loading…</p>
        </div>
      </section>
    );
  }

  const linkedItem = artifact.item_id ? items.find((it) => it.id === artifact.item_id) || artifact.item : null;
  const linkedNum = artifact.item_number || itemNum(linkedItem) || artifact.item_id;
  const uploader = actorDisplay(artifact.uploader_emoji, artifact.uploader_name, artifact.uploaded_by, participants);
  const updater = actorDisplay(artifact.updater_emoji, artifact.updater_name, artifact.updated_by, participants);
  const hasCustomTitle = artifact.title && artifact.title !== artifact.name;
  const imageSrc = token ? blobUrl : `${artifact.raw_url}?v=${artifact.sha256}`;

  const modeLabels =
    kind === "markdown"
      ? [
          ["preview", "Rendered"],
          ["source", "Source"],
        ]
      : kind === "html" || isSvg
        ? [
            ["preview", "Preview"],
            ["source", "Source"],
          ]
        : kind === "json"
          ? [
              ["preview", "Formatted"],
              ["source", "Raw"],
            ]
          : null;

  return (
    <section className="screen artifact-viewer-screen" aria-label={`Artifact ${artifact.name}`}>
      <ScreenHead
        title={
          <span className="artifact-viewer-heading">
            <a className="artifact-back" {...spaLink(backHref)} title="Back to Artifacts">
              <ArrowLeft aria-hidden="true" />
              <span>Artifacts</span>
            </a>
            <span className="artifact-sep" aria-hidden="true">
              /
            </span>
            <span className="artifact-viewer-title">{hasCustomTitle ? artifact.title : artifact.name}</span>
            {hasCustomTitle && <code className="artifact-filename">{artifact.name}</code>}
            <span className="chip chip-accent" title={`Version ${artifact.version}`}>
              v{artifact.version}
            </span>
          </span>
        }
        tools={
          modeLabels && (
            <div className="seg" role="group" aria-label="View mode">
              {modeLabels.map(([id, label]) => (
                <button key={id} type="button" aria-pressed={mode === id} onClick={() => setMode(id)}>
                  {label}
                </button>
              ))}
            </div>
          )
        }
      >
        {head}
      </ScreenHead>
      {error}

      <div className="artifact-meta-bar">
        <div className="artifact-meta-facts">
          <span>{formatArtifactSize(artifact.size)}</span>
          <span>·</span>
          <span>
            {artifact.version > 1 ? `Updated ${ago(artifact.updated_at)}` : `Uploaded ${ago(artifact.created_at)}`}
            {(updater || uploader) ? ` by ${updater || uploader}` : ""}
          </span>
          {artifact.item_id && (
            <a
              className="chip todo-link-chip"
              {...itemLink(linkedNum)}
              title={linkedItem?.title ? `#${linkedNum}: ${linkedItem.title}` : `Item #${linkedNum}`}
            >
              #{linkedNum}
              {linkedItem?.title ? ` · ${linkedItem.title}` : ""}
            </a>
          )}
        </div>
        <div className="artifact-meta-actions">
          <Button size="small" variant="ghost" onClick={() => setEditingMeta(true)} title="Edit title or linked item">
            <Pencil aria-hidden="true" />
            <span className="artifact-action-label">Edit</span>
          </Button>
          <Button size="small" variant="ghost" onClick={handleCopyLink} title="Copy permalink">
            {copied ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
            <span>{copied ? "Copied" : "Copy link"}</span>
          </Button>
          <Button
            size="small"
            variant="ghost"
            onClick={() => downloadArtifact(workspace.id, token, artifact, onError)}
            title={`Download ${artifact.name}`}
          >
            <Download aria-hidden="true" />
            <span className="artifact-action-label">Download</span>
          </Button>
          <Button
            size="small"
            disabled={replacing}
            onClick={() => replaceInputRef.current?.click()}
            title="Replace artifact in-place (keeps permalink)"
          >
            <RefreshCw aria-hidden="true" />
            <span>{replacing ? "Replacing…" : "Replace"}</span>
          </Button>
          <Button
            size="small"
            variant="ghost"
            className="button-danger"
            onClick={handleDelete}
            title={`Delete ${artifact.name}`}
          >
            <Trash2 aria-hidden="true" />
            <span className="artifact-action-label">Delete</span>
          </Button>
        </div>
      </div>

      <div className={kind === "html" && mode === "preview" ? "screen-body artifact-iframe-wrap" : "screen-body artifact-viewer-body"}>
        {needsText && content === null && loadingContent ? (
          <p className="recurring-empty">Loading content…</p>
        ) : kind === "markdown" ? (
          mode === "preview" ? (
            <article className="artifact-doc">
              <MemoMarkdown text={content || ""} />
            </article>
          ) : (
            <pre className="artifact-source">
              <code>{content || ""}</code>
            </pre>
          )
        ) : kind === "html" ? (
          mode === "preview" ? (
            <iframe
              key={artifact.sha256}
              className="artifact-iframe"
              title={artifact.title || artifact.name}
              sandbox="allow-scripts allow-forms"
              srcDoc={content || ""}
            />
          ) : (
            <pre className="artifact-source">
              <code>{content || ""}</code>
            </pre>
          )
        ) : kind === "image" ? (
          isSvg && mode === "source" ? (
            <pre className="artifact-source">
              <code>{content || ""}</code>
            </pre>
          ) : (
            <div className="artifact-image-stage">
              {imageSrc ? (
                <img className="artifact-image" src={imageSrc} alt={artifact.title || artifact.name} />
              ) : (
                <p className="muted">Loading image…</p>
              )}
            </div>
          )
        ) : kind === "json" ? (
          <pre className="artifact-source">
            <code>{formattedJson}</code>
          </pre>
        ) : kind === "text" ? (
          <pre className="artifact-source">
            <code>{content || ""}</code>
          </pre>
        ) : (
          <div className="artifact-binary-card">
            <div className="artifact-binary-icon">
              <ArtifactTypeIcon artifact={artifact} />
            </div>
            <p className="artifact-binary-name">{artifact.name}</p>
            <p className="muted">
              {artifact.mime_type} · {formatArtifactSize(artifact.size)} · SHA-256 {artifact.sha256.slice(0, 12)}…
            </p>
            <Button variant="primary" onClick={() => downloadArtifact(workspace.id, token, artifact, onError)}>
              <Download aria-hidden="true" />
              Download file
            </Button>
          </div>
        )}
      </div>

      <input ref={replaceInputRef} type="file" hidden onChange={handleReplaceFile} />

      {editingMeta && (
        <EditArtifactMetaDialog
          artifact={artifact}
          items={items}
          onClose={() => setEditingMeta(false)}
          onSave={handleSaveMeta}
        />
      )}
    </section>
  );
}

export function UploadArtifactDialog({ workspaceId, token, items = [], initialItemId = 0, onClose, onUploaded }) {
  const initialItemNum = useMemo(() => {
    if (!initialItemId) return 0;
    const match = items.find((it) => itemNum(it) === initialItemId) || items.find((it) => it.id === initialItemId);
    return match ? itemNum(match) : initialItemId;
  }, [items, initialItemId]);

  const [file, setFile] = useState(null);
  const [name, setName] = useState("");
  const [nameEdited, setNameEdited] = useState(false);
  const [title, setTitle] = useState("");
  const [itemId, setItemId] = useState(initialItemNum);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const fileInputRef = useRef(null);

  const selectableItems = useMemo(() => {
    const queued = items.filter((it) => it.status !== "done");
    if (initialItemNum && !queued.some((it) => itemNum(it) === initialItemNum)) {
      const extra = items.find((it) => itemNum(it) === initialItemNum || it.id === initialItemId);
      if (extra) return [extra, ...queued];
    }
    return queued;
  }, [items, initialItemNum, initialItemId]);

  const chooseFile = (picked) => {
    if (!picked) return;
    setErr("");
    if (picked.size > MAX_ARTIFACT_BYTES) {
      setErr(`"${picked.name}" is over 10 MB.`);
      return;
    }
    setFile(picked);
    if (!nameEdited || !name.trim()) {
      setName(picked.name);
    }
  };

  const submit = async (e) => {
    e.preventDefault();
    if (!file) {
      setErr("Choose a file to upload.");
      return;
    }
    const cleanName = (name || file.name).trim();
    if (!cleanName) {
      setErr("Artifact name is required.");
      return;
    }
    setBusy(true);
    setErr("");
    try {
      const created = await uploadArtifact(workspaceId, token, file, {
        name: cleanName,
        title: title.trim(),
        item_id: itemId > 0 ? itemId : undefined,
      });
      onUploaded?.(created);
      onClose();
    } catch (uploadErr) {
      setErr(uploadErr.message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog title="Upload artifact" onClose={onClose} className="item-dialog">
      <form className="form item-form" onSubmit={submit} onKeyDown={submitOnModEnter}>
        <Field label="File">
          <div className="artifact-file-picker">
            <Button type="button" data-autofocus onClick={() => fileInputRef.current?.click()}>
              <Upload aria-hidden="true" />
              {file ? "Change file…" : "Choose file…"}
            </Button>
            <span className="muted">{file ? `${file.name} (${formatArtifactSize(file.size)})` : "Up to 10 MB (.md, .html, .json, .svg, .png, …)"}</span>
            <input
              ref={fileInputRef}
              type="file"
              hidden
              onChange={(e) => {
                chooseFile(e.target.files?.[0]);
                e.target.value = "";
              }}
            />
          </div>
        </Field>
        <Field label="Filename (permalink)">
          <Input
            required
            value={name}
            onChange={(e) => {
              setNameEdited(true);
              setName(e.target.value);
            }}
            placeholder="design-doc.md"
          />
        </Field>
        <Field label="Title (optional)">
          <Input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="Defaults to filename" />
        </Field>
        <Field label="Linked backlog item (optional)">
          <Select value={itemId} onChange={(e) => setItemId(Number(e.target.value))}>
            <option value={0}>None</option>
            {selectableItems.map((it) => (
              <option key={it.id} value={itemNum(it)}>
                #{itemNum(it)} — {it.title}
              </option>
            ))}
          </Select>
        </Field>
        {err && <p role="alert">{err}</p>}
        <div className="actions-row dialog-actions">
          <span className="muted dialog-hint">⌘/Ctrl+Enter to upload</span>
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={busy || !file}>
            {busy ? "Uploading…" : "Upload"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function EditArtifactMetaDialog({ artifact, items = [], onClose, onSave }) {
  const initialItemNum = useMemo(() => {
    if (!artifact.item_id) return 0;
    if (artifact.item_number) return artifact.item_number;
    const match = items.find((it) => it.id === artifact.item_id);
    return match ? itemNum(match) : artifact.item_id;
  }, [artifact.item_id, artifact.item_number, items]);

  const [title, setTitle] = useState(artifact.title === artifact.name ? "" : artifact.title || "");
  const [itemId, setItemId] = useState(initialItemNum);
  const [busy, setBusy] = useState(false);

  const selectableItems = useMemo(() => {
    const queued = items.filter((it) => it.status !== "done");
    if (initialItemNum && !queued.some((it) => itemNum(it) === initialItemNum)) {
      const extra = items.find((it) => itemNum(it) === initialItemNum || it.id === artifact.item_id);
      if (extra) return [extra, ...queued];
    }
    return queued;
  }, [items, initialItemNum, artifact.item_id]);

  const submit = async (e) => {
    e.preventDefault();
    setBusy(true);
    const ok = await onSave({ title: title.trim(), item_id: Number(itemId) || 0 });
    setBusy(false);
    if (ok) onClose();
  };

  return (
    <Dialog title={`Edit ${artifact.name}`} onClose={onClose}>
      <form className="form item-form" onSubmit={submit} onKeyDown={submitOnModEnter}>
        <Field label="Title (optional)">
          <Input data-autofocus value={title} onChange={(e) => setTitle(e.target.value)} placeholder={artifact.name} />
        </Field>
        <Field label="Linked backlog item">
          <Select value={itemId} onChange={(e) => setItemId(Number(e.target.value))}>
            <option value={0}>None</option>
            {selectableItems.map((it) => (
              <option key={it.id} value={itemNum(it)}>
                #{itemNum(it)} — {it.title}
              </option>
            ))}
          </Select>
        </Field>
        <div className="actions-row dialog-actions">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={busy}>
            Save
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
