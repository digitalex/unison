import { useEffect, useId, useLayoutEffect, useRef, useState } from "react";
import { ChevronDown, ChevronRight, GitPullRequest, GripVertical, MessageSquare, Plus } from "lucide-react";
import { ItemDialog, NewItemDialog } from "./BacklogItemDialog.jsx";
import { meta, name, prLabel } from "./backlogFormat.js";
import { OPEN_ITEM } from "./nav.js";
import RecurringSection, { RecurringMarker } from "./Recurring.jsx";
import Button from "./ui/Button.jsx";
import ScreenHead from "./ui/ScreenHead.jsx";

// The prioritized backlog, grouped into In progress (assigned), Open, the
// recurring tasks that add items to it (#113), and Done.
// Queued items (open + assigned) share one priority order (the position
// numbers); done items follow. Rows are compact: clicking one opens the item
// dialog (also deep-linkable as ?item=12), and dragging reorders. It fills
// the workspace's main pane: head is the header's shared controls (who's
// here, Invite), error the workspace's error bar.
export default function Backlog({ items, schedules, participants, me, workspace, call, onError, head, error }) {
  const [showDone, setShowDone] = useState(false);
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState(null); // The item just added: scrolled to and highlighted.
  const [notice, setNotice] = useState("");
  const [openId, openItem, closeItem] = useItemParam();
  const run = (method, suffix, payload) => call(method, suffix, payload).catch((e) => onError(e.message));
  const moveTo = (item, position) => {
    if (position !== item.position) run("POST", `/backlog/${item.id}/move`, { position });
  };
  const queued = items.filter((it) => it.status !== "done");
  const groups = { assigned: queued.filter((it) => it.status === "assigned"), open: queued.filter((it) => it.status === "open") };
  // Most recently closed first (items closed live are added at the end).
  const done = items.filter((it) => it.status === "done").sort((a, b) => (b.closed_at || 0) - (a.closed_at || 0));
  const reorder = useReorder(groups, moveTo);
  const openedItem = useOpenItem(openId, items, closeItem, setNotice);
  useDismiss(notice, setNotice);
  useRevealCreated(created, items, setCreated);
  const hintId = useId();

  const rows = (status, list) =>
    reorder.arrange(status, list).map((it) => (
      <BacklogRow
        key={it.id}
        item={it}
        list={list}
        highlight={created === it.id}
        {...{ participants, me, run, reorder, hintId }}
        repos={workspace?.repos || []}
        onOpen={() => openItem(it.id)}
      />
    ));
  const onCreated = (item) => {
    setCreating(false);
    setCreated(item.id);
  };
  // Run now (#116): a new item is revealed like one just added; if the last
  // run is still open, the run was noted on it instead.
  const onRan = ({ item, added }) => {
    if (added) setCreated(item.id);
    else setNotice(`#${item.id} from this recurring task is still open, so the run was noted on it.`);
  };

  return (
    <section className="screen" aria-label="Backlog">
      <ScreenHead title="Backlog">
        {head}
        <Button variant="primary" size="small" onClick={() => setCreating(true)} aria-haspopup="dialog">
          <Plus aria-hidden="true" />
          <span className="label">New item</span>
        </Button>
      </ScreenHead>
      {error}
      <div className="screen-body">
        {notice && (
          <p role="status" className="error-bar notice-bar">
            {notice}{" "}
            <Button variant="ghost" onClick={() => setNotice("")}>
              dismiss
            </Button>
          </p>
        )}
        <p id={hintId} className="sr-only">
          Reorder: press Space or Enter to pick the item up, the arrow keys to move it, Space or Enter to drop it, Escape to cancel.
        </p>
        <p className="sr-only" role="status" aria-live="assertive">
          {reorder.announcement}
        </p>

        <GroupHead status="assigned" label="In progress" count={groups.assigned.length} />
        {rows("assigned", groups.assigned)}

        <GroupHead status="open" label="Open" count={groups.open.length} />
        {queued.length === 0 && <p className="empty-row">Nothing queued.</p>}
        {rows("open", groups.open)}

        <RecurringSection schedules={schedules} run={run} onRan={onRan} />

        <button type="button" className="group-head" aria-expanded={showDone} onClick={() => setShowDone(!showDone)}>
          {showDone ? <ChevronDown aria-hidden="true" /> : <ChevronRight aria-hidden="true" />}
          <span className="status done" />
          Done
          <span className="count">{done.length}</span>
        </button>
        {showDone && rows("done", done)}
      </div>

      {creating && <NewItemDialog run={run} onClose={() => setCreating(false)} onCreated={onCreated} />}
      {openedItem && <ItemDialog item={openedItem} {...{ participants, me, call, run }} onClose={closeItem} />}
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

function BacklogRow({ item, list, highlight, participants, me, repos, run, reorder, hintId, onOpen }) {
  const queued = item.status !== "done";
  const classes = ["item", item.status, ...reorder.classesFor(item)];
  if (highlight) classes.push("item-created");
  const open = (e) => {
    // Focus the title so closing the dialog returns focus to this row.
    e.currentTarget.querySelector(".row-title").focus();
    onOpen();
  };
  return (
    <div className={classes.join(" ")} id={`item-${item.id}`} data-id={item.id} data-status={item.status}>
      <div className="row" onClick={open} onPointerDown={(e) => reorder.pointerDown(e, item, false)}>
        <span className="rank">
          {queued && (
            <button
              type="button"
              className="grip"
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
          <span className="rank-number">{queued ? item.position : ""}</span>
        </span>
        <span className={`status ${item.status}`} title={item.status} />
        <span className="item-id">#{item.id}</span>
        <RecurringMarker item={item} />
        <button type="button" className="row-title" aria-haspopup="dialog">
          {item.title}
        </button>
        <PRChips prs={item.prs} repos={repos} />
        {item.comment_count > 0 && (
          <span className="comment-count" title={`${item.comment_count} comment${item.comment_count === 1 ? "" : "s"}`}>
            <MessageSquare aria-hidden="true" />
            {item.comment_count}
          </span>
        )}
        {item.assignee_id && <span className="chip">{name(participants, item.assignee_id)}</span>}
        <span className="meta">{meta(item)}</span>
        {item.status === "open" && me && (
          <span className="row-actions" onClick={(e) => e.stopPropagation()}>
            <Button title="Assign to me" onClick={() => run("POST", `/backlog/${item.id}/assign`)}>
              Claim
            </Button>
          </span>
        )}
      </div>
    </div>
  );
}

function PRChips({ prs, repos }) {
  if (prs.length === 0) return null;
  return (
    <span className="pr-chips" onClick={(e) => e.stopPropagation()}>
      {prs.map((pr) => (
        <a key={pr.id} className="chip pr-chip" href={pr.url} target="_blank" rel="noopener noreferrer" title={pr.url}>
          <GitPullRequest aria-hidden="true" />
          {prLabel(pr.url, repos)}
        </a>
      ))}
      <span className="chip pr-count" title={`${prs.length} linked PR${prs.length === 1 ? "" : "s"}`}>
        <GitPullRequest aria-hidden="true" />
        {prs.length}
      </span>
    </span>
  );
}

// ---- The open item: ?item=12 in the URL ----

function itemFromURL() {
  const v = new URLSearchParams(window.location.search).get("item");
  return v && /^\d+$/.test(v) ? Number(v) : null;
}

function urlWithItem(id) {
  const u = new URL(window.location.href);
  if (id) u.searchParams.set("item", id);
  else u.searchParams.delete("item");
  return u.pathname + u.search + u.hash;
}

// useItemParam keeps the open item in the URL: opening pushes ?item=12,
// closing goes back (so the browser's back button closes the dialog too).
// Links elsewhere on the board (itemLink, e.g. a chat message's chip) open
// items through the OPEN_ITEM event.
function useItemParam() {
  const [id, setId] = useState(itemFromURL);
  const pushed = useRef(false);
  useEffect(() => {
    const onPop = () => {
      pushed.current = false;
      setId(itemFromURL());
    };
    const onOpen = (e) => open(e.detail);
    window.addEventListener("popstate", onPop);
    window.addEventListener(OPEN_ITEM, onOpen);
    return () => {
      window.removeEventListener("popstate", onPop);
      window.removeEventListener(OPEN_ITEM, onOpen);
    };
  }, []);
  function open(next) {
    window.history.pushState(null, "", urlWithItem(next));
    pushed.current = true;
    setId(next);
  }
  const close = () => {
    if (pushed.current) {
      pushed.current = false;
      window.history.back();
    } else {
      window.history.replaceState(null, "", urlWithItem(null));
    }
    setId(null);
  };
  return [id, open, close];
}

// useOpenItem returns the open item from the live list. If it disappears
// (someone deleted it), or a deep link names no item, the dialog closes
// with a notice.
function useOpenItem(openId, items, close, setNotice) {
  const seen = useRef(null);
  const item = openId === null ? null : items.find((it) => it.id === openId);
  useEffect(() => {
    if (openId === null || item) {
      seen.current = item ? openId : null;
      return;
    }
    if (seen.current === openId) {
      setNotice(`#${openId} was deleted.`);
      close();
    } else if (items.length > 0) {
      setNotice(`There's no item #${openId}.`);
      close();
    }
  }, [openId, item, items.length]);
  return item;
}

function useDismiss(notice, setNotice) {
  useEffect(() => {
    if (!notice) return;
    const t = setTimeout(() => setNotice(""), 6000);
    return () => clearTimeout(t);
  }, [notice]);
}

// useRevealCreated scrolls a newly created item into view once the live list
// has it, and drops the highlight after a moment.
function useRevealCreated(created, items, setCreated) {
  const present = created !== null && items.some((it) => it.id === created);
  useEffect(() => {
    if (!present) return;
    document.getElementById(`item-${created}`)?.scrollIntoView({ block: "nearest" });
    const t = setTimeout(() => setCreated(null), 2500);
    return () => clearTimeout(t);
  }, [present, created]);
}

// ---- Reordering: drag (mouse, touch) and keyboard ----

const DRAG_THRESHOLD = 4; // px a mouse moves before a press becomes a drag
const LONG_PRESS_MS = 250; // a touch on the grip held this long picks up
const TOUCH_SLOP = 10; // px a touch may wander during the long press
const EDGE = 48; // px from the scroller's edge where dragging scrolls

// hitTest finds the row of a group under y, and which half of it.
function hitTest(y, status) {
  for (const el of document.querySelectorAll(`.item[data-status="${status}"]`)) {
    const r = el.getBoundingClientRect();
    if (y >= r.top && y < r.bottom) return { id: Number(el.dataset.id), where: y < r.top + r.height / 2 ? "before" : "after" };
  }
  return null;
}

// The server removes the item and re-inserts it at the new position.
function dropPosition(from, to, where) {
  if (where === "before") return from < to ? to - 1 : to;
  return from < to ? to : to + 1;
}

function autoScroll(scroller, y) {
  const own = scroller && scroller.scrollHeight > scroller.clientHeight;
  const top = own ? scroller.getBoundingClientRect().top : 0;
  const bottom = own ? scroller.getBoundingClientRect().bottom : window.innerHeight;
  const dy = y < top + EDGE ? -12 : y > bottom - EDGE ? 12 : 0;
  if (dy) (own ? scroller : window).scrollBy(0, dy);
}

// A click right after a drag ends shouldn't open the item.
function swallowNextClick() {
  const swallow = (e) => {
    e.stopPropagation();
    e.preventDefault();
  };
  window.addEventListener("click", swallow, { capture: true, once: true });
  setTimeout(() => window.removeEventListener("click", swallow, { capture: true }), 0);
}

// useReorder reorders within a group (moving an open item among in-progress
// ones would need an assignment, not a move), through the move endpoint.
//   - Mouse: drag a row (or its grip).
//   - Touch: long-press the grip, then drag; the grip has touch-action: none
//     and the rest of the row scrolls as usual.
//   - Keyboard: on the grip, Space/Enter picks up, arrows move, Space/Enter
//     drops, Escape cancels; moves are announced (announcement).
function useReorder(groups, moveTo) {
  const [drag, setDrag] = useState(null); // The item being dragged.
  const [drop, setDrop] = useState(null); // { id, where: "before" | "after" }
  const [lifted, setLifted] = useState(null); // Keyboard: { id, status, from, index }
  const [announcement, setAnnouncement] = useState("");
  const live = useRef({});
  live.current = { drag, drop, groups };
  const refocus = useRef(null); // Keyboard: the grip to keep focused after a drop.

  const end = () => {
    setDrag(null);
    setDrop(null);
    document.documentElement.classList.remove("reordering");
  };
  const commitDrop = (item) => {
    const target = live.current.drop;
    const over = target && live.current.groups[item.status]?.find((it) => it.id === target.id);
    end();
    if (over) moveTo(item, dropPosition(item.position, over.position, target.where));
  };
  const begin = (item) => {
    setDrag(item);
    document.documentElement.classList.add("reordering");
    window.getSelection()?.removeAllRanges();
    navigator.vibrate?.(10);
  };

  const pointerDown = (e, item, fromGrip) => {
    if (item.status === "done" || live.current.drag) return;
    const touch = e.pointerType === "touch";
    if (touch ? !fromGrip : e.button !== 0) return;
    if (!fromGrip && e.target.closest("a, select, input, textarea, button:not(.row-title)")) return;
    trackPointer(e, item, touch);
  };

  const trackPointer = (down, item, touch) => {
    const scroller = down.currentTarget.closest(".screen-body");
    const start = { x: down.clientX, y: down.clientY, id: down.pointerId };
    let started = false;
    const timer = touch
      ? setTimeout(() => {
          started = true;
          begin(item);
        }, LONG_PRESS_MS)
      : null;
    const move = (e) => {
      if (e.pointerId !== start.id) return;
      const dist = Math.hypot(e.clientX - start.x, e.clientY - start.y);
      if (!started) {
        if (touch) {
          if (dist > TOUCH_SLOP) stop();
          return;
        }
        if (dist < DRAG_THRESHOLD) return;
        started = true;
        begin(item);
      }
      e.preventDefault();
      const hit = hitTest(e.clientY, item.status);
      if (hit && hit.id !== item.id) setDrop((d) => (d?.id === hit.id && d.where === hit.where ? d : hit));
      else if (hit) setDrop(null);
      autoScroll(scroller, e.clientY);
    };
    const up = (e) => {
      if (e.pointerId !== start.id) return;
      stop();
      if (!started) return;
      swallowNextClick();
      if (e.type === "pointerup") commitDrop(item);
      else end();
    };
    const key = (e) => {
      if (e.key !== "Escape" || !started) return;
      e.preventDefault();
      e.stopPropagation();
      stop();
      end();
      setAnnouncement("Reorder cancelled.");
    };
    function stop() {
      clearTimeout(timer);
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      window.removeEventListener("pointercancel", up);
      window.removeEventListener("keydown", key, true);
    }
    window.addEventListener("pointermove", move, { passive: false });
    window.addEventListener("pointerup", up);
    window.addEventListener("pointercancel", up);
    window.addEventListener("keydown", key, true);
  };

  const describe = (item, index, total) => `#${item.id} ${item.title}, position ${index + 1} of ${total}`;
  const keyDown = (e, item, list) => {
    const idx = list.findIndex((it) => it.id === item.id);
    const isLifted = lifted?.id === item.id;
    if (e.key === " " || e.key === "Enter") {
      e.preventDefault();
      if (!isLifted) {
        setLifted({ id: item.id, status: item.status, from: idx, index: idx });
        setAnnouncement(`Picked up ${describe(item, idx, list.length)}. Arrow keys move it, Space drops it, Escape cancels.`);
        return;
      }
      setLifted(null);
      refocus.current = item.id;
      if (lifted.index !== lifted.from) moveTo(item, list[lifted.index].position);
      setAnnouncement(`Dropped ${describe(item, lifted.index, list.length)}.`);
      return;
    }
    if (!isLifted) return;
    if (e.key === "ArrowUp" || e.key === "ArrowDown") {
      e.preventDefault();
      const index = Math.min(list.length - 1, Math.max(0, lifted.index + (e.key === "ArrowUp" ? -1 : 1)));
      setLifted({ ...lifted, index });
      setAnnouncement(`Moved to ${describe(item, index, list.length)}.`);
    } else if (e.key === "Escape" || e.key === "Tab") {
      if (e.key === "Escape") e.preventDefault();
      setLifted(null);
      setAnnouncement(`Cancelled. ${describe(item, lifted.from, list.length)}.`);
    }
  };

  // Moving the lifted row in the DOM can drop focus: keep it on its grip,
  // also when the list re-renders in the new order after a drop.
  useLayoutEffect(() => {
    const id = lifted?.id ?? refocus.current;
    const grip = id && document.querySelector(`[data-grip="${id}"]`);
    if (grip && document.activeElement !== grip && (lifted || document.activeElement === document.body)) grip.focus();
  });
  useEffect(() => {
    if (lifted || !refocus.current) return;
    const t = setTimeout(() => (refocus.current = null), 3000);
    return () => clearTimeout(t);
  }, [lifted]);
  // A pointer press elsewhere puts a keyboard-lifted item back.
  useEffect(() => {
    if (!lifted) return;
    const cancel = (e) => {
      if (!e.target.closest?.(`[data-grip="${lifted.id}"]`)) setLifted(null);
    };
    window.addEventListener("pointerdown", cancel);
    return () => window.removeEventListener("pointerdown", cancel);
  }, [lifted]);

  // arrange previews a keyboard move: the lifted item shown at its new index.
  const arrange = (status, list) => {
    if (!lifted || lifted.status !== status || lifted.index === lifted.from) return list;
    const out = list.filter((it) => it.id !== lifted.id);
    out.splice(lifted.index, 0, list[lifted.from]);
    return out;
  };
  const classesFor = (item) => {
    const c = [];
    if (drag?.id === item.id) c.push("dragging");
    if (drop?.id === item.id) c.push(`drop-${drop.where}`);
    if (lifted?.id === item.id) c.push("lifted");
    return c;
  };
  return { pointerDown, keyDown, arrange, classesFor, isLifted: (item) => lifted?.id === item.id, announcement };
}
