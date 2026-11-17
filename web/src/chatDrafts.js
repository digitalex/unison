// Persists unsent main-chat and thread-reply drafts per workspace (#309):
// in-memory cache mirrored to localStorage, validated on read, and bounded to
// MAX_THREAD_DRAFTS thread drafts per workspace.

const MAX_THREAD_DRAFTS = 50;
const keyFor = (wsId) => `unison.chat.drafts.${wsId}`;

// workspaceId -> { main: Draft | null, threads: Map<string, Draft> }
const mem = new Map();

function sanitizeDraft(raw) {
  if (!raw || typeof raw !== "object") return null;
  const text = typeof raw.text === "string" ? raw.text : "";
  const linked = typeof raw.linked === "boolean" ? raw.linked : null;
  const needsAction = Boolean(raw.needsAction);
  if (!text && linked === null && !needsAction) return null;
  const updatedAt = typeof raw.updatedAt === "number" && Number.isFinite(raw.updatedAt) ? raw.updatedAt : Date.now();
  return { text, linked, needsAction, updatedAt };
}

function loadWorkspace(wsId) {
  if (!wsId) return { main: null, threads: new Map() };
  if (mem.has(wsId)) return mem.get(wsId);
  const state = { main: null, threads: new Map() };
  try {
    const raw = localStorage.getItem(keyFor(wsId));
    if (raw) {
      const parsed = JSON.parse(raw);
      if (parsed && typeof parsed === "object") {
        state.main = sanitizeDraft(parsed.main);
        if (parsed.threads && typeof parsed.threads === "object") {
          const entries = Object.entries(parsed.threads)
            .map(([id, d]) => [id, sanitizeDraft(d)])
            .filter(([, d]) => Boolean(d))
            .sort((a, b) => b[1].updatedAt - a[1].updatedAt)
            .slice(0, MAX_THREAD_DRAFTS);
          for (const [id, d] of entries) state.threads.set(id, d);
        }
      }
    }
  } catch {
    // Ignore corrupt localStorage entries.
  }
  mem.set(wsId, state);
  return state;
}

function saveWorkspace(wsId, state) {
  if (!wsId) return;
  try {
    if (!state.main && state.threads.size === 0) {
      localStorage.removeItem(keyFor(wsId));
      return;
    }
    const threads = {};
    for (const [id, d] of state.threads) threads[id] = d;
    localStorage.setItem(keyFor(wsId), JSON.stringify({ main: state.main, threads }));
  } catch {
    // Ignore storage quota / privacy mode errors.
  }
}

export function getDraft(wsId, threadId) {
  const state = loadWorkspace(wsId);
  return threadId ? state.threads.get(String(threadId)) || null : state.main;
}

export function setDraft(wsId, threadId, draft) {
  if (!wsId) return;
  const state = loadWorkspace(wsId);
  const next = sanitizeDraft({ ...draft, updatedAt: Date.now() });
  if (!threadId) {
    state.main = next;
  } else {
    const k = String(threadId);
    if (!next) {
      state.threads.delete(k);
    } else {
      state.threads.delete(k);
      state.threads.set(k, next);
      while (state.threads.size > MAX_THREAD_DRAFTS) {
        const oldest = state.threads.keys().next().value;
        state.threads.delete(oldest);
      }
    }
  }
  saveWorkspace(wsId, state);
}

export function clearDraft(wsId, threadId) {
  if (!wsId) return;
  const state = loadWorkspace(wsId);
  if (!threadId) state.main = null;
  else state.threads.delete(String(threadId));
  saveWorkspace(wsId, state);
}

export function clearWorkspaceDrafts(wsId) {
  if (!wsId) return;
  mem.delete(wsId);
  try {
    localStorage.removeItem(keyFor(wsId));
  } catch {
    // Ignore storage errors.
  }
}

export function resolveImageUploadInDraft(wsId, threadId, placeholder, replacement) {
  if (!wsId) return;
  const cur = getDraft(wsId, threadId);
  if (!cur) {
    if (replacement) setDraft(wsId, threadId, { text: replacement });
    return;
  }
  const text = cur.text.includes(placeholder)
    ? cur.text.replace(placeholder, replacement)
    : replacement
      ? `${cur.text}${cur.text && !/\s$/.test(cur.text) ? "\n" : ""}${replacement}`
      : cur.text;
  setDraft(wsId, threadId, { ...cur, text });
}
