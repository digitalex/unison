import { useEffect, useMemo, useRef, useState } from "react";
import { ArrowUpRight, FolderGit2, Inbox as InboxIcon, KeyRound, LayoutGrid, Link2, ListTodo, MessageCircle, RefreshCw, Settings, UserPlus } from "lucide-react";
import { ApiError, getToken, setToken, workspaceApi } from "./api.js";
import { AccountMenu } from "./Account.jsx";
import Backlog from "./Backlog.jsx";
import Chat from "./Chat.jsx";
import { GatePage, JoinForm } from "./Gate.jsx";
import Inbox from "./Inbox.jsx";
import { OPEN_ITEM, SHOW_MESSAGE, spaLink } from "./nav.js";
import { agentStatus } from "./agents.js";
import { repoName } from "./repos.js";
import SettingsPage from "./settings/SettingsPage.jsx";
import { UrlBox } from "./settings/Webhooks.jsx";
import Button from "./ui/Button.jsx";
import { Textarea } from "./ui/Input.jsx";
import Popover from "./ui/Popover.jsx";
import ThemeSwitch from "./ui/ThemeSwitch.jsx";
import WorkspaceSwitcher from "./WorkspaceSwitcher.jsx";

const ACTIVE_WINDOW_MS = 2 * 60 * 1000;

// Which collections each event invalidates.
function collectionFor({ type, data }) {
  // A message about an item changes the item's message count.
  if (type.startsWith("message.")) return data?.item_id ? ["messages", "backlog"] : ["messages"];
  if (type.startsWith("backlog.") || type.startsWith("comment.") || type.startsWith("pr.")) return ["backlog"];
  if (type.startsWith("webhook.")) return ["webhooks"];
  if (type.startsWith("invite.")) return ["invites"];
  // Inbox events say only whose inbox changed; refetching gets ours.
  if (type.startsWith("inbox.")) return ["inbox"];
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

// Workspace is a workspace's page. A sidebar (a tab bar at the bottom on
// phones) switches between its screens, and the current one fills the main
// pane: view is "inbox", "backlog", "chat" or "settings". They all share the
// data and live updates.
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
  const [inbox, setInbox] = useState([]);
  const [live, setLive] = useState(false);
  const screens = useScreens(id, view, navigate);
  const shownMessage = useShownMessage(screens.show);
  const unread = useChatUnread(id, messages, me, view === "chat");
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
    // Members have an inbox; anyone else (403) just has nothing in it.
    inbox: () => call("GET", "/inbox").then(setInbox, () => setInbox([])),
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

  const counts = { inbox: inbox.length, backlog: backlog.filter((it) => it.status !== "done").length, chat: unread };
  // The screen header's controls on phones: Invite and the account menu.
  const headExtras = (
    <HeadExtras workspace={workspace} call={call} account={account} members={members} settingsLink={screens.link("settings")} />
  );
  const errorBar = error && (
    <p role="alert" className="error-bar">
      Error: {error}{" "}
      <Button variant="ghost" onClick={() => setError("")}>
        dismiss
      </Button>
    </p>
  );

  return (
    <div className={joined ? "shell" : "shell shell-unjoined"}>
      <Sidebar
        workspace={workspace}
        view={view}
        joined={joined}
        live={live}
        counts={counts}
        screens={screens}
        account={account}
        navigate={navigate}
        people={
          joined && (
            <>
              <People participants={participants} me={me} call={call} onError={setError} />
              <InviteButton workspace={workspace} account={account} members={members} call={call} className="nav-link nav-invite" align="start" />
            </>
          )
        }
      />
      {view === "settings" && joined ? (
        <main className="shell-main shell-settings">
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
        </main>
      ) : (
        <main className="shell-main panel">
          {!joined ? (
            <>
              {errorBar}
              <JoinCard workspaceId={id} account={account} onJoined={onJoined} />
            </>
          ) : view === "inbox" ? (
            <Inbox entries={inbox} participants={participants} call={call} onError={setError} head={headExtras} error={errorBar} />
          ) : view === "chat" ? (
            <Chat
              messages={messages}
              items={backlog}
              participants={participants}
              me={me}
              call={call}
              onError={setError}
              shown={shownMessage}
              head={headExtras}
              error={errorBar}
            />
          ) : (
            <Backlog items={backlog} participants={participants} me={me} workspace={workspace} call={call} onError={setError} head={headExtras} error={errorBar} />
          )}
        </main>
      )}
      {joined && <TabBar view={view} counts={counts} screens={screens} />}
    </div>
  );
}

