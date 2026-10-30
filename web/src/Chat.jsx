import { useEffect, useRef, useState } from "react";
import { Check, CircleAlert, CircleCheck, Ellipsis, ListChecks, ListPlus, MessageSquare, Play, Repeat, Reply, RotateCcw, Undo2, UserCheck, UserMinus, UserPlus, X } from "lucide-react";
import { itemLink } from "./nav.js";
import Button from "./ui/Button.jsx";
import { Textarea } from "./ui/Input.jsx";
import Markdown, { refersToPR } from "./Markdown.jsx";
import ScreenHead from "./ui/ScreenHead.jsx";

// The workspace chat room, a screen of its own. Threads are one level deep.
// The main log lists the messages that start threads; one with replies shows
// a thread summary that opens the thread in a panel beside the log (over it
// on phones), with its own composer. head is the
// header's shared controls (who's here, Invite), error the workspace's
// error bar. updateMessages updates the shown messages in place (for
// optimistic reactions). Messages after newAfter are new to you: those that
// mention you are highlighted. mention (a name) starts the message with
// "@Name "; onMentioned says it was used. A message can ask the people it
// @mentions to act (action_required): it stands out for them, with Done.
// A message can be about a backlog item: it shows a chip that opens the item.
// `shown` ({id, at}) is a message to scroll to and highlight. Hovering (or
// focusing) a message shows its actions: reply (in its thread), and the
// reactions. activity (events) shows between the messages, by time, as
// one-line updates: items added, closed and reopened, people joining and
// leaving (#93).

// REACTIONS are the emoji anyone can put on a message (the server accepts
// exactly these).
const REACTIONS = [
  { emoji: "👍", label: "Thumbs up" },
  { emoji: "👎", label: "Thumbs down" },
  { emoji: "❤️", label: "Heart" },
  { emoji: "💪", label: "Muscle" },
  { emoji: "💯", label: "100" },
];

