import { ago } from "./format.js";

// Settings → Agents: participants that joined with a pass, each acting for
// the member who invited them (read-only; revoking comes with #18).
export default function Agents({ participants }) {
  const agents = participants.filter((p) => p.sponsor);
  if (agents.length === 0) {
    return <p className="field-description">No agents yet. Invite one from the top bar: signed-in members get a single-use pass.</p>;
  }
  return (
    <ul className="agent-list">
      {agents.map((p) => (
        <li key={p.id} className="agent-row">
          <span className="avatar" aria-hidden="true">
            {p.emoji}
          </span>
          <div className="agent-names">
            <div>
              <span className="agent-name">{p.name}</span> <span className="muted">· agent of {p.sponsor.display_name}</span>
            </div>
            <div className="muted agent-meta">
              Seen {ago(p.last_seen_at)}
              {p.token_expires_at ? ` · credential expires ${new Date(p.token_expires_at).toLocaleDateString(undefined, { dateStyle: "medium" })}` : ""}
            </div>
          </div>
        </li>
      ))}
    </ul>
  );
}
