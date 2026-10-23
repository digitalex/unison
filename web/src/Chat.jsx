import { useEffect, useRef, useState } from "react";
import Button from "./ui/Button.jsx";
import { Textarea } from "./ui/Input.jsx";

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
    <section className="section">
      <h2>Chat</h2>
      <div className="scroll-area">
        {roots.length === 0 && <p>No messages yet. Say hello.</p>}
        {roots.map((m) => (
          <div key={m.id}>
            <Message message={m} participants={participants} me={me} onReply={setReplyTo} />
            <div className="thread-replies">
              {(replies.get(m.id) || []).map((r) => (
                <Message key={r.id} message={r} participants={participants} me={me} onReply={() => setReplyTo(m)} />
              ))}
            </div>
          </div>
        ))}
        <div ref={bottom} />
      </div>
      <form className="section" onSubmit={send}>
        {replyTo && (
          <p className="row">
            Replying in thread #{replyTo.id}{" "}
            <Button variant="ghost" onClick={() => setReplyTo(null)}>
              cancel
            </Button>
          </p>
        )}
        <Textarea
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) send(e);
          }}
          rows={3}
          placeholder="Message (Enter to send, Shift+Enter for a newline, @Name to mention)"
        />
        <div className="row">
          <Button variant="primary" type="submit">
            Send
          </Button>
        </div>
      </form>
    </section>
  );
}

function Message({ message, participants, me, onReply }) {
  const author = participants.find((p) => p.id === message.author_id);
  const mentioned = me && new RegExp(`@(${me.name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}|all|everyone)\\b`, "i").test(message.body);
  return (
    <div className={mentioned ? "message message-mentioned" : "message"}>
      <small>
        #{message.id} {new Date(message.created_at).toLocaleTimeString()}
      </small>{" "}
      <b>{author ? `${author.emoji} ${author.name}` : `participant ${message.author_id}`}</b>:{" "}
      <span className="message-body">{message.body}</span>{" "}
      <Button variant="ghost" onClick={() => onReply(message)}>
        reply
      </Button>
    </div>
  );
}