export default function Chat({ messages, activity = [], updateMessages, newAfter, mention, onMentioned, items, participants, me, call, onError, shown, head, error }) {
  const [actionsFor, setActionsFor] = useState(null); // Touch: the message whose actions are shown.
  const [threadId, setThreadId] = useState(null); // The thread open in the panel (its first message).
  const [prefill, setPrefill] = useState(null); // {text, at}: start the thread composer with this.
  const flashed = useShowMessage(shown, messages);
  const bottom = useRef(null);
  // Messages you sent that the server hasn't confirmed yet (#128): shown at
  // once, kept out of messages so they never count as read or unread.
  const [pending, setPending] = useState([]);
  const shownMessages = pending.length ? [...messages, ...pending] : messages;

  useEffect(() => {
    bottom.current?.scrollIntoView({ block: "nearest" });
  }, [shownMessages.length, activity.length]);

  const byId = new Map(shownMessages.map((m) => [m.id, m]));
  const replies = new Map();
  for (const m of shownMessages) {
    if (m.parent_id && byId.has(m.parent_id)) {
      replies.set(m.parent_id, [...(replies.get(m.parent_id) || []), m]);
    }
  }
  const roots = shownMessages.filter((m) => !m.parent_id || !byId.has(m.parent_id));
  const log = byTime(roots, activity);
  const thread = threadId ? byId.get(threadId) : null;

  useOpenShownThread(shown, byId, setThreadId);
  const readUpTo = useThreadsRead(thread, replies);
  useEffect(() => {
    if (!thread) return;
    const onKey = (e) => e.key === "Escape" && setThreadId(null);
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [Boolean(thread)]);

  // Reply: in the message's thread (opening it), to its author (a
  // prefilled @mention).
  const startReply = (root, target) => {
    setThreadId(root.id);
    const author = participants.find((p) => p.id === target.author_id);
    setPrefill({ text: author && author.id !== me?.id ? `@${author.name} ` : "", at: Date.now() });
    setActionsFor(null);
  };
  // react toggles your reaction. It shows at once (optimistically); the
  // live update that follows brings what the server says everyone sees, and
  // a failed call puts it back.
  const react = async (m, emoji) => {
    const mine = m.reactions?.some((r) => r.emoji === emoji && r.by.includes(me?.id));
    const show = (on) => updateMessages((list) => list.map((x) => (x.id === m.id ? { ...x, reactions: withReaction(x.reactions || [], emoji, me?.id, on) } : x)));
    show(!mine);
    try {
      await call(mine ? "DELETE" : "PUT", `/messages/${m.id}/reactions/${encodeURIComponent(emoji)}`);
    } catch (err) {
      show(mine);
      onError(err.message);
    }
  };
  // setDone marks a message that asks you to act done (or not), at once;
  // a failed call puts it back.
  const setDone = async (m, done) => {
    const show = (on) => updateMessages((list) => list.map((x) => (x.id === m.id ? { ...x, done_by: withDone(x, me?.id, on) } : x)));
    show(done);
    try {
      await call(done ? "POST" : "DELETE", `/messages/${m.id}/done`);
    } catch (err) {
      show(!done);
      onError(err.message);
    }
  };
  const body = useRef(null);
  const [threadWidth, setThreadWidth] = useStoredWidth();
  // send posts a message, showing it at once (#128). When the server
  // answers, its copy takes the pending one's place (the live update may
  // have brought it already); a failure takes it away and says why.
  // shown adds what only the pending copy shows (the linked item's chip).
  const send = async (fields, shown = {}) => {
    // Who it asks to act is the server's to say, so the pending copy doesn't.
    const temp = { ...fields, ...shown, action_required: undefined, id: `pending-${Date.now()}`, pending: true, author_id: me?.id, created_at: Date.now(), reactions: [] };
    setPending((list) => [...list, temp]);
    try {
      const m = await call("POST", "/messages", fields);
      updateMessages((list) => (list.some((x) => x.id === m.id) ? list : [...list, m].sort((a, b) => a.id - b.id)));
      return true;
    } catch (err) {
      onError(err.message);
      return false;
    } finally {
      setPending((list) => list.filter((x) => x !== temp));
    }
  };
  const messageProps = (m, root) => ({
    message: m,
    onDone: (done) => setDone(m, done),
    isNew: m.id > newAfter,
    participants,
    me,
    flashed,
    onReply: () => startReply(root, m),
    onReact: (emoji) => react(m, emoji),
    actionsOpen: actionsFor === m.id,
    onToggleActions: () => setActionsFor(actionsFor === m.id ? null : m.id),
  });
  const composerProps = { items, send };

  return (
    <section className={thread ? "screen chat chat-thread-open" : "screen chat"} aria-label="Chat">
      <ScreenHead title="Chat">{head}</ScreenHead>
      {error}
      <div className="chat-body" ref={body} style={threadWidth ? { "--thread-width": `${threadWidth}px` } : undefined}>
        <div className="chat-main">
          <div className="chat-log">
            {roots.length === 0 && <p className="chat-empty">No messages yet. Say hello.</p>}
            {log.map(({ message: m, event }) =>
              event ? (
                <ActivityLine key={`e${event.seq}`} event={event} participants={participants} />
              ) : (
              <Message key={m.id} {...messageProps(m, m)}>
                {replies.has(m.id) && (
                  <ThreadSummary
                    replies={replies.get(m.id)}
                    participants={participants}
                    me={me}
                    newAfter={Math.max(newAfter, readUpTo[m.id] || 0)}
                    open={threadId === m.id}
                    onOpen={() => setThreadId(threadId === m.id ? null : m.id)}
                  />
                )}
              </Message>
              ),
            )}
            <div ref={bottom} />
          </div>
          <Composer {...composerProps} mention={mention} onMentioned={onMentioned} placeholder="Message the workspace…  @Name to mention" />
        </div>
        {thread && <ThreadResizer body={body} width={threadWidth} onWidth={setThreadWidth} />}
        {thread && (
          <aside className="thread-panel" aria-label={`Thread #${thread.id}`}>
            <header className="thread-head">
              <MessageSquare aria-hidden="true" />
              <span className="thread-title">Thread</span>
              <span className="thread-count">{countLabel((replies.get(thread.id) || []).length)}</span>
              <span className="spacer" />
              <Button variant="ghost" iconOnly aria-label="Close thread" title="Close thread (Esc)" onClick={() => setThreadId(null)}>
                <X aria-hidden="true" />
              </Button>
            </header>
            <ThreadLog key={thread.id} replies={replies.get(thread.id) || []}>
              <Message {...messageProps(thread, thread)} />
              <div className="thread-replies">
                {(replies.get(thread.id) || []).map((r) => (
                  <Message key={r.id} {...messageProps(r, thread)} />
                ))}
              </div>
            </ThreadLog>
            <Composer key={thread.id} {...composerProps} parent={thread} prefill={prefill} placeholder="Reply in thread…" autoFocus />
          </aside>
        )}
      </div>
    </section>
  );
}

// byTime merges messages and activity events into one log, oldest first:
// [{message} | {event}].
function byTime(messages, events) {
  const out = [...messages.map((message) => ({ message, at: message.created_at })), ...events.map((event) => ({ event, at: event.created_at }))];
  return out.sort((a, b) => a.at - b.at);
}

const ACTIVITY_ICONS = {
  "backlog.added": ListPlus,
  "backlog.assigned": UserCheck,
  "backlog.started": Play,
  "backlog.closed": CircleCheck,
  "backlog.reopened": RotateCcw,
  "participant.joined": UserPlus,
  "member.removed": UserMinus,
};

// activityText says what an activity event did, as [who, rest, item]: who
// in bold, item (if any) a link to the backlog item.
function activityText(event, participants) {
  const d = event.data || {};
  const actor = participants.find((p) => p.id === event.actor_id);
  const name = (p) => (p ? `${p.emoji ? `${p.emoji} ` : ""}${p.name}` : "Someone");
  switch (event.type) {
    case "backlog.added": {
      // A recurring task's run is added by the system (#112).
      if (isRecurringRun(event)) return ["Recurring task", "added", d];
      if (d.assignee_id) {
        const assignee = participants.find((p) => p.id === d.assignee_id);
        return [name(actor), `added (for ${name(assignee)})`, d];
      }
      return [name(actor), "added", d];
    }
    case "backlog.assigned": {
      if (d.assignee_id === event.actor_id) return [name(actor), "assigned to themselves", d];
      const assignee = participants.find((p) => p.id === d.assignee_id);
      return [name(actor), `assigned ${name(assignee)} to`, d];
    }
    case "backlog.started":
      return [name(actor), "started working on", d];
    case "backlog.closed":
      return [name(actor), "closed", d];
    case "backlog.reopened":
      return [name(actor), "reopened", d];
    case "participant.joined":
      return [name(d), d.sponsor ? `joined · agent of ${d.sponsor.display_name}` : "joined"];
    case "member.removed":
      // Removing yourself is leaving.
      return actor && actor.user_id === d.user_id ? [d.display_name, "left the workspace"] : [name(actor), `removed ${d.display_name}`];
    default:
      return [name(actor), event.type];
  }
}

function isRecurringRun(event) {
  return event.type === "backlog.added" && !event.actor_id && Boolean(event.data?.schedule_id);
}

// ActivityLine is one activity event in the main log: a small pill on a
// divider, unlike a message (no author block, reactions or thread).
function ActivityLine({ event, participants }) {
  const [who, rest, item] = activityText(event, participants);
  const Icon = isRecurringRun(event) ? Repeat : ACTIVITY_ICONS[event.type] || ListChecks;
  const at = new Date(event.created_at);
  return (
    <div className={`activity activity-${event.type.replace(".", "-")}`} role="note">
      <span className="activity-pill">
        <Icon aria-hidden="true" />
        <span className="activity-text">
          <span className="activity-who">{who}</span> {rest}
          {item?.id && (
            <>
              {" "}
              <a {...itemLink(item.id)}>
                #{item.id} {item.title}
              </a>
            </>
          )}
        </span>
        <span className="activity-time" title={at.toLocaleString()}>
          {at.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}
        </span>
      </span>
    </div>
  );
}

function countLabel(n) {
  return n === 1 ? "1 reply" : `${n} replies`;
}

// ThreadSummary is a thread's line under its first message in the main
// log: how many replies, who replied, and when the last one came. It
// stands out (bold, with an "N new" badge) only when the thread has replies
// new to you; clicking it opens (or closes) the thread.
function ThreadSummary({ replies, participants, me, newAfter, open, onOpen }) {
  const last = replies[replies.length - 1];
  const authors = [...new Set(replies.map((r) => r.author_id))].slice(0, 4).map((id) => participants.find((p) => p.id === id)).filter(Boolean);
  const fresh = replies.filter((r) => r.id > newAfter && r.author_id !== me?.id).length;
  const at = new Date(last.created_at);
  return (
    <button type="button" className={["thread-summary", fresh && "has-new", open && "open"].filter(Boolean).join(" ")} aria-expanded={open} onClick={onOpen}>
      <span className="thread-avatars" aria-hidden="true">
        {authors.map((p) => (
          <span key={p.id} className="thread-avatar" title={p.name}>
            {p.emoji}
          </span>
        ))}
      </span>
      <span className="thread-replies-count">{countLabel(replies.length)}</span>
      {fresh > 0 && <span className="thread-new">{fresh} new</span>}
      <span className="thread-last" title={at.toLocaleString()}>
        Last reply {at.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}
      </span>
    </button>
  );
}

// useThreadsRead remembers, per thread, the last reply you have seen in the
// thread panel this visit: opening a thread reads it, so its summary stops
// standing out (#117), as do replies that arrive while it's open.
function useThreadsRead(thread, replies) {
  const [readUpTo, setReadUpTo] = useState({});
  const list = thread ? replies.get(thread.id) || [] : [];
  const lastId = list.length ? list[list.length - 1].id : 0;
  useEffect(() => {
    if (thread && lastId > (readUpTo[thread.id] || 0)) setReadUpTo((r) => ({ ...r, [thread.id]: lastId }));
  }, [thread?.id, lastId]);
  return readUpTo;
}

// ThreadLog scrolls the thread panel to its latest reply when the thread
// opens and when a reply arrives.
function ThreadLog({ replies, children }) {
  const end = useRef(null);
  useEffect(() => {
    end.current?.scrollIntoView({ block: "nearest" });
  }, [replies.length]);
  return (
    <div className="thread-log">
      {children}
      <div ref={end} />
    </div>
  );
}

// useOpenShownThread opens the thread of the shown message when it is a
// reply (from a notification or a link), so it can be scrolled to.
function useOpenShownThread(shown, byId, setThreadId) {
  const message = shown ? byId.get(shown.id) : null;
  const parent = message?.parent_id && byId.has(message.parent_id) ? message.parent_id : null;
  useEffect(() => {
    if (parent) setThreadId(parent);
  }, [parent, shown?.at]);
}

// Composer writes a message: to the main chat, or with parent a reply in
// parent's thread (tied to its backlog item, if it has one). mention (a
// name) starts the text with "@Name "; onMentioned says it was used.
// prefill ({text, at}) starts the text with text, e.g. the @mention of the
// author being replied to.
function Composer({ parent, items, send: post, mention, onMentioned, prefill, placeholder, autoFocus }) {
  const [text, setText] = useState("");
  const [linked, setLinked] = useState(Boolean(parent?.item)); // Tie the message to the item it mentions.
  const [needsAction, setNeedsAction] = useState(false); // Ask the people the message mentions to act.
  const composer = useRef(null);
  const mentionedItem = itemMentioned(text, items) || parent?.item || null;
  const canAsk = /@\S/.test(text);

  const startWith = (prefix) => {
    if (prefix) setText((t) => (t.startsWith(prefix) ? t : prefix + t));
    // Once the text is in: type after it.
    requestAnimationFrame(() => {
      const el = composer.current;
      el?.focus();
      el?.setSelectionRange(el.value.length, el.value.length);
    });
  };
  useEffect(() => {
    if (!mention) return;
    startWith(`@${mention} `);
    onMentioned();
  }, [mention]);
  useEffect(() => {
    if (prefill) startWith(prefill.text);
    else if (autoFocus) composer.current?.focus();
  }, [prefill?.at]);

  // The message shows at once and the composer clears; if sending fails,
  // the text comes back so nothing is lost.
  const send = async (e) => {
    e.preventDefault();
    if (!text.trim()) return;
    const sent = text;
    const item = linked && mentionedItem ? mentionedItem : null;
    setText("");
    setNeedsAction(false);
    if (!parent) setLinked(false);
    const ok = await post(
      {
        body: sent,
        parent_id: parent?.id,
        item_id: item?.id,
        action_required: needsAction && canAsk ? true : undefined,
      },
      { item: item ? { id: item.id, title: item.title, status: item.status } : undefined },
    );
    if (!ok) setText((t) => t || sent);
  };

  return (
    <form className="composer" onSubmit={send}>
      <Textarea
        ref={composer}
        aria-label={parent ? `Reply in thread #${parent.id}` : "Message"}
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && !e.shiftKey) send(e);
        }}
        rows={parent ? 2 : 3}
        placeholder={placeholder}
      />
      <div className="composer-foot">
        {mentionedItem ? (
          <button
            type="button"
            className={linked ? "chip link-item linked" : "chip link-item"}
            aria-pressed={linked}
            title={linked ? "The message will be tied to this backlog item" : "Tie the message to this backlog item"}
            onClick={() => setLinked(!linked)}
          >
            <ListChecks aria-hidden="true" />
            {linked ? "Linked to" : "Link to"} #{mentionedItem.id} <span className="link-item-title">{mentionedItem.title}</span>
          </button>
        ) : (
          <span className="hint">Enter to send · Shift+Enter for a new line</span>
        )}
        {canAsk && (
          <button
            type="button"
            className={needsAction ? "chip needs-action-toggle on" : "chip needs-action-toggle"}
            aria-pressed={needsAction}
            title="Ask the people you @mention to act: it stands out for them until they're done"
            onClick={() => {
              setNeedsAction(!needsAction);
              composer.current?.focus();
            }}
          >
            <CircleAlert aria-hidden="true" />
            Needs action
          </button>
        )}
      </div>
    </form>
  );
}

