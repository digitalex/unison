import { useEffect, useMemo, useState } from "react";
import { FolderGit2, KeyRound, LayoutGrid, ArrowUpRight, RefreshCw, Settings, UserPlus } from "lucide-react";
import { ApiError, getToken, setToken, workspaceApi } from "./api.js";
import { AccountMenu } from "./Account.jsx";
import Backlog from "./Backlog.jsx";
import Chat from "./Chat.jsx";
import { GatePage, JoinForm } from "./Gate.jsx";
import { SHOW_MESSAGE } from "./nav.js";
import SettingsPage from "./settings/SettingsPage.jsx";
import Button from "./ui/Button.jsx";
import { Textarea } from "./ui/Input.jsx";
import Popover from "./ui/Popover.jsx";
import ThemeSwitch from "./ui/ThemeSwitch.jsx";
import useSectionOpen from "./useSectionOpen.js";
import Splitter, { useChatWidth } from "./ui/Splitter.jsx";

const ACTIVE_WINDOW_MS = 2 * 60 * 1000;

// Which collections each event invalidates.
function collectionFor({ type, data }) {
  // A message about an item changes the item's message count.
  if (type.startsWith("message.")) return data?.item_id || type !== "message.created" ? ["messages", "backlog"] : ["messages"];
  if (type.startsWith("backlog.") || type.startsWith("comment.") || type.startsWith("pr.")) return ["backlog"];
  if (type.startsWith("webhook.")) return ["webhooks"];
  if (type.startsWith("invite.")) return ["invites"];
  // Members list each member's participants (names, emoji, links).
  if (type.startsWith("participant.")) return ["participants", "members"];
  if (type.startsWith("member.")) return ["members"];
  if (type.startsWith("workspace.") || type.startsWith("repo.")) return ["workspace"];
  return [];
}

// The server's refusals that mean "you can't see this workspace" (as
// opposed to an error in a request you may make).
const GATE_CODES = ["sign_in", "legacy", "not_member", "revoked", "other_space"];

function isGateError(e) {
  return e instanceof ApiError && GATE_CODES.includes(e.code);
}

// Workspace is a workspace's page: the board (backlog and chat) or, with
// view "settings", its settings. Both share the data and live updates.
// Workspaces are private: signed in, the session cookie is the credential
// (the server finds your participant); signed out, this browser's
// participant token, if any. Whoever the server refuses sees GatePage.
export default function Workspace({ id, view, navigate, account }) {
  const token = account.user ? "" : getToken(id);
  const call = useMemo(() => workspaceApi(id, token), [id, token]);
  const [gate, setGate] = useState(null); // {code, message} when refused
  const [joins, setJoins] = useState(0); // bumped on joining: reload everything
  const [workspace, setWorkspace] = useState(null);
  const [me, setMe] = useState(null);
  const [participants, setParticipants] = useState([]);
  const [messages, setMessages] = useState([]);
  const [backlog, setBacklog] = useState([]);
  const [webhooks, setWebhooks] = useState([]);
  const [members, setMembers] = useState({ members: [], unlinked: [] });
  const [invites, setInvites] = useState([]);
  const [live, setLive] = useState(false);
  const [chatOpen, setChatOpen] = useSectionOpen("chat", true);
  const shownMessage = useShownMessage(setChatOpen);
  const chatWidth = useChatWidth();
  const [error, setError] = useState("");
  const joined = Boolean(me);

  const loaders = {
    workspace: () => call("GET").then(setWorkspace),
    participants: () => call("GET", "/participants").then(setParticipants),
    messages: () => call("GET", "/messages?limit=500").then(setMessages),
    backlog: () => call("GET", "/backlog?status=all").then(setBacklog),
    webhooks: () => call("GET", "/webhooks").then(setWebhooks),
    members: () => call("GET", "/members").then(setMembers),
    invites: () => call("GET", "/invites").then(setInvites),
    // "join_first": a member (or, with privacy off, a visitor) who hasn't joined.
    me: () => call("GET", "/me").then(setMe, (e) => (e instanceof ApiError && e.code === "join_first" ? setMe(null) : Promise.reject(e))),
  };
  const refused = (e) => {
    if (!isGateError(e)) return setError(e.message);
    // A token that stopped working is useless: forget it (legacy ones are
    // kept, so the page can explain them until you sign in).
    if (token && e.code !== "legacy" && e.code !== "not_member") setToken(id, "");
    setGate({ code: e.code, message: e.message });
  };
  const reload = (name) => loaders[name]().catch(refused);
  const onJoined = () => {
    setGate(null);
    setJoins((n) => n + 1);
  };

  // Initial load, once we know whether you're signed in.
  useEffect(() => {
    if (!account.ready) return;
    setGate(null);
    Object.keys(loaders).forEach(reload);
  }, [id, token, account.ready, joins]);

  // Live updates: every event refreshes the collection it touches. Holding
  // the stream open also keeps us present in the roster.
  useEffect(() => {
    if (!account.ready || gate) return;
    const src = new EventSource(`/api/workspaces/${id}/stream${token ? `?token=${encodeURIComponent(token)}` : ""}`);
    src.onopen = () => setLive(true);
    src.onerror = () => setLive(false);
    src.onmessage = (msg) => {
      collectionFor(JSON.parse(msg.data)).forEach(reload);
    };
    const roster = setInterval(() => reload("participants"), 30000);
    return () => {
      src.close();
      clearInterval(roster);
    };
  }, [id, token, account.ready, Boolean(gate), joins]);

  if (!account.ready) return <main className="page"><p>Loading…</p></main>;
  if (gate) return <GatePage gate={gate} account={account} workspaceId={id} onJoined={onJoined} />;
  if (!workspace) return <main className="page">{error ? <p role="alert">Error: {error}</p> : <p>Loading…</p>}</main>;

  return (
    <div className="app">
      <TopBar
        workspace={workspace}
        view={view}
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
      {view === "settings" && joined ? (
        <SettingsPage
          workspace={workspace}
          members={members}
          invites={invites}
          participants={participants}
          webhooks={webhooks}
          account={account}
          call={call}
          error={error}
          onError={setError}
          navigate={navigate}
        />
      ) : (
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
            {joined ? (
              <Backlog items={backlog} participants={participants} me={me} workspace={workspace} call={call} onError={setError} />
            ) : (
              <JoinCard workspaceId={id} account={account} onJoined={onJoined} />
            )}
          </main>
          {joined && chatOpen && (
            <Splitter width={chatWidth.width} min={chatWidth.min} max={chatWidth.max} onChange={chatWidth.setWidth} />
          )}
          {joined && (
            <Chat
              workspaceId={id}
              messages={messages}
              items={backlog}
              participants={participants}
              me={me}
              call={call}
              onError={setError}
              open={chatOpen}
              onToggle={setChatOpen}
              shown={shownMessage}
            />
          )}
        </div>
      )}
    </div>
  );
}

