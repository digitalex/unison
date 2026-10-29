import { useEffect, useMemo, useRef, useState } from "react";
import { ArrowUpRight, ChevronDown, FolderGit2, KeyRound, LayoutGrid, Link2, ListTodo, MessageCircle, RefreshCw, Settings, UserPlus } from "lucide-react";
import { ApiError, getToken, setToken, workspaceApi } from "./api.js";
import { AccountMenu } from "./Account.jsx";
import Backlog from "./Backlog.jsx";
import Chat from "./Chat.jsx";
import { GatePage, JoinForm } from "./Gate.jsx";
import { OPEN_ITEM, SHOW_MESSAGE, spaLink } from "./nav.js";
import { notifySupported, useNotifier, useNotifyPref } from "./notify.js";
import { agentStatus } from "./agents.js";
import { repoName } from "./repos.js";
import SettingsPage from "./settings/SettingsPage.jsx";
import { UrlBox } from "./settings/Webhooks.jsx";
import Button from "./ui/Button.jsx";
import Dialog from "./ui/Dialog.jsx";
import { Textarea } from "./ui/Input.jsx";
import ThemeSwitch from "./ui/ThemeSwitch.jsx";
import WorkspaceSwitcher from "./WorkspaceSwitcher.jsx";

const ACTIVE_WINDOW_MS = 2 * 60 * 1000;
const DORMANT_WINDOW_MS = 15 * 60 * 1000;

function isDormant(p, now) {
  return agentStatus(p, now) || !p.last_seen_at || now - p.last_seen_at >= DORMANT_WINDOW_MS;
}

// The events chat shows as one-line activity updates (#93); the server's
// store.ActivityTypes.
const ACTIVITY_TYPES = ["backlog.added", "backlog.assigned", "backlog.closed", "backlog.reopened", "participant.joined", "member.removed"];

// Item events after which other items' positions may have changed.
const RENUMBERS = ["backlog.added", "backlog.moved", "backlog.closed", "backlog.reopened", "backlog.deleted"];

// Which collections each event invalidates.
function collectionFor({ type, data }) {
  // Message events carry the message: it is applied to the list as is
  // (applyMessageEvent), so only what it changes elsewhere is reloaded. One
  // about an item changes the item's message count; one that asks people to
  // act (or is marked done) changes counts of open actions (on /me).
  if (type.startsWith("message.")) {
    if (!data?.id) return ["messages", "me"];
    return type === "message.done" || type === "message.undone" || data.action_required ? ["me"] : [];
  }
  // Item events carry the item and are applied in place too (#105). Those
  // that renumber the queue (adding, moving, closing, reopening, deleting)
  // also reload the open and assigned items: small, unlike every done item.
  if (type.startsWith("backlog.") || type.startsWith("comment.") || type.startsWith("pr.")) {
    const reloads = RENUMBERS.includes(type) || !(data?.id || data?.item?.id) ? ["queued"] : [];
    if (ACTIVITY_TYPES.includes(type)) reloads.push("activity");
    return reloads;
  }
  if (type.startsWith("webhook.")) return ["webhooks"];
  if (type.startsWith("schedule.")) return ["schedules"];
  if (type.startsWith("invite.")) return ["invites"];
  // Members list each member's participants (names, emoji, links).
  if (type === "participant.joined") return ["participants", "members", "activity"];
  if (type.startsWith("participant.")) return ["participants", "members"];
  if (type === "member.removed") return ["members", "activity"];
  if (type.startsWith("member.")) return ["members"];
  if (type.startsWith("workspace.") || type.startsWith("repo.")) return ["workspace"];
  return [];
}

// withMessage returns list (oldest first) with message m in it: replacing
// the one with its id, or added in order.
function withMessage(list, m) {
  if (list.some((x) => x.id === m.id)) return list.map((x) => (x.id === m.id ? m : x));
  const out = [...list, m];
  return list.length && list[list.length - 1].id > m.id ? out.sort((a, b) => a.id - b.id) : out;
}

// withItem returns the backlog with item it in it (replaced by id, or added).
function withItem(list, it) {
  return list.some((x) => x.id === it.id) ? list.map((x) => (x.id === it.id ? it : x)) : [...list, it];
}

// withQueued returns the backlog with its open and assigned items replaced by
// queued (fresh from the server, in priority order), keeping the done ones.
function withQueued(list, queued) {
  const ids = new Set(queued.map((it) => it.id));
  return [...queued, ...list.filter((it) => it.status === "done" && !ids.has(it.id))];
}

