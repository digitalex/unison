import { useCallback, useMemo, useRef, useState } from "react";
import { Check, Clock, GitPullRequest, Plus, RotateCcw, Trash2 } from "lucide-react";
import { ago, name as participantLabel, prLabel, submitOnModEnter } from "./backlogFormat.js";
import { MemoMarkdown } from "./Markdown.jsx";
import { MentionTextarea } from "./mentions.jsx";
import { itemLink } from "./nav.js";
import { SectionIcon } from "./settings/Sections.jsx";
import { useKeyShortcut } from "./shortcuts.jsx";
import Button from "./ui/Button.jsx";
import Checkbox from "./ui/Checkbox.jsx";
import Dialog from "./ui/Dialog.jsx";
import Field from "./ui/Field.jsx";
import { Input, Select } from "./ui/Input.jsx";
import ScreenHead from "./ui/ScreenHead.jsx";

const EMPTY_REPOS = [];

const STATUS_TABS = [
  { id: "active", label: "Active" },
  { id: "snoozed", label: "Snoozed" },
  { id: "open", label: "All open" },
  { id: "done", label: "Done" },
];

export function isTodoSnoozed(td, now) {
  return td.status === "open" && Boolean(td.snoozed_until && td.snoozed_until > now);
}

export function isTodoActive(td, now) {
  return td.status === "open" && (!td.snoozed_until || td.snoozed_until <= now);
}

export function withTodo(list, td) {
  const rest = list.filter((x) => x.id !== td.id);
  return [td, ...rest].sort((a, b) => {
    const aDone = a.status === "done" ? 1 : 0;
    const bDone = b.status === "done" ? 1 : 0;
    if (aDone !== bDone) return aDone - bDone;
    const aTime = a.done_at || a.created_at || 0;
    const bTime = b.done_at || b.created_at || 0;
    return bTime - aTime || b.id - a.id;
  });
}

