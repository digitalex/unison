import { useEffect, useState } from "react";
import { Lock, UserPlus } from "lucide-react";
import { AccountMenu, SignInOptions, primaryEmail } from "./Account.jsx";
import { api } from "./api.js";
import { spaLink } from "./nav.js";
import Button from "./ui/Button.jsx";
import Field from "./ui/Field.jsx";
import { Input } from "./ui/Input.jsx";
import ThemeSwitch from "./ui/ThemeSwitch.jsx";

// Workspaces are private to their members. These are the pages everyone
// else sees: a sign-in or "ask for an invite" card when the server refuses
// (GatePage), and the invite-link page (/w/{ws}/join?invite=…, JoinPage).

function GateShell({ account, title, children }) {
  return (
    <main className="page">
      <header className="page-head">
        <a className="ws-mark" aria-label="All workspaces" {...spaLink("/")}>
          U
        </a>
        <h1>Unison</h1>
        <span className="spacer" />
        <ThemeSwitch />
        <AccountMenu account={account} />
      </header>
      <section className="card gate-card">
        <h2 className="card-head gate-head">
          <Lock aria-hidden="true" />
          {title}
        </h2>
        <div className="card-body form gate-body">{children}</div>
      </section>
    </main>
  );
}

function SignIn({ providers }) {
  return providers.length > 0 ? (
    <SignInOptions providers={providers} />
  ) : (
    <p className="muted">
      Sign-in isn't configured on this server. Whoever runs it can add GitHub or Google sign-in (or set <code>UNISON_AUTH=off</code> for local
      development).
    </p>
  );
}

// GatePage explains why the server refused (gate.code from the API) and
// offers the way in: sign in, or join (members, or people invited by email).
export function GatePage({ gate, account, workspaceId, onJoined }) {
  const { user, providers } = account;
  if (user) {
    const email = primaryEmail(user);
    return (
      <GateShell account={account} title="You're not a member of this workspace">
        <p>Workspaces are private. Ask a member for an invite link.</p>
        <p className="muted">
          If a member invited your email{email ? ` (${email})` : ""}, join now:
        </p>
        <JoinForm workspaceId={workspaceId} onJoined={onJoined} />
      </GateShell>
    );
  }
  if (gate.code === "legacy") {
    return (
      <GateShell account={account} title="This browser's old sign-in no longer works">
        <p>It's from before accounts. Sign in and you're back in if you're a member; otherwise ask a member for an invite.</p>
        <SignIn providers={providers} />
      </GateShell>
    );
  }
  if (gate.code === "not_member") {
    return (
      <GateShell account={account} title="You're no longer a member of this workspace">
        <p>Ask a member for a new invite, then sign in.</p>
        <SignIn providers={providers} />
      </GateShell>
    );
  }
  return (
    <GateShell account={account} title="This workspace is private">
      <p>Sign in to open it. Not a member yet? Ask a member for an invite link.</p>
      <SignIn providers={providers} />
    </GateShell>
  );
}

// JoinForm joins a workspace as the signed-in account (the session cookie),
// optionally with an invite link's token; onJoined gets the response
// ({participant, token}).
export function JoinForm({ workspaceId, invite = "", onJoined }) {
  const [name, setName] = useState("");
  const [emoji, setEmoji] = useState("");
  const [error, setError] = useState("");
  const join = async (e) => {
    e.preventDefault();
    setError("");
    try {
      onJoined(await api("POST", `/api/workspaces/${workspaceId}/join`, { body: { name, emoji, invite } }));
    } catch (err) {
      setError(err.message);
    }
  };
  return (
    <form className="form" onSubmit={join}>
      {error && (
        <p role="alert" className="field-error">
          {error}
        </p>
      )}
      <Field label="Name">
        <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="one word" />
      </Field>
      <Field label="Emoji">
        <Input value={emoji} onChange={(e) => setEmoji(e.target.value)} placeholder="🦊" />
      </Field>
      <p className="field-description">Leave either blank to be assigned one.</p>
      <div className="actions-row">
        <Button variant="primary" type="submit">
          <UserPlus aria-hidden="true" />
          Join
        </Button>
      </div>
    </form>
  );
}

// JoinPage is where an invite link lands. Signed out, it asks you to sign
// in (and comes back here); signed in, it shows the workspace and joins.
export function JoinPage({ id, account, navigate }) {
  const invite = new URLSearchParams(window.location.search).get("invite") || "";
  const { ready, user, providers } = account;
  const [target, setTarget] = useState(null); // {workspace_id, workspace_name, expires_at}
  const [error, setError] = useState("");
  useEffect(() => {
    if (!user || !invite) return;
    api("GET", `/api/invites/lookup?token=${encodeURIComponent(invite)}`).then(setTarget, (e) => setError(e.message));
  }, [user, invite]);

  if (!ready) {
    return (
      <GateShell account={account} title="Invitation">
        <p className="muted">Loading…</p>
      </GateShell>
    );
  }
  if (!user) {
    return (
      <GateShell account={account} title="You're invited to a Unison workspace">
        <p>Sign in to accept: you'll join as your account, and come right back here.</p>
        <SignIn providers={providers} />
      </GateShell>
    );
  }
  if (!invite || error) {
    return (
      <GateShell account={account} title="This invite link doesn't work">
        <p>It may have expired or been revoked. Ask a member for a new one.</p>
      </GateShell>
    );
  }
  if (!target) {
    return (
      <GateShell account={account} title="Invitation">
        <p className="muted">Loading…</p>
      </GateShell>
    );
  }
  return (
    <GateShell account={account} title={`Join ${target.workspace_name}`}>
      <p>
        You'll join as <strong>{user.display_name || primaryEmail(user)}</strong>. Pick a name and emoji for this workspace.
      </p>
      <JoinForm workspaceId={target.workspace_id || id} invite={invite} onJoined={() => navigate(`/w/${target.workspace_id || id}`)} />
    </GateShell>
  );
}