// The server's refusals that mean "you can't see this workspace" (as
// opposed to an error in a request you may make).
const GATE_CODES = ["sign_in", "not_member", "revoked", "other_space"];

function isGateError(e) {
  return e instanceof ApiError && GATE_CODES.includes(e.code);
}

// Workspace is a workspace's page. A sidebar (a tab bar at the bottom on
// phones) switches between its screens, and the current one fills the main
// pane: view is "backlog", "chat" or "settings". They all share the
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
  const [activity, setActivity] = useState([]);
  const [backlog, setBacklog] = useState([]);
  const [webhooks, setWebhooks] = useState([]);
  const [schedules, setSchedules] = useState([]);
  const [members, setMembers] = useState({ members: [] });
  const [invites, setInvites] = useState([]);
  const [live, setLive] = useState(false);
  const screens = useScreens(id, view, navigate);
  const shownMessage = useShownMessage(screens.show);
  const { unread, newAfter } = useChatUnread(id, messages, me, view === "chat");
  const [mention, setMention] = useState(null); // A name to start a chat message with.
  const [error, setError] = useState("");
  const joined = Boolean(me);
  const notify = useNotifier(me, participants, view);

  const loaders = {
    workspace: () => call("GET").then(setWorkspace),
    participants: () => call("GET", "/participants").then(setParticipants),
    messages: () => call("GET", "/messages?limit=500").then(setMessages),
    activity: () => call("GET", "/activity").then(setActivity),
    // Everything, done items included: on load and after a reconnect only.
    backlog: () => call("GET", "/backlog?status=all").then(setBacklog),
    // Open and assigned items, after events that renumber them (#105).
    queued: () => call("GET", "/backlog").then((queued) => setBacklog((list) => withQueued(list, queued))),
    webhooks: () => call("GET", "/webhooks").then(setWebhooks),
    schedules: () => call("GET", "/schedules").then(setSchedules),
    members: () => call("GET", "/members").then(setMembers),
    invites: () => call("GET", "/invites").then(setInvites),
    // "join_first": a member (or, with privacy off, a visitor) who hasn't joined.
    me: () => call("GET", "/me").then(setMe, (e) => (e instanceof ApiError && e.code === "join_first" ? setMe(null) : Promise.reject(e))),
  };
  const refused = (e) => {
    if (!isGateError(e)) return setError(e.message);
    // A token that stopped working is useless: forget it.
    if (token && e.code !== "not_member") setToken(id, "");
    setGate({ code: e.code, message: e.message });
  };
  // At most one request per collection in flight, plus one queued behind it
  // for events that arrive meanwhile (#104): a burst of events costs one or
  // two fetches per collection, and the newest response always lands last.
  const loading = useRef({});
  const reload = (name) => {
    const state = (loading.current[name] ||= { busy: false, again: false });
    if (state.busy) {
      state.again = true;
      return;
    }
    state.busy = true;
    loaders[name]()
      .catch(refused)
      .finally(() => {
        state.busy = false;
        if (state.again) {
          state.again = false;
          reload(name);
        }
      });
  };
  // Everything ("queued" is part of "backlog").
  const loadAll = () => Object.keys(loaders).filter((name) => name !== "queued").forEach(reload);
  const onJoined = () => {
    setGate(null);
    setJoins((n) => n + 1);
  };

  // Initial load, once we know whether you're signed in.
  useEffect(() => {
    if (!account.ready) return;
    setGate(null);
    loadAll();
  }, [id, token, account.ready, joins]);

  // Live updates: message events are applied as they come (#103); other
  // events refresh the collection they touch. After a dropped connection
  // everything is reloaded, since events may have been missed. Holding the
  // stream open also keeps us present in the roster.
  const messagesNow = useRef(messages);
  messagesNow.current = messages;
  // Item events carry the item (comment events too, as data.item).
  const applyItemEvent = ({ type, data }) => {
    if (type === "backlog.deleted" && data?.id) return setBacklog((list) => list.filter((it) => it.id !== data.id));
    const it = type.startsWith("comment.") ? data?.item : data;
    if ((type.startsWith("backlog.") || type.startsWith("comment.") || type.startsWith("pr.")) && it?.id) setBacklog((list) => withItem(list, it));
  };
  const applyMessageEvent = ({ type, data }) => {
    if (!type.startsWith("message.") || !data?.id) return;
    setMessages((list) => withMessage(list, data));
    // A message about an item changes that item's message count.
    if (type === "message.created" && data.item_id) {
      call("GET", `/backlog/${data.item_id}`).then((it) => setBacklog((list) => withItem(list, it)), refused);
    }
    // A reply can mark its thread's first message done, which has no event
    // of its own: refetch just that message when it asks people to act.
    const parent = type === "message.created" && data.parent_id && messagesNow.current.find((m) => m.id === data.parent_id);
    if (parent?.action_required) {
      call("GET", `/messages/${parent.id}`).then((m) => setMessages((list) => withMessage(list, m)), refused);
      reload("me");
    }
  };
  useEffect(() => {
    if (!account.ready || gate) return;
    const src = new EventSource(`/api/workspaces/${id}/stream${token ? `?token=${encodeURIComponent(token)}` : ""}`);
    let opened = false;
    src.onopen = () => {
      setLive(true);
      if (opened) loadAll();
      opened = true;
    };
    src.onerror = () => setLive(false);
    src.onmessage = (msg) => {
      const event = JSON.parse(msg.data);
      notify(event);
      applyMessageEvent(event);
      applyItemEvent(event);
      collectionFor(event).forEach(reload);
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

  const counts = { backlog: backlog.filter((it) => it.status !== "done").length, chat: unread, actions: me?.open_actions || 0 };
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
        participants={participants}
        people={
          joined && (
            <>
              <People
                participants={participants}
                me={me}
                call={call}
                onError={setError}
                onMention={(name) => {
                  setMention(name);
                  screens.show("chat");
                }}
              />
              <InviteButton workspace={workspace} account={account} members={members} call={call} className="nav-link nav-invite" />
            </>
          )
        }
      />
      {view === "settings" && joined ? (
        <main className="shell-main panel">
          <SettingsPage
            workspace={workspace}
            members={members}
            invites={invites}
            participants={participants}
            webhooks={webhooks}
            account={account}
            call={call}
            error={errorBar}
            onError={setError}
            head={headExtras}
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
          ) : view === "chat" ? (
            <Chat
              messages={messages}
              activity={activity}
              updateMessages={setMessages}
              newAfter={newAfter}
              mention={mention}
              onMentioned={() => setMention(null)}
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
            <Backlog items={backlog} schedules={schedules} participants={participants} me={me} workspace={workspace} call={call} onError={setError} head={headExtras} error={errorBar} />
          )}
        </main>
      )}
      {joined && <TabBar view={view} counts={counts} screens={screens} />}
    </div>
  );
}

