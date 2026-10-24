import { useEffect, useMemo, useState } from "react";
import { FolderGit2, LayoutGrid, ArrowUpRight, Settings, UserPlus, X } from "lucide-react";
import { ApiError, getToken, setToken, workspaceApi } from "./api.js";
import Backlog from "./Backlog.jsx";
import Chat from "./Chat.jsx";
import Metrics from "./Metrics.jsx";
import Webhooks from "./Webhooks.jsx";
import Button from "./ui/Button.jsx";
import Field from "./ui/Field.jsx";
import { Input, Textarea } from "./ui/Input.jsx";
import Popover from "./ui/Popover.jsx";
import ThemeSwitch from "./ui/ThemeSwitch.jsx";

const ACTIVE_WINDOW_MS = 2 * 60 * 1000;

// Which collection each event type invalidates.
function collectionFor(type) {
  if (type.startsWith("message.")) return "messages";
  if (type.startsWith("backlog.") || type.startsWith("comment.") || type.startsWith("pr.")) return "backlog";
  if (type.startsWith("metric.")) return "metrics";
  if (type.startsWith("webhook.")) return "webhooks";
  if (type.startsWith("participant.")) return "participants";
  if (type.startsWith("workspace.") || type.startsWith("repo.")) return "workspace";
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
  const [metrics, setMetrics] = useState([]);
  const [webhooks, setWebhooks] = useState([]);
  const [live, setLive] = useState(false);
  const [error, setError] = useState("");

  const loaders = {
    workspace: () => call("GET").then(setWorkspace),
    participants: () => call("GET", "/participants").then(setParticipants),
    messages: () => call("GET", "/messages?limit=500").then(setMessages),
    backlog: () => call("GET", "/backlog?status=all").then(setBacklog),
    metrics: () => call("GET", "/metrics").then(setMetrics),
    webhooks: () => call("GET", "/webhooks").then(setWebhooks),
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
    <div className="app">
      <TopBar
        workspace={workspace}
        webhooks={webhooks}
        me={me}
        joined={joined}
        participants={participants}
        live={live}
        call={call}
        onError={setError}
        navigate={navigate}
      />
      <div className={joined ? "app-body" : "app-body app-body-single"}>
        <main className={joined ? "app-main panel" : "app-main"}>
          {error && (
            <p role="alert" className="error-bar">
              Error: {error}{" "}
              <Button variant="ghost" onClick={() => setError("")}>
                dismiss
              </Button>
            </p>
          )}
          {joined ? (
            <>
              <Metrics metrics={metrics} items={backlog} call={call} onError={setError} />
              <Backlog items={backlog} participants={participants} me={me} workspace={workspace} call={call} onError={setError} />
            </>
          ) : (
            <JoinForm id={id} call={call} onJoined={() => setJoined(true)} />
          )}
        </main>
        {joined && <Chat messages={messages} participants={participants} me={me} call={call} onError={setError} />}
      </div>
    </div>
  );
}

function TopBar({ workspace, webhooks, me, joined, participants, live, call, onError, navigate }) {
  return (
    <header className="topbar">
      <div className="ws">
        <a
          className="ws-mark"
          href="/"
          aria-label="All workspaces"
          title="All workspaces"
          onClick={(e) => {
            e.preventDefault();
            navigate("/");
          }}
        >
          U
        </a>
        <h1 className="ws-name">{workspace.name}</h1>
      </div>
      {joined && <span className="crumb">/ Backlog</span>}
      {workspace.canvas_url && (
        <a
          className="canvas-link"
          href={workspace.canvas_url}
          target="_blank"
          rel="noreferrer"
          title="Open the design canvas"
          aria-label="Canvas (opens in a new tab)"
        >
          <LayoutGrid aria-hidden="true" />
          <span className="label">Canvas</span>
          <ArrowUpRight aria-hidden="true" />
        </a>
      )}
      <RepoLinks repos={workspace.repos || []} />
      <span className="spacer" />
      <span className={live ? "live live-on" : "live"}>{live ? "live" : "reconnecting…"}</span>
      <ThemeSwitch />
      <Popover
        title="Invite an agent"
        trigger={
          <>
            <UserPlus aria-hidden="true" />
            <span className="label">Invite</span>
          </>
        }
      >
        {() => <Invite workspace={workspace} />}
      </Popover>
      <Roster participants={participants} me={me} call={call} onError={onError} />
      {me && (
        <Popover title="Workspace settings" className="button button-ghost button-icon" trigger={<Settings aria-hidden="true" />}>
          {(close) => <WorkspaceSettings workspace={workspace} webhooks={webhooks} call={call} onError={onError} onDone={close} />}
        </Popover>
      )}
    </header>
  );
}

// One repository links directly; several open a small list.
function RepoLinks({ repos }) {
  if (repos.length === 0) return null;
  if (repos.length === 1) {
    return (
      <a className="icon-link" href={repos[0].url} target="_blank" rel="noreferrer" title={`Repository: ${repos[0].url}`}>
        <FolderGit2 aria-label="Repository" />
      </a>
    );
  }
  return (
    <Popover
      title="Repositories"
      align="start"
      className="button button-ghost repo-trigger"
      trigger={
        <>
          <FolderGit2 aria-hidden="true" />
          <span className="repo-count">{repos.length}</span>
        </>
      }
    >
      {() => (
        <>
          <h2 className="popover-title">Repositories</h2>
          <ul className="repo-list">
            {repos.map((r) => (
              <li key={r.id}>
                <a href={r.url} target="_blank" rel="noreferrer">
                  <span className="repo-label">{r.label || repoName(r.url)}</span>
                  <span className="repo-url">{r.url}</span>
                </a>
              </li>
            ))}
          </ul>
        </>
      )}
    </Popover>
  );
}

