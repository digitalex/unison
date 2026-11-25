import { useCallback, useEffect, useRef, useState } from "react";
import { ArrowRight, Lightbulb, MessageSquare, Pencil, Plus, Trash2 } from "lucide-react";
import { ago, name, submitOnModEnter, useStableParticipants } from "./backlogFormat.js";
import { Comments } from "./BacklogItemDialog.jsx";
import { MemoMarkdown } from "./Markdown.jsx";
import { MentionTextarea } from "./mentions.jsx";
import { itemLink } from "./nav.js";
import { useKeyShortcut } from "./shortcuts.jsx";
import Button from "./ui/Button.jsx";
import Dialog from "./ui/Dialog.jsx";
import Field from "./ui/Field.jsx";
import { Input } from "./ui/Input.jsx";
import ScreenHead from "./ui/ScreenHead.jsx";

// Ideas (#917): a top-level screen for early-stage proposals that aren't
// ready for the prioritized backlog. Each idea has a Markdown body and a
// discussion; Move to backlog turns it into a backlog item (its body, a
// reference to the idea, and the discussion) and marks it moved. Ideas are
// numbered on their own, so they're "idea N" here, never "#N" (an item).

// withIdea returns the list with d added or replaced, in the server's order:
// open ideas newest first, then moved ones, most recently moved first.
function withIdea(list, d) {
  return [...list.filter((x) => x.id !== d.id), d].sort(
    (a, b) => (a.status === "promoted") - (b.status === "promoted") || (b.promoted_at || 0) - (a.promoted_at || 0) || b.id - a.id,
  );
}

// ItemChip links a moved idea's row to the backlog item it became; clicking
// it opens the item, not the idea.
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

