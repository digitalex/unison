import { useEffect, useMemo, useState } from "react";
import { ApiError, getToken, setToken, workspaceApi } from "./api.js";
import Backlog from "./Backlog.jsx";
import Chat from "./Chat.jsx";
import Button from "./ui/Button.jsx";
import Field from "./ui/Field.jsx";
import { Input, Textarea } from "./ui/Input.jsx";

const ACTIVE_WINDOW_MS = 2 * 60 * 1000;

// Which collection each event type invalidates.
function collectionFor(type) {
  if (type.startsWith("message.")) return "messages";
  if (type.startsWith("backlog.")) return "backlog";
  if (type.startsWith("participant.")) return "participants";
  if (type.startsWith("workspace.")) return "workspace";
  return null;
}

export default function Workspace({ id, navigate }) {
  const call = useMemo(() => workspaceApi(id), [id]);
  const [workspace, setWorkspace] = useState(null);
  const [me, setMe] = useState(null);
  const [joined, setJoined] = useState(Boolean(getToken(id)));
  const [participants, setParticipants] = useState([]);
  const [messages, setMessages] = useState([]);
  const [backlog, setBacklog] = useState([]);
  const [live, setLive] = useState(false);
  const [error, setError] = useState("");

  const loaders = {
    workspace: () => call("GET").then(setWorkspace),
    participants: () => call("GET", "/participants").then(setParticipants),
    messages: () => call("GET", "/messages?limit=500").then(setMessages),
    backlog: () => call("GET", "/backlog?status=all").then(setBacklog),
  };
  const reload = (name) => loaders[name]().catch((e) => setError(e.message));

  // Initial load and identity check.
  useEffect(() => {
    Object.keys(loaders).forEach(reload);
    if (!joined) return;
    call("GET", "/me").then(setMe, (e) => {
      if (e instanceof ApiError && (e.status === 401 || e.status === 403)) {
        setToken(id, "");
        setJoined(false);
      } else setError(e.message);
    });
  }, [id, joined]);

  // Live updates: every event refreshes the collection it touches. Holding
  // the stream open also keeps us present in the roster.
  useEffect(() => {
    const src = new EventSource(`/api/workspaces/${id}/stream?token=${encodeURIComponent(getToken(id))}`);
    src.onopen = () => setLive(true);
    src.onerror = () => setLive(false);
    src.onmessage = (msg) => {
      const name = collectionFor(JSON.parse(msg.data).type);
      if (name) reload(name);
    };
    const roster = setInterval(() => reload("participants"), 30000);
    return () => {
      src.close();
      clearInterval(roster);
    };
  }, [id, joined]);

  if (!workspace) return <main className="page">{error ? <p role="alert">Error: {error}</p> : <p>Loading…</p>}</main>;

  return (
    <main className="page">
      <p className="meta">
        <a
          href="/"
          onClick={(e) => {
            e.preventDefault();
            navigate("/");
          }}
        >
          ← All workspaces
        </a>
      </p>
      <h1>{workspace.name}</h1>
      {error && (
        <p role="alert" className="row">
          Error: {error}{" "}
          <Button variant="ghost" onClick={() => setError("")}>
            dismiss
          </Button>
        </p>
      )}
      <WorkspaceLinks workspace={workspace} call={call} canEdit={Boolean(me)} onError={setError} />
      <p className="meta">
        <small>{live ? "🟢 live" : "🔴 reconnecting…"}</small>
        {me && (
          <small>
            {" "}
            · You are {me.emoji} {me.name}
          </small>
        )}
      </p>
      <Invite workspace={workspace} />
      {!joined ? (
        <JoinForm id={id} call={call} onJoined={() => setJoined(true)} />
      ) : (
        <>
          <Roster participants={participants} me={me} call={call} onError={setError} />
          <Backlog items={backlog} participants={participants} me={me} call={call} onError={setError} />
          <Chat messages={messages} participants={participants} me={me} call={call} onError={setError} />
        </>
      )}
    </main>
  );
}

