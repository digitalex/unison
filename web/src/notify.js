import { useEffect, useRef, useState } from "react";
import { SHOW_MESSAGE } from "./nav.js";

// Desktop (browser) notifications: for chat messages that mention you or
// ask you to act, and for people and agents joining. They are off until you
// turn them on (browsers only ask for permission after a click), remembered
// per browser, and shown only while the tab isn't in front: what is on
// screen needs no notification.

const KEY = "unison.notify";

// notifySupported says whether this browser has notifications at all.
export const notifySupported = typeof window !== "undefined" && "Notification" in window;

// useNotifyPref is [on, turnOn, turnOff]: on only while permission is
// granted and you haven't turned them off here.
export function useNotifyPref() {
  const [on, setOn] = useState(() => notifySupported && Notification.permission === "granted" && localStorage.getItem(KEY) !== "off");

  useEffect(() => {
    const update = () => setOn(enabled());
    window.addEventListener("unison.notify.change", update);
    window.addEventListener("storage", update);
    return () => {
      window.removeEventListener("unison.notify.change", update);
      window.removeEventListener("storage", update);
    };
  }, []);

  const turnOn = async () => {
    const permission = Notification.permission === "default" ? await Notification.requestPermission() : Notification.permission;
    if (permission !== "granted") return setOn(false);
    localStorage.setItem(KEY, "on");
    setOn(true);
    window.dispatchEvent(new Event("unison.notify.change"));
  };
  const turnOff = () => {
    localStorage.setItem(KEY, "off");
    setOn(false);
    window.dispatchEvent(new Event("unison.notify.change"));
  };
  return [on, turnOn, turnOff];
}

function enabled() {
  return notifySupported && Notification.permission === "granted" && localStorage.getItem(KEY) !== "off";
}

// mentions says whether body @mentions name (or @all / @everyone), as the
// highlight in chat does.
function mentions(body, name) {
  const escaped = name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return new RegExp(`@(${escaped}|all|everyone)\\b`, "i").test(body);
}

function preview(text) {
  const t = text.replace(/\s+/g, " ").trim();
  return t.length > 200 ? `${t.slice(0, 197)}…` : t;
}

// notification turns a workspace event into {title, body, tag, message} to
// show, or null when it isn't for you. mine is the set of your participant
// IDs (all of your account's devices).
export function notification(event, me, mine, participants) {
  if (!me || mine.has(event.actor_id)) return null;
  const who = participants.find((p) => p.id === event.actor_id);
  const name = who ? `${who.emoji ? `${who.emoji} ` : ""}${who.name}` : "Someone";
  const d = event.data || {};
  if (event.type === "message.created") {
    const asksMe = d.action_required && (d.action_for || []).some((id) => mine.has(id));
    if (asksMe) return { title: `${name} needs you`, body: preview(d.body), tag: `msg-${d.id}`, message: d.id };
    if (mentions(d.body, me.name)) return { title: `${name} mentioned you`, body: preview(d.body), tag: `msg-${d.id}`, message: d.id };
    return null;
  }
  if (event.type === "participant.joined") {
    const agentOf = d.sponsor ? ` (agent of ${d.sponsor.display_name})` : "";
    return { title: `${d.emoji ? `${d.emoji} ` : ""}${d.name} joined`, body: `${d.name}${agentOf} joined the workspace.`, tag: `joined-${d.id}` };
  }
  return null;
}

// useNotifier returns a function to call with each workspace event: it
// shows the event's notification when they are on and the tab is in the
// background. Clicking one brings the tab forward (and opens the message).
export function useNotifier(me, participants) {
  const latest = useRef({ me, participants });
  // People who joined since the list was last fetched: their first message
  // can arrive before the refetch does.
  const joined = useRef([]);
  useEffect(() => {
    latest.current = { me, participants };
  });
  return (event) => {
    if (event.type === "participant.joined" && event.data) joined.current.push(event.data);
    if (!enabled() || (document.visibilityState === "visible" && document.hasFocus())) return;
    const { me } = latest.current;
    const participants = [...latest.current.participants, ...joined.current];
    const mine = new Set(participants.filter((p) => p.id === me?.id || (me?.user_id && p.user_id === me.user_id)).map((p) => p.id));
    if (me) mine.add(me.id);
    const n = notification(event, me, mine, participants);
    if (!n) return;
    const shown = new Notification(n.title, { body: n.body, tag: n.tag });
    shown.onclick = () => {
      window.focus();
      if (n.message) window.dispatchEvent(new CustomEvent(SHOW_MESSAGE, { detail: n.message }));
      shown.close();
    };
  };
}
