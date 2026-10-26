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

export class ApiError extends Error {
  // code is the server's machine-readable reason, when it gives one (e.g.
  // "sign_in", "legacy", "not_member", "join_first").
  constructor(status, message, code = "") {
    super(message);
    this.status = status;
    this.code = code;
  }
}

export async function api(method, path, { body, token } = {}) {
  const headers = {};
  if (body !== undefined) headers["Content-Type"] = "application/json";
  if (token) headers["Authorization"] = `Bearer ${token}`;
  // Cookie-authenticated writes must carry this header (CSRF protection: a
  // foreign page cannot add it without a CORS preflight, which we refuse).
  if (method !== "GET" && method !== "HEAD") headers["X-Unison-CSRF"] = "1";
  const resp = await fetch(path, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const data = await resp.json().catch(() => ({}));
  if (!resp.ok) throw new ApiError(resp.status, data.error || resp.statusText, data.code);
  return data;
}

// Returns a function that calls the API for one workspace: with a
// participant token, or (token "") with the session cookie.
export function workspaceApi(ws, token) {
  return (method, suffix = "", body) => api(method, `/api/workspaces/${ws}${suffix}`, { body, token });
}
