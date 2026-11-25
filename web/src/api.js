// A thin client for the Unison HTTP API. The CLI calls exactly the same
// endpoints; see internal/guide/agent.md.tmpl for the full mapping.

const tokenKey = (ws) => `unison.token.${ws}`;

export function getToken(ws) {
  return localStorage.getItem(tokenKey(ws)) || "";
}

export function setToken(ws, token) {
  if (token) localStorage.setItem(tokenKey(ws), token);
  else localStorage.removeItem(tokenKey(ws));
}

let pageUnloading = false;
if (typeof window !== "undefined") {
  window.addEventListener("beforeunload", () => {
    pageUnloading = true;
  });
  window.addEventListener("pagehide", () => {
    pageUnloading = true;
  });
  window.addEventListener("pageshow", () => {
    pageUnloading = false;
  });
}

export class PageUnloadingError extends Error {
  constructor() {
    super("page unloading");
    this.name = "PageUnloadingError";
  }
}

export class ApiError extends Error {
  // code is the server's machine-readable reason, when it gives one (e.g.
  // "sign_in", "legacy", "not_member", "join_first", "session_expired").
  constructor(status, message, code = "") {
    super(message);
    this.status = status;
    this.code = code;
  }
}

const delay = (ms) => new Promise((r) => setTimeout(r, ms));

export async function api(method, path, { body, token } = {}) {
  const headers = {};
  if (body !== undefined) headers["Content-Type"] = "application/json";
  if (token) headers["Authorization"] = `Bearer ${token}`;
  // Cookie-authenticated writes must carry this header (CSRF protection: a
  // foreign page cannot add it without a CORS preflight, which we refuse).
  if (method !== "GET" && method !== "HEAD") headers["X-Unison-CSRF"] = "1";
  const opts = {
    method,
    headers,
    redirect: "manual",
    body: body === undefined ? undefined : JSON.stringify(body),
  };
  let resp;
  try {
    resp = await fetch(path, opts);
  } catch (err) {
    if (pageUnloading) throw new PageUnloadingError();
    if ((method === "GET" || method === "HEAD") && err?.name !== "AbortError") {
      await delay(150);
      try {
        resp = await fetch(path, opts);
      } catch (retryErr) {
        if (pageUnloading) throw new PageUnloadingError();
        throw retryErr;
      }
    } else {
      throw err;
    }
  }
  if (resp.type === "opaqueredirect" || (resp.status >= 300 && resp.status < 400)) {
    throw new ApiError(401, "Your session expired. Reload the page to sign in again.", "session_expired");
  }
  const data = await resp.json().catch(() => ({}));
  if (!resp.ok) throw new ApiError(resp.status, data.error || resp.statusText, data.code);
  return data;
}

// uploadImage sends an image file's bytes to POST …/images (#292) and
// returns the image ({id, name, url, markdown, …}).
export async function uploadImage(ws, token, file) {
  const headers = { "Content-Type": "application/octet-stream", "X-Unison-CSRF": "1" };
  if (token) headers["Authorization"] = `Bearer ${token}`;
  const name = encodeURIComponent(file.name || "screenshot.png");
  const resp = await fetch(`/api/workspaces/${ws}/images?name=${name}`, { method: "POST", headers, body: file });
  const data = await resp.json().catch(() => ({}));
  if (!resp.ok) throw new ApiError(resp.status, data.error || resp.statusText, data.code);
  return data;
}

// uploadArtifact uploads a file to POST …/artifacts (#918) and returns its metadata.
export async function uploadArtifact(ws, token, file, { name, title, item_id } = {}) {
  const headers = { "Content-Type": "application/octet-stream", "X-Unison-CSRF": "1" };
  if (token) headers["Authorization"] = `Bearer ${token}`;
  const params = new URLSearchParams();
  params.set("name", (name || file.name || "artifact").trim());
  if (title && title.trim()) params.set("title", title.trim());
  if (item_id && Number(item_id) > 0) params.set("item_id", String(Number(item_id)));
  const resp = await fetch(`/api/workspaces/${ws}/artifacts?${params.toString()}`, {
    method: "POST",
    headers,
    redirect: "manual",
    body: file,
  });
  if (resp.type === "opaqueredirect" || (resp.status >= 300 && resp.status < 400)) {
    throw new ApiError(401, "Your session expired. Reload the page to sign in again.", "session_expired");
  }
  const data = await resp.json().catch(() => ({}));
  if (!resp.ok) throw new ApiError(resp.status, data.error || resp.statusText, data.code);
  return data;
}

// replaceArtifact replaces an existing artifact's bytes in-place via PUT …/artifacts/{file} (#918).
export async function replaceArtifact(ws, token, artifactName, file, { title, item_id } = {}) {
  const headers = { "Content-Type": "application/octet-stream", "X-Unison-CSRF": "1" };
  if (token) headers["Authorization"] = `Bearer ${token}`;
  const params = new URLSearchParams();
  if (title !== undefined) params.set("title", title);
  if (item_id !== undefined) params.set("item_id", String(Number(item_id) || 0));
  const qs = params.toString();
  const suffix = `/api/workspaces/${ws}/artifacts/${encodeURIComponent(artifactName)}${qs ? `?${qs}` : ""}`;
  const resp = await fetch(suffix, {
    method: "PUT",
    headers,
    redirect: "manual",
    body: file,
  });
  if (resp.type === "opaqueredirect" || (resp.status >= 300 && resp.status < 400)) {
    throw new ApiError(401, "Your session expired. Reload the page to sign in again.", "session_expired");
  }
  const data = await resp.json().catch(() => ({}));
  if (!resp.ok) throw new ApiError(resp.status, data.error || resp.statusText, data.code);
  return data;
}

// fetchArtifactBytes fetches raw artifact bytes with cache: "no-cache" so ETag
// revalidation always runs and live SSE updates show immediately (#918).
export async function fetchArtifactBytes(ws, token, artifactName, { signal } = {}) {
  const headers = {};
  if (token) headers["Authorization"] = `Bearer ${token}`;
  const resp = await fetch(`/api/workspaces/${ws}/artifacts/${encodeURIComponent(artifactName)}`, {
    method: "GET",
    headers,
    cache: "no-cache",
    redirect: "manual",
    signal,
  });
  if (resp.type === "opaqueredirect" || (resp.status >= 300 && resp.status < 400)) {
    throw new ApiError(401, "Your session expired. Reload the page to sign in again.", "session_expired");
  }
  if (!resp.ok) {
    const data = await resp.json().catch(() => ({}));
    throw new ApiError(resp.status, data.error || resp.statusText, data.code);
  }
  return resp;
}

// Returns a function that calls the API for one workspace: with a
// participant token, or (token "") with the session cookie.
export function workspaceApi(ws, token) {
  return (method, suffix = "", body) => api(method, `/api/workspaces/${ws}${suffix}`, { body, token });
}
