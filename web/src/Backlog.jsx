import { useState } from "react";
import Button from "./ui/Button.jsx";
import { Input, Select } from "./ui/Input.jsx";

// The prioritized backlog. Queued items (open + assigned) come first in
// priority order; done items follow.
export default function Backlog({ items, participants, me, call, onError }) {
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [showDone, setShowDone] = useState(false);
  const queued = items.filter((it) => it.status !== "done");
  const done = items.filter((it) => it.status === "done");
  const run = (method, suffix, payload) => call(method, suffix, payload).catch((e) => onError(e.message));

  const add = async (e) => {
    e.preventDefault();
    await run("POST", "/backlog", { title, body });
    setTitle("");
    setBody("");
  };

  return (
    <section className="section">
      <h2>Backlog</h2>
      <form className="backlog-add" onSubmit={add}>
        <Input required value={title} onChange={(e) => setTitle(e.target.value)} placeholder="New item title" />
        <Input value={body} onChange={(e) => setBody(e.target.value)} placeholder="Description (optional)" />
        <Button variant="primary" type="submit">
          Add
        </Button>
      </form>
      {queued.length === 0 ? (
        <p>Nothing queued.</p>
      ) : (
        <ol className="backlog-list">
          {queued.map((it) => (
            <BacklogItem key={it.id} item={it} last={it.position === queued.length} {...{ participants, me, run }} />
          ))}
        </ol>
      )}
      <div className="row">
        <Button onClick={() => setShowDone(!showDone)}>
          {showDone ? "Hide" : "Show"} done ({done.length})
        </Button>
      </div>
      {showDone && (
        <ul className="backlog-list list-plain">
          {done.map((it) => (
            <BacklogItem key={it.id} item={it} {...{ participants, me, run }} />
          ))}
        </ul>
      )}
    </section>
  );
}

function name(participants, id) {
  const p = participants.find((p) => p.id === id);
  return p ? `${p.emoji} ${p.name}` : `participant ${id}`;
}

function hoursLeft(ms) {
  return Math.max(0, (ms - Date.now()) / 3600000).toFixed(1);
}

function BacklogItem({ item, last, participants, me, run }) {
  const [expanded, setExpanded] = useState(false);
  const path = `/backlog/${item.id}`;

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
    <li>
      <div className="row">
        <span className="backlog-title" onClick={() => setExpanded(!expanded)}>
          #{item.id} <b>{item.title}</b>
        </span>
        {item.status === "open" && <span className="meta">open</span>}
        {item.status === "assigned" && (
          <span className="meta">
            assigned → {name(participants, item.assignee_id)} (lease {hoursLeft(item.lease_expires_at)}h left)
          </span>
        )}
        {item.status === "done" && (
          <span className="meta">
            done{item.assignee_id && <> by {name(participants, item.assignee_id)}</>}
            {item.close_note && <>: {item.close_note}</>}
          </span>
        )}
        {item.status !== "done" ? (
          <>
            <Button
              variant="outline"
              disabled={item.position === 1}
              onClick={() => run("POST", `${path}/move`, { position: item.position - 1 })}
            >
              ↑
            </Button>
            <Button variant="outline" disabled={last} onClick={() => run("POST", `${path}/move`, { position: item.position + 1 })}>
              ↓
            </Button>
            {item.status === "open" && me && (
              <Button variant="outline" onClick={() => run("POST", `${path}/assign`)}>
                Assign to me
              </Button>
            )}
            {item.status === "open" && (
              <Select defaultValue="" onChange={assignTo}>
                <option value="">Assign to…</option>
                {participants.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.emoji} {p.name}
                  </option>
                ))}
              </Select>
            )}
            {item.status === "assigned" && (
              <Button variant="outline" onClick={() => run("POST", `${path}/release`)}>
                Release
              </Button>
            )}
            <Button variant="outline" onClick={close}>
              Close
            </Button>
          </>
        ) : (
          <Button variant="outline" onClick={() => run("POST", `${path}/reopen`)}>
            Reopen
          </Button>
        )}
        <Button variant="outline" onClick={edit}>
          Edit
        </Button>
        <Button variant="danger" onClick={() => window.confirm(`Delete #${item.id}?`) && run("DELETE", path)}>
          Delete
        </Button>
      </div>
      {expanded && (
        <div className="backlog-detail">
          <pre>{item.body || "(no description)"}</pre>
          <small>Created by {name(participants, item.created_by)}</small>
        </div>
      )}
    </li>
  );
}
