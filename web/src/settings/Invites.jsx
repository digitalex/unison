import { useState } from "react";
import { Link2, Mail, X } from "lucide-react";
import Button from "../ui/Button.jsx";
import { Input } from "../ui/Input.jsx";
import { ago } from "./format.js";
import { UrlBox } from "./Webhooks.jsx";

function when(ms) {
  return new Date(ms).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}

// Settings → Invites: invite people (agents use passes) by email (whoever
// signs in with that verified email may join, once) or by link (anyone
// signed in with it may join). Every invite works for 48 hours, unless
// revoked. Every member may invite and revoke.
export default function Invites({ invites, account, call, onError }) {
  const [email, setEmail] = useState("");
  const [created, setCreated] = useState(null); // {url, invite}
  const run = (method, suffix, body) =>
    call(method, suffix, body).catch((e) => {
      onError(e.message);
      return null;
    });
  if (!account.user) return <p className="field-description">Sign in to invite people.</p>;

  const inviteEmail = async (e) => {
    e.preventDefault();
    if (await run("POST", "/invites", { email })) setEmail("");
  };
  const createLink = async (e) => {
    e.preventDefault();
    const r = await run("POST", "/invites", { link: true });
    if (r) setCreated(r);
  };
  const revoke = async (inv) => {
    const what = inv.kind === "email" ? `the invite for ${inv.email}` : "this invite link";
    if (!window.confirm(`Revoke ${what}? It stops working right away.`)) return;
    await run("DELETE", `/invites/${inv.id}`);
  };

  return (
    <>
      <form className="webhook-form-row" onSubmit={inviteEmail}>
        <Input type="email" required aria-label="Email to invite" placeholder="name@example.com" value={email} onChange={(e) => setEmail(e.target.value)} />
        <Button type="submit">
          <Mail aria-hidden="true" />
          Invite by email
        </Button>
      </form>
      <form className="webhook-form-row" onSubmit={createLink}>
        <span className="invite-link-label">Or share a link anyone signed in can use</span>
        <Button type="submit">
          <Link2 aria-hidden="true" />
          Create link
        </Button>
      </form>
      {created && (
        <div className="webhook-created">
          <p className="field-description">Invite link: shown only now, so copy it. Anyone signed in with it can join for the next 48 hours.</p>
          <UrlBox url={created.url} label="Invite link" />
          <Button type="button" variant="ghost" onClick={() => setCreated(null)}>
            Done
          </Button>
        </div>
      )}
      {invites.length === 0 ? (
        <p className="field-description">No pending invites.</p>
      ) : (
        <ul className="agent-list invite-list">
          {invites.map((inv) => (
            <li key={inv.id} className="agent-row">
              {inv.kind === "email" ? <Mail aria-hidden="true" /> : <Link2 aria-hidden="true" />}
              <div className="agent-names">
                <div className="agent-name">{inv.kind === "email" ? inv.email : "Invite link"}</div>
                <div className="muted agent-meta">
                  By {inv.created_by_name || `user ${inv.created_by}`} {ago(inv.created_at)}
                  {inv.expires_at ? ` · expires ${when(inv.expires_at)}` : ""}
                  {inv.kind === "link" ? ` · used ${inv.uses} ${inv.uses === 1 ? "time" : "times"}` : ""}
                </div>
              </div>
              <span className="spacer" />
              <Button
                variant="ghost"
                iconOnly
                className="button-danger"
                aria-label={`Revoke ${inv.kind === "email" ? `the invite for ${inv.email}` : "this invite link"}`}
                title="Revoke"
                onClick={() => revoke(inv)}
              >
                <X aria-hidden="true" />
              </Button>
            </li>
          ))}
        </ul>
      )}
    </>
  );
}
