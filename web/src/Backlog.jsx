import { Fragment, memo, useCallback, useEffect, useId, useLayoutEffect, useMemo, useRef, useState } from "react";
import { Columns3, GitPullRequest, GripVertical, List, MessageSquare, Plus } from "lucide-react";
import { ItemDialog, NewItemDialog } from "./BacklogItemDialog.jsx";
import { STATUS_LABELS, blockerLabel, blockerTitle, blockersOf, itemNum, meta, name, prLabel, useStableParticipants } from "./backlogFormat.js";
import {
  BacklogToolbar,
  CollapsibleGroupHead,
  buildBacklogSectionView,
  buildBacklogStatusView,
  formatStatusLabel,
  useBacklogGroup,
  useBacklogSort,
  useCollapsedGroups,
} from "./backlogView.jsx";
import Kanban from "./Kanban.jsx";
import { OPEN_ITEM } from "./nav.js";
import { useKeyShortcut } from "./shortcuts.jsx";
import { RecurringMarker } from "./Recurring.jsx";
import { SectionIcon } from "./settings/Sections.jsx";
import Button from "./ui/Button.jsx";
import ScreenHead from "./ui/ScreenHead.jsx";

const EMPTY_REPOS = [];

// The prioritized backlog, grouped into In progress, Open, and Done: as a
// list, or as a board (Kanban.jsx), as the header's switch picks.
// Queued items (open + paused + in_progress) share one priority order (the position
// numbers); done items follow. Rows are compact: clicking one opens the item
// dialog (also deep-linkable as ?item=12), and dragging reorders. It fills
// the workspace's main pane: head is the header's shared controls (who's
// here, Invite), error the workspace's error bar.
export default function Backlog({ section = null, sections = [], items, allItems = null, todos = [], setTodos, artifacts = [], setArtifacts, token = "", onReloadMe, participants, me, workspace, call, onError, head, error }) {
  const [view, setView] = useBacklogView();
  const [sort, setSort] = useBacklogSort();
  const [groupBy, setGroupBy] = useBacklogGroup();
  const { isExpanded, toggleGroup, expandGroups } = useCollapsedGroups();
  const [creating, setCreating] = useState(false);
  useKeyShortcut(
    (e) => !e.shiftKey && (e.key === "c" || e.key === "C"),
    () => setCreating(true),
  );
  const [created, setCreated] = useState(null); // The item just added: scrolled to and highlighted.
  // Items you added that aren't in the list yet (#128): shown at once, at the
  // bottom of Open, until the server's copy arrives (realId) or the call fails.
  const [adding, setAdding] = useState([]);
  const [notice, setNotice] = useState("");
  const [openId, openItem, closeItem] = useItemParam();
  const onErrorRef = useRef(onError);
  onErrorRef.current = onError;
  const run = useCallback(
    (method, suffix, payload) => call(method, suffix, payload).catch((e) => onErrorRef.current(e.message)),
    [call],
  );
  const moveTo = useCallback(
    (item, position) => {
      if (position !== item.position) run("POST", `/backlog/${itemNum(item)}/move`, { position });
    },
    [run],
  );
  const stableParticipants = useStableParticipants(participants);
  const showGroupControl = view === "list" && !section && sections.length > 0;
  const effectiveGroupBy = showGroupControl ? groupBy : "status";
  const canReorder = sort === "priority" && effectiveGroupBy === "status";
  const lookupItems = allItems || items;
  const { queued, groups, extraStatuses, done } = useMemo(() => buildBacklogStatusView(items, sort), [items, sort]);
  const kanbanGroups = useMemo(
    () => ({
      in_progress: groups.in_progress,
      open: queued.filter((it) => it.status !== "in_progress"),
    }),
    [groups.in_progress, queued],
  );
  const sectionView = useMemo(
    () => (effectiveGroupBy === "section" ? buildBacklogSectionView(lookupItems, sections, sort) : null),
    [effectiveGroupBy, lookupItems, sections, sort],
  );
  const reorder = useReorder(view === "kanban" ? kanbanGroups : groups, moveTo, canReorder);
  const openedItem = useOpenItem(openId, lookupItems, closeItem, setNotice);
  useDismiss(notice, setNotice);
  useRevealCreated(created, effectiveGroupBy === "section" ? lookupItems : items, setCreated);
  const hintId = useId();
  const repos = useMemo(() => workspace?.repos || EMPTY_REPOS, [workspace?.repos]);

  const rows = (groupKey, list) =>
    reorder.arrange(groupKey, list).map((it) => (
      <BacklogRow
        key={it.id}
        item={it}
        groupKey={groupKey}
        list={list}
        highlight={created === it.id}
        participants={stableParticipants}
        me={me}
        run={run}
        canReorder={canReorder}
        dragClass={reorder.classesFor(it).join(" ")}
        lifted={reorder.isLifted(it)}
        onReorderPointerDown={reorder.pointerDown}
        onReorderKeyDown={reorder.keyDown}
        hintId={hintId}
        repos={repos}
        onOpen={openItem}
      />
    ));
  const create = async ({ title, body, assignee_id, section_id, choices, close_on_answer }) => {
    setCreating(false);
    const targetSectionId = section_id !== undefined ? Number(section_id) : section?.id || 0;
    const staysInSection = targetSectionId === (section?.id || 0);
    const staysHere = staysInSection || (!section && effectiveGroupBy === "section");
    if (staysHere) {
      expandGroups([
        staysInSection ? "open" : null,
        !section ? (targetSectionId > 0 ? `section:${targetSectionId}` : "section:backlog") : null,
      ]);
    }
    const temp = staysHere ? { id: `pending-${Date.now()}`, title, body, assignee_id, section_id: targetSectionId } : null;
    if (temp) setAdding((list) => [...list, temp]);
    const payload = { title, body };
    if (assignee_id > 0) payload.assignee_id = assignee_id;
    if (targetSectionId > 0) payload.section_id = targetSectionId;
    if (choices?.length > 0) {
      payload.choices = choices;
      if (close_on_answer) payload.close_on_answer = true;
    }
    const item = await run("POST", "/backlog", payload);
    if (item) {
      if (temp) {
        setAdding((list) => list.map((x) => (x === temp ? { ...x, realId: item.id } : x)));
        setCreated(item.id);
      }
      if (item.section_notify) onReloadMe?.();
    } else {
      // Failed (the error bar says why): give the text back to try again.
      if (temp) setAdding((list) => list.filter((x) => x !== temp));
      setCreating({ title, body, assignee_id, section_id: targetSectionId, choices, close_on_answer });
    }
  };
  const ids = new Set(lookupItems.map((it) => it.id));
  const pendingRows = adding.filter((x) => !ids.has(x.realId));
  const statusPendingRows = pendingRows.filter((x) => (x.section_id || 0) === (section?.id || 0));
  useEffect(() => {
    if (pendingRows.length < adding.length) setAdding(pendingRows);
  }, [pendingRows.length, adding.length]);

  const screenLabel = section ? section.name : "Backlog";
  const newButtonLabel = section ? `New ${section.name}` : "New item";
  const headTitle = section ? (
    <span className="section-head-title">
      <SectionIcon icon={section.icon} aria-hidden="true" />
      <span>{section.name}</span>
      {section.notify && <span className="chip section-notify-chip">Actionable</span>}
    </span>
  ) : (
    "Backlog"
  );
  const toolbar = (
    <BacklogToolbar
      viewSwitch={<ViewSwitch view={view} onChange={setView} />}
      sort={sort}
      onSortChange={setSort}
      showGroupControl={showGroupControl}
      groupBy={groupBy}
      onGroupChange={setGroupBy}
    />
  );

  return (
    <section className="screen" aria-label={screenLabel}>
      <ScreenHead title={headTitle} tools={toolbar}>
        {head}
        <Button variant="primary" size="small" onClick={() => setCreating(true)} aria-haspopup="dialog" aria-label={newButtonLabel}>
          <Plus aria-hidden="true" />
          <span className="label">{newButtonLabel}</span>
        </Button>
      </ScreenHead>
      {error}
      <div className={view === "kanban" ? "screen-body kanban-body" : "screen-body"}>
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

        {view === "kanban" ? (
          <Kanban
            groups={kanbanGroups}
            {...{ done, me, repos, run, reorder, canReorder, hintId, created }}
            sectionLabel={screenLabel}
            participants={stableParticipants}
            pending={statusPendingRows}
            onOpen={openItem}
            onAdd={create}
          />
        ) : effectiveGroupBy === "section" && sectionView ? (
          <SectionGroupList
            sectionView={sectionView}
            pendingRows={pendingRows}
            participants={stableParticipants}
            isExpanded={isExpanded}
            onToggle={toggleGroup}
            renderRows={rows}
          />
        ) : (
          <StatusGroupList
            queued={queued}
            groups={groups}
            extraStatuses={extraStatuses}
            done={done}
            pendingRows={statusPendingRows}
            participants={stableParticipants}
            isExpanded={isExpanded}
            onToggle={toggleGroup}
            renderRows={rows}
          />
        )}
      </div>

      {creating && (
        <NewItemDialog
          initial={creating === true ? null : creating}
          section={section}
          sections={sections}
          participants={stableParticipants}
          onClose={() => setCreating(false)}
          onCreate={create}
        />
      )}
      {openedItem && (
        <ItemDialog
          item={openedItem}
          items={lookupItems}
          sections={sections}
          todos={todos}
          setTodos={setTodos}
          artifacts={artifacts}
          setArtifacts={setArtifacts}
          workspace={workspace}
          token={token}
          onReloadMe={onReloadMe}
          participants={stableParticipants}
          me={me}
          call={call}
          run={run}
          onClose={closeItem}
        />
      )}
    </section>
  );
}

