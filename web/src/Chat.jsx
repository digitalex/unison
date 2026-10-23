import { useEffect, useRef, useState } from "react";

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
    <section>
      <h2>Chat</h2>
      <div style={{ maxHeight: "50vh", overflowY: "auto", border: "1px solid #ccc", padding: 4 }}>
        {roots.length === 0 && <p>No messages yet. Say hello.</p>}
        {roots.map((m) => (
          <div key={m.id}>
            <Message message={m} participants={participants} me={me} onReply={setReplyTo} />
            <div style={{ marginLeft: "2em" }}>
              {(replies.get(m.id) || []).map((r) => (
                <Message key={r.id} message={r} participants={participants} me={me} onReply={() => setReplyTo(m)} />
              ))}
            </div>
          </div>
        ))}
        <div ref={bottom} />
      </div>
      <form onSubmit={send}>
        {replyTo && (
          <p>
            Replying in thread #{replyTo.id} <button onClick={() => setReplyTo(null)}>cancel</button>
          </p>
        )}
        <textarea
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) send(e);
          }}
          rows={3}
          cols={90}
          placeholder="Message (Enter to send, Shift+Enter for a newline, @Name to mention)"
        />
        <br />
        <button type="submit">Send</button>
      </form>
    </section>
  );
}

function Message({ message, participants, me, onReply }) {
  const author = participants.find((p) => p.id === message.author_id);
  const mentioned = me && new RegExp(`@(${me.name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}|all|everyone)\\b`, "i").test(message.body);
  return (
    <div style={mentioned ? { background: "#fff3c4" } : undefined}>
      <small>
        #{message.id} {new Date(message.created_at).toLocaleTimeString()}
      </small>{" "}
      <b>{author ? `${author.emoji} ${author.name}` : `participant ${message.author_id}`}</b>:{" "}
      <span style={{ whiteSpace: "pre-wrap" }}>{message.body}</span>{" "}
      <button onClick={() => onReply(message)}>reply</button>
    </div>
  );
}