// useScreens moves between the workspace's screens: link(screen) gives an
// <a>'s props, show(screen, rest) goes there (rest: a query or hash to
// keep). Links that open a backlog item from another screen (OPEN_ITEM: an
// inbox entry's or a chat message's chip) go to the backlog, which opens it.
function useScreens(id, view, navigate) {
  const path = (screen) => (screen === "backlog" ? `/w/${id}` : `/w/${id}/${screen}`);
  const current = useRef(view);
  current.current = view;
  const show = (screen, rest = "") => {
    if (current.current !== screen || rest) navigate(path(screen) + rest);
  };
  useEffect(() => {
    const onOpenItem = (e) => {
      if (current.current !== "backlog") show("backlog", `?item=${e.detail}`);
    };
    window.addEventListener(OPEN_ITEM, onOpenItem);
    return () => window.removeEventListener(OPEN_ITEM, onOpenItem);
  }, [id]);
  const link = (screen) => ({
    href: path(screen),
    "aria-current": view === screen ? "page" : undefined,
    onClick: (e) => {
      if (e.metaKey || e.ctrlKey || e.shiftKey || e.button === 1) return;
      e.preventDefault();
      show(screen);
    },
  });
  return { show, link };
}

// useChatUnread counts the messages from others that arrived since you last
// had the chat screen open (remembered across reloads).
function useChatUnread(workspaceId, messages, me, active) {
  const seenKey = `unison.chat.seen.${workspaceId}`;
  const [seenId, setSeenId] = useState(() => Number(localStorage.getItem(seenKey)) || 0);
  const latestId = messages.reduce((max, m) => Math.max(max, m.id), 0);
  useEffect(() => {
    if (active && latestId > seenId) {
      localStorage.setItem(seenKey, String(latestId));
      setSeenId(latestId);
    }
  }, [active, latestId, seenId, seenKey]);
  return messages.filter((m) => m.id > seenId && m.author_id !== me?.id).length;
}