// itemMentioned is the first backlog item the text names as #id, if any.
function itemMentioned(text, items) {
  for (const m of text.matchAll(/(?:^|[^\w&/])#(\d+)\b/g)) {
    if (refersToPR(text.slice(Math.max(0, m.index - 16), m.index + m[0].indexOf("#")))) continue;
    const item = items.find((it) => it.id === Number(m[1]));
    if (item) return item;
  }
  return null;
}

// useShowMessage scrolls to the shown message once it is rendered, focuses
// it, and returns its id while it should be highlighted.
function useShowMessage(shown, messages) {
  const [flashed, setFlashed] = useState(null);
  const present = Boolean(shown && messages.some((m) => m.id === shown.id));
  useEffect(() => {
    if (!present) return;
    setFlashed(shown.id);
    // After the bottom-scroll and any dialog's focus restore.
    const go = setTimeout(() => {
      const el = document.getElementById(`msg-${shown.id}`);
      el?.scrollIntoView({ block: "center" });
      el?.focus({ preventScroll: true });
    }, 80);
    const done = setTimeout(() => setFlashed(null), 2500);
    return () => {
      clearTimeout(go);
      clearTimeout(done);
    };
  }, [present, shown?.at]);
  return flashed;
}

function Message({ message, onDone, isNew, participants, me, flashed, onReply, onReact, actionsOpen, onToggleActions, children }) {
  const author = participants.find((p) => p.id === message.author_id);
  const asksMe = Boolean(me) && (message.action_for || []).includes(me.id);
  const needsMe = asksMe && !(message.done_by || []).includes(me.id);
  const mentioned = isNew && me && message.author_id !== me.id && new RegExp(`@(${me.name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}|all|everyone)\\b`, "i").test(message.body);
  const created = new Date(message.created_at);
  const isReply = Boolean(message.parent_id);
  return (
    <div
      id={`msg-${message.id}`}
      tabIndex={-1}
      className={["msg", message.pending && "msg-pending", needsMe ? "needs-me" : !asksMe && mentioned && "mention", flashed === message.id && "msg-flash", actionsOpen && "msg-actions-open"].filter(Boolean).join(" ")}
    >
      {!isReply && (
        <span className="avatar" aria-hidden="true">
          {author?.emoji}
        </span>
      )}
      <div>
        <span className="who">{author ? (isReply ? `${author.emoji} ${author.name}` : author.name) : `participant ${message.author_id}`}</span>
        {author?.sponsor && <span className="agent-of">· agent of {author.sponsor.display_name}</span>}
        <span className="time" title={created.toLocaleString()}>
          {created.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })} · {message.pending ? "sending…" : `#${message.id}`}
        </span>
        <div className="text">
          <Markdown text={message.body} />
        </div>
        {message.item && (
          <a className="chip msg-item" title={`About backlog item #${message.item.id}: ${message.item.title}`} {...itemLink(message.item.id)}>
            <ListChecks aria-hidden="true" />#{message.item.id} <span className="msg-item-title">{message.item.title}</span>
          </a>
        )}
        {message.action_required && <ActionLine message={message} participants={participants} asksMe={asksMe} needsMe={needsMe} onDone={onDone} />}
        <Reactions reactions={message.reactions || []} participants={participants} me={me} onReact={onReact} />
        {children}
      </div>
      <div className="msg-actions" role="toolbar" aria-label={`Message #${message.id}`}>
        <Button variant="ghost" className="msg-action" title="Reply in thread" onClick={onReply}>
          <Reply aria-hidden="true" />
          Reply
        </Button>
        {REACTIONS.map(({ emoji, label }) => {
          const mine = message.reactions?.some((r) => r.emoji === emoji && r.by.includes(me?.id));
          return (
            <button key={emoji} type="button" className="msg-action msg-emoji" aria-label={`React ${label}`} aria-pressed={Boolean(mine)} title={label} onClick={() => onReact(emoji)}>
              {emoji}
            </button>
          );
        })}
      </div>
      {/* Touch screens have no hover: this shows the actions instead. */}
      <button type="button" className="msg-more" aria-label="Message actions" aria-expanded={actionsOpen} onClick={onToggleActions}>
        <Ellipsis aria-hidden="true" />
      </button>
    </div>
  );
}