// ---- List | Kanban (#293): remembered per browser ----

const VIEW_KEY = "unison.backlog.view";
const VIEWS = [
  { id: "list", label: "List", Icon: List },
  { id: "kanban", label: "Kanban", Icon: Columns3 },
];

function useBacklogView() {
  const [view, setView] = useState(() => {
    try {
      return localStorage.getItem(VIEW_KEY) === "kanban" ? "kanban" : "list";
    } catch {
      return "list";
    }
  });
  const choose = (v) => {
    setView(v);
    try {
      localStorage.setItem(VIEW_KEY, v);
    } catch {
      // Storage blocked: the choice lasts until reload.
    }
  };
  return [view, choose];
}

// ViewSwitch is a segmented List | Kanban switch; phones show icons only.
function ViewSwitch({ view, onChange }) {
  return (
    <div className="seg view-switch" role="group" aria-label="Backlog view">
      {VIEWS.map(({ id, label, Icon }) => (
        <button key={id} type="button" aria-pressed={view === id} aria-label={label} title={label} onClick={() => onChange(id)}>
          <Icon aria-hidden="true" />
          <span className="label">{label}</span>
        </button>
      ))}
    </div>
  );
}

function StatusGroupList({ queued, groups, extraStatuses, done, pendingRows, participants, isExpanded, onToggle, renderRows }) {
  const inProgressOpen = isExpanded("in_progress");
  const openExpanded = isExpanded("open");
  const doneExpanded = isExpanded("done");
  return (
    <>
      <CollapsibleGroupHead
        expanded={inProgressOpen}
        onToggle={() => onToggle("in_progress")}
        status="in_progress"
        label="In progress"
        count={groups.in_progress.length}
      />
      {inProgressOpen && renderRows("in_progress", groups.in_progress)}

      {extraStatuses.map((status) => {
        const list = groups[status] || [];
        const expanded = isExpanded(status);
        return (
          <Fragment key={status}>
            <CollapsibleGroupHead
              expanded={expanded}
              onToggle={() => onToggle(status)}
              status={status}
              label={formatStatusLabel(status)}
              count={list.length}
            />
            {expanded && renderRows(status, list)}
          </Fragment>
        );
      })}

      <CollapsibleGroupHead
        expanded={openExpanded}
        onToggle={() => onToggle("open")}
        status="open"
        label="Open"
        count={groups.open.length}
      />
      {openExpanded && (
        <>
          {queued.length === 0 && pendingRows.length === 0 && <p className="empty-row">Nothing queued.</p>}
          {renderRows("open", groups.open)}
          {pendingRows.map((x) => (
            <PendingRow key={x.id} title={x.title} assigneeId={x.assignee_id} participants={participants} />
          ))}
        </>
      )}

      <CollapsibleGroupHead
        expanded={doneExpanded}
        onToggle={() => onToggle("done")}
        status="done"
        label="Done"
        count={done.length}
      />
      {doneExpanded && renderRows("done", done)}
    </>
  );
}

