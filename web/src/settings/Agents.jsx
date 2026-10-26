import { AGENT_STATUS_LABELS, agentStatus } from "../agents.js";
import { ago } from "./format.js";

// Settings → Agents: participants that joined with a pass, each acting for
// the member who invited them (read-only; members revoke their own agents
// from their profile). Agents idle for 6 hours are logged out.
export default function Agents({ participants }) {
  const agents = participants.filter((p) => p.sponsor);
  if (agents.length === 0) {
    return <p className="field-description">No agents yet. Invite one from the top bar: signed-in members get a single-use pass.</p>;
  }
  return (
    <ul className="agent-list">
      {agents.map((p) => (
        <AgentRow key={p.id} p={p} />
      ))}
    </ul>
  );
}

function AgentRow({ p }) {
  const status = agentStatus(p);
  return (
    <li className={status ? "agent-row agent-out" : "agent-row"}>
      <span className="avatar" aria-hidden="true">
        {p.emoji}
      </span>
      <div className="agent-names">
        <div>
          <span className="agent-name">{p.name}</span> <span className="muted">· agent of {p.sponsor.display_name}</span>
        </div>
        <div className="muted agent-meta">
          Seen {ago(p.last_seen_at)}
          {status === "idle" && p.last_active_at ? ` · last active ${ago(p.last_active_at)}` : ""}
        </div>
      </div>
      {status && <span className="chip agent-status">{AGENT_STATUS_LABELS[status]}</span>}
    </li>
  );
}