// ActionLine says whom a message asks to act (a check for those done) and,
// when it asks you, lets you mark it done or undo that.
function ActionLine({ message, participants, asksMe, needsMe, onDone }) {
  const nameOf = (id) => participants.find((p) => p.id === id)?.name || `participant ${id}`;
  const done = message.done_by || [];
  return (
    <div className="action-line">
      <span className={needsMe ? "chip action-chip needs" : "chip action-chip"}>
        <CircleAlert aria-hidden="true" />
        {needsMe ? "Needs you" : "Needs"}
        {message.action_for.map((id) => (
          <span key={id} className={done.includes(id) ? "action-who done" : "action-who"}>
            @{nameOf(id)}
            {done.includes(id) && <Check aria-label="done" />}
          </span>
        ))}
      </span>
      {asksMe &&
        (needsMe ? (
          <Button variant="primary" size="small" className="action-done" onClick={() => onDone(true)}>
            <Check aria-hidden="true" />
            Done
          </Button>
        ) : (
          <Button variant="ghost" size="small" className="action-done" title="Mark it not done" onClick={() => onDone(false)}>
            <Undo2 aria-hidden="true" />
            Undo
          </Button>
        ))}
    </div>
  );
}

// withDone returns message m's done_by with you (myId) marked done (on, if
// it asks you to act) or not.
function withDone(m, myId, on) {
  const done = (m.done_by || []).filter((id) => id !== myId);
  return on && (m.action_for || []).includes(myId) ? [...done, myId] : done;
}

