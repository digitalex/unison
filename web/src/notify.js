import { useCallback, useEffect, useRef, useState } from "react";
import { itemNum } from "./backlogFormat.js";
import { OPEN_ITEM, SHOW_MESSAGE, spaNavigate } from "./nav.js";

// Desktop (browser) notifications (#193): configurable per event category,
// remembered per browser in localStorage, synced across tabs, and suppressed
// for your own actions or when you are already viewing the relevant screen.

export const NOTIFY_KEY = "unison.notify";
export const NOTIFY_EVENTS_KEY = "unison.notify.events";
export const NOTIFY_CHANGE_EVENT = "unison.notify.change";

export const NOTIFY_EVENTS = [
  {
    id: "mentions",
    label: "@mentions & action requests in chat",
    description: "Messages that @mention you (or @all) or ask you to act.",
    defaultOn: true,
  },
  {
    id: "prs",
    label: "PR & review events on your items",
    description: "Pull requests linked to items you own or created, and GitHub PR/review webhooks on them.",
    defaultOn: true,
  },
  {
    id: "joins",
    label: "People & agents joining",
    description: "When a teammate or agent joins the workspace.",
    defaultOn: true,
  },
  {
    id: "backlog_added",
    label: "Backlog item added",
    description: "When someone else (or a recurring schedule) adds a backlog item.",
    defaultOn: false,
  },
  {
    id: "backlog_closed",
    label: "Backlog item closed",
    description: "When someone else closes a backlog item.",
    defaultOn: false,
  },
  {
    id: "todo_assigned",
    label: "TODO assigned to you",
    description: "When someone assigns an actionable TODO to you.",
    defaultOn: false,
  },
];

export const DEFAULT_NOTIFY_EVENTS = Object.freeze(
  Object.fromEntries(NOTIFY_EVENTS.map((e) => [e.id, e.defaultOn])),
);

// notifySupported says whether this browser has notifications at all.
export const notifySupported = typeof window !== "undefined" && "Notification" in window;

function defaultStorage() {
  try {
    return typeof localStorage !== "undefined" ? localStorage : null;
  } catch {
    return null;
  }
}

export function readEventPrefs(storage = defaultStorage()) {
  const out = { ...DEFAULT_NOTIFY_EVENTS };
  if (!storage) return out;
  try {
    const raw = storage.getItem(NOTIFY_EVENTS_KEY);
    if (!raw) return out;
    const parsed = JSON.parse(raw);
    if (parsed && typeof parsed === "object") {
      for (const { id } of NOTIFY_EVENTS) {
        if (typeof parsed[id] === "boolean") out[id] = parsed[id];
      }
    }
  } catch {
    // Ignore malformed or blocked storage; keep defaults.
  }
  return out;
}

export function writeEventPrefs(prefs, storage = defaultStorage()) {
  const clean = { ...DEFAULT_NOTIFY_EVENTS };
  for (const { id } of NOTIFY_EVENTS) {
    if (typeof prefs?.[id] === "boolean") clean[id] = prefs[id];
  }
  if (storage) {
    try {
      storage.setItem(NOTIFY_EVENTS_KEY, JSON.stringify(clean));
    } catch {
      // Storage blocked: in-memory state still updates for this tab.
    }
  }
  if (typeof window !== "undefined") {
    window.dispatchEvent(new Event(NOTIFY_CHANGE_EVENT));
  }
  return clean;
}

function enabled(storage = defaultStorage()) {
  if (!notifySupported || Notification.permission !== "granted") return false;
  try {
    return storage?.getItem(NOTIFY_KEY) !== "off";
  } catch {
    return true;
  }
}

function sameEventPrefs(a, b) {
  if (a === b) return true;
  if (!a || !b) return false;
  for (const { id } of NOTIFY_EVENTS) {
    if (Boolean(a[id]) !== Boolean(b[id])) return false;
  }
  return true;
}

// useNotifyPref returns [on, turnOn, turnOff, eventPrefs, setEventPref]:
// `on` is true while browser permission is granted and master notifications
// aren't turned off; `eventPrefs` holds the per-event toggles.
export function useNotifyPref() {
  const [on, setOn] = useState(() => enabled());
  const [eventPrefs, setEventPrefs] = useState(() => readEventPrefs());

  useEffect(() => {
    const update = (e) => {
      if (e?.type === "storage" && e.key && e.key !== NOTIFY_KEY && e.key !== NOTIFY_EVENTS_KEY) return;
      setOn(enabled());
      setEventPrefs((prev) => {
        const next = readEventPrefs();
        return sameEventPrefs(prev, next) ? prev : next;
      });
    };
    window.addEventListener(NOTIFY_CHANGE_EVENT, update);
    window.addEventListener("storage", update);
    return () => {
      window.removeEventListener(NOTIFY_CHANGE_EVENT, update);
      window.removeEventListener("storage", update);
    };
  }, []);

  const turnOn = useCallback(async () => {
    const permission = Notification.permission === "default" ? await Notification.requestPermission() : Notification.permission;
    if (permission !== "granted") return setOn(false);
    try {
      localStorage.setItem(NOTIFY_KEY, "on");
    } catch {
      // Ignore storage quota errors.
    }
    setOn(true);
    window.dispatchEvent(new Event(NOTIFY_CHANGE_EVENT));
    try {
      new Notification("Desktop notifications enabled", {
        body: "You'll be notified for your selected workspace events.",
        tag: "unison-enabled",
        icon: "/apple-touch-icon.png",
      });
    } catch {
      // Ignore if browser restricts immediate notification.
    }
  }, []);

  const turnOff = useCallback(() => {
    try {
      localStorage.setItem(NOTIFY_KEY, "off");
    } catch {
      // Ignore storage quota errors.
    }
    setOn(false);
    window.dispatchEvent(new Event(NOTIFY_CHANGE_EVENT));
  }, []);

  const setEventPref = useCallback((id, checked) => {
    setEventPrefs((prev) => writeEventPrefs({ ...prev, [id]: Boolean(checked) }));
  }, []);

  return [on, turnOn, turnOff, eventPrefs, setEventPref];
}