function SectionGroupList({ sectionView, pendingRows, participants, isExpanded, onToggle, renderRows }) {
  const { activeCount, sectionGroups, done } = sectionView;
  const doneExpanded = isExpanded("done");
  return (
    <>
      {sectionGroups.map((group) => {
        const expanded = isExpanded(group.key);
        const groupPending = pendingRows.filter((x) => (x.section_id || 0) === group.sectionId);
        return (
          <Fragment key={group.key}>
            <CollapsibleGroupHead
              expanded={expanded}
              onToggle={() => onToggle(group.key)}
              isBacklogSection={group.isBacklog}
              sectionIcon={group.icon}
              label={group.label}
              notify={group.notify}
              count={group.items.length}
            />
            {expanded && (
              <>
                {group.isBacklog && activeCount === 0 && pendingRows.length === 0 && <p className="empty-row">Nothing queued.</p>}
                {renderRows(group.key, group.items)}
                {groupPending.map((x) => (
                  <PendingRow key={x.id} title={x.title} assigneeId={x.assignee_id} participants={participants} />
                ))}
              </>
            )}
          </Fragment>
        );
      })}

      <CollapsibleGroupHead
        expanded={doneExpanded}
        onToggle={() => onToggle("done")}
        status="done"
        label="Done"
        count={done.length}
      />
      {doneExpanded && renderRows("done", done)}
    </>
  );
}