// useScreens moves between the workspace's screens: link(screen) gives an
// <a>'s props, show(screen, rest) goes there (rest: a query or hash to
// keep). Links that open a backlog item from another screen (OPEN_ITEM: a
// chat message's chip) go to the backlog, which opens it.
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
// had the chat screen open (remembered across reloads). newAfter is the
// newest message you had seen when you opened it: later ones are new to you.
function useChatUnread(workspaceId, messages, me, active) {
  const seenKey = `unison.chat.seen.${workspaceId}`;
  const [seenId, setSeenId] = useState(() => Number(localStorage.getItem(seenKey)) || 0);
  const [seenAtOpen, setSeenAtOpen] = useState(null);
  const latestId = messages.reduce((max, m) => Math.max(max, m.id), 0);
  // Before the effect below marks everything seen.
  useEffect(() => setSeenAtOpen(active ? seenId : null), [active]);
  useEffect(() => {
    if (active && latestId > seenId) {
      localStorage.setItem(seenKey, String(latestId));
      setSeenId(latestId);
    }
  }, [active, latestId, seenId, seenKey]);
  return {
    // Your own messages are never unread.
    unread: messages.filter((m) => m.id > seenId && m.author_id !== me?.id).length,
    newAfter: seenAtOpen ?? seenId,
  };
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
  { id: "backlog", label: "Backlog", Icon: ListTodo },
  { id: "chat", label: "Chat", Icon: MessageCircle },
];

// The count beside a screen's name: the open backlog as a number; for chat,
// the messages that need you (strong), else new messages as a dot.
function ScreenCount({ screen, counts }) {
  if (screen === "chat") {
    const { actions, chat } = counts;
    if (actions) return <span className="nav-count nav-count-strong" aria-label={`${actions} need${actions === 1 ? "s" : ""} you`}>{actions}</span>;
    if (chat) return <span className="nav-unread" aria-label={`${chat} new message${chat === 1 ? "" : "s"}`} />;
    return null;
  }
  return counts[screen] ? <span className="nav-count">{counts[screen]}</span> : null;
}

