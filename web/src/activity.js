import { useEffect, useState } from "react";

// How far this account has seen each workspace's collaboration (#300):
// { [workspaceId]: seq }, in localStorage, so it survives a reload and is
// shared between tabs. A workspace's list entry carries activity_seq, the
// number of its latest event by somebody else; when that is past what was
// seen here, the workspace switcher marks the workspace.
//
// The server keeps no read state for this: it is per browser, like the
// drafts and the theme.
const CHANGED = "unison:activity-seen";
const key = (userId) => `unison.activity.${userId || 0}`;

function read(userId) {
  try {
    const seen = JSON.parse(localStorage.getItem(key(userId)) || "{}");
    return seen && typeof seen === "object" ? seen : {};
  } catch {
    return {};
  }
}

// markSeen records that the account has seen workspace wsId up to seq. It
// only moves forward. With baseline it writes nothing for a workspace that
// already has a record: that gives a workspace met for the first time a
// starting point, so only what happens from then on is new.
export function markSeen(userId, wsId, seq, { baseline = false } = {}) {
  const seen = read(userId);
  const known = Object.hasOwn(seen, wsId);
  if (known && (baseline || seen[wsId] >= seq)) return;
  seen[wsId] = seq;
  try {
    localStorage.setItem(key(userId), JSON.stringify(seen));
  } catch {
    return; // No storage (private mode, quota): no marks rather than stuck ones.
  }
  // Other tabs hear "storage"; this one needs telling.
  window.dispatchEvent(new Event(CHANGED));
}

// useSeen is the record, kept current as this tab or another one moves it.
export function useSeen(userId) {
  const [seen, setSeen] = useState(() => read(userId));
  useEffect(() => {
    const refresh = () => setSeen(read(userId));
    refresh();
    const onStorage = (e) => {
      if (e.key === null || e.key === key(userId)) refresh();
    };
    window.addEventListener("storage", onStorage);
    window.addEventListener(CHANGED, refresh);
    return () => {
      window.removeEventListener("storage", onStorage);
      window.removeEventListener(CHANGED, refresh);
    };
  }, [userId]);
  return seen;
}

// hasUnseen: has somebody done something in ws since it was last seen?
export function hasUnseen(ws, seen) {
  return Object.hasOwn(seen, ws.id) && (ws.activity_seq || 0) > seen[ws.id];
}
