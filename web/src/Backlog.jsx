import { useState } from "react";
import { ArrowDown, ArrowUp, ChevronDown, ChevronRight, Pencil, Plus, Trash2 } from "lucide-react";
import Button from "./ui/Button.jsx";
import { Select } from "./ui/Input.jsx";

// The prioritized backlog, grouped into In progress (assigned), Open and Done.
// Queued items (open + assigned) keep their priority rank; done items follow.
export default function Backlog({ items, participants, me, call, onError }) {
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [showDone, setShowDone] = useState(false);
  const queued = items.filter((it) => it.status !== "done");
  const assigned = queued.filter((it) => it.status === "assigned");
  const open = queued.filter((it) => it.status === "open");
  const done = items.filter((it) => it.status === "done");
  const run = (method, suffix, payload) => call(method, suffix, payload).catch((e) => onError(e.message));
  const row = (it) => (
    <BacklogItem key={it.id} item={it} last={it.position === queued.length} {...{ participants, me, run }} />
  );

  const add = async (e) => {
    e.preventDefault();
    await run("POST", "/backlog", { title, body });
    setTitle("");
    setBody("");
  };

  return (
    <section aria-label="Backlog">
      <div className="toolbar">
        <h2>Backlog</h2>
        <span className="muted toolbar-sub">· prioritized queue, top is most important</span>
        <span className="spacer" />
        <Button variant="primary" onClick={() => run("POST", "/backlog/claim")}>
          Claim next
        </Button>
      </div>

      <GroupHead status="assigned" label="In progress" count={assigned.length} />
      {assigned.map(row)}

      <GroupHead status="open" label="Open" count={open.length} />
      {queued.length === 0 && <p className="empty-row">Nothing queued.</p>}
      {open.map(row)}
      <form className="add-row" onSubmit={add}>
        <span className="rank" />
        <Plus aria-hidden="true" className="muted" />
        <input
          required
          aria-label="New item title"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="Add an item…  Enter to save"
        />
        <input
          aria-label="Description (optional)"
          value={body}
          onChange={(e) => setBody(e.target.value)}
          placeholder="Description (optional)"
        />
        <Button type="submit">Add</Button>
      </form>

      <button type="button" className="group-head" aria-expanded={showDone} onClick={() => setShowDone(!showDone)}>
        {showDone ? <ChevronDown aria-hidden="true" /> : <ChevronRight aria-hidden="true" />}
        <span className="status done" />
        Done
        <span className="count">{done.length}</span>
      </button>
      {showDone && done.map(row)}
    </section>
  );
}

function GroupHead({ status, label, count }) {
  return (
    <h3 className="group-head">
      <span className={`status ${status}`} />
      {label}
      <span className="count">{count}</span>
    </h3>
  );
}

function name(participants, id) {
  const p = participants.find((p) => p.id === id);
  return p ? `${p.emoji} ${p.name}` : `participant ${id}`;
}

function hoursLeft(ms) {
  return Math.max(0, (ms - Date.now()) / 3600000).toFixed(1);
}

function ago(ms) {
  const minutes = Math.max(0, Math.round((Date.now() - ms) / 60000));
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.round(minutes / 60);
  return hours < 48 ? `${hours}h ago` : `${Math.round(hours / 24)}d ago`;
}

function meta(item) {
  if (item.status === "assigned") return `lease ${hoursLeft(item.lease_expires_at)}h left`;
  if (item.status === "open") return `added ${ago(item.created_at)}`;
  return item.close_note || "done";
}

function BacklogItem({ item, last, participants, me, run }) {
  const [expanded, setExpanded] = useState(false);
  const path = `/backlog/${item.id}`;
  const queued = item.status !== "done";

  const edit = () => {
    const title = window.prompt("Title", item.title);
    if (title === null) return;
    const body = window.prompt("Description", item.body);
    if (body === null) return;
    run("PATCH", path, { title, body });
  };
  const close = () => {
    const note = window.prompt("Closing note (e.g. PR URL)", "");
    if (note !== null) run("POST", `${path}/close`, { note });
  };
  const assignTo = (e) => {
    const id = Number(e.target.value);
    e.target.value = "";
    if (id) run("POST", `${path}/assign`, { assignee_id: id });
  };

  return (
    <div className={`item ${item.status}`} id={`item-${item.id}`}>
      <div className="row" onClick={() => setExpanded(!expanded)}>
        <span className="rank">{queued ? item.position : ""}</span>
        <span className={`status ${item.status}`} title={item.status} />
        <span className="item-id">#{item.id}</span>
        <button type="button" className="row-title" aria-expanded={expanded}>
          {item.title}
        </button>
        {item.assignee_id && <span className="chip">{name(participants, item.assignee_id)}</span>}
        <span className="meta">{meta(item)}</span>
        <span className="row-actions" onClick={(e) => e.stopPropagation()}>
          {queued ? (
            <>
              <Button
                iconOnly
                variant="ghost"
                aria-label="Move up"
                title="Move up"
                disabled={item.position === 1}
                onClick={() => run("POST", `${path}/move`, { position: item.position - 1 })}
              >
                <ArrowUp aria-hidden="true" />
              </Button>
              <Button
                iconOnly
                variant="ghost"
                aria-label="Move down"
                title="Move down"
                disabled={last}
                onClick={() => run("POST", `${path}/move`, { position: item.position + 1 })}
              >
                <ArrowDown aria-hidden="true" />
              </Button>
              {item.status === "open" && me && (
                <Button title="Assign to me" onClick={() => run("POST", `${path}/assign`)}>
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
              <Button onClick={close}>Close…</Button>
            </>
          ) : (
            <Button onClick={() => run("POST", `${path}/reopen`)}>Reopen</Button>
          )}
          <Button iconOnly variant="ghost" aria-label="Edit" title="Edit" onClick={edit}>
            <Pencil aria-hidden="true" />
          </Button>
          <Button
            iconOnly
            variant="ghost"
            aria-label="Delete"
            title="Delete"
            className="button-danger"
            onClick={() => window.confirm(`Delete #${item.id}?`) && run("DELETE", path)}
          >
            <Trash2 aria-hidden="true" />
          </Button>
        </span>
      </div>
      {expanded && (
        <div className="item-detail">
          <pre>{item.body || "(no description)"}</pre>
          <small>Created by {name(participants, item.created_by)}</small>
          {item.status === "done" && item.assignee_id && <small>Done by {name(participants, item.assignee_id)}</small>}
        </div>
      )}
    </div>
  );
}