// PendingRow is an item on its way to the server (#128): its title, dimmed,
// without the controls that need its number.
function PendingRow({ title, assigneeId, participants }) {
  return (
    <div className="item open item-pending" aria-busy="true">
      <div className="row">
        <span className="rank" />
        <span className="status open" />
        <span className="item-id">#…</span>
        <span className="row-title">{title}</span>
        {assigneeId > 0 && <span className="chip">{name(participants, assigneeId)}</span>}
        <span className="meta">adding…</span>
      </div>
    </div>
  );
}

const BacklogRow = memo(function BacklogRow({
  item,
  groupKey,
  list,
  highlight,
  participants,
  me,
  repos,
  run,
  canReorder = true,
  dragClass,
  lifted,
  onReorderPointerDown,
  onReorderKeyDown,
  hintId,
  onOpen,
}) {
  const num = itemNum(item);
  const queued = item.status !== "done";
  const blockers = blockersOf(item);
  const canStart = item.status === "open" && blockers.length === 0 && me && (!item.assignee_id || item.assignee_id === me.id);
  const canResume = item.status === "paused" && Boolean(me);
  const classes = ["item", item.status];
  if (dragClass) classes.push(dragClass);
  if (highlight) classes.push("item-created");
  const open = (e) => {
    // Focus the title so closing the dialog returns focus to this row.
    e.currentTarget.querySelector(".row-title").focus();
    onOpen(num);
  };
  return (
    <div className={classes.join(" ")} id={`item-${item.id}`} data-id={item.id} data-status={item.status} data-group={groupKey || item.status}>
      <div className="row" onClick={open} onPointerDown={canReorder ? (e) => onReorderPointerDown(e, item, false) : undefined}>
        <span className="rank">
          {canReorder && queued && (
            <button
              type="button"
              className="grip"
              data-grip={item.id}
              aria-label={`Reorder #${num} ${item.title}`}
              aria-describedby={hintId}
              aria-pressed={lifted}
              onPointerDown={(e) => {
                e.stopPropagation();
                onReorderPointerDown(e, item, true);
              }}
              onClick={(e) => e.stopPropagation()}
              onKeyDown={(e) => onReorderKeyDown(e, item, list)}
            >
              <GripVertical aria-hidden="true" />
            </button>
          )}
          <span className="rank-number">{queued ? item.position : ""}</span>
        </span>
        <span className={`status ${item.status}`} title={STATUS_LABELS[item.status] || item.status} />
        <span className="item-id">#{num}</span>
        <RecurringMarker item={item} />
        <button type="button" className="row-title" aria-haspopup="dialog">
          {item.title}
          {!queued && item.close_note && <span className="row-close-note"> — {item.close_note}</span>}
        </button>
        {queued ? (
          <>
            {item.status === "paused" && <span className="chip status-chip-paused">Paused</span>}
            {blockers.length > 0 && (
              <span className="chip blocker-chip" title={blockerTitle(blockers)}>
                {blockerLabel(blockers)}
              </span>
            )}
            {item.choices?.length > 0 && (
              <span className={item.answered_choice ? "chip choice-chip choice-chip-answered" : "chip choice-chip"}>
                {item.answered_choice ? `✓ ${item.answered_choice}` : `${item.choices.length} choices`}
              </span>
            )}
            <PRChips prs={item.prs} repos={repos} />
            {item.comment_count > 0 && (
              <span className="comment-count" title={`${item.comment_count} comment${item.comment_count === 1 ? "" : "s"}`}>
                <MessageSquare aria-hidden="true" />
                {item.comment_count}
              </span>
            )}
            <span className="item-reporter">by {name(participants, item.created_by)}</span>
            {item.assignee_id && <span className="chip">{name(participants, item.assignee_id)}</span>}
          </>
        ) : (
          <>
            <span className="item-reporter">by {name(participants, item.created_by)}</span>
            {item.assignee_id && <span className="chip">{name(participants, item.assignee_id)}</span>}
            {item.comment_count > 0 && (
              <span className="comment-count" title={`${item.comment_count} comment${item.comment_count === 1 ? "" : "s"}`}>
                <MessageSquare aria-hidden="true" />
                {item.comment_count}
              </span>
            )}
            <PRChips prs={item.prs} repos={repos} />
          </>
        )}
        <span className="meta">{meta(item)}</span>
        {(canStart || canResume) && (
          <span className="row-actions" onClick={(e) => e.stopPropagation()}>
            {canStart ? (
              <Button title={item.assignee_id ? "Start working on this item" : "Assign to me and start working"} onClick={() => run("POST", `/backlog/${num}/start`)}>
                Start
              </Button>
            ) : (
              <Button title="Resume this item back to open" onClick={() => run("POST", `/backlog/${num}/resume`)}>
                Resume
              </Button>
            )}
          </span>
        )}
      </div>
    </div>
  );
});

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
  const pushDepth = useRef(0);
  const open = useCallback((next) => {
    const nextNum = Number(next);
    window.history.pushState(null, "", urlWithItem(nextNum));
    pushDepth.current += 1;
    setId(nextNum);
  }, []);
  useEffect(() => {
    const onPop = () => {
      pushDepth.current = 0;
      setId(itemFromURL());
    };
    const onOpen = (e) => open(e.detail);
    window.addEventListener("popstate", onPop);
    window.addEventListener(OPEN_ITEM, onOpen);
    return () => {
      window.removeEventListener("popstate", onPop);
      window.removeEventListener(OPEN_ITEM, onOpen);
    };
  }, [open]);
  const close = useCallback(() => {
    if (pushDepth.current > 0) {
      const steps = pushDepth.current;
      pushDepth.current = 0;
      window.history.go(-steps);
    } else {
      window.history.replaceState(null, "", urlWithItem(null));
    }
    setId(null);
  }, []);
  return [id, open, close];
}