// withReaction returns reactions with participant id's reaction emoji on or
// off. A new emoji goes last, as the server orders them (first reacted first).
function withReaction(reactions, emoji, id, on) {
  const has = reactions.some((r) => r.emoji === emoji && r.by.includes(id));
  if (has === on) return reactions;
  if (!on) {
    return reactions
      .map((r) => (r.emoji === emoji ? { ...r, count: r.count - 1, by: r.by.filter((b) => b !== id) } : r))
      .filter((r) => r.count > 0);
  }
  if (!reactions.some((r) => r.emoji === emoji)) return [...reactions, { emoji, count: 1, by: [id] }];
  return reactions.map((r) => (r.emoji === emoji ? { ...r, count: r.count + 1, by: [...r.by, id] } : r));
}

// Reactions are a message's reaction chips: highlighted when one is yours,
// with who reacted in the tooltip; clicking one toggles your reaction.
function Reactions({ reactions, participants, me, onReact }) {
  if (reactions.length === 0) return null;
  const nameOf = (id) => (id === me?.id ? "you" : participants.find((p) => p.id === id)?.name || `participant ${id}`);
  return (
    <div className="reactions">
      {reactions.map((r) => {
        const mine = r.by.includes(me?.id);
        const label = REACTIONS.find((x) => x.emoji === r.emoji)?.label || r.emoji;
        const who = r.by.map(nameOf).join(", ");
        return (
          <button
            key={r.emoji}
            type="button"
            className={mine ? "reaction mine" : "reaction"}
            aria-pressed={mine}
            aria-label={`${label}, ${r.count}: ${who}`}
            title={who}
            onClick={() => onReact(r.emoji)}
          >
            <span aria-hidden="true">{r.emoji}</span>
            <span className="reaction-count">{r.count}</span>
          </button>
        );
      })}
    </div>
  );
}