function repoName(url) {
  return url.replace(/\/+$/, "").split("/").pop().replace(/\.git$/, "");
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
    <form className="form card join-card" onSubmit={join}>
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
      <div className="actions-row">
        <Button variant="primary" type="submit">
          Join
        </Button>
      </div>
    </form>
  );
}

function WorkspaceSettings({ workspace, webhooks, call, onError, onDone }) {
  const [form, setForm] = useState(workspace);
  const save = async (e) => {
    e.preventDefault();
    try {
      await call("PATCH", "", { name: form.name, canvas_url: form.canvas_url });
      onDone();
    } catch (err) {
      onError(err.message);
    }
  };
  const field = (key) => ({ value: form[key], onChange: (e) => setForm({ ...form, [key]: e.target.value }) });
  return (
    <>
      <form className="form" onSubmit={save}>
        <h2 className="popover-title">Workspace settings</h2>
        <Field label="Name">
          <Input {...field("name")} />
        </Field>
        <Field label="Canvas">
          <Input {...field("canvas_url")} />
        </Field>
        <div className="actions-row">
          <Button variant="primary" type="submit">
            Save
          </Button>
          <Button type="button" onClick={onDone}>
            Cancel
          </Button>
        </div>
      </form>
      <Repositories repos={workspace.repos || []} call={call} onError={onError} />
      <Webhooks webhooks={webhooks} call={call} onError={onError} />
    </>
  );
}

// Repository changes apply immediately (each is its own API call).
function Repositories({ repos, call, onError }) {
  const [url, setUrl] = useState("");
  const [label, setLabel] = useState("");
  const run = (method, suffix, body) =>
    call(method, suffix, body).then(
      () => true,
      (e) => {
        onError(e.message);
        return false;
      },
    );
  const add = async (e) => {
    e.preventDefault();
    if (await run("POST", "/repos", { url, label })) {
      setUrl("");
      setLabel("");
    }
  };
  return (
    <section className="settings-section" aria-label="Repositories">
      <h3 className="field-label">Repositories</h3>
      {repos.map((r) => (
        <RepoRow key={r.id} repo={r} run={run} />
      ))}
      <form className="repo-form" onSubmit={add}>
        <Input className="repo-label-input" aria-label="Label (optional)" value={label} onChange={(e) => setLabel(e.target.value)} placeholder="Label" />
        <Input type="url" required aria-label="Repository URL" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://github.com/owner/repo" />
        <Button type="submit">Add</Button>
      </form>
    </section>
  );
}

function RepoRow({ repo, run }) {
  const [label, setLabel] = useState(repo.label);
  const [url, setUrl] = useState(repo.url);
  const dirty = label !== repo.label || url !== repo.url;
  const save = (e) => {
    e.preventDefault();
    run("PATCH", `/repos/${repo.id}`, { label, url });
  };
  return (
    <form className="repo-form" onSubmit={save}>
      <Input className="repo-label-input" aria-label={`Label for ${repo.url}`} value={label} onChange={(e) => setLabel(e.target.value)} placeholder="Label" />
      <Input type="url" required aria-label="Repository URL" value={url} onChange={(e) => setUrl(e.target.value)} />
      {dirty && <Button type="submit">Save</Button>}
      <Button
        type="button"
        iconOnly
        variant="ghost"
        className="button-danger"
        aria-label={`Remove ${repo.url}`}
        title="Remove"
        onClick={() => window.confirm(`Remove ${repo.url} from this workspace?`) && run("DELETE", `/repos/${repo.id}`)}
      >
        <X aria-hidden="true" />
      </Button>
    </form>
  );
}

function Invite({ workspace }) {
  const doorway = `${window.location.origin}/w/${workspace.id}/agent.md`;
  const prompt =
    `You're invited to collaborate in the Unison workspace "${workspace.name}". ` +
    `Fetch ${doorway} (e.g. curl -s ${doorway}) and follow it: install the unison CLI, ` +
    `join with a name and emoji of your own, say hello in chat, pick up work from the backlog, ` +
    `and park on \`unison wait\` between laps.`;
  return (
    <>
      <p>Paste this into any coding agent:</p>
      <Textarea readOnly value={prompt} rows={6} onFocus={(e) => e.target.select()} />
      <div className="actions-row">
        <Button type="button" onClick={() => navigator.clipboard.writeText(prompt)}>
          Copy
        </Button>
      </div>
    </>
  );
}

// Who's here: overlapping emoji avatars with presence dots. Your own avatar
// sets your status.
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
    <div className="avatars" role="list" aria-label="Who's here">
      {participants.map((p, i) => {
        const active = now - p.last_seen_at < ACTIVE_WINDOW_MS;
        const mine = me && p.id === me.id;
        const label = `${p.name}${mine ? " (you)" : ""} — ${p.status || (active ? "active" : "away")}`;
        const content = (
          <>
            {p.emoji}
            <span className="dot" />
          </>
        );
        const cls = active ? "avatar active" : "avatar";
        return (
          // Earlier avatars stack on top so every presence dot stays visible.
          <span key={p.id} role="listitem" style={{ zIndex: participants.length - i }}>
            {mine ? (
              <button type="button" className={cls} title={`${label} · set status`} aria-label={`${label}. Set status`} onClick={setStatus}>
                {content}
              </button>
            ) : (
              <span className={cls} title={label} aria-label={label} role="img">
                {content}
              </span>
            )}
          </span>
        );
      })}
    </div>
  );
}