// mentions says whether body @mentions name (or @all / @everyone), as the
// highlight in chat does.
function mentions(body, name) {
  const escaped = name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return new RegExp(`@(${escaped}|all|everyone)\\b`, "i").test(body);
}

function mentionsAny(body, names) {
  if (!body) return false;
  for (const name of names) {
    if (!name) continue;
    if (mentions(body, name)) return true;
  }
  return false;
}

function preview(text) {
  const t = String(text || "").replace(/\s+/g, " ").trim();
  return t.length > 200 ? `${t.slice(0, 197)}…` : t;
}

const GITHUB_PR_EVENTS = new Set(["pull_request", "pull_request_review", "pull_request_review_comment"]);

export function eventCategory(event) {
  switch (event?.type) {
    case "message.created":
      return "mentions";
    case "pr.linked":
      return "prs";
    case "webhook.received":
      return event.data?.source === "github" && GITHUB_PR_EVENTS.has(event.data?.event) ? "prs" : null;
    case "participant.joined":
      return "joins";
    case "backlog.added":
      return "backlog_added";
    case "backlog.closed":
      return "backlog_closed";
    case "todo.created":
    case "todo.added":
      return "todo_assigned";
    default:
      return null;
  }
}

function isMyItem(item, me) {
  return Boolean(item && me && (item.assignee_id === me.id || item.created_by === me.id));
}