// Thresholds for auto-collapsing sidebar sections on low-height screens
const LOW_HEIGHT_THRESHOLD = 720;
const VERY_LOW_HEIGHT_THRESHOLD = 560;

function useWindowHeight() {
  const [height, setHeight] = useState(() => (typeof window !== "undefined" ? window.innerHeight : 900));
  useEffect(() => {
    const onResize = () => setHeight(window.innerHeight);
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);
  return height;
}

function SectionHeading({ title, count, open, onToggle, ariaControls }) {
  return (
    <button
      type="button"
      className="nav-heading nav-heading-button"
      onClick={onToggle}
      aria-expanded={open}
      aria-controls={ariaControls}
      title={open ? `Collapse ${title}` : `Expand ${title}`}
    >
      <span className="nav-heading-title">{title}</span>
      {count != null && <span className="nav-heading-count">({count})</span>}
      <span className="nav-heading-spacer" />
      <ChevronDown
        aria-hidden="true"
        className={open ? "nav-heading-chevron" : "nav-heading-chevron nav-heading-chevron-closed"}
      />
    </button>
  );
}

// Sidebar: the workspace's navigation on wide screens. It has no panel of
// its own (it sits on the base): the workspace switcher, the screens, who's
// here (people, with Invite), links out (canvas, repositories), then
// settings and your account at the bottom.
function Sidebar({ workspace, view, joined, live, counts, screens, account, navigate, participants, people }) {
  const repos = workspace.repos || [];
  const height = useWindowHeight();
  const isLowHeight = height < LOW_HEIGHT_THRESHOLD;
  const isVeryLowHeight = height < VERY_LOW_HEIGHT_THRESHOLD;

  const [userPeopleOpen, setUserPeopleOpen] = useState(null);
  const [userLinksOpen, setUserLinksOpen] = useState(null);

  // If the user hasn't explicitly toggled, collapse some sections by default:
  // low height collapses people first; very low height collapses links too.
  const peopleOpen = userPeopleOpen !== null ? userPeopleOpen : !isLowHeight;
  const linksOpen = userLinksOpen !== null ? userLinksOpen : !isVeryLowHeight;

  const peopleCount = participants ? participants.length : null;
  const linksCount = (workspace.canvas_url ? 1 : 0) + repos.length;

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
      <div className="sidebar-content">
        {joined && (
          <ul className="nav-list" aria-label="Screens">
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
          <section className="sidebar-section" aria-label="People">
            <SectionHeading
              title="People"
              count={peopleCount}
              open={peopleOpen}
              onToggle={() => setUserPeopleOpen(!peopleOpen)}
              ariaControls="sidebar-people"
            />
            {peopleOpen && <div id="sidebar-people">{people}</div>}
          </section>
        )}
        {(workspace.canvas_url || repos.length > 0) && (
          <section className="sidebar-section" aria-label="Links">
            <SectionHeading
              title="Links"
              count={linksCount}
              open={linksOpen}
              onToggle={() => setUserLinksOpen(!linksOpen)}
              ariaControls="sidebar-links"
            />
            {linksOpen && (
              <ul id="sidebar-links" className="nav-list nav-list-small">
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
            )}
          </section>
        )}
        <span className="spacer" />
      </div>
      <div className="sidebar-foot">
        {joined && (
          <a className="nav-link" {...screens.link("settings")} title="Workspace settings">
            <Settings aria-hidden="true" />
            Settings
          </a>
        )}
        <ThemeSwitch />
        {account.user && <AccountMenu account={account} showName menuExtra={() => <ThemeRow />} />}
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
    <>
      <div className="account-theme">
        <span className="muted">Theme</span>
        <ThemeSwitch />
      </div>
      {notifySupported && <NotifyRow />}
    </>
  );
}

// NotifyRow turns desktop notifications on or off (see notify.js).
function NotifyRow() {
  const [on, turnOn, turnOff] = useNotifyPref();
  const blocked = Notification.permission === "denied";
  return (
    <div className="account-theme">
      <span className="muted">Desktop notifications</span>
      {blocked ? (
        <span className="muted" title="Allow notifications for this site in your browser's settings">
          Blocked
        </span>
      ) : (
        <div className="seg" role="group" aria-label="Desktop notifications">
          <button type="button" aria-pressed={on} onClick={turnOn}>
            On
          </button>
          <button type="button" aria-pressed={!on} onClick={turnOff}>
            Off
          </button>
        </div>
      )}
    </div>
  );
}

