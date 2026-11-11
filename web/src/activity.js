import { useEffect, useState } from "react";

// Unread workspace activity (#300). GET /api/workspaces gives each
// workspace's activity_seq: the seq of its latest event by someone else.
// This remembers, per account and in localStorage, the last seq seen in
// each workspace; a workspace whose activity_seq is past it has unread
// activity. Other tabs follow along through the storage event.

const PREFIX = "unison.activity.seen.";
const CHANGE = "unison-activity-seen";

function key(userId) {
  return PREFIX + (userId || "open");
}

function read(userId) {
  try {
    return JSON.parse(localStorage.getItem(key(userId))) || {};
  } catch {
    return {};
  }
}

// markSeen records seq as seen in wsId, if it is newer than what was.
export function markSeen(userId, wsId, seq) {
  const seen = read(userId);
  if (seq == null || (wsId in seen && seen[wsId] >= seq)) return;
  seen[wsId] = seq;
  try {
    localStorage.setItem(key(userId), JSON.stringify(seen));
  } catch {
    return;
  }
  window.dispatchEvent(new Event(CHANGE));
}

// useSeen returns the account's seen seqs, kept current across tabs.
export function useSeen(userId) {
  const [seen, setSeen] = useState(() => read(userId));
  useEffect(() => {
    const update = () => setSeen(read(userId));
    const onStorage = (e) => {
      if (e.key === key(userId)) update();
    };
    update();
    window.addEventListener(CHANGE, update);
    window.addEventListener("storage", onStorage);
    return () => {
      window.removeEventListener(CHANGE, update);
      window.removeEventListener("storage", onStorage);
    };
  }, [userId]);
  return seen;
}

// isUnread says whether ws has activity past what was seen. A workspace
// never seen on this device isn't: there is nothing to compare with yet,
// and baseline() records where it stands.
export function isUnread(seen, ws) {
  return ws.activity_seq > 0 && ws.id in seen && ws.activity_seq > seen[ws.id];
}

// baseline records the current activity of workspaces this device hasn't
// seen yet, so only what happens from now on shows as unread.
export function baseline(userId, list) {
  const seen = read(userId);
  for (const ws of list) {
    if (!(ws.id in seen)) markSeen(userId, ws.id, ws.activity_seq || 0);
  }
}