// messageFromURL reads a deep link to a chat message: #msg-12 or ?msg=12.
function messageFromURL() {
  const m = window.location.hash.match(/^#msg-(\d+)$/) || window.location.search.match(/[?&]msg=(\d+)/);
  return m ? Number(m[1]) : null;
}

// useShownMessage is the chat message to scroll to and highlight: from a
// deep link, or a link elsewhere on the board (messageLink). Showing one
// opens a minimized chat. `at` makes showing the same message again count.
function useShownMessage(setChatOpen) {
  const [shown, setShown] = useState(() => {
    const id = messageFromURL();
    return id ? { id, at: Date.now() } : null;
  });
  useEffect(() => {
    if (shown) setChatOpen(true);
    const onShow = (e) => {
      setChatOpen(true);
      setShown({ id: e.detail, at: Date.now() });
    };
    window.addEventListener(SHOW_MESSAGE, onShow);
    return () => window.removeEventListener(SHOW_MESSAGE, onShow);
  }, []);
  return shown;
}

function TopBar({ workspace, view, me, joined, participants, live, call, onError, navigate, account, members }) {
  const settings = `/w/${workspace.id}/settings`;
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
        <h1 className="ws-name">
          <a
            href={`/w/${workspace.id}`}
            onClick={(e) => {
              e.preventDefault();
              navigate(`/w/${workspace.id}`);
            }}
          >
            {workspace.name}
          </a>
        </h1>
      </div>
      {joined && <span className="crumb">/ {view === "settings" ? "Settings" : "Backlog"}</span>}
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
        {() => <Invite account={account} members={members} call={call} />}
      </Popover>
      <Roster participants={participants} me={me} call={call} onError={onError} />
      {me && (
        <a
          className="button button-ghost button-icon"
          href={settings}
          title="Workspace settings"
          aria-label="Workspace settings"
          aria-current={view === "settings" ? "page" : undefined}
          onClick={(e) => {
            e.preventDefault();
            navigate(settings);
          }}
        >
          <Settings aria-hidden="true" />
        </a>
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

// JoinCard: a member who hasn't joined yet picks a name. Signed out, you
// only get this far when the server runs with privacy off (UNISON_AUTH=off,
// local development): then anyone may join with a name, as this browser.
function JoinCard({ workspaceId, account, onJoined }) {
  const anonymous = (resp) => {
    setToken(workspaceId, resp.token);
    onJoined();
  };
  return (
    <section className="card join-card">
      <h2>Join this workspace</h2>
      {!account.user && <p className="field-description">Privacy is off on this server (local development): join with a name, or sign in.</p>}
      <JoinForm workspaceId={workspaceId} onJoined={account.user ? onJoined : anonymous} />
    </section>
  );
}

// Invite an agent. A signed-in member gets a single-use pass (15 minutes)
// embedded in the prompt, so the agent joins as their agent. Agents can't
// join without one. (People are invited from Settings → Invites.)
function Invite({ account, members, call }) {
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
  const left = minted ? Math.max(0, Math.round((minted.expires_at - now) / 1000)) : 0;
  const prompt = minted && left > 0 ? minted.invite_prompt : "";
  if (!member) {
    return (
      <p className="pass-status muted">
        <KeyRound aria-hidden="true" />
        {account.user ? "Join this workspace to invite agents." : "Sign in to invite agents: each gets a secure, single-use pass."}
      </p>
    );
  }
  return (
    <>
      <p>Paste this into any coding agent:</p>
      <Textarea readOnly value={prompt} rows={8} onFocus={(e) => e.target.select()} placeholder={!error ? "Making a pass…" : ""} />
      {minted && (
        <p className="pass-status muted" aria-live="polite">
          <KeyRound aria-hidden="true" />
          {left > 0 ? `Pass works once · expires in ${Math.floor(left / 60)}:${String(left % 60).padStart(2, "0")}` : "Pass expired"}
        </p>
      )}
      {error && <p role="alert">Error: {error}</p>}
      <div className="actions-row">
        <Button type="button" disabled={!prompt} onClick={() => navigator.clipboard.writeText(prompt)}>
          Copy
        </Button>
        <Button type="button" variant="ghost" onClick={mint}>
          <RefreshCw aria-hidden="true" />
          New pass
        </Button>
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