// The thread panel's width, as dragged (#115): per browser, so it survives
// reloads. null means the default width.
const THREAD_WIDTH_KEY = "unison.threadWidth";
const THREAD_MIN = 280; // px; the chat keeps at least CHAT_MIN
const CHAT_MIN = 320;

function useStoredWidth() {
  const [width, setWidth] = useState(() => {
    try {
      return Number(localStorage.getItem(THREAD_WIDTH_KEY)) || null;
    } catch {
      return null;
    }
  });
  const save = (w) => {
    setWidth(w);
    try {
      if (w) localStorage.setItem(THREAD_WIDTH_KEY, String(w));
      else localStorage.removeItem(THREAD_WIDTH_KEY);
    } catch {
      // Storage may be unavailable (private windows); the width just won't stick.
    }
  };
  return [width, save];
}

// ThreadResizer is the draggable border between the chat and the thread
// panel: dragging left widens the thread, right narrows it. Arrow keys move
// it too; a double-click restores the default width.
function ThreadResizer({ body, width, onWidth }) {
  const clamp = (w) => {
    const total = body.current?.getBoundingClientRect().width || 0;
    return Math.round(Math.max(THREAD_MIN, Math.min(w, total - CHAT_MIN)));
  };
  const current = () => body.current?.querySelector(".thread-panel")?.getBoundingClientRect().width || width || 400;
  const onPointerDown = (e) => {
    if (e.button !== 0) return;
    e.preventDefault();
    const el = e.currentTarget;
    el.setPointerCapture(e.pointerId);
    const right = body.current.getBoundingClientRect().right;
    const move = (ev) => onWidth(clamp(right - ev.clientX));
    const up = () => {
      el.removeEventListener("pointermove", move);
      el.removeEventListener("pointerup", up);
      el.removeEventListener("pointercancel", up);
      document.documentElement.classList.remove("resizing");
    };
    el.addEventListener("pointermove", move);
    el.addEventListener("pointerup", up);
    el.addEventListener("pointercancel", up);
    document.documentElement.classList.add("resizing");
  };
  const onKeyDown = (e) => {
    const step = e.shiftKey ? 80 : 24;
    if (e.key === "ArrowLeft") onWidth(clamp(current() + step));
    else if (e.key === "ArrowRight") onWidth(clamp(current() - step));
    else return;
    e.preventDefault();
  };
  return (
    <div
      className="thread-resizer"
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize thread"
      aria-valuenow={width || undefined}
      title="Drag to resize · double-click to reset"
      tabIndex={0}
      onPointerDown={onPointerDown}
      onKeyDown={onKeyDown}
      onDoubleClick={() => onWidth(null)}
    />
  );
}
