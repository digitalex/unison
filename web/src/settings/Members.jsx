import { useState } from "react";
import { BellPlus, X } from "lucide-react";
import { Avatar } from "../Account.jsx";
import { SendToInboxDialog } from "../Inbox.jsx";
import Button from "../ui/Button.jsx";

function participantNames(ps) {
  return ps.map((p) => `${p.emoji} ${p.name}`).join(" · ");
}

// Settings → Members: the workspace's accounts with their linked
// participants. Every member is equal: any signed-in member may remove one,
// and anyone may send something to a member's inbox.
export default function Members({ members, invites, account, call, onError }) {
  const [sendTo, setSendTo] = useState(null); // The member whose inbox to send to.
  const pending = invites.filter((i) => i.kind === "email");
  const remove = async (m) => {
    const name = m.display_name || `user ${m.user_id}`;
    if (!window.confirm(`Remove ${name} from this workspace's members? Their participants and history stay.`)) return;
    try {
      await call("DELETE", `/members/${m.user_id}`);
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
              <div className="member-name">{m.display_name || `User ${m.user_id}`}</div>
              <div className="muted member-participants">{participantNames(m.participants) || "No participants"}</div>
            </div>
            {m.participants.length > 0 && (
              <Button variant="ghost" iconOnly aria-label={`Send to ${m.display_name || `user ${m.user_id}`}'s inbox`} title="Send to inbox" onClick={() => setSendTo(m)}>
                <BellPlus aria-hidden="true" />
              </Button>
            )}
            {account.user && (
              <Button
                variant="ghost"
                iconOnly
                className="button-danger"
                aria-label={`Remove ${m.display_name || `user ${m.user_id}`}`}
                title="Remove member"
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
      {members.unlinked.length > 0 && (
        <p className="muted member-unlinked">From before accounts, no longer able to get in: {participantNames(members.unlinked)}</p>
      )}
      {!account.user && members.members.length > 0 && <p className="field-description">Sign in to remove members.</p>}
      {sendTo && (
        <SendToInboxDialog
          to={sendTo.participants[0].name}
          label={sendTo.display_name || sendTo.participants[0].name}
          call={call}
          onClose={() => setSendTo(null)}
          onError={onError}
        />
      )}
    </>
  );
}
