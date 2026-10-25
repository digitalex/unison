import { useEffect, useMemo, useState } from "react";
import { FolderGit2, KeyRound, LayoutGrid, ArrowUpRight, RefreshCw, Settings, UserPlus, X } from "lucide-react";
import { ApiError, getToken, setToken, workspaceApi } from "./api.js";
import { AccountMenu, ClaimPrompt, Members } from "./Account.jsx";
import Backlog from "./Backlog.jsx";
import Chat from "./Chat.jsx";
import Webhooks from "./Webhooks.jsx";
import Button from "./ui/Button.jsx";
import Field from "./ui/Field.jsx";
import { Input, Textarea } from "./ui/Input.jsx";
import Popover from "./ui/Popover.jsx";
import ThemeSwitch from "./ui/ThemeSwitch.jsx";
import useSectionOpen from "./useSectionOpen.js";
import Splitter, { useChatWidth } from "./ui/Splitter.jsx";

const ACTIVE_WINDOW_MS = 2 * 60 * 1000;

// Which collections each event type invalidates.
function collectionFor(type) {
  if (type.startsWith("message.")) return ["messages"];
  if (type.startsWith("backlog.") || type.startsWith("comment.") || type.startsWith("pr.")) return ["backlog"];
  if (type.startsWith("webhook.")) return ["webhooks"];
  // Members list each member's participants (names, emoji, links).
  if (type === "participant.claimed") return ["participants", "members", "me"];
  if (type.startsWith("participant.")) return ["participants", "members"];
  if (type.startsWith("member.")) return ["members"];
  if (type.startsWith("workspace.") || type.startsWith("repo.")) return ["workspace"];
  return [];
}

export default function Workspace({ id, navigate, account }) {
  const call = useMemo(() => workspaceApi(id), [id]);
  const [workspace, setWorkspace] = useState(null);
  const [me, setMe] = useState(null);
  const [joined, setJoined] = useState(Boolean(getToken(id)));
  const [participants, setParticipants] = useState([]);
  const [messages, setMessages] = useState([]);
  const [backlog, setBacklog] = useState([]);
  const [webhooks, setWebhooks] = useState([]);
  const [members, setMembers] = useState({ members: [], unlinked: [] });
  const [live, setLive] = useState(false);
  const [chatOpen, setChatOpen] = useSectionOpen("chat", true);
  const chatWidth = useChatWidth();
  const [error, setError] = useState("");

  const loaders = {
    workspace: () => call("GET").then(setWorkspace),
    participants: () => call("GET", "/participants").then(setParticipants),
    messages: () => call("GET", "/messages?limit=500").then(setMessages),
    backlog: () => call("GET", "/backlog?status=all").then(setBacklog),
    webhooks: () => call("GET", "/webhooks").then(setWebhooks),
    members: () => call("GET", "/members").then(setMembers),
  };
  const reload = (name) =>
    (name === "me" ? (joined ? call("GET", "/me").then(setMe) : Promise.resolve()) : loaders[name]()).catch((e) => setError(e.message));

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
      collectionFor(JSON.parse(msg.data).type).forEach(reload);
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
        account={account}
        members={members}
      />
      <div
        className={!joined ? "app-body app-body-single" : chatOpen ? "app-body app-body-resizable" : "app-body app-body-chat-minimized"}
        style={joined && chatOpen ? { "--chat-width": `${chatWidth.width}px` } : undefined}
      >
        <main className={joined ? "app-main panel" : "app-main"}>
          {error && (
            <p role="alert" className="error-bar">
              Error: {error}{" "}
              <Button variant="ghost" onClick={() => setError("")}>
                dismiss
              </Button>
            </p>
          )}
          {joined && me && <ClaimPrompt me={me} account={account} workspaceId={id} call={call} onClaimed={setMe} onError={setError} />}
          {joined ? (
            <Backlog items={backlog} participants={participants} me={me} workspace={workspace} call={call} onError={setError} />
          ) : (
            <JoinForm id={id} call={call} onJoined={() => setJoined(true)} />
          )}
        </main>
        {joined && chatOpen && (
          <Splitter width={chatWidth.width} min={chatWidth.min} max={chatWidth.max} onChange={chatWidth.setWidth} />
        )}
        {joined && (
          <Chat
            workspaceId={id}
            messages={messages}
            participants={participants}
            me={me}
            call={call}
            onError={setError}
            open={chatOpen}
            onToggle={setChatOpen}
          />
        )}
      </div>
    </div>
  );
}

