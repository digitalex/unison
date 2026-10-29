import { useEffect, useRef, useState } from "react";
import { Check, CircleAlert, CircleCheck, Ellipsis, ListChecks, ListPlus, MessageSquare, Reply, RotateCcw, Undo2, UserMinus, UserPlus, X } from "lucide-react";
import { itemLink } from "./nav.js";
import Button from "./ui/Button.jsx";
import { Textarea } from "./ui/Input.jsx";
import Markdown from "./Markdown.jsx";
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

  useEffect(() => {
    bottom.current?.scrollIntoView({ block: "nearest" });
  }, [messages.length, activity.length]);

  const byId = new Map(messages.map((m) => [m.id, m]));
  const replies = new Map();
  for (const m of messages) {
    if (m.parent_id && byId.has(m.parent_id)) {
      replies.set(m.parent_id, [...(replies.get(m.parent_id) || []), m]);
    }
  }
  const roots = messages.filter((m) => !m.parent_id || !byId.has(m.parent_id));
  const log = byTime(roots, activity);
  const thread = threadId ? byId.get(threadId) : null;
  // You, on any of your account's devices: actions are per account.
  const mine = new Set(participants.filter((p) => p.id === me?.id || (me?.user_id && p.user_id === me.user_id)).map((p) => p.id));
  if (me) mine.add(me.id);

  useOpenShownThread(shown, byId, setThreadId);
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
    const show = (on) => updateMessages((list) => list.map((x) => (x.id === m.id ? { ...x, done_by: withDone(x, mine, on) } : x)));
    show(done);
    try {
      await call(done ? "POST" : "DELETE", `/messages/${m.id}/done`);
    } catch (err) {
      show(!done);
      onError(err.message);
    }
  };
  const messageProps = (m, root) => ({
    message: m,
    mine,
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
  const composerProps = { items, call, onError };

  return (
    <section className={thread ? "screen chat chat-thread-open" : "screen chat"} aria-label="Chat">
      <ScreenHead title="Chat">{head}</ScreenHead>
      {error}
      <div className="chat-body">
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
                    newAfter={newAfter}
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
    case "backlog.added":
      return [name(actor), "added", d];
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

// ActivityLine is one activity event in the main log: a small pill on a
// divider, unlike a message (no author block, reactions or thread).
function ActivityLine({ event, participants }) {
  const [who, rest, item] = activityText(event, participants);
  const Icon = ACTIVITY_ICONS[event.type] || ListChecks;
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
// stands out when the thread has replies new to you; clicking it opens (or
// closes) the thread.
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
function Composer({ parent, items, call, onError, mention, onMentioned, prefill, placeholder, autoFocus }) {
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

  const send = async (e) => {
    e.preventDefault();
    if (!text.trim()) return;
    try {
      await call("POST", "/messages", {
        body: text,
        parent_id: parent?.id,
        item_id: linked && mentionedItem ? mentionedItem.id : undefined,
        action_required: needsAction && canAsk ? true : undefined,
      });
      setText("");
      setNeedsAction(false);
      if (!parent) setLinked(false);
    } catch (err) {
      onError(err.message);
    }
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

function Message({ message, mine, onDone, isNew, participants, me, flashed, onReply, onReact, actionsOpen, onToggleActions, children }) {
  const author = participants.find((p) => p.id === message.author_id);
  const asksMe = (message.action_for || []).some((id) => mine.has(id));
  const needsMe = asksMe && (message.action_for || []).some((id) => mine.has(id) && !(message.done_by || []).includes(id));
  const mentioned = isNew && me && new RegExp(`@(${me.name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}|all|everyone)\\b`, "i").test(message.body);
  const created = new Date(message.created_at);
  const isReply = Boolean(message.parent_id);
  return (
    <div
      id={`msg-${message.id}`}
      tabIndex={-1}
      className={["msg", needsMe ? "needs-me" : !asksMe && mentioned && "mention", flashed === message.id && "msg-flash", actionsOpen && "msg-actions-open"].filter(Boolean).join(" ")}
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
          {created.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })} · #{message.id}
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

// withDone returns message m's done_by with your participants (mine) that
// it asks to act marked done (on) or not.
function withDone(m, mine, on) {
  const done = (m.done_by || []).filter((id) => !mine.has(id));
  return on ? [...done, ...(m.action_for || []).filter((id) => mine.has(id))] : done;
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

