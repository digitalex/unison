import { useState } from "react";

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
    <section>
      <h2>Backlog</h2>
      <form onSubmit={add}>
        <input required value={title} onChange={(e) => setTitle(e.target.value)} placeholder="New item title" size={40} />{" "}
        <input value={body} onChange={(e) => setBody(e.target.value)} placeholder="Description (optional)" size={40} />{" "}
        <button type="submit">Add</button>
      </form>
      {queued.length === 0 ? (
        <p>Nothing queued.</p>
      ) : (
        <ol>
          {queued.map((it) => (
            <BacklogItem key={it.id} item={it} last={it.position === queued.length} {...{ participants, me, run }} />
          ))}
        </ol>
      )}
      <button onClick={() => setShowDone(!showDone)}>
        {showDone ? "Hide" : "Show"} done ({done.length})
      </button>
      {showDone && (
        <ul>
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
      <span onClick={() => setExpanded(!expanded)} style={{ cursor: "pointer" }}>
        #{item.id} <b>{item.title}</b>
      </span>{" "}
      {item.status === "open" && <i>open</i>}
      {item.status === "assigned" && (
        <i>
          assigned → {name(participants, item.assignee_id)} (lease {hoursLeft(item.lease_expires_at)}h left)
        </i>
      )}
      {item.status === "done" && (
        <i>
          done{item.assignee_id && <> by {name(participants, item.assignee_id)}</>}
          {item.close_note && <>: {item.close_note}</>}
        </i>
      )}{" "}
      {item.status !== "done" ? (
        <>
          <button disabled={item.position === 1} onClick={() => run("POST", `${path}/move`, { position: item.position - 1 })}>
            ↑
          </button>
          <button disabled={last} onClick={() => run("POST", `${path}/move`, { position: item.position + 1 })}>
            ↓
          </button>{" "}
          {item.status === "open" && me && <button onClick={() => run("POST", `${path}/assign`)}>Assign to me</button>}
          {item.status === "open" && (
            <select defaultValue="" onChange={assignTo}>
              <option value="">Assign to…</option>
              {participants.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.emoji} {p.name}
                </option>
              ))}
            </select>
          )}
          {item.status === "assigned" && <button onClick={() => run("POST", `${path}/release`)}>Release</button>}{" "}
          <button onClick={close}>Close</button>
        </>
      ) : (
        <button onClick={() => run("POST", `${path}/reopen`)}>Reopen</button>
      )}{" "}
      <button onClick={edit}>Edit</button>{" "}
      <button onClick={() => window.confirm(`Delete #${item.id}?`) && run("DELETE", path)}>Delete</button>
      {expanded && (
        <div>
          <pre style={{ whiteSpace: "pre-wrap" }}>{item.body || "(no description)"}</pre>
          <small>Created by {name(participants, item.created_by)}</small>
        </div>
      )}
    </li>
  );
}
