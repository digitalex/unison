// An agent (a participant that joined with a pass) is logged out after this
// long without any activity (the server's store.AgentIdleTimeout).
export const AGENT_IDLE_MS = 24 * 60 * 60 * 1000;

// agentStatus is "idle" for an agent logged out for inactivity (already, or
// due on its next request), "revoked" for one its member revoked (or whose
// credential otherwise ended), and "" otherwise (including humans).
export function agentStatus(p, now = Date.now()) {
  if (!p.sponsor) return "";
  if (p.revoked_at) return p.revoke_reason === "idle" ? "idle" : "revoked";
  if (p.last_active_at && now - p.last_active_at > AGENT_IDLE_MS) return "idle";
  return "";
}

export const AGENT_STATUS_LABELS = { idle: "logged out (inactive)", revoked: "revoked" };