// messageFromURL reads a deep link to a chat message: #msg-12 or ?msg=12.
function messageFromURL() {
  const m = window.location.hash.match(/^#msg-(\d+)$/) || window.location.search.match(/[?&]msg=(\d+)/);
  return m ? Number(m[1]) : null;
}

// useShownMessage is the chat message to scroll to and highlight: from a
// deep link, or a link elsewhere on the board (messageLink). Showing one
// goes to the chat screen. `at` makes showing the same message again count.
function useShownMessage(show) {
  const [shown, setShown] = useState(() => {
    const id = messageFromURL();
    return id ? { id, at: Date.now() } : null;
  });
  useEffect(() => {
    if (shown) show("chat", `#msg-${shown.id}`);
    const onShow = (e) => {
      show("chat", `#msg-${e.detail}`);
      setShown({ id: e.detail, at: Date.now() });
    };
    window.addEventListener(SHOW_MESSAGE, onShow);
    return () => window.removeEventListener(SHOW_MESSAGE, onShow);
  }, []);
  return shown;
}

const SCREENS = [
  { id: "inbox", label: "Inbox", Icon: InboxIcon },
  { id: "backlog", label: "Backlog", Icon: ListTodo },
  { id: "chat", label: "Chat", Icon: MessageCircle },
];

// The count beside a screen's name: the inbox's entries and the open backlog
// as numbers, chat's unread messages as a dot.
function ScreenCount({ screen, counts }) {
  const n = counts[screen];
  if (!n) return null;
  if (screen === "chat") return <span className="nav-unread" aria-label={`${n} new message${n === 1 ? "" : "s"}`} />;
  return <span className={screen === "inbox" ? "nav-count nav-count-strong" : "nav-count"}>{n}</span>;
}

// Sidebar: the workspace's navigation on wide screens. It has no panel of
// its own (it sits on the base): the workspace switcher, the screens, who's
// here (people, with Invite), links out (canvas, repositories), then
// settings and your account at the bottom.
function Sidebar({ workspace, view, joined, live, counts, screens, account, navigate, people }) {
  const repos = workspace.repos || [];
  return (
    <nav className="sidebar" aria-label="Workspace">
      <div className="sidebar-ws">
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
          <WorkspaceSwitcher workspace={workspace} account={account} navigate={navigate} />
        </h1>
        <span className={live ? "live live-on" : "live"} title={live ? "Live" : "Reconnecting…"} aria-label={live ? "Live" : "Reconnecting…"} />
      </div>
      {joined && (
        <ul className="nav-list">
          {SCREENS.map(({ id, label, Icon }) => (
            <li key={id}>
              <a className="nav-link" {...screens.link(id)}>
                <Icon aria-hidden="true" />
                {label}
                <ScreenCount screen={id} counts={counts} />
              </a>
            </li>
          ))}
        </ul>
      )}
      {people && (
        <>
          <h2 className="nav-heading">People</h2>
          {people}
        </>
      )}
      {(workspace.canvas_url || repos.length > 0) && (
        <>
          <h2 className="nav-heading">Links</h2>
          <ul className="nav-list nav-list-small">
            {workspace.canvas_url && (
              <li>
                <a className="nav-link" href={workspace.canvas_url} target="_blank" rel="noreferrer" title="Open the design canvas">
                  <LayoutGrid aria-hidden="true" />
                  Canvas
                  <ArrowUpRight className="nav-ext" aria-label="(opens in a new tab)" />
                </a>
              </li>
            )}
            {repos.map((r) => (
              <li key={r.id}>
                <a className="nav-link" href={r.url} target="_blank" rel="noreferrer" title={`Repository: ${r.url}`}>
                  <FolderGit2 aria-hidden="true" />
                  <span className="nav-text">{repoName(r.url)}</span>
                  <ArrowUpRight className="nav-ext" aria-label="(opens in a new tab)" />
                </a>
              </li>
            ))}
          </ul>
        </>
      )}
      <span className="spacer" />
      <div className="sidebar-foot">
        {joined && (
          <a className="nav-link" {...screens.link("settings")} title="Workspace settings">
            <Settings aria-hidden="true" />
            Settings
          </a>
        )}
        {account.user ? <AccountMenu account={account} showName menuExtra={() => <ThemeRow />} /> : <ThemeSwitch />}
      </div>
    </nav>
  );
}

// TabBar: the screens as tabs at the bottom, on phones.
function TabBar({ view, counts, screens }) {
  return (
    <nav className="tabbar" aria-label="Screens">
      {SCREENS.map(({ id, label, Icon }) => (
        <a key={id} className="tab" {...screens.link(id)}>
          <span className="tab-icon">
            <Icon aria-hidden="true" />
            <ScreenCount screen={id} counts={counts} />
          </span>
          {label}
        </a>
      ))}
    </nav>
  );
}

function ThemeRow() {
  return (
    <div className="account-theme">
      <span className="muted">Theme</span>
      <ThemeSwitch />
    </div>
  );
}

// InviteButton: Invite a person or an agent, from a popover.
function InviteButton({ workspace, account, members, call, className, align }) {
  return (
    <Popover
      title="Invite a person or an agent"
      className={className}
      align={align}
      trigger={
        <>
          <UserPlus aria-hidden="true" />
          <span className="label">Invite</span>
        </>
      }
    >
      {(close) => <Invite workspaceId={workspace.id} account={account} members={members} call={call} onNavigate={close} />}
    </Popover>
  );
}

// HeadExtras: on phones (no sidebar), Invite and the account menu at the
// right of each screen's header; the menu also holds settings.
function HeadExtras({ workspace, call, account, members, settingsLink }) {
  return (
    <>
      <span className="head-account">
        <InviteButton workspace={workspace} account={account} members={members} call={call} className="button button-ghost button-icon" />
        {account.user ? (
          <AccountMenu
            account={account}
            menuExtra={(close) => (
              <>
                <ThemeRow />
                <a className="button button-secondary" href={settingsLink.href} onClick={(e) => {
                    close();
                    settingsLink.onClick(e);
                  }}>
                  <Settings aria-hidden="true" />
                  Workspace settings
                </a>
              </>
            )}
          />
        ) : (
          <>
            <ThemeSwitch />
            <a className="button button-ghost button-icon" {...settingsLink} title="Workspace settings" aria-label="Workspace settings">
              <Settings aria-hidden="true" />
            </a>
          </>
        )}
      </span>
    </>
  );
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

// Invite: people get an invite link (Settings → Invites has email invites,
// expiry choices and revoking); agents get a single-use pass.
function Invite({ workspaceId, account, members, call, onNavigate }) {
  const member = Boolean(account.user && members.members.some((m) => m.user_id === account.user.id));
  const [kind, setKind] = useState("person");
  if (!member) {
    return (
      <p className="pass-status muted">
        <KeyRound aria-hidden="true" />
        {account.user ? "Join this workspace to invite people and agents." : "Sign in to invite people and agents."}
      </p>
    );
  }
  return (
    <>
      <div className="seg invite-kind" role="group" aria-label="Invite">
        {[
          ["person", "A person"],
          ["agent", "An agent"],
        ].map(([k, label]) => (
          <button key={k} type="button" aria-pressed={kind === k} onClick={() => setKind(k)}>
            {label}
          </button>
        ))}
      </div>
      {kind === "person" ? <InvitePerson workspaceId={workspaceId} call={call} onNavigate={onNavigate} /> : <InviteAgent call={call} />}
    </>
  );
}

// InvitePerson makes an invite link (48 hours, any number of people) to send
// to someone: they sign in (or sign up) with it and land in the workspace.
function InvitePerson({ workspaceId, call, onNavigate }) {
  const [created, setCreated] = useState(null); // {url, invite}
  const [error, setError] = useState("");
  const create = () => {
    setError("");
    call("POST", "/invites", { link: true }).then(setCreated, (e) => setError(e.message));
  };
  return (
    <>
      <p>Send a link: whoever opens it signs in and goes straight to this workspace.</p>
      {created ? (
        <>
          <UrlBox url={created.url} label="Invite link" />
          <p className="pass-status muted">
            <KeyRound aria-hidden="true" />
            Shown once · works for 48 hours
          </p>
        </>
      ) : (
        <div className="actions-row">
          <Button type="button" variant="primary" onClick={create}>
            <Link2 aria-hidden="true" />
            Create invite link
          </Button>
        </div>
      )}
      {error && <p role="alert">Error: {error}</p>}
      <p className="field-description">
        Email invites and revoking: <a {...spaLink(`/w/${workspaceId}/settings#invites`, onNavigate)}>Settings → Invites</a>.
      </p>
    </>
  );
}

// InviteAgent: a signed-in member gets a single-use pass (15 minutes)
// embedded in the prompt, so the agent joins as their agent. Agents can't
// join without one.
function InviteAgent({ call }) {
  const [minted, setMinted] = useState(null); // {pass, expires_at, invite_prompt}
  const [error, setError] = useState("");
  const [now, setNow] = useState(Date.now());
  const mint = () => {
    setError("");
    call("POST", "/passes").then(setMinted, (e) => setError(e.message));
  };
  useEffect(mint, []);
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);
  const left = minted ? Math.max(0, Math.round((minted.expires_at - now) / 1000)) : 0;
  const prompt = minted && left > 0 ? minted.invite_prompt : "";
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

// People: who's here, one row per person (participants linked to the same
// account are grouped), with a presence dot and their status. Your own row
// sets your status.
function People({ participants, me, call, onError }) {
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
    <ul className="nav-list people" aria-label="Who's here">
      {rosterGroups(participants).map((g) => {
        const p = g.primary;
        // Logged-out (or revoked) agents are never shown as active.
        const active = g.all.some((q) => now - q.last_seen_at < ACTIVE_WINDOW_MS && !agentStatus(q, now));
        const mine = me && g.all.some((q) => q.id === me.id);
        const also = g.all.length > 1 ? ` (also ${g.all.slice(1).map((q) => q.name).join(", ")})` : "";
        const agentOf = p.sponsor ? ` · agent of ${p.sponsor.display_name}` : "";
        const status = p.status || (active ? "active" : "away");
        const label = `${p.name}${agentOf}${also}${mine ? " (you)" : ""} — ${status}`;
        const content = (
          <>
            <span className={active ? "avatar active" : "avatar"} aria-hidden="true">
              {p.emoji}
              <span className="dot" />
            </span>
            <span className="person-name">{p.name}</span>
            {p.status && <span className="person-status">{p.status}</span>}
          </>
        );
        return (
          <li key={g.key}>
            {mine ? (
              <button type="button" className="nav-link person" title={`${label} · set status`} aria-label={`${label}. Set status`} onClick={setStatus}>
                {content}
              </button>
            ) : (
              <span className="nav-link person" title={label} aria-label={label} role="img">
                {content}
              </span>
            )}
          </li>
        );
      })}
    </ul>
  );
}