function formatSnoozeUntil(ms) {
  return new Date(ms).toLocaleString([], { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" });
}

function personDisplay(emoji, pname, id, participants) {
  if (pname) return emoji ? `${emoji} ${pname}` : pname;
  return participantLabel(participants, id);
}

export default function Todos({ todos, setTodos, items, sections = [], participants, me, workspace, now, call, onReloadMe, onError, head, error }) {
  const isAgent = Boolean(me?.sponsor);
  const [filter, setFilter] = useState("active");
  const [scope, setScope] = useState(() => (isAgent ? "all" : "mine"));
  const [creating, setCreating] = useState(false);
  const [completingTodo, setCompletingTodo] = useState(null);

  useKeyShortcut(
    (e) => !e.shiftKey && (e.key === "c" || e.key === "C"),
    () => setCreating(true),
  );

  const onErrorRef = useRef(onError);
  onErrorRef.current = onError;
  const run = useCallback(
    (method, suffix, payload) => call(method, suffix, payload).catch((e) => onErrorRef.current(e.message)),
    [call],
  );

  const repos = useMemo(() => workspace?.repos || EMPTY_REPOS, [workspace?.repos]);
  const humans = useMemo(() => participants.filter((p) => !p.sponsor && !p.revoked_at), [participants]);

  const combinedTodos = useMemo(() => {
    const notifyMap = new Map(sections.filter((s) => s.notify).map((s) => [s.id, s]));
    if (notifyMap.size === 0) return todos;
    const humanIds = new Set(humans.map((p) => p.id));
    const notifyEntries = [];
    for (const it of items) {
      const sec = it.section_id && notifyMap.get(it.section_id);
      if (!sec || !it.assignee_id || !humanIds.has(it.assignee_id)) continue;
      notifyEntries.push({
        kind: "section_item",
        id: it.id,
        item: it,
        section: sec,
        assignee_id: it.assignee_id,
        status: it.status === "done" ? "done" : "open",
        created_at: it.created_at,
        done_at: it.closed_at,
      });
    }
    if (notifyEntries.length === 0) return todos;
    return [...todos, ...notifyEntries].sort((a, b) => {
      const aDone = a.status === "done" ? 1 : 0;
      const bDone = b.status === "done" ? 1 : 0;
      if (aDone !== bDone) return aDone - bDone;
      const aTime = a.done_at || a.created_at || 0;
      const bTime = b.done_at || b.created_at || 0;
      return bTime - aTime || b.id - a.id;
    });
  }, [todos, items, sections, humans]);

  const scopedTodos = useMemo(() => {
    if (scope === "mine" && me) return combinedTodos.filter((t) => t.assignee_id === me.id);
    return combinedTodos;
  }, [combinedTodos, scope, me]);

  const tabCounts = useMemo(
    () => ({
      active: scopedTodos.filter((t) => (t.kind === "section_item" ? t.status === "open" : isTodoActive(t, now))).length,
      snoozed: scopedTodos.filter((t) => t.kind !== "section_item" && isTodoSnoozed(t, now)).length,
      open: scopedTodos.filter((t) => t.status === "open").length,
      done: scopedTodos.filter((t) => t.status === "done").length,
    }),
    [scopedTodos, now],
  );

  const visibleTodos = useMemo(() => {
    switch (filter) {
      case "snoozed":
        return scopedTodos.filter((t) => t.kind !== "section_item" && isTodoSnoozed(t, now));
      case "open":
        return scopedTodos.filter((t) => t.status === "open");
      case "done":
        return scopedTodos.filter((t) => t.status === "done");
      default:
        return scopedTodos.filter((t) => (t.kind === "section_item" ? t.status === "open" : isTodoActive(t, now)));
    }
  }, [scopedTodos, filter, now]);

  const handleCreate = async (payload) => {
    const td = await run("POST", "/todos", payload);
    if (!td) return false;
    setTodos((list) => withTodo(list, td));
    onReloadMe?.();
    return true;
  };

  const handleToggleDone = async (td) => {
    if (td.status === "done") {
      const reopened = await run("POST", `/todos/${td.id}/reopen`);
      if (reopened) {
        setTodos((list) => withTodo(list, reopened));
        onReloadMe?.();
      }
      return;
    }
    if (isAgent) {
      setCompletingTodo(td);
      return;
    }
    const done = await run("POST", `/todos/${td.id}/done`, { note: "" });
    if (done) {
      setTodos((list) => withTodo(list, done));
      onReloadMe?.();
    }
  };

  const handleAgentDone = async (td, note) => {
    const done = await run("POST", `/todos/${td.id}/done`, { note });
    if (!done) return false;
    setTodos((list) => withTodo(list, done));
    onReloadMe?.();
    setCompletingTodo(null);
    return true;
  };

  const handleSnooze = async (td, duration) => {
    const updated = await run("POST", `/todos/${td.id}/snooze`, { duration });
    if (updated) {
      setTodos((list) => withTodo(list, updated));
      onReloadMe?.();
    }
  };

  const handleDelete = async (td) => {
    if (!window.confirm(`Delete TODO #${td.id} "${td.title}"?`)) return;
    const res = await run("DELETE", `/todos/${td.id}`);
    if (res !== undefined) {
      setTodos((list) => list.filter((x) => x.id !== td.id));
      onReloadMe?.();
    }
  };

  const tools = (
    <div className="todo-toolbar">
      <div className="seg" role="group" aria-label="TODO status">
        {STATUS_TABS.map(({ id, label }) => (
          <button key={id} type="button" aria-pressed={filter === id} onClick={() => setFilter(id)}>
            {label}
            {tabCounts[id] > 0 && <span className="todo-tab-count"> {tabCounts[id]}</span>}
          </button>
        ))}
      </div>
      <div className="seg" role="group" aria-label="TODO assignee filter">
        <button type="button" aria-pressed={scope === "mine"} onClick={() => setScope("mine")}>
          Assigned to me
        </button>
        <button type="button" aria-pressed={scope === "all"} onClick={() => setScope("all")}>
          Everyone
        </button>
      </div>
    </div>
  );

  return (
    <section className="screen" aria-label="TODOs">
      <ScreenHead title="TODOs" tools={tools}>
        {head}
        <Button
          variant="primary"
          size="small"
          onClick={() => setCreating(true)}
          disabled={humans.length === 0}
          aria-haspopup="dialog"
          aria-label="New TODO"
        >
          <Plus aria-hidden="true" />
          <span className="label">New TODO</span>
        </Button>
      </ScreenHead>
      {error}
      <div className="screen-body">
        {visibleTodos.length === 0 ? (
          <p className="recurring-empty">
            {filter === "active"
              ? scope === "mine"
                ? "No active TODOs assigned to you."
                : "No active TODOs."
              : filter === "snoozed"
                ? "No snoozed TODOs."
                : filter === "done"
                  ? "No completed TODOs yet."
                  : "No open TODOs."}
          </p>
        ) : (
          <div className="todo-list">
            {visibleTodos.map((td) =>
              td.kind === "section_item" ? (
                <NotifyItemRow
                  key={`sec-item-${td.id}`}
                  entry={td}
                  participants={participants}
                  repos={repos}
                  showAssignee={scope === "all"}
                  run={run}
                  onReloadMe={onReloadMe}
                />
              ) : (
                <TodoRow
                  key={`todo-${td.id}`}
                  todo={td}
                  items={items}
                  participants={participants}
                  repos={repos}
                  now={now}
                  showAssignee={scope === "all"}
                  onToggleDone={handleToggleDone}
                  onSnooze={handleSnooze}
                  onDelete={handleDelete}
                />
              ),
            )}
          </div>
        )}
      </div>

      {creating && (
        <NewTodoDialog
          humans={humans}
          items={items}
          me={me}
          onClose={() => setCreating(false)}
          onCreate={handleCreate}
        />
      )}

      {completingTodo && (
        <CompleteTodoDialog
          todo={completingTodo}
          isAgent={isAgent}
          onClose={() => setCompletingTodo(null)}
          onComplete={handleAgentDone}
        />
      )}
    </section>
  );
}

function NotifyItemRow({ entry, participants, repos, showAssignee, run, onReloadMe }) {
  const { item, section } = entry;
  const [busyChoice, setBusyChoice] = useState(null);
  const isDone = item.status === "done";
  const assignee = participantLabel(participants, item.assignee_id);
  const creator = participantLabel(participants, item.created_by);

  const toggleClose = async () => {
    const updated = isDone
      ? await run("POST", `/backlog/${item.id}/reopen`)
      : await run("POST", `/backlog/${item.id}/close`, { note: "" });
    if (updated) onReloadMe?.();
  };

  const answerChoice = async (choice) => {
    setBusyChoice(choice);
    const updated = await run("POST", `/backlog/${item.id}/answer`, { choice });
    setBusyChoice(null);
    if (updated) onReloadMe?.();
  };

  return (
    <div className={isDone ? "todo-item todo-done" : "todo-item"}>
      <div className="todo-main">
        <div className="todo-check">
          <Checkbox
            checked={isDone}
            onChange={toggleClose}
            aria-label={isDone ? `Reopen item #${item.id}: ${item.title}` : `Close item #${item.id}: ${item.title}`}
          />
        </div>
        <div className="todo-content">
          <div className="todo-head-row">
            <span className="item-id">#{item.id}</span>
            <a className="todo-title todo-item-link" {...itemLink(item.id)}>
              {item.title}
            </a>
            <span className="chip section-chip">
              <SectionIcon icon={section.icon} aria-hidden="true" />
              {section.name}
            </span>
            {showAssignee && <span className="chip">{assignee}</span>}
            {item.prs?.map((pr) => (
              <a key={pr.id} className="chip todo-link-chip" href={pr.url} target="_blank" rel="noopener noreferrer" title={pr.url}>
                <GitPullRequest aria-hidden="true" />
                {prLabel(pr.url, repos)}
              </a>
            ))}
          </div>
          {item.body && (
            <div className="todo-body">
              <MemoMarkdown text={item.body} />
            </div>
          )}
          {item.choices?.length > 0 && (
            <div className="todo-choices-bar" role="group" aria-label={`Decision choices for #${item.id}`}>
              {item.choices.map((choice) => {
                const selected = item.answered_choice === choice;
                return (
                  <button
                    key={choice}
                    type="button"
                    className={selected ? "decision-choice decision-choice-inline selected" : "decision-choice decision-choice-inline"}
                    aria-pressed={selected}
                    disabled={busyChoice !== null || isDone}
                    onClick={() => answerChoice(choice)}
                  >
                    {selected && <Check aria-hidden="true" />}
                    <span>{choice}</span>
                  </button>
                );
              })}
            </div>
          )}
          <div className="todo-sub">
            {item.answered_choice && (
              <span className="todo-done-note">
                Decision: <b>{item.answered_choice}</b>
                {item.answered_by_id ? ` by ${participantLabel(participants, item.answered_by_id)}` : ""}
              </span>
            )}
            {isDone ? (
              <span>Closed {item.closed_at ? ago(item.closed_at) : ""}</span>
            ) : (
              <span>
                Added {ago(item.created_at)} by {creator}
              </span>
            )}
          </div>
        </div>
        <div className="todo-actions">
          <a className="button button-ghost" {...itemLink(item.id)}>
            Open
          </a>
          <Button size="small" variant="ghost" onClick={toggleClose}>
            {isDone ? "Reopen" : "Close"}
          </Button>
        </div>
      </div>
    </div>
  );
}

function TodoRow({ todo, items, participants, repos, now, showAssignee, onToggleDone, onSnooze, onDelete }) {
  const isDone = todo.status === "done";
  const snoozed = isTodoSnoozed(todo, now);
  const linkedItem = todo.item_id ? items.find((it) => it.id === todo.item_id) || todo.item : null;
  const assignee = personDisplay(todo.assignee_emoji, todo.assignee_name, todo.assignee_id, participants);
  const creator = todo.creator_id ? personDisplay(todo.creator_emoji, todo.creator_name, todo.creator_id, participants) : "";
  const doneBy = todo.done_by_id ? personDisplay(todo.done_by_emoji, todo.done_by_name, todo.done_by_id, participants) : "";

  return (
    <div className={isDone ? "todo-item todo-done" : snoozed ? "todo-item todo-snoozed" : "todo-item"}>
      <div className="todo-main">
        <div className="todo-check">
          <Checkbox
            checked={isDone}
            onChange={() => onToggleDone(todo)}
            aria-label={isDone ? `Reopen TODO #${todo.id}: ${todo.title}` : `Mark TODO #${todo.id} done: ${todo.title}`}
          />
        </div>
        <div className="todo-content">
          <div className="todo-head-row">
            <span className="item-id">#{todo.id}</span>
            <span className="todo-title">{todo.title}</span>
            {showAssignee && <span className="chip">{assignee}</span>}
            {todo.item_id && (
              <a className="chip todo-link-chip" {...itemLink(todo.item_id)} title={linkedItem?.title ? `#${todo.item_id}: ${linkedItem.title}` : `Item #${todo.item_id}`}>
                #{todo.item_id}
                {linkedItem?.title ? ` · ${linkedItem.title}` : ""}
              </a>
            )}
            {todo.pr_url && (
              <a className="chip todo-link-chip" href={todo.pr_url} target="_blank" rel="noopener noreferrer" title={todo.pr_url}>
                <GitPullRequest aria-hidden="true" />
                {prLabel(todo.pr_url, repos)}
              </a>
            )}
            {snoozed && (
              <span className="chip todo-snooze-chip" title={new Date(todo.snoozed_until).toLocaleString()}>
                <Clock aria-hidden="true" />
                Until {formatSnoozeUntil(todo.snoozed_until)}
              </span>
            )}
          </div>
          {todo.body && (
            <div className="todo-body">
              <MemoMarkdown text={todo.body} />
            </div>
          )}
          <div className="todo-sub">
            {isDone ? (
              <>
                <span>
                  Done {todo.done_at ? ago(todo.done_at) : ""}
                  {doneBy ? ` by ${doneBy}` : ""}
                </span>
                {todo.done_note && <span className="todo-done-note">— {todo.done_note}</span>}
              </>
            ) : (
              <span>
                Added {ago(todo.created_at)}
                {creator ? ` by ${creator}` : ""}
              </span>
            )}
          </div>
        </div>
        <div className="todo-actions">
          {!isDone ? (
            <>
              {snoozed && (
                <Button size="small" variant="ghost" onClick={() => onSnooze(todo, "none")} title="Wake this TODO now">
                  Unsnooze
                </Button>
              )}
              <Button size="small" variant="ghost" onClick={() => onSnooze(todo, "1d")} title="Hide from active list for 24 hours">
                Snooze 1d
              </Button>
              <Button size="small" variant="ghost" onClick={() => onSnooze(todo, "1w")} title="Hide from active list for 7 days">
                Snooze 1w
              </Button>
            </>
          ) : (
            <Button size="small" variant="ghost" onClick={() => onToggleDone(todo)} title="Reopen TODO">
              <RotateCcw aria-hidden="true" />
              Reopen
            </Button>
          )}
          <Button
            iconOnly
            variant="ghost"
            className="button-danger"
            aria-label={`Delete TODO #${todo.id}`}
            title="Delete TODO"
            onClick={() => onDelete(todo)}
          >
            <Trash2 aria-hidden="true" />
          </Button>
        </div>
      </div>
    </div>
  );
}

export function NewTodoDialog({ initial, humans = [], items = [], me, onClose, onCreate }) {
  const defaultAssignee = useMemo(() => {
    if (initial?.assignee_id) return initial.assignee_id;
    if (me && !me.sponsor && humans.some((p) => p.id === me.id)) return me.id;
    return humans[0]?.id || 0;
  }, [initial?.assignee_id, me, humans]);

  const [title, setTitle] = useState(initial?.title || "");
  const [assigneeId, setAssigneeId] = useState(defaultAssignee);
  const [body, setBody] = useState(initial?.body || "");
  const [itemId, setItemId] = useState(initial?.item_id || 0);
  const [prUrl, setPrUrl] = useState(initial?.pr_url || "");
  const [busy, setBusy] = useState(false);

  const selectableItems = useMemo(() => {
    const queued = items.filter((it) => it.status !== "done");
    if (initial?.item_id && !queued.some((it) => it.id === initial.item_id)) {
      const extra = items.find((it) => it.id === initial.item_id);
      if (extra) return [extra, ...queued];
    }
    return queued;
  }, [items, initial?.item_id]);

  const submit = async (e) => {
    e.preventDefault();
    if (!title.trim() || !assigneeId) return;
    setBusy(true);
    const payload = { title, assignee_id: Number(assigneeId) };
    if (body.trim()) payload.body = body;
    if (itemId > 0) payload.item_id = Number(itemId);
    if (prUrl.trim()) payload.pr_url = prUrl.trim();
    const ok = await onCreate(payload);
    setBusy(false);
    if (ok) onClose();
  };

  return (
    <Dialog title="New TODO" onClose={onClose} className="item-dialog">
      <form className="form item-form" onSubmit={submit} onKeyDown={submitOnModEnter}>
        <Field label="Title">
          <Input required data-autofocus value={title} onChange={(e) => setTitle(e.target.value)} placeholder="What needs a human's action?" />
        </Field>
        <Field label="Assignee">
          <Select required value={assigneeId} onChange={(e) => setAssigneeId(Number(e.target.value))}>
            {humans.map((p) => (
              <option key={p.id} value={p.id}>
                {p.emoji} {p.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Details (optional)">
          <MentionTextarea rows={4} value={body} onChange={(e) => setBody(e.target.value)} placeholder="Context, instructions, or links… (Markdown)" />
        </Field>
        <Field label="Linked backlog item (optional)">
          <Select value={itemId} onChange={(e) => setItemId(Number(e.target.value))}>
            <option value={0}>None</option>
            {selectableItems.map((it) => (
              <option key={it.id} value={it.id}>
                #{it.id} — {it.title}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Linked GitHub PR URL (optional)">
          <Input
            type="url"
            value={prUrl}
            onChange={(e) => setPrUrl(e.target.value)}
            placeholder="https://github.com/owner/repo/pull/123"
          />
        </Field>
        <p className="muted field-description">
          PR-linked TODOs close automatically when the pull request is merged or closed.
        </p>
        <div className="actions-row dialog-actions">
          <span className="muted dialog-hint">⌘/Ctrl+Enter to create</span>
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={busy || !assigneeId}>
            Create TODO
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function CompleteTodoDialog({ todo, isAgent, onClose, onComplete }) {
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);

  const submit = async (e) => {
    e.preventDefault();
    if (isAgent && !note.trim()) return;
    setBusy(true);
    await onComplete(todo, note);
    setBusy(false);
  };

  return (
    <Dialog title={`Complete TODO #${todo.id}`} onClose={onClose}>
      <form className="form item-form" onSubmit={submit} onKeyDown={submitOnModEnter}>
        <p className="muted">{todo.title}</p>
        <Field label={isAgent ? "Completion note (required for agents)" : "Completion note (optional)"}>
          <Input
            required={isAgent}
            data-autofocus
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder="What was done or verified?"
          />
        </Field>
        <div className="actions-row dialog-actions">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={busy || (isAgent && !note.trim())}>
            Mark done
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