export default function Ideas({ ideas, setIdeas, participants, me, call, onError, head, error }) {
  const [opened, setOpened] = useState(null); // An idea id, or "new".
  useKeyShortcut(
    (e) => !e.shiftKey && (e.key === "c" || e.key === "C"),
    () => setOpened("new"),
  );
  const [moved, setMoved] = useState(null); // Last Move to backlog result: { idea, item }.
  // Ideas you added that the server hasn't confirmed yet: shown at once, at
  // the top, until the response arrives or the call fails.
  const [adding, setAdding] = useState([]);
  const onErrorRef = useRef(onError);
  onErrorRef.current = onError;
  const run = useCallback((method, suffix, payload) => call(method, suffix, payload).catch((e) => onErrorRef.current(e.message)), [call]);
  const stableParticipants = useStableParticipants(participants);
  // Saves apply their response at once rather than waiting for the event.
  const saved = useCallback((d) => setIdeas((list) => withIdea(list, d)), [setIdeas]);
  const add = async (payload) => {
    const temp = { ...payload, id: `pending-${Date.now()}` };
    setAdding((list) => [temp, ...list]);
    const d = await run("POST", "/ideas", payload);
    if (d) saved(d);
    setAdding((list) => list.filter((x) => x !== temp));
  };
  const removed = (id) => setIdeas((list) => list.filter((x) => x.id !== id));
  const promoted = (result) => {
    saved(result.idea);
    setMoved(result);
  };
  const activeIdea = opened && opened !== "new" ? ideas.find((d) => d.id === opened) : null;
  const open = ideas.filter((d) => d.status !== "promoted");
  const done = ideas.filter((d) => d.status === "promoted");

  useEffect(() => {
    if (opened !== null && opened !== "new" && !activeIdea) setOpened(null);
  }, [opened, activeIdea]);

  useEffect(() => {
    if (!moved) return;
    const t = setTimeout(() => setMoved(null), 8000);
    return () => clearTimeout(t);
  }, [moved]);

  const row = (d) => (
    <div key={d.id} className="item idea">
      <div
        className="row"
        onClick={(e) => {
          e.currentTarget.querySelector(".row-title").focus();
          setOpened(d.id);
        }}
      >
        <span className="recurring-marker" aria-hidden="true">
          <Lightbulb />
        </span>
        <button type="button" className="row-title" aria-haspopup="dialog">
          {d.title}
        </button>
        {d.comment_count > 0 && (
          <span className="comment-count" title={`${d.comment_count} comment${d.comment_count === 1 ? "" : "s"}`}>
            <MessageSquare aria-hidden="true" />
            {d.comment_count}
          </span>
        )}
        <span className="item-reporter">by {name(stableParticipants, d.created_by)}</span>
        {d.status === "promoted" && d.promoted_item_id && <ItemChip id={d.promoted_item_id} />}
        <span className="meta" title={new Date(d.created_at).toLocaleString()}>
          {ago(d.created_at)}
        </span>
      </div>
    </div>
  );

  return (
    <section className="screen" aria-label="Ideas">
      <ScreenHead title="Ideas">
        {head}
        <Button variant="primary" size="small" onClick={() => setOpened("new")} aria-haspopup="dialog" aria-label="New idea">
          <Plus aria-hidden="true" />
          <span className="label">New idea</span>
        </Button>
      </ScreenHead>
      {error}
      <div className="screen-body">
        {moved && (
          <p role="status" className="error-bar notice-bar">
            <span>
              Moved idea {moved.idea.id} to the backlog as <a {...itemLink(moved.item.id)}>#{moved.item.id}</a>.
            </span>
            <Button variant="ghost" onClick={() => setMoved(null)}>
              dismiss
            </Button>
          </p>
        )}
        <div className="recurring-list">
          <h3 className="group-head">
            <span className="status open" />
            Open
            <span className="count">{open.length}</span>
          </h3>
          {open.length === 0 && adding.length === 0 && <p className="empty-row">No ideas yet. Add one to start a discussion before it's ready for the backlog.</p>}
          {adding.map((d) => (
            <div key={d.id} className="item idea item-pending" aria-busy="true">
              <div className="row">
                <span className="recurring-marker" aria-hidden="true">
                  <Lightbulb />
                </span>
                <span className="row-title">{d.title}</span>
                <span className="meta">adding…</span>
              </div>
            </div>
          ))}
          {open.map(row)}
          {done.length > 0 && (
            <>
              <h3 className="group-head">
                <span className="status done" />
                Moved to backlog
                <span className="count">{done.length}</span>
              </h3>
              {done.map(row)}
            </>
          )}
        </div>
      </div>
      {opened === "new" && <NewIdeaDialog onClose={() => setOpened(null)} onCreate={add} />}
      {activeIdea && (
        <IdeaDialog
          key={activeIdea.id}
          idea={activeIdea}
          participants={stableParticipants}
          {...{ me, call, run, saved, removed, promoted }}
          onClose={() => setOpened(null)}
        />
      )}
    </section>
  );
}

// NewIdeaDialog asks for a title and a body; the dialog closes at once and
// the idea shows as pending until the server has it.
function NewIdeaDialog({ onClose, onCreate }) {
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const save = (e) => {
    e.preventDefault();
    onClose();
    onCreate({ title, body });
  };
  return (
    <Dialog title="New idea" onClose={onClose} className="item-dialog">
      <form className="form item-form" onSubmit={save} onKeyDown={submitOnModEnter}>
        <Field label="Title">
          <Input required data-autofocus value={title} onChange={(e) => setTitle(e.target.value)} placeholder="What's the idea?" />
        </Field>
        <Field label="Description">
          <MentionTextarea rows={10} value={body} onChange={(e) => setBody(e.target.value)} placeholder="The problem, a rough approach, open questions… (Markdown)" />
        </Field>
        <div className="actions-row dialog-actions">
          <span className="spacer" />
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary">
            Add idea
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// IdeaDialog shows one idea: its body (editable), who added it, its
// discussion, and Move to backlog. It renders whatever the live list holds,
// so other people's changes show up here.
function IdeaDialog({ idea, participants, me, call, run, saved, removed, promoted, onClose }) {
  const path = `/ideas/${idea.id}`;
  const [editing, setEditing] = useState(false);
  const [busy, setBusy] = useState(false);
  const isPromoted = idea.status === "promoted";
  const promote = async () => {
    setBusy(true);
    const result = await run("POST", `${path}/promote`);
    setBusy(false);
    if (result) {
      promoted(result);
      onClose();
    }
  };
  const remove = async () => {
    if (window.confirm(`Delete idea ${idea.id} and its discussion? This can't be undone.`) && (await run("DELETE", path))) {
      removed(idea.id);
      onClose();
    }
  };
  const facts = [["Status", isPromoted ? "Moved to backlog" : "Open"]];
  facts.push(["Added", `${ago(idea.created_at)} by ${name(participants, idea.created_by)}`]);
  if (isPromoted) {
    facts.push([
      "Moved",
      <>
        to <a {...itemLink(idea.promoted_item_id)}>#{idea.promoted_item_id}</a>
        {idea.promoted_at ? ` ${ago(idea.promoted_at)}` : ""}
        {idea.promoted_by ? ` by ${name(participants, idea.promoted_by)}` : ""}
      </>,
    ]);
  }
  return (
    <Dialog
      title={
        <>
          <span className="recurring-marker" aria-hidden="true">
            <Lightbulb />
          </span>
          <span className="item-id">idea {idea.id}</span>
          <span className="dialog-title-text">{idea.title}</span>
        </>
      }
      head={
        !editing && (
          <Button iconOnly variant="ghost" aria-label="Edit title and description" title="Edit" onClick={() => setEditing(true)}>
            <Pencil aria-hidden="true" />
          </Button>
        )
      }
      onClose={onClose}
      className="item-dialog"
    >
      {editing ? (
        <IdeaEditForm idea={idea} path={path} run={run} saved={saved} onDone={() => setEditing(false)} />
      ) : (
        <div className="item-description">{idea.body ? <MemoMarkdown text={idea.body} /> : <p className="muted">No description.</p>}</div>
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
          <Button variant="primary" onClick={promote} disabled={busy} title="Add this idea to the bottom of the backlog, with its description and discussion">
            <ArrowRight aria-hidden="true" />
            Move to backlog
          </Button>
        )}
        <span className="spacer" />
        <Button variant="ghost" className="button-danger" onClick={remove}>
          <Trash2 aria-hidden="true" />
          Delete
        </Button>
      </div>
      <Comments key={path} item={idea} path={path} participants={participants} me={me} call={call} run={run} title="Discussion" />
    </Dialog>
  );
}

function IdeaEditForm({ idea, path, run, saved, onDone }) {
  const [title, setTitle] = useState(idea.title);
  const [body, setBody] = useState(idea.body);
  const save = async (e) => {
    e.preventDefault();
    const d = await run("PATCH", path, { title, body });
    if (d) {
      saved(d);
      onDone();
    }
  };
  return (
    <form className="form item-form" onSubmit={save} onKeyDown={submitOnModEnter}>
      <Field label="Title">
        <Input required autoFocus value={title} onChange={(e) => setTitle(e.target.value)} />
      </Field>
      <Field label="Description">
        <MentionTextarea rows={10} value={body} onChange={(e) => setBody(e.target.value)} />
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
