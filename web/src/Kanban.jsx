import { useState } from "react";
import { GitPullRequest, GripVertical, MessageSquare, MessagesSquare, Plus } from "lucide-react";
import { meta, name, prLabel } from "./backlogFormat.js";
import { RecurringMarker } from "./Recurring.jsx";
import Button from "./ui/Button.jsx";

// The backlog as a board (#293): Backlog (open), In progress and Done
// columns of cards. The same items and actions as the list: cards open the
// item dialog, open cards can be claimed, and queued cards reorder within
// their column (the list's useReorder, passed in as reorder). Done shows the
// last DONE_RECENT closed items unless expanded.
const DONE_RECENT = 10;

export default function Kanban({ groups, done, pending, participants, me, repos, run, reorder, hintId, created, onOpen, onAdd }) {
  const [allDone, setAllDone] = useState(false);
  const shownDone = allDone ? done : done.slice(0, DONE_RECENT);
  const cards = (status, list) =>
    reorder.arrange(status, list).map((it) => (
      <KanbanCard
        key={it.id}
        item={it}
        list={list}
        highlight={created === it.id}
        {...{ participants, me, repos, run, reorder, hintId }}
        onOpen={() => onOpen(it.id)}
      />
    ));
  return (
    <div className="kanban">
      <Column status="open" label="Backlog" count={groups.open.length} add={<AddCard onAdd={onAdd} />}>
        {cards("open", groups.open)}
        {pending.map((x) => (
          <PendingCard key={x.id} title={x.title} assigneeId={x.assignee_id} participants={participants} />
        ))}
        {groups.open.length === 0 && pending.length === 0 && <p className="kanban-empty">Nothing queued.</p>}
      </Column>
      <Column status="in_progress" label="In progress" count={groups.in_progress.length}>
        {cards("in_progress", groups.in_progress)}
        {groups.in_progress.length === 0 && <p className="kanban-empty">Nobody is working on anything.</p>}
      </Column>
      <Column status="done" label="Done" count={done.length}>
        {cards("done", shownDone)}
        {done.length > DONE_RECENT && (
          <Button variant="ghost" className="kanban-more" aria-expanded={allDone} onClick={() => setAllDone(!allDone)}>
            {allDone ? "Show recent" : `Show all ${done.length}`}
          </Button>
        )}
      </Column>
    </div>
  );
}

function Column({ status, label, count, add, children }) {
  return (
    <section className="kanban-col" aria-label={label}>
      <h3 className="kanban-col-head">
        <span className={`status ${status}`} />
        {label}
        <span className="count">{count}</span>
      </h3>
      <div className="kanban-col-body">
        {children}
        {add}
      </div>
    </section>
  );
}

// AddCard adds an item to the bottom of Backlog by title: Enter adds it and
// keeps the box open for the next one, Escape (or an empty blur) closes it.
function AddCard({ onAdd }) {
  const [open, setOpen] = useState(false);
  const [title, setTitle] = useState("");
  if (!open) {
    return (
      <Button variant="ghost" className="kanban-add" onClick={() => setOpen(true)}>
        <Plus aria-hidden="true" />
        <span className="label">Add item</span>
      </Button>
    );
  }
  const close = () => {
    setTitle("");
    setOpen(false);
  };
  return (
    <form
      className="kanban-add-form"
      onSubmit={(e) => {
        e.preventDefault();
        const t = title.trim();
        if (!t) return;
        onAdd({ title: t, body: "" });
        setTitle("");
      }}
    >
      <textarea
        className="control kanban-add-title"
        aria-label="New item title"
        placeholder="Title, then Enter"
        rows={2}
        autoFocus
        value={title}
        onChange={(e) => setTitle(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
            e.preventDefault();
            e.currentTarget.form.requestSubmit();
          } else if (e.key === "Escape") {
            e.preventDefault();
            close();
          }
        }}
        onBlur={() => !title.trim() && close()}
      />
      <span className="kanban-add-actions">
        <Button variant="primary" size="small" type="submit" disabled={!title.trim()}>
          Add
        </Button>
        <Button variant="ghost" size="small" type="button" onMouseDown={(e) => e.preventDefault()} onClick={close}>
          Cancel
        </Button>
      </span>
    </form>
  );
}