// InviteButton: Invite a person or an agent, in a modal.
function InviteButton({ workspace, account, members, call, className }) {
  const [open, setOpen] = useState(false);
  const close = () => setOpen(false);
  return (
    <>
      <button type="button" className={className} title="Invite a person or an agent" aria-haspopup="dialog" onClick={() => setOpen(true)}>
        <UserPlus aria-hidden="true" />
        <span className="label">Invite</span>
      </button>
      {open && (
        <Dialog title="Invite a person or an agent" onClose={close} className="invite-dialog">
          <Invite workspaceId={workspace.id} account={account} members={members} call={call} onNavigate={close} />
        </Dialog>
      )}
    </>
  );
}

// HeadExtras: on phones (no sidebar), Invite and the account menu at the
// right of each screen's header; the menu also holds settings.
function HeadExtras({ workspace, call, account, members, settingsLink }) {
  return (
    <>
      <span className="head-account">
        <InviteButton workspace={workspace} account={account} members={members} call={call} className="button button-ghost button-icon" />
        <ThemeSwitch />
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
          <a className="button button-ghost button-icon" {...settingsLink} title="Workspace settings" aria-label="Workspace settings">
            <Settings aria-hidden="true" />
          </a>
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

// byName sorts participants in a stable order (by name), so the roster
// doesn't reshuffle as people come and go.
function byName(participants) {
  return [...participants].sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: "base" }) || a.id - b.id);
}

// People: who's here, one row per participant (an account has one per
// workspace, #99), with a presence dot and their status. Inactive users
// (15m without activity) are greyed, have their status hidden, and are stacked
// at the end of the list. Your own row sets your status; anyone else's starts a
// chat message to them (@Name).
function People({ participants, me, call, onError, onMention }) {
  const setStatus = async () => {
    const status = window.prompt("Your status", me?.status || "");
    if (status === null) return;
    try {
      await call("PATCH", "/me", { status });
    } catch (err) {
      onError(err.message);
    }
  };
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 15000);
    return () => clearInterval(timer);
  }, []);

  const activePeople = [];
  const dormantPeople = [];
  for (const p of byName(participants)) {
    if (isDormant(p, now)) {
      dormantPeople.push(p);
    } else {
      activePeople.push(p);
    }
  }

  return (
    <ul className="nav-list people" aria-label="Who's here">
      {activePeople.map((p) => {
        // Logged-out (or revoked) agents are never shown as active.
        const active = now - p.last_seen_at < ACTIVE_WINDOW_MS && !agentStatus(p, now);
        const mine = me && p.id === me.id;
        const agentOf = p.sponsor ? ` · agent of ${p.sponsor.display_name}` : "";
        const status = p.status || (active ? "active" : "away");
        const label = `${p.name}${agentOf}${mine ? " (you)" : ""} — ${status}`;
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
          <li key={p.id}>
            {mine ? (
              <button type="button" className="nav-link person" title={`${label} · set status`} aria-label={`${label}. Set status`} onClick={setStatus}>
                {content}
              </button>
            ) : (
              <button type="button" className="nav-link person" title={`${label} · message @${p.name}`} aria-label={`${label}. Message @${p.name}`} onClick={() => onMention(p.name)}>
                {content}
              </button>
            )}
          </li>
        );
      })}
      {dormantPeople.length > 0 && (
        <li className={activePeople.length > 0 ? "dormant-row with-sep" : "dormant-row"} key="dormant">
          <div className="dormant-stack" aria-label="Inactive users">
            {dormantPeople.map((p) => {
              const mine = me && p.id === me.id;
              const agentOf = p.sponsor ? ` · agent of ${p.sponsor.display_name}` : "";
              const label = `${p.name}${agentOf}${mine ? " (you)" : ""} — inactive`;
              return (
                <button
                  key={p.id}
                  type="button"
                  className="dormant-chip"
                  title={mine ? `${label} · set status` : `${label} · message @${p.name}`}
                  aria-label={mine ? `${label}. Set status` : `${label}. Message @${p.name}`}
                  onClick={mine ? setStatus : () => onMention(p.name)}
                >
                  <span className="avatar dormant" aria-hidden="true">
                    {p.emoji}
                  </span>
                </button>
              );
            })}
          </div>
        </li>
      )}
    </ul>
  );
}