function matchWebhookPRItem(delivery, me, items = []) {
  if (!delivery || delivery.source !== "github" || !GITHUB_PR_EVENTS.has(delivery.event) || !me) return null;
  const summary = String(delivery.summary || "");
  const prMatch = summary.match(/#(\d+)\b/);
  if (!prMatch) return null;
  const prNum = prMatch[1];
  const senderMatch = summary.match(/\bby\s+([^\s"]+)\s*$/i);
  if (senderMatch) {
    const sender = senderMatch[1].toLowerCase();
    const myLogins = [me.name, me.user?.display_name].filter(Boolean).map((s) => s.toLowerCase());
    if (myLogins.includes(sender)) return null;
  }
  const suffixRe = new RegExp(`/pull/${prNum}(?:[/?#]|$)`, "i");
  const item = items.find((it) => isMyItem(it, me) && (it.prs || []).some((pr) => suffixRe.test(pr.url || "")));
  return item ? { item, prNumber: Number(prNum) } : null;
}

export function isWindowFocused(doc = typeof document !== "undefined" ? document : null) {
  return Boolean(doc && doc.visibilityState === "visible" && typeof doc.hasFocus === "function" && doc.hasFocus());
}

// shouldSuppressForFocus reports whether a notification should be suppressed
// because the user's browser tab is visible & focused on the relevant screen.
export function shouldSuppressForFocus(event, context = {}, focused = isWindowFocused()) {
  if (!focused || !event) return false;
  const { view, sectionSlug = null, sections = [], items = [], me = null } = context;
  switch (event.type) {
    case "message.created":
      return view === "chat";
    case "participant.joined":
      return true;
    case "todo.created":
    case "todo.added":
      return view === "todos";
    case "backlog.added":
    case "backlog.closed":
    case "pr.linked": {
      const it = event.data;
      if (!it?.section_id) return view === "backlog";
      const sec = sections.find((s) => s.id === it.section_id);
      const slug = sec?.slug || it.section_slug;
      return view === "section" && Boolean(sectionSlug) && slug === sectionSlug;
    }
    case "webhook.received": {
      const matched = matchWebhookPRItem(event.data, me, items);
      if (!matched) return false;
      const it = matched.item;
      if (!it?.section_id) return view === "backlog";
      const sec = sections.find((s) => s.id === it.section_id);
      const slug = sec?.slug || it.section_slug;
      return view === "section" && Boolean(sectionSlug) && slug === sectionSlug;
    }
    default:
      return false;
  }
}

// notification turns a workspace event into {title, body, tag, message?, item?, screen?}
// to show, or null when it shouldn't notify you (disabled category, self-authored,
// or not addressed to you).
export function notification(event, me, participants = [], options = {}) {
  if (!event || !me) return null;
  if (event.actor_id && event.actor_id === me.id) return null;

  const prefs = options.prefs || DEFAULT_NOTIFY_EVENTS;
  const category = eventCategory(event);
  if (!category || !prefs[category]) return null;

  const who = participants.find((p) => p.id === event.actor_id);
  const name = who ? `${who.emoji ? `${who.emoji} ` : ""}${who.name}` : "Someone";
  const d = event.data || {};

  switch (event.type) {
    case "message.created": {
      const asksMe = d.action_required && (d.action_for || []).includes(me.id);
      if (asksMe) return { title: `${name} needs you`, body: preview(d.body), tag: `msg-${d.id}`, message: d.id };
      const myNames = new Set([me.name, me.user?.display_name].filter(Boolean));
      if (mentionsAny(d.body, myNames)) return { title: `${name} mentioned you`, body: preview(d.body), tag: `msg-${d.id}`, message: d.id };
      return null;
    }
    case "pr.linked": {
      if (!isMyItem(d, me)) return null;
      const num = itemNum(d);
      const latestPR = d.prs?.length ? d.prs[d.prs.length - 1]?.url : "";
      const body = latestPR ? `${d.title} — ${latestPR}` : d.title;
      return {
        title: `${name} linked a PR to #${num}`,
        body: preview(body),
        tag: `pr-${d.id}-${d.prs?.length || 0}`,
        item: num,
      };
    }
    case "webhook.received": {
      const matched = matchWebhookPRItem(d, me, options.items || []);
      if (!matched) return null;
      const num = itemNum(matched.item);
      return {
        title: `PR #${matched.prNumber} update on #${num}`,
        body: preview(String(d.summary || "").replace(/^github:\s*/, "")),
        tag: `wh-${d.delivery_id || num}`,
        item: num,
      };
    }
    case "participant.joined": {
      const agentOf = d.sponsor ? ` (agent of ${d.sponsor.display_name})` : "";
      return {
        title: `${d.emoji ? `${d.emoji} ` : ""}${d.name} joined`,
        body: `${d.name}${agentOf} joined the workspace.`,
        tag: `joined-${d.id}`,
      };
    }
    case "backlog.added": {
      const num = itemNum(d);
      const author = !event.actor_id && d.schedule_id ? "Recurring task" : name;
      return {
        title: `${author} added #${num}`,
        body: preview(d.title),
        tag: `backlog-added-${d.id}`,
        item: num,
      };
    }
    case "backlog.closed": {
      const num = itemNum(d);
      const body = d.close_note ? `${d.title} — ${d.close_note}` : d.title;
      return {
        title: `${name} closed #${num}`,
        body: preview(body),
        tag: `backlog-closed-${d.id}`,
        item: num,
      };
    }
    case "todo.created":
    case "todo.added": {
      if (d.creator_id && d.creator_id === me.id) return null;
      if (d.assignee_id !== me.id) return null;
      const linkedItem = d.item_number || d.item?.number || d.item_id || null;
      return {
        title: `${name} assigned you a TODO`,
        body: preview(d.title),
        tag: `todo-${d.id}`,
        ...(linkedItem ? { item: linkedItem } : { screen: "todos" }),
      };
    }
    default:
      return null;
  }
}

// useNotifier returns a function to call with each workspace event: it shows
// the event's notification when enabled, permitted by per-event settings, and
// not already visible on the user's focused screen.
export function useNotifier(me, participants, view, context = {}) {
  const { workspaceId = "", sectionSlug = null, sections = [], items = [] } = context;
  const latest = useRef({ me, participants, view, workspaceId, sectionSlug, sections, items });
  // People who joined since the list was last fetched: their first message
  // can arrive before the refetch does.
  const joined = useRef([]);
  useEffect(() => {
    latest.current = { me, participants, view, workspaceId, sectionSlug, sections, items };
  });
  return (event) => {
    if (event.type === "participant.joined" && event.data) joined.current.push(event.data);
    if (!enabled()) return;
    const cur = latest.current;
    if (shouldSuppressForFocus(event, cur)) return;

    const allParticipants = [...cur.participants, ...joined.current];
    const prefs = readEventPrefs();
    const n = notification(event, cur.me, allParticipants, { prefs, items: cur.items });
    if (!n) return;
    try {
      const shown = new Notification(n.title, { body: n.body, tag: n.tag, icon: "/apple-touch-icon.png" });
      shown.onclick = () => {
        window.focus();
        if (n.message) {
          window.dispatchEvent(new CustomEvent(SHOW_MESSAGE, { detail: n.message }));
        } else if (n.item) {
          window.dispatchEvent(new CustomEvent(OPEN_ITEM, { detail: n.item }));
        } else if (n.screen === "todos" && cur.workspaceId) {
          spaNavigate(`/w/${cur.workspaceId}/todos`);
        }
        shown.close();
      };
    } catch (err) {
      console.warn("Failed to show notification:", err);
    }
  };
}
