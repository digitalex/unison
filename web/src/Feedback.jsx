import { useCallback, useEffect, useRef, useState } from "react";
import { Archive, ArrowRight, Bug, Image as ImageIcon, Lightbulb, Pencil, Plus, RotateCcw, Trash2 } from "lucide-react";
import { getToken } from "./api.js";
import { ago, itemNum, name, submitOnModEnter, useStableParticipants } from "./backlogFormat.js";
import { FEEDBACK_KINDS, FEEDBACK_SEVERITIES } from "./FeedbackDialog.jsx";
import { imagePath } from "./images.jsx";
import { MemoMarkdown } from "./Markdown.jsx";
import { MentionTextarea } from "./mentions.jsx";
import { itemLink } from "./nav.js";
import { useKeyShortcut } from "./shortcuts.jsx";
import Button from "./ui/Button.jsx";
import Dialog from "./ui/Dialog.jsx";
import Field from "./ui/Field.jsx";
import { Input } from "./ui/Input.jsx";
import ScreenHead from "./ui/ScreenHead.jsx";

const STATUS_ORDER = { open: 0, promoted: 1, dismissed: 2 };

const STATUS_FILTERS = [
  { id: "open", label: "Open" },
  { id: "promoted", label: "Moved" },
  { id: "dismissed", label: "Dismissed" },
  { id: "all", label: "All" },
];

const SEVERITY_LABELS = {
  low: "Low",
  medium: "Medium",
  high: "High",
  critical: "Critical",
};

// withFeedback returns list with f inserted or replaced in store.ListFeedback order.
export function withFeedback(list, f) {
  return [...list.filter((x) => x.id !== f.id), f].sort((a, b) => {
    const sa = STATUS_ORDER[a.status] ?? 2;
    const sb = STATUS_ORDER[b.status] ?? 2;
    if (sa !== sb) return sa - sb;
    const ta = a.status === "promoted" ? a.promoted_at || a.created_at : a.created_at;
    const tb = b.status === "promoted" ? b.promoted_at || b.created_at : b.created_at;
    return tb - ta || b.id - a.id;
  });
}

function ItemChip({ id }) {
  const link = itemLink(id);
  const onClick = (e) => {
    e.stopPropagation();
    link.onClick(e);
  };
  return (
    <a className="chip idea-item-chip" href={link.href} onClick={onClick} title={`Moved to the backlog as #${id}`}>
      <ArrowRight aria-hidden="true" />#{id}
    </a>
  );
}