function JoinForm({ id, call, onJoined }) {
  const [name, setName] = useState("");
  const [emoji, setEmoji] = useState("");
  const [error, setError] = useState("");
  const join = async (e) => {
    e.preventDefault();
    try {
      const resp = await call("POST", "/join", { name, emoji });
      setToken(id, resp.token);
      onJoined();
    } catch (err) {
      setError(err.message);
    }
  };
  return (
    <form className="form" onSubmit={join}>
      <h2>Join this workspace</h2>
      {error && (
        <p role="alert" className="field-error">
          {error}
        </p>
      )}
      <Field label="Name">
        <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="one word" />
      </Field>
      <Field label="Emoji">
        <Input value={emoji} onChange={(e) => setEmoji(e.target.value)} placeholder="🦊" />
      </Field>
      <p className="field-description">Leave either blank to be assigned one.</p>
      <div className="row">
        <Button variant="primary" type="submit">
          Join
        </Button>
      </div>
    </form>
  );
}

function WorkspaceLinks({ workspace, call, canEdit, onError }) {
  const [editing, setEditing] = useState(false);
  const [form, setForm] = useState(workspace);
  const save = async (e) => {
    e.preventDefault();
    try {
      await call("PATCH", "", { name: form.name, repo_url: form.repo_url, canvas_url: form.canvas_url });
      setEditing(false);
    } catch (err) {
      onError(err.message);
    }
  };
  if (editing) {
    const field = (key) => ({ value: form[key], onChange: (e) => setForm({ ...form, [key]: e.target.value }) });
    return (
      <form className="form" onSubmit={save}>
        <Field label="Name">
          <Input {...field("name")} />
        </Field>
        <Field label="Repository">
          <Input {...field("repo_url")} />
        </Field>
        <Field label="Canvas">
          <Input {...field("canvas_url")} />
        </Field>
        <div className="row">
          <Button variant="primary" type="submit">
            Save
          </Button>
          <Button onClick={() => setEditing(false)}>Cancel</Button>
        </div>
      </form>
    );
  }
  const link = (url) => (url ? <a href={url} target="_blank" rel="noreferrer">{url}</a> : <span className="meta">not set</span>);
  return (
    <ul className="list">
      <li>Repository: {link(workspace.repo_url)}</li>
      <li>Isocan canvas: {link(workspace.canvas_url)}</li>
      {canEdit && (
        <li>
          <Button
            onClick={() => {
              setForm(workspace);
              setEditing(true);
            }}
          >
            Edit workspace
          </Button>
        </li>
      )}
    </ul>
  );
}

function Invite({ workspace }) {
  const [open, setOpen] = useState(false);
  const doorway = `${window.location.origin}/w/${workspace.id}/agent.md`;
  const prompt =
    `You're invited to collaborate in the Unison workspace "${workspace.name}". ` +
    `Fetch ${doorway} (e.g. curl -s ${doorway}) and follow it: install the unison CLI, ` +
    `join with a name and emoji of your own, say hello in chat, pick up work from the backlog, ` +
    `and park on \`unison wait\` between laps.`;
  return (
    <section className="section">
      <div className="row">
        <Button onClick={() => setOpen(!open)}>{open ? "Hide invite" : "Invite an agent"}</Button>
      </div>
      {open && (
        <div className="section">
          <p>Paste this into any coding agent:</p>
          <Textarea readOnly value={prompt} rows={4} onFocus={(e) => e.target.select()} />
          <div className="row">
            <Button onClick={() => navigator.clipboard.writeText(prompt)}>Copy</Button>
          </div>
        </div>
      )}
    </section>
  );
}

function Roster({ participants, me, call, onError }) {
  const setStatus = async () => {
    const status = window.prompt("Your status", me?.status || "");
    if (status === null) return;
    try {
      await call("PATCH", "/me", { status });
    } catch (err) {
      onError(err.message);
    }
  };
  const now = Date.now();
  return (
    <section className="section">
      <h2>Who's here</h2>
      <ul className="list">
        {participants.map((p) => (
          <li key={p.id}>
            {now - p.last_seen_at < ACTIVE_WINDOW_MS ? "🟢" : "⚪"} {p.emoji} <b>{p.name}</b>
            {p.status && <> — {p.status}</>}
            {me && p.id === me.id && (
              <>
                {" "}
                <Button variant="ghost" onClick={setStatus}>
                  set status
                </Button>
              </>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}
