import { X } from "lucide-react";
import { Avatar } from "../Account.jsx";
import Button from "../ui/Button.jsx";

function participantNames(ps) {
  return ps.map((p) => `${p.emoji} ${p.name}`).join(" · ");
}

// Settings → Members: the workspace's accounts with their linked
// participants. The owner (#296) may remove anyone else; everyone else may
// only leave. The owner can't leave: they delete the workspace instead.
export default function Members({ members, invites, account, ownerId, call, onError, navigate }) {
  const pending = invites.filter((i) => i.kind === "email");
  const me = account.user?.id;
  const isOwner = Boolean(me) && me === ownerId;
  const canRemove = (m) => Boolean(me) && m.user_id !== ownerId && (isOwner || m.user_id === me);
  const remove = async (m) => {
    const name = m.display_name || `user ${m.user_id}`;
    const self = m.user_id === me;
    const question = self
      ? "Leave this workspace? Your participants and history stay; you need a new invite to come back."
      : `Remove ${name} from this workspace's members? Their participants and history stay.`;
    if (!window.confirm(question)) return;
    try {
      await call("DELETE", `/members/${m.user_id}`);
      if (self) navigate("/");
    } catch (e) {
      onError(e.message);
    }
  };
  return (
    <>
      {members.members.length === 0 && <p className="muted member-empty">No members yet.</p>}
      <ul className="member-list">
        {members.members.map((m) => (
          <li key={m.user_id} className="member-row">
            <Avatar user={m} size={28} />
            <div className="member-names">
              <div className="member-name">
                {m.display_name || `User ${m.user_id}`}
                {m.user_id === ownerId && <span className="chip chip-accent prow-chip">Owner</span>}
              </div>
              <div className="muted member-participants">{participantNames(m.participants) || "No participants"}</div>
            </div>
            {canRemove(m) && (
              <Button
                variant="ghost"
                iconOnly
                className="button-danger"
                aria-label={m.user_id === me ? "Leave workspace" : `Remove ${m.display_name || `user ${m.user_id}`}`}
                title={m.user_id === me ? "Leave workspace" : "Remove member"}
                onClick={() => remove(m)}
              >
                <X aria-hidden="true" />
              </Button>
            )}
          </li>
        ))}
      </ul>
      {pending.length > 0 && (
        <p className="muted member-unlinked">
          Invited, not joined yet: {pending.map((i) => i.email).join(" · ")} (<a href="#invites">manage</a>)
        </p>
      )}
      {!account.user && members.members.length > 0 && <p className="field-description">Sign in to leave or manage members.</p>}
    </>
  );
}