function PendingCard({ title, assigneeId, participants }) {
  return (
    <div className="kcard item-pending" aria-busy="true">
      <div className="kcard-top">
        <span className="item-id">#…</span>
        <span className="meta">adding…</span>
      </div>
      <span className="kcard-title">{title}</span>
      {assigneeId > 0 && (
        <div className="kcard-foot">
          <span className="chip">{name(participants, assigneeId)}</span>
        </div>
      )}
    </div>
  );
}

function KanbanCard({ item, list, highlight, participants, me, repos, run, reorder, hintId, onOpen }) {
  const queued = item.status !== "done";
  const canClaim = item.status === "open" && me && (!item.assignee_id || item.assignee_id === me.id);
  const classes = ["item", "kcard", item.status, ...reorder.classesFor(item)];
  if (highlight) classes.push("item-created");
  const open = (e) => {
    // Focus the title so closing the dialog returns focus to this card.
    e.currentTarget.querySelector(".kcard-title").focus();
    onOpen();
  };
  return (
    <article
      className={classes.join(" ")}
      id={`item-${item.id}`}
      data-id={item.id}
      data-status={item.status}
      onClick={open}
      onPointerDown={(e) => reorder.pointerDown(e, item, false)}
    >
      <div className="kcard-top">
        {queued && (
          <button
            type="button"
            className="kcard-grip"
            data-grip={item.id}
            aria-label={`Reorder #${item.id} ${item.title}`}
            aria-describedby={hintId}
            aria-pressed={reorder.isLifted(item)}
            onPointerDown={(e) => {
              e.stopPropagation();
              reorder.pointerDown(e, item, true);
            }}
            onClick={(e) => e.stopPropagation()}
            onKeyDown={(e) => reorder.keyDown(e, item, list)}
          >
            <GripVertical aria-hidden="true" />
          </button>
        )}
        <span className="item-id">#{item.id}</span>
        <RecurringMarker item={item} />
        <span className="meta">{meta(item)}</span>
      </div>
      <button type="button" className="row-title kcard-title" aria-haspopup="dialog">
        {item.title}
      </button>
      <div className="kcard-foot">
        <span className="kcard-people">
          <span className="kcard-reporter" title="Added by">
            {name(participants, item.created_by)}
          </span>
          {item.assignee_id && <span className="chip">{name(participants, item.assignee_id)}</span>}
        </span>
        <span className="kcard-counts">
          {item.prs.map((pr) => (
            <a key={pr.id} className="chip pr-chip" href={pr.url} target="_blank" rel="noopener noreferrer" title={pr.url} onClick={(e) => e.stopPropagation()}>
              <GitPullRequest aria-hidden="true" />
              {prLabel(pr.url, repos)}
            </a>
          ))}
          {item.comment_count > 0 && (
            <span className="comment-count" title={`${item.comment_count} comment${item.comment_count === 1 ? "" : "s"}`}>
              <MessageSquare aria-hidden="true" />
              {item.comment_count}
            </span>
          )}
          {item.message_count > 0 && (
            <span className="comment-count" title={`${item.message_count} chat message${item.message_count === 1 ? "" : "s"}`}>
              <MessagesSquare aria-hidden="true" />
              {item.message_count}
            </span>
          )}
        </span>
        {canClaim && (
          <span className="kcard-actions" onClick={(e) => e.stopPropagation()}>
            <Button size="small" title={item.assignee_id ? "Start working on this item" : "Assign to me and start working"} onClick={() => run("POST", `/backlog/${item.id}/start`)}>
              Claim
            </Button>
          </span>
        )}
      </div>
    </article>
  );
}