// FeedbackScreenshot renders a workspace-authenticated screenshot or external image URL.
function FeedbackScreenshot({ url, alt = "Feedback screenshot" }) {
  const parsed = imagePath(url || "");
  const token = parsed ? getToken(parsed.ws) : "";
  const [src, setSrc] = useState(parsed && token ? "" : url);

  useEffect(() => {
    if (!parsed || !token) {
      setSrc(url);
      return;
    }
    let objectUrl = "";
    let active = true;
    fetch(parsed.path, { headers: { Authorization: `Bearer ${token}` } })
      .then((r) => (r.ok ? r.blob() : Promise.reject()))
      .then((b) => {
        if (!active) return;
        objectUrl = URL.createObjectURL(b);
        setSrc(objectUrl);
      })
      .catch(() => {});
    return () => {
      active = false;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [url, parsed?.path, token]);

  if (!src) return <div className="muted">Loading screenshot…</div>;
  return (
    <a className="feedback-detail-shot-link" href={src} target="_blank" rel="noopener noreferrer">
      <img className="feedback-detail-shot" src={src} alt={alt} loading="lazy" />
    </a>
  );
}

export default function Feedback({ feedback, setFeedback, participants, call, onError, head, error, onOpenSendFeedback }) {
  const [filter, setFilter] = useState("open");
  const [openedId, setOpenedId] = useState(null);
  const [moved, setMoved] = useState(null); // { feedback, item }

  useKeyShortcut(
    (e) => !e.shiftKey && (e.key === "c" || e.key === "C"),
    () => onOpenSendFeedback?.(),
  );

  const onErrorRef = useRef(onError);
  onErrorRef.current = onError;
  const run = useCallback(
    (method, suffix, payload) => call(method, suffix, payload).catch((e) => onErrorRef.current(e.message)),
    [call],
  );

  const stableParticipants = useStableParticipants(participants);
  const saved = useCallback((f) => setFeedback((list) => withFeedback(list, f)), [setFeedback]);
  const removed = useCallback((id) => setFeedback((list) => list.filter((x) => x.id !== id)), [setFeedback]);
  const promoted = useCallback(
    (result) => {
      saved(result.feedback);
      setMoved(result);
    },
    [saved],
  );

  const activeReport = openedId !== null ? feedback.find((f) => f.id === openedId) : null;

  useEffect(() => {
    if (openedId !== null && !activeReport) setOpenedId(null);
  }, [openedId, activeReport]);

  useEffect(() => {
    if (!moved) return;
    const t = setTimeout(() => setMoved(null), 8000);
    return () => clearTimeout(t);
  }, [moved]);

  const openList = feedback.filter((f) => f.status === "open");
  const promotedList = feedback.filter((f) => f.status === "promoted");
  const dismissedList = feedback.filter((f) => f.status === "dismissed");

  const visible =
    filter === "open"
      ? openList
      : filter === "promoted"
        ? promotedList
        : filter === "dismissed"
          ? dismissedList
          : feedback;

  const renderRow = (f) => {
    const KindIcon = f.kind === "suggestion" ? Lightbulb : Bug;
    const promotedNum = f.promoted_item_number || f.promoted_item_id;
    return (
      <div key={f.id} className={f.status === "dismissed" ? "item feedback-row feedback-row-dismissed" : "item feedback-row"}>
        <div
          className="row"
          onClick={(e) => {
            e.currentTarget.querySelector(".row-title")?.focus();
            setOpenedId(f.id);
          }}
        >
          <span className="recurring-marker" title={f.kind === "suggestion" ? "Suggestion" : "Bug"} aria-hidden="true">
            <KindIcon />
          </span>
          <button type="button" className="row-title" aria-haspopup="dialog">
            {f.title}
          </button>
          <span className={`chip feedback-severity feedback-severity-${f.severity}`}>
            {SEVERITY_LABELS[f.severity] || f.severity}
          </span>
          {f.image_url && (
            <span className="comment-count" title="Includes screenshot">
              <ImageIcon aria-hidden="true" />
            </span>
          )}
          <span className="item-reporter">by {name(stableParticipants, f.created_by)}</span>
          {f.status === "promoted" && promotedNum && <ItemChip id={promotedNum} />}
          {f.status === "dismissed" && <span className="chip">dismissed</span>}
          <span className="meta" title={new Date(f.created_at).toLocaleString()}>
            {ago(f.created_at)}
          </span>
        </div>
      </div>
    );
  };

  return (
    <section className="screen" aria-label="Feedback">
      <ScreenHead
        title="Feedback"
        tools={
          <div className="seg view-switch" role="group" aria-label="Filter feedback">
            {STATUS_FILTERS.map((f) => {
              const count =
                f.id === "open"
                  ? openList.length
                  : f.id === "promoted"
                    ? promotedList.length
                    : f.id === "dismissed"
                      ? dismissedList.length
                      : feedback.length;
              return (
                <button key={f.id} type="button" aria-pressed={filter === f.id} onClick={() => setFilter(f.id)}>
                  {f.label}
                  {count > 0 && <span className="muted"> {count}</span>}
                </button>
              );
            })}
          </div>
        }
      >
        {head}
        <Button variant="primary" size="small" onClick={onOpenSendFeedback} aria-haspopup="dialog" aria-label="Send feedback">
          <Plus aria-hidden="true" />
          <span className="label">Send feedback</span>
        </Button>
      </ScreenHead>
      {error}
      <div className="screen-body">
        {moved && (
          <p role="status" className="error-bar notice-bar">
            <span>
              Moved feedback {moved.feedback.id} to the backlog as{" "}
              <a {...itemLink(itemNum(moved.item))}>#{itemNum(moved.item)}</a>.
            </span>
            <Button variant="ghost" onClick={() => setMoved(null)}>
              dismiss
            </Button>
          </p>
        )}

        <div className="recurring-list">
          {filter === "all" ? (
            <>
              <h3 className="group-head">
                <span className="status open" />
                Open
                <span className="count">{openList.length}</span>
              </h3>
              {openList.length === 0 ? (
                <p className="empty-row">No open feedback reports.</p>
              ) : (
                openList.map(renderRow)
              )}
              {promotedList.length > 0 && (
                <>
                  <h3 className="group-head">
                    <span className="status done" />
                    Moved to backlog
                    <span className="count">{promotedList.length}</span>
                  </h3>
                  {promotedList.map(renderRow)}
                </>
              )}
              {dismissedList.length > 0 && (
                <>
                  <h3 className="group-head">
                    <span className="status paused" />
                    Dismissed
                    <span className="count">{dismissedList.length}</span>
                  </h3>
                  {dismissedList.map(renderRow)}
                </>
              )}
            </>
          ) : visible.length === 0 ? (
            <p className="empty-row">
              {filter === "open"
                ? "No open feedback to triage. Use Send feedback to report a bug or suggestion with a screenshot."
                : `No ${filter === "promoted" ? "moved" : "dismissed"} feedback reports.`}
            </p>
          ) : (
            visible.map(renderRow)
          )}
        </div>
      </div>

      {activeReport && (
        <FeedbackDetailDialog
          key={activeReport.id}
          report={activeReport}
          participants={stableParticipants}
          run={run}
          saved={saved}
          removed={removed}
          promoted={promoted}
          onClose={() => setOpenedId(null)}
        />
      )}
    </section>
  );
}

function FeedbackDetailDialog({ report, participants, run, saved, removed, promoted, onClose }) {
  const path = `/feedback/${report.id}`;
  const [editing, setEditing] = useState(false);
  const [busy, setBusy] = useState(false);

  const isPromoted = report.status === "promoted";
  const isDismissed = report.status === "dismissed";
  const KindIcon = report.kind === "suggestion" ? Lightbulb : Bug;

  const promote = async () => {
    setBusy(true);
    const result = await run("POST", `${path}/promote`);
    setBusy(false);
    if (result) {
      promoted(result);
      onClose();
    }
  };

  const toggleDismiss = async () => {
    setBusy(true);
    const updated = await run("POST", `${path}/${isDismissed ? "reopen" : "dismiss"}`);
    setBusy(false);
    if (updated) saved(updated);
  };

  const remove = async () => {
    if (window.confirm(`Delete feedback ${report.id}? This can't be undone.`) && (await run("DELETE", path))) {
      removed(report.id);
      onClose();
    }
  };

  const statusText = isPromoted ? "Moved to backlog" : isDismissed ? "Dismissed" : "Open";
  const facts = [
    ["Type", report.kind === "suggestion" ? "Suggestion" : "Bug"],
    [
      "Severity",
      <span className={`chip feedback-severity feedback-severity-${report.severity}`}>
        {SEVERITY_LABELS[report.severity] || report.severity}
      </span>,
    ],
    ["Status", statusText],
    ["Reported", `${ago(report.created_at)} by ${name(participants, report.created_by)}`],
  ];
  if (report.page_url) {
    facts.push(["Page", <code>{report.page_url}</code>]);
  }
  if (isPromoted) {
    const promotedNum = report.promoted_item_number || report.promoted_item_id;
    facts.push([
      "Moved",
      <>
        to <a {...itemLink(promotedNum)}>#{promotedNum}</a>
        {report.promoted_at ? ` ${ago(report.promoted_at)}` : ""}
        {report.promoted_by ? ` by ${name(participants, report.promoted_by)}` : ""}
      </>,
    ]);
  }

  return (
    <Dialog
      title={
        <>
          <span className="recurring-marker" aria-hidden="true">
            <KindIcon />
          </span>
          <span className="item-id">feedback {report.id}</span>
          <span className="dialog-title-text">{report.title}</span>
        </>
      }
      head={
        !editing && (
          <Button iconOnly variant="ghost" aria-label="Edit feedback report" title="Edit" onClick={() => setEditing(true)}>
            <Pencil aria-hidden="true" />
          </Button>
        )
      }
      onClose={onClose}
      className="item-dialog feedback-detail-dialog"
    >
      {editing ? (
        <FeedbackEditForm report={report} path={path} run={run} saved={saved} onDone={() => setEditing(false)} />
      ) : (
        <>
          <div className="item-description">
            {report.body ? <MemoMarkdown text={report.body} /> : <p className="muted">No additional details.</p>}
          </div>
          {report.image_url && (
            <section className="detail-section">
              <h3>Screenshot</h3>
              <FeedbackScreenshot url={report.image_url} alt={report.title} />
            </section>
          )}
        </>
      )}

      <dl className="item-facts">
        {facts.map(([k, v]) => (
          <div key={k}>
            <dt>{k}</dt>
            <dd>{v}</dd>
          </div>
        ))}
      </dl>

      <div className="item-actions">
        {!isPromoted && (
          <Button
            variant="primary"
            onClick={promote}
            disabled={busy}
            title="Create a backlog item with this report's details and screenshot"
          >
            <ArrowRight aria-hidden="true" />
            Move to backlog
          </Button>
        )}
        {!isPromoted && (
          <Button onClick={toggleDismiss} disabled={busy}>
            {isDismissed ? <RotateCcw aria-hidden="true" /> : <Archive aria-hidden="true" />}
            {isDismissed ? "Reopen" : "Dismiss"}
          </Button>
        )}
        <span className="spacer" />
        <Button variant="ghost" className="button-danger" onClick={remove}>
          <Trash2 aria-hidden="true" />
          Delete
        </Button>
      </div>
    </Dialog>
  );
}

function FeedbackEditForm({ report, path, run, saved, onDone }) {
  const [kind, setKind] = useState(report.kind);
  const [severity, setSeverity] = useState(report.severity);
  const [title, setTitle] = useState(report.title);
  const [body, setBody] = useState(report.body || "");

  const save = async (e) => {
    e.preventDefault();
    const updated = await run("PATCH", path, { kind, severity, title, body });
    if (updated) {
      saved(updated);
      onDone();
    }
  };

  return (
    <form className="form feedback-form" onSubmit={save} onKeyDown={submitOnModEnter}>
      <div className="feedback-controls-row">
        <Field label="Type">
          <div className="seg feedback-seg" role="group" aria-label="Feedback type">
            {FEEDBACK_KINDS.map(({ id, label, Icon }) => (
              <button key={id} type="button" aria-pressed={kind === id} onClick={() => setKind(id)}>
                <Icon aria-hidden="true" />
                {label}
              </button>
            ))}
          </div>
        </Field>
        <Field label="Severity">
          <div className="seg feedback-seg" role="group" aria-label="Severity">
            {FEEDBACK_SEVERITIES.map(({ id, label }) => (
              <button key={id} type="button" aria-pressed={severity === id} onClick={() => setSeverity(id)}>
                {label}
              </button>
            ))}
          </div>
        </Field>
      </div>
      <Field label="Title">
        <Input required autoFocus value={title} onChange={(e) => setTitle(e.target.value)} />
      </Field>
      <Field label="Details">
        <MentionTextarea rows={5} value={body} onChange={(e) => setBody(e.target.value)} />
      </Field>
      <div className="actions-row dialog-actions">
        <Button type="button" variant="ghost" onClick={onDone}>
          Cancel
        </Button>
        <Button type="submit" variant="primary">
          Save
        </Button>
      </div>
    </form>
  );
}