function TopBar({ workspace, webhooks, me, joined, participants, live, call, onError, navigate, account, members }) {
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
        {() => <Invite workspace={workspace} account={account} members={members} call={call} />}
      </Popover>
      <Roster participants={participants} me={me} call={call} onError={onError} />
      {me && (
        <Popover title="Workspace settings" className="button button-ghost button-icon" trigger={<Settings aria-hidden="true" />}>
          {(close) => (
            <WorkspaceSettings workspace={workspace} webhooks={webhooks} members={members} account={account} call={call} onError={onError} onDone={close} />
          )}
        </Popover>
      )}
      <AccountMenu account={account} />
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

function WorkspaceSettings({ workspace, webhooks, members, account, call, onError, onDone }) {
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
      <Members members={members} account={account} call={call} onError={onError} />
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

// Invite an agent. A signed-in member gets a single-use pass (15 minutes)
// embedded in the prompt, so the agent joins as their agent; otherwise the
// prompt is passless (anyone with the link can join, as before).
function Invite({ workspace, account, members, call }) {
  const member = Boolean(account.user && members.members.some((m) => m.user_id === account.user.id));
  const [minted, setMinted] = useState(null); // {pass, expires_at, invite_prompt}
  const [error, setError] = useState("");
  const [now, setNow] = useState(Date.now());
  const mint = () => {
    setError("");
    call("POST", "/passes").then(setMinted, (e) => setError(e.message));
  };
  useEffect(() => {
    if (member) mint();
  }, [member]);
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);
  const doorway = `${window.location.origin}/w/${workspace.id}/agent.md`;
  const passless =
    `You're invited to collaborate in the Unison workspace "${workspace.name}". ` +
    `Fetch ${doorway} (e.g. curl -s ${doorway}) and follow it: install the unison CLI, ` +
    `join with a name and emoji of your own, say hello in chat, pick up work from the backlog, ` +
    `and park on \`unison wait\` between laps.`;
  const left = minted ? Math.max(0, Math.round((minted.expires_at - now) / 1000)) : 0;
  const prompt = member ? (minted && left > 0 ? minted.invite_prompt : "") : passless;
  return (
    <>
      <p>Paste this into any coding agent:</p>
      <Textarea readOnly value={prompt} rows={member ? 8 : 6} onFocus={(e) => e.target.select()} placeholder={member && !error ? "Making a pass…" : ""} />
      {member && minted && (
        <p className="pass-status muted" aria-live="polite">
          <KeyRound aria-hidden="true" />
          {left > 0 ? `Pass works once · expires in ${Math.floor(left / 60)}:${String(left % 60).padStart(2, "0")}` : "Pass expired"}
        </p>
      )}
      {error && <p role="alert">Error: {error}</p>}
      {!member && (
        <p className="pass-status muted">
          <KeyRound aria-hidden="true" />
          {account.user
            ? "Join or link your participant to send a secure, single-use invite."
            : "Sign in for a secure, single-use invite: the agent then works as your agent."}
        </p>
      )}
      <div className="actions-row">
        <Button type="button" disabled={!prompt} onClick={() => navigator.clipboard.writeText(prompt)}>
          Copy
        </Button>
        {member && (
          <Button type="button" variant="ghost" onClick={mint}>
            <RefreshCw aria-hidden="true" />
            New pass
          </Button>
        )}
      </div>
    </>
  );
}

// rosterGroups folds the participants linked to one account into a single
// entry shown with its most recently active participant (the list arrives
// most recently active first). Unlinked participants are their own entries.
function rosterGroups(participants) {
  const groups = [];
  const byUser = new Map();
  for (const p of participants) {
    if (p.user_id && byUser.has(p.user_id)) {
      byUser.get(p.user_id).all.push(p);
      continue;
    }
    const g = { key: p.user_id ? `u${p.user_id}` : `p${p.id}`, primary: p, all: [p] };
    if (p.user_id) byUser.set(p.user_id, g);
    groups.push(g);
  }
  return groups;
}

// Who's here: overlapping emoji avatars with presence dots, one per person
// (participants linked to the same account are grouped). Your own avatar
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
  const groups = rosterGroups(participants);
  return (
    <div className="avatars" role="list" aria-label="Who's here">
      {groups.map((g, i) => {
        const p = g.primary;
        const active = g.all.some((q) => now - q.last_seen_at < ACTIVE_WINDOW_MS);
        const mine = me && g.all.some((q) => q.id === me.id);
        const also = g.all.length > 1 ? ` (also ${g.all.slice(1).map((q) => q.name).join(", ")})` : "";
        const agentOf = p.sponsor ? ` · agent of ${p.sponsor.display_name}` : "";
        const label = `${p.name}${agentOf}${also}${mine ? " (you)" : ""} — ${p.status || (active ? "active" : "away")}`;
        const content = (
          <>
            {p.emoji}
            <span className="dot" />
            {g.all.length > 1 && (
              <span className="avatar-count" aria-hidden="true">
                {g.all.length}
              </span>
            )}
          </>
        );
        const cls = active ? "avatar active" : "avatar";
        return (
          // Earlier avatars stack on top so every presence dot stays visible.
          <span key={g.key} role="listitem" style={{ zIndex: groups.length - i }}>
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