// useOpenItem returns the open item from the live list. If it disappears
// (someone deleted it), or a deep link names no item, the dialog closes
// with a notice.
function useOpenItem(openId, items, close, setNotice) {
  const seen = useRef(null);
  const item = openId === null ? null : items.find((it) => it.number === openId) || items.find((it) => it.id === openId) || null;
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
function hitTest(y, group) {
  for (const el of document.querySelectorAll(`.item[data-group="${group}"]`)) {
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

// useReorder reorders within a group (a list group or a board column; moving an open item among in-progress
// ones would need an assignment, not a move), through the move endpoint.
//   - Mouse: drag a row (or its grip).
//   - Touch: long-press the grip, then drag; the grip has touch-action: none
//     and the rest of the row scrolls as usual.
//   - Keyboard: on the grip, Space/Enter picks up, arrows move, Space/Enter
//     drops, Escape cancels; moves are announced (announcement).
function useReorder(groups, moveTo, canReorder = true) {
  const [drag, setDrag] = useState(null); // The item being dragged.
  const [drop, setDrop] = useState(null); // { id, where: "before" | "after" }
  const [lifted, setLifted] = useState(null); // Keyboard: { id, group, from, index }
  const [announcement, setAnnouncement] = useState("");
  const live = useRef({});
  live.current = { canReorder, drag, drop, groups, lifted, moveTo };
  const refocus = useRef(null); // Keyboard: the grip to keep focused after a drop.

  useEffect(() => {
    if (!canReorder) {
      setDrag(null);
      setDrop(null);
      setLifted(null);
      document.documentElement.classList.remove("reordering");
    }
  }, [canReorder]);

  const pointerDown = useCallback((e, item, fromGrip) => {
    if (!live.current.canReorder || item.status === "done" || live.current.drag) return;
    const group = e.currentTarget.closest(".item")?.dataset.group || item.status;
    const touch = e.pointerType === "touch";
    if (touch ? !fromGrip : e.button !== 0) return;
    if (!fromGrip && e.target.closest("a, select, input, textarea, button:not(.row-title)")) return;
    const scroller = e.currentTarget.closest(".kanban-col-body, .screen-body");
    const start = { x: e.clientX, y: e.clientY, id: e.pointerId };
    let started = false;
    const end = () => {
      setDrag(null);
      setDrop(null);
      document.documentElement.classList.remove("reordering");
    };
    const commitDrop = () => {
      const target = live.current.drop;
      const over = target && live.current.groups[group]?.find((it) => it.id === target.id);
      end();
      if (over) live.current.moveTo(item, dropPosition(item.position, over.position, target.where));
    };
    const begin = () => {
      setDrag(item);
      document.documentElement.classList.add("reordering");
      window.getSelection()?.removeAllRanges();
      navigator.vibrate?.(10);
    };
    const timer = touch
      ? setTimeout(() => {
          started = true;
          begin();
        }, LONG_PRESS_MS)
      : null;
    const move = (ev) => {
      if (ev.pointerId !== start.id) return;
      const dist = Math.hypot(ev.clientX - start.x, ev.clientY - start.y);
      if (!started) {
        if (touch) {
          if (dist > TOUCH_SLOP) stop();
          return;
        }
        if (dist < DRAG_THRESHOLD) return;
        started = true;
        begin();
      }
      ev.preventDefault();
      const hit = hitTest(ev.clientY, group);
      if (hit && hit.id !== item.id) setDrop((d) => (d?.id === hit.id && d.where === hit.where ? d : hit));
      else if (hit) setDrop(null);
      autoScroll(scroller, ev.clientY);
    };
    const up = (ev) => {
      if (ev.pointerId !== start.id) return;
      stop();
      if (!started) return;
      swallowNextClick();
      if (ev.type === "pointerup") commitDrop();
      else end();
    };
    const key = (ev) => {
      if (ev.key !== "Escape" || !started) return;
      ev.preventDefault();
      ev.stopPropagation();
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
  }, []);

  const keyDown = useCallback((e, item, list) => {
    if (!live.current.canReorder) return;
    const group = e.currentTarget.closest(".item")?.dataset.group || item.status;
    const describe = (it, index, total) => `#${itemNum(it)} ${it.title}, position ${index + 1} of ${total}`;
    const curLifted = live.current.lifted;
    const idx = list.findIndex((it) => it.id === item.id);
    const isLifted = curLifted?.id === item.id;
    if (e.key === " " || e.key === "Enter") {
      e.preventDefault();
      if (!isLifted) {
        setLifted({ id: item.id, group, from: idx, index: idx });
        setAnnouncement(`Picked up ${describe(item, idx, list.length)}. Arrow keys move it, Space drops it, Escape cancels.`);
        return;
      }
      setLifted(null);
      refocus.current = item.id;
      if (curLifted.index !== curLifted.from) live.current.moveTo(item, list[curLifted.index].position);
      setAnnouncement(`Dropped ${describe(item, curLifted.index, list.length)}.`);
      return;
    }
    if (!isLifted) return;
    if (e.key === "ArrowUp" || e.key === "ArrowDown") {
      e.preventDefault();
      const index = Math.min(list.length - 1, Math.max(0, curLifted.index + (e.key === "ArrowUp" ? -1 : 1)));
      setLifted({ ...curLifted, index });
      setAnnouncement(`Moved to ${describe(item, index, list.length)}.`);
    } else if (e.key === "Escape" || e.key === "Tab") {
      if (e.key === "Escape") e.preventDefault();
      setLifted(null);
      setAnnouncement(`Cancelled. ${describe(item, curLifted.from, list.length)}.`);
    }
  }, []);

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
  const arrange = (group, list) => {
    if (!canReorder || !lifted || lifted.group !== group || lifted.index === lifted.from) return list;
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
