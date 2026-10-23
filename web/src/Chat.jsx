import { useEffect, useRef, useState } from "react";
import { Reply, X } from "lucide-react";
import Button from "./ui/Button.jsx";
import { Textarea } from "./ui/Input.jsx";
import Linkify from "./Linkify.jsx";

// The workspace chat room. Threads are one level deep: replies are listed
// under the message that started the thread.
export default function Chat({ messages, participants, me, call, onError }) {
  const [text, setText] = useState("");
  const [replyTo, setReplyTo] = useState(null);
  const bottom = useRef(null);

  useEffect(() => {
    bottom.current?.scrollIntoView({ block: "nearest" });
  }, [messages.length]);

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
      await call("POST", "/messages", { body: text, parent_id: replyTo?.id });
      setText("");
      setReplyTo(null);
    } catch (err) {
      onError(err.message);
    }
  };

  return (
    <aside className="chat panel" aria-label="Chat">
      <h2 className="chat-head">Chat</h2>
      <div className="chat-log">
        {roots.length === 0 && <p className="chat-empty">No messages yet. Say hello.</p>}
        {roots.map((m) => (
          <Message key={m.id} message={m} participants={participants} me={me} onReply={() => setReplyTo(m)}>
            {replies.has(m.id) && (
              <div className="thread">
                {replies.get(m.id).map((r) => (
                  <Message key={r.id} message={r} participants={participants} me={me} onReply={() => setReplyTo(m)} />
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
            <Button type="button" variant="ghost" iconOnly aria-label="Cancel reply" title="Cancel reply" onClick={() => setReplyTo(null)}>
              <X aria-hidden="true" />
            </Button>
          </p>
        )}
        <Textarea
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
          <span className="hint">Enter to send · Shift+Enter for a new line</span>
          <Button variant="primary" type="submit">
            Send
          </Button>
        </div>
      </form>
    </aside>
  );
}

function Message({ message, participants, me, onReply, children }) {
  const author = participants.find((p) => p.id === message.author_id);
  const mentioned = me && new RegExp(`@(${me.name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}|all|everyone)\\b`, "i").test(message.body);
  const created = new Date(message.created_at);
  const isReply = Boolean(message.parent_id);
  return (
    <div className={mentioned ? "msg mention" : "msg"}>
      {!isReply && (
        <span className="avatar" aria-hidden="true">
          {author?.emoji}
        </span>
      )}
      <div>
        <span className="who">{author ? (isReply ? `${author.emoji} ${author.name}` : author.name) : `participant ${message.author_id}`}</span>
        <span className="time" title={created.toLocaleString()}>
          {created.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })} · #{message.id}
        </span>
        <div className="text">
          <Linkify text={message.body} />
        </div>
        {children}
      </div>
      <Button variant="ghost" className="msg-reply" title="Reply in thread" onClick={onReply}>
        <Reply aria-hidden="true" />
        Reply
      </Button>
    </div>
  );
}
