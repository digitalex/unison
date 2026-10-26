import { useEffect, useRef, useState } from "react";
import { Ellipsis, ListChecks, MessageSquare, Reply, X } from "lucide-react";
import { itemLink } from "./nav.js";
import Button from "./ui/Button.jsx";
import { Textarea } from "./ui/Input.jsx";
import Markdown from "./Markdown.jsx";
import SectionToggle from "./ui/SectionToggle.jsx";

// The workspace chat room. Threads are one level deep: replies are listed
// under the message that started the thread. It can be minimized (open and
// onToggle come from Workspace, which also gives the space back to the backlog).
// A message can be about a backlog item: it shows a chip that opens the item.
// `shown` ({id, at}) is a message to scroll to and highlight. Hovering (or
// focusing) a message shows its actions: reply, and the reactions.

// REACTIONS are the emoji anyone can put on a message (the server accepts
// exactly these).
const REACTIONS = [
  { emoji: "👍", label: "Thumbs up" },
  { emoji: "👎", label: "Thumbs down" },
  { emoji: "❤️", label: "Heart" },
  { emoji: "💪", label: "Muscle" },
  { emoji: "💯", label: "100" },
];

export default function Chat({ workspaceId, messages, items, participants, me, call, onError, open, onToggle, shown }) {
  const [text, setText] = useState("");
  const [replyTo, setReplyTo] = useState(null);
  const [linked, setLinked] = useState(false); // Tie the message to the item it mentions.
  const [replyItem, setReplyItem] = useState(null); // The item of the message replied to.
  const [actionsFor, setActionsFor] = useState(null); // Touch: the message whose actions are shown.
  const composer = useRef(null);
  const mentionedItem = itemMentioned(text, items) || replyItem;
  const flashed = useShowMessage(shown, messages, open);
  const seenKey = `unison.chat.seen.${workspaceId}`;
  const [seenId, setSeenId] = useState(() => Number(localStorage.getItem(seenKey)) || 0);
  const latestId = messages.reduce((max, m) => Math.max(max, m.id), 0);
  const bottom = useRef(null);

  useEffect(() => {
    bottom.current?.scrollIntoView({ block: "nearest" });
  }, [messages.length, open]);

  // While chat is open everything is seen; remember the newest message so a
  // minimized chat can count what arrived since (across reloads too).
  useEffect(() => {
    if (open && latestId > seenId) {
      localStorage.setItem(seenKey, String(latestId));
      setSeenId(latestId);
    }
  }, [open, latestId, seenId, seenKey]);

  const byId = new Map(messages.map((m) => [m.id, m]));
  const replies = new Map();
  for (const m of messages) {
    if (m.parent_id && byId.has(m.parent_id)) {
      replies.set(m.parent_id, [...(replies.get(m.parent_id) || []), m]);
    }
  }
  const roots = messages.filter((m) => !m.parent_id || !byId.has(m.parent_id));

  const send = async (e) => {
    e.preventDefault();
    if (!text.trim()) return;
    try {
      await call("POST", "/messages", { body: text, parent_id: replyTo?.id, item_id: linked && mentionedItem ? mentionedItem.id : undefined });
      setText("");
      cancelReply();
    } catch (err) {
      onError(err.message);
    }
  };

  // Reply: in the message's thread, to its author (prefilled @mention),
  // about the same item if it has one.
  const startReply = (root, target) => {
    setReplyTo(root);
    const author = participants.find((p) => p.id === target.author_id);
    const mention = author && author.id !== me?.id ? `@${author.name} ` : "";
    if (mention && !text.startsWith(mention)) setText(mention + text);
    if (target.item) {
      setReplyItem(target.item);
      setLinked(true);
    }
    setActionsFor(null);
    composer.current?.focus();
  };
  const cancelReply = () => {
    setReplyTo(null);
    setReplyItem(null);
    setLinked(false);
  };
  // react toggles your reaction (the server says what everyone sees).
  const react = async (m, emoji) => {
    const mine = m.reactions?.some((r) => r.emoji === emoji && r.by.includes(me?.id));
    try {
      await call(mine ? "DELETE" : "PUT", `/messages/${m.id}/reactions/${encodeURIComponent(emoji)}`);
    } catch (err) {
      onError(err.message);
    }
  };
  const messageProps = (m, root) => ({
    message: m,
    participants,
    me,
    flashed,
    onReply: () => startReply(root, m),
    onReact: (emoji) => react(m, emoji),
    actionsOpen: actionsFor === m.id,
    onToggleActions: () => setActionsFor(actionsFor === m.id ? null : m.id),
  });

  if (!open) {
    return <MinimizedChat unread={messages.filter((m) => m.id > seenId).length} onOpen={() => onToggle(true)} />;
  }

  return (
    <aside className="chat panel" aria-label="Chat">
      <h2 className="chat-head">
        <SectionToggle open={open} onToggle={onToggle}>
          Chat
        </SectionToggle>
      </h2>
      <div className="chat-log">
        {roots.length === 0 && <p className="chat-empty">No messages yet. Say hello.</p>}
        {roots.map((m) => (
          <Message key={m.id} {...messageProps(m, m)}>
            {replies.has(m.id) && (
              <div className="thread">
                {replies.get(m.id).map((r) => (
                  <Message key={r.id} {...messageProps(r, m)} />
                ))}
              </div>
            )}
          </Message>
        ))}
        <div ref={bottom} />
      </div>
      <form className="composer" onSubmit={send}>
        {replyTo && (
          <p className="replying">
            Replying in thread #{replyTo.id}
            <Button type="button" variant="ghost" iconOnly aria-label="Cancel reply" title="Cancel reply" onClick={cancelReply}>
              <X aria-hidden="true" />
            </Button>
          </p>
        )}
        <Textarea
          ref={composer}
          aria-label="Message"
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) send(e);
          }}
          rows={3}
          placeholder="Message the workspace…  @Name to mention"
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
          <Button variant="primary" type="submit">
            Send
          </Button>
        </div>
      </form>
    </aside>
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

// useShowMessage scrolls to the shown message once it is rendered (and chat
// is open), focuses it, and returns its id while it should be highlighted.
function useShowMessage(shown, messages, open) {
  const [flashed, setFlashed] = useState(null);
  const present = Boolean(shown && open && messages.some((m) => m.id === shown.id));
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

function Message({ message, participants, me, flashed, onReply, onReact, actionsOpen, onToggleActions, children }) {
  const author = participants.find((p) => p.id === message.author_id);
  const mentioned = me && new RegExp(`@(${me.name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}|all|everyone)\\b`, "i").test(message.body);
  const created = new Date(message.created_at);
  const isReply = Boolean(message.parent_id);
  return (
    <div
      id={`msg-${message.id}`}
      tabIndex={-1}
      className={["msg", mentioned && "mention", flashed === message.id && "msg-flash", actionsOpen && "msg-actions-open"].filter(Boolean).join(" ")}
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

// The minimized chat: a slim rail (a header row on narrow screens) that shows
// how many messages arrived since it was minimized.
function MinimizedChat({ unread, onOpen }) {
  const label = unread > 0 ? `Open chat, ${unread} new message${unread === 1 ? "" : "s"}` : "Open chat";
  return (
    <aside className="chat chat-minimized panel" aria-label="Chat">
      <button type="button" className="chat-rail" aria-expanded={false} aria-label={label} title={label} onClick={onOpen}>
        <MessageSquare aria-hidden="true" />
        <span className="chat-rail-label">Chat</span>
        {unread > 0 && <span className="chat-unread">{unread}</span>}
      </button>
    </aside>
  );
}
