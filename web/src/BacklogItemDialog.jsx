import { useEffect, useState } from "react";
import { GitPullRequest, Pencil, Trash2, X } from "lucide-react";
import { STATUS_LABELS, ago, hoursLeft, name, submitOnModEnter } from "./backlogFormat.js";
import Markdown from "./Markdown.jsx";
import Button from "./ui/Button.jsx";
import Dialog from "./ui/Dialog.jsx";
import Field from "./ui/Field.jsx";
import { Input, Select, Textarea } from "./ui/Input.jsx";

// NewItemDialog adds a backlog item: a title and a roomy description.
// ⌘/Ctrl+Enter submits; onCreated gets the new item.
export function NewItemDialog({ run, onClose, onCreated }) {
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [busy, setBusy] = useState(false);
  const create = async (e) => {
    e.preventDefault();
    setBusy(true);
    const item = await run("POST", "/backlog", { title, body });
    setBusy(false);
    if (item) onCreated(item);
  };
  return (
    <Dialog title="New backlog item" onClose={onClose} className="item-dialog">
      <form className="form item-form" onSubmit={create} onKeyDown={submitOnModEnter}>
        <Field label="Title">
          <Input required data-autofocus value={title} onChange={(e) => setTitle(e.target.value)} placeholder="What needs doing?" />
        </Field>
        <Field label="Description">
          <Textarea rows={10} value={body} onChange={(e) => setBody(e.target.value)} placeholder="Details, acceptance criteria, links… (Markdown)" />
        </Field>
        <div className="actions-row dialog-actions">
          <span className="muted dialog-hint">⌘/Ctrl+Enter to create</span>
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={busy}>
            Create item
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// ItemDialog shows one item with everything you can do to it. It renders
// whatever the live list holds, so other people's changes show up here.
export function ItemDialog({ item, participants, me, call, run, onClose }) {
  const path = `/backlog/${item.id}`;
  const [editing, setEditing] = useState(false);
  return (
    <Dialog
      title={
        <>
          <span className={`status ${item.status}`} title={STATUS_LABELS[item.status]} />
          <span className="item-id">#{item.id}</span>
          <span className="dialog-title-text">{item.title}</span>
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
        <EditForm item={item} path={path} run={run} onDone={() => setEditing(false)} />
      ) : (
        <div className="item-description">{item.body ? <Markdown text={item.body} /> : <p className="muted">No description.</p>}</div>
      )}
      <ItemFacts item={item} participants={participants} />
      <ItemActions item={item} path={path} participants={participants} me={me} run={run} onDeleted={onClose} />
      <PRList item={item} path={path} run={run} />
      <Comments item={item} path={path} participants={participants} me={me} call={call} run={run} />
    </Dialog>
  );
}

function EditForm({ item, path, run, onDone }) {
  const [title, setTitle] = useState(item.title);
  const [body, setBody] = useState(item.body);
  const save = async (e) => {
    e.preventDefault();
    if (await run("PATCH", path, { title, body })) onDone();
  };
  return (
    <form className="form item-form" onSubmit={save} onKeyDown={submitOnModEnter}>
      <Field label="Title">
        <Input required autoFocus value={title} onChange={(e) => setTitle(e.target.value)} />
      </Field>
      <Field label="Description">
        <Textarea rows={10} value={body} onChange={(e) => setBody(e.target.value)} />
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

function ItemFacts({ item, participants }) {
  const facts = [["Status", STATUS_LABELS[item.status]]];
  if (item.assignee_id) facts.push([item.status === "done" ? "Done by" : "Assignee", name(participants, item.assignee_id)]);
  if (item.status === "assigned") facts.push(["Lease", `${hoursLeft(item.lease_expires_at)}h left`]);
  if (item.status === "done" && item.close_note) facts.push(["Note", item.close_note]);
  facts.push(["Created", `${ago(item.created_at)} by ${name(participants, item.created_by)}`]);
  return (
    <dl className="item-facts">
      {facts.map(([k, v]) => (
        <div key={k}>
          <dt>{k}</dt>
          <dd>{v}</dd>
        </div>
      ))}
    </dl>
  );
}

function ItemActions({ item, path, participants, me, run, onDeleted }) {
  const [closing, setClosing] = useState(false);
  const assignTo = (e) => {
    const id = Number(e.target.value);
    e.target.value = "";
    if (id) run("POST", `${path}/assign`, { assignee_id: id });
  };
  const remove = async () => {
    if (window.confirm(`Delete #${item.id}? This can't be undone.`) && (await run("DELETE", path))) onDeleted();
  };
  if (closing) return <CloseForm path={path} run={run} onDone={() => setClosing(false)} />;
  return (
    <div className="item-actions">
      {item.status === "open" && me && (
        <Button variant="primary" onClick={() => run("POST", `${path}/assign`)}>
          Claim
        </Button>
      )}
      {item.status === "open" && (
        <Select defaultValue="" onChange={assignTo} aria-label="Assign to">
          <option value="">Assign to…</option>
          {participants.map((p) => (
            <option key={p.id} value={p.id}>
              {p.emoji} {p.name}
            </option>
          ))}
        </Select>
      )}
      {item.status === "assigned" && <Button onClick={() => run("POST", `${path}/release`)}>Release</Button>}
      {item.status !== "done" ? (
        <Button onClick={() => setClosing(true)}>Close…</Button>
      ) : (
        <Button onClick={() => run("POST", `${path}/reopen`)}>Reopen</Button>
      )}
      <span className="spacer" />
      <Button variant="ghost" className="button-danger" onClick={remove}>
        <Trash2 aria-hidden="true" />
        Delete
      </Button>
    </div>
  );
}

function CloseForm({ path, run, onDone }) {
  const [note, setNote] = useState("");
  const close = async (e) => {
    e.preventDefault();
    if (await run("POST", `${path}/close`, { note })) onDone();
  };
  return (
    <form className="item-actions" onSubmit={close}>
      <Input
        autoFocus
        aria-label="Closing note"
        value={note}
        onChange={(e) => setNote(e.target.value)}
        placeholder="Closing note (GitHub PR URLs are linked to the item)"
      />
      <Button type="submit" variant="primary">
        Close item
      </Button>
      <Button type="button" variant="ghost" onClick={onDone}>
        Cancel
      </Button>
    </form>
  );
}

function PRList({ item, path, run }) {
  const [url, setUrl] = useState("");
  // Not `required`: after a successful submit clears the field, it would
  // show as invalid.
  const link = async (e) => {
    e.preventDefault();
    if (url.trim() && (await run("POST", `${path}/prs`, { url }))) setUrl("");
  };
  return (
    <section className="detail-section">
      <h3>Pull requests</h3>
      {item.prs.map((pr) => (
        <div key={pr.id} className="pr-line">
          <GitPullRequest aria-hidden="true" className="muted" />
          <a href={pr.url} target="_blank" rel="noopener noreferrer">
            {pr.url}
          </a>
          <Button
            iconOnly
            variant="ghost"
            aria-label={`Unlink ${pr.url}`}
            title="Unlink"
            onClick={() => run("DELETE", `${path}/prs`, { url: pr.url })}
          >
            <X aria-hidden="true" />
          </Button>
        </div>
      ))}
      <form className="detail-form" onSubmit={link}>
        <Input
          type="url"
          aria-label="Pull request URL"
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          placeholder="https://github.com/owner/repo/pull/123"
        />
        <Button type="submit">Link PR</Button>
      </form>
    </section>
  );
}

function Comments({ item, path, participants, me, call, run }) {
  const [comments, setComments] = useState(null);
  const [text, setText] = useState("");
  // Refetch when the count changes (live updates reload the item list).
  useEffect(() => {
    let current = true;
    call("GET", `${path}/comments`).then(
      (list) => current && setComments(list),
      () => current && setComments([]),
    );
    return () => {
      current = false;
    };
  }, [path, item.comment_count]);

  const send = async (e) => {
    e.preventDefault();
    if (text.trim() && (await run("POST", `${path}/comments`, { body: text }))) setText("");
  };
  return (
    <section className="detail-section">
      <h3>Comments</h3>
      {comments?.length === 0 && <p className="muted">No comments yet.</p>}
      {comments?.map((c) => (
        <div key={c.id} className="comment">
          <div className="comment-head">
            <span className="who">{name(participants, c.author_id)}</span>
            <span className="time" title={new Date(c.created_at).toLocaleString()}>
              {ago(c.created_at)}
            </span>
            {me?.id === c.author_id && (
              <Button
                iconOnly
                variant="ghost"
                className="comment-delete"
                aria-label="Delete comment"
                title="Delete comment"
                onClick={() => window.confirm("Delete this comment?") && run("DELETE", `${path}/comments/${c.id}`)}
              >
                <X aria-hidden="true" />
              </Button>
            )}
          </div>
          <div className="comment-body">
            <Markdown text={c.body} />
          </div>
        </div>
      ))}
      <form className="detail-form comment-form" onSubmit={send} onKeyDown={submitOnModEnter}>
        <Textarea
          rows={2}
          aria-label="Comment"
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder="Add a comment…  @Name to mention, ⌘/Ctrl+Enter to send"
        />
        <Button type="submit">Comment</Button>
      </form>
    </section>
  );
}
