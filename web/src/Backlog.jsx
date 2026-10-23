import { useEffect, useState } from "react";
import {
  ArrowDown,
  ArrowUp,
  ChevronDown,
  ChevronRight,
  GitPullRequest,
  GripVertical,
  MessageSquare,
  Pencil,
  Plus,
  Trash2,
  X,
} from "lucide-react";
import Linkify from "./Linkify.jsx";
import Button from "./ui/Button.jsx";
import { Input, Select } from "./ui/Input.jsx";

// Drag-and-drop reordering is for mouse-like pointers; on touch screens
// (coarse primary pointer) rows stay plain scrollable rows and the up/down
// buttons move items.
const CAN_DRAG = typeof window !== "undefined" && !window.matchMedia?.("(pointer: coarse)").matches;

// The prioritized backlog, grouped into In progress (assigned), Open and Done.
// Queued items (open + assigned) share one priority order (the position
// numbers); done items follow.
export default function Backlog({ items, participants, me, workspace, call, onError }) {
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [showDone, setShowDone] = useState(false);
  const [drag, setDrag] = useState(null); // The item being dragged.
  const [drop, setDrop] = useState(null); // { id, where: "before" | "after" }
  const queued = items.filter((it) => it.status !== "done");
  const assigned = queued.filter((it) => it.status === "assigned");
  const open = queued.filter((it) => it.status === "open");
  const done = items.filter((it) => it.status === "done");
  const run = (method, suffix, payload) => call(method, suffix, payload).catch((e) => onError(e.message));
  const moveTo = (item, position) => {
    if (position !== item.position) run("POST", `/backlog/${item.id}/move`, { position });
  };

  // Dragging reorders within a group: dropping an open item among in-progress
  // ones would need an assignment, not a move.
  const dnd = {
    drag,
    drop,
    start: (item) => setDrag(item),
    end: () => {
      setDrag(null);
      setDrop(null);
    },
    over: (item, where) => {
      if (!drag || drag.id === item.id || drag.status !== item.status) return false;
      if (drop?.id !== item.id || drop.where !== where) setDrop({ id: item.id, where });
      return true;
    },
    dropOn: (item, where) => {
      const from = drag.position;
      const to = item.position;
      // The server removes the item and re-inserts it at the new position.
      const position = where === "before" ? (from < to ? to - 1 : to) : from < to ? to : to + 1;
      dnd.end();
      moveTo(drag, position);
    },
  };

  const group = (list) =>
    list.map((it, i) => (
      <BacklogItem
        key={it.id}
        item={it}
        prev={list[i - 1]}
        next={list[i + 1]}
        {...{ participants, me, workspace, call, run, moveTo, dnd }}
      />
    ));

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
      {group(assigned)}

      <GroupHead status="open" label="Open" count={open.length} />
      {queued.length === 0 && <p className="empty-row">Nothing queued.</p>}
      {group(open)}
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
      {showDone && group(done)}
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

const GITHUB_PR = /^https?:\/\/github\.com\/([^/]+)\/([^/]+)\/pull\/(\d+)/i;
const GITHUB_REPO = /github\.com\/([^/]+)\/([^/#?]+?)(?:\.git)?\/?$/i;

// prLabel shortens a PR URL: "#4" for the workspace's own repository,
// "owner/repo#4" for another GitHub repository, else the host name.
function prLabel(url, repoUrl) {
  const pr = url.match(GITHUB_PR);
  if (!pr) {
    try {
      return new URL(url).hostname;
    } catch {
      return url;
    }
  }
  const [, owner, repo, number] = pr;
  const ws = (repoUrl || "").match(GITHUB_REPO);
  const same = ws && ws[1].toLowerCase() === owner.toLowerCase() && ws[2].toLowerCase() === repo.toLowerCase();
  return same ? `#${number}` : `${owner}/${repo}#${number}`;
}

function BacklogItem({ item, prev, next, participants, me, workspace, call, run, moveTo, dnd }) {
  const [expanded, setExpanded] = useState(false);
  const path = `/backlog/${item.id}`;
  const queued = item.status !== "done";
  const draggable = CAN_DRAG && queued;
  const repoUrl = workspace?.repo_url;

  const edit = () => {
    const title = window.prompt("Title", item.title);
    if (title === null) return;
    const body = window.prompt("Description", item.body);
    if (body === null) return;
    run("PATCH", path, { title, body });
  };
  const close = () => {
    const note = window.prompt("Closing note (GitHub PR URLs are linked to the item)", "");
    if (note !== null) run("POST", `${path}/close`, { note });
  };
  const assignTo = (e) => {
    const id = Number(e.target.value);
    e.target.value = "";
    if (id) run("POST", `${path}/assign`, { assignee_id: id });
  };
  const where = (e) => {
    const r = e.currentTarget.getBoundingClientRect();
    return e.clientY < r.top + r.height / 2 ? "before" : "after";
  };
  const dragProps = draggable
    ? {
        draggable: true,
        onDragStart: (e) => {
          e.dataTransfer.effectAllowed = "move";
          e.dataTransfer.setData("text/plain", `#${item.id}`);
          dnd.start(item);
        },
        onDragEnd: dnd.end,
      }
    : {};
  const dropProps = queued
    ? {
        onDragOver: (e) => {
          if (dnd.over(item, where(e))) {
            e.preventDefault();
            e.dataTransfer.dropEffect = "move";
          }
        },
        onDrop: (e) => {
          if (!dnd.drag) return;
          e.preventDefault();
          dnd.dropOn(item, where(e));
        },
      }
    : {};
  const classes = ["item", item.status];
  if (dnd.drag?.id === item.id) classes.push("dragging");
  if (dnd.drop?.id === item.id) classes.push(`drop-${dnd.drop.where}`);

  return (
    <div className={classes.join(" ")} id={`item-${item.id}`} {...dropProps}>
      <div className="row" onClick={() => setExpanded(!expanded)} {...dragProps}>
        <span className="rank">
          {draggable && <GripVertical className="grip" aria-hidden="true" />}
          <span className="rank-number">{queued ? item.position : ""}</span>
        </span>
        <span className={`status ${item.status}`} title={item.status} />
        <span className="item-id">#{item.id}</span>
        <button type="button" className="row-title" aria-expanded={expanded}>
          {item.title}
        </button>
        {item.prs.length > 0 && (
          <span className="pr-chips" onClick={(e) => e.stopPropagation()}>
            {item.prs.map((pr) => (
              <a key={pr.id} className="chip pr-chip" href={pr.url} target="_blank" rel="noopener noreferrer" title={pr.url}>
                <GitPullRequest aria-hidden="true" />
                {prLabel(pr.url, repoUrl)}
              </a>
            ))}
            <span className="chip pr-count" title={`${item.prs.length} linked PR${item.prs.length === 1 ? "" : "s"}`}>
              <GitPullRequest aria-hidden="true" />
              {item.prs.length}
            </span>
          </span>
        )}
        {item.comment_count > 0 && (
          <span className="comment-count" title={`${item.comment_count} comment${item.comment_count === 1 ? "" : "s"}`}>
            <MessageSquare aria-hidden="true" />
            {item.comment_count}
          </span>
        )}
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
                disabled={!prev}
                onClick={() => moveTo(item, prev.position)}
              >
                <ArrowUp aria-hidden="true" />
              </Button>
              <Button
                iconOnly
                variant="ghost"
                aria-label="Move down"
                title="Move down"
                disabled={!next}
                onClick={() => moveTo(item, next.position)}
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
      {expanded && <ItemDetail {...{ item, participants, me, call, run }} />}
    </div>
  );
}

function ItemDetail({ item, participants, me, call, run }) {
  const path = `/backlog/${item.id}`;
  return (
    <div className="item-detail">
      <pre>{item.body || "(no description)"}</pre>
      <small>Created by {name(participants, item.created_by)}</small>
      {item.status === "done" && item.assignee_id && <small>Done by {name(participants, item.assignee_id)}</small>}
      <PRList item={item} path={path} run={run} />
      <Comments item={item} path={path} participants={participants} me={me} call={call} run={run} />
    </div>
  );
}

function PRList({ item, path, run }) {
  const [url, setUrl] = useState("");
  const link = async (e) => {
    e.preventDefault();
    if (await run("POST", `${path}/prs`, { url })) setUrl("");
  };
  return (
    <div className="detail-section">
      <h4>Pull requests</h4>
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
          required
          aria-label="Pull request URL"
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          placeholder="https://github.com/owner/repo/pull/123"
        />
        <Button type="submit">Link PR</Button>
      </form>
    </div>
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
    if (await run("POST", `${path}/comments`, { body: text })) setText("");
  };
  return (
    <div className="detail-section">
      <h4>Comments</h4>
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
            <Linkify text={c.body} />
          </div>
        </div>
      ))}
      <form className="detail-form" onSubmit={send}>
        <Input
          required
          aria-label="Comment"
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder="Add a comment…  @Name to mention"
        />
        <Button type="submit">Comment</Button>
      </form>
    </div>
  );
}
