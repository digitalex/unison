import { useEffect, useState } from "react";
import { Lock, UserPlus } from "lucide-react";
import { AccountMenu, SignInOptions, participantName, primaryEmail } from "./Account.jsx";
import { ApiError, api } from "./api.js";
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

function SignIn({ providers, returnTo }) {
  return providers.length > 0 ? (
    <SignInOptions providers={providers} returnTo={returnTo} />
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

// The invite token travels in the link's fragment (#invite=…), which the
// browser never sends to the server. The join page keeps it in this tab's
// sessionStorage while the visitor signs in, and takes it out of the address
// bar right away, so it never appears in a URL the server (or its request
// logs) sees: not the page, not the sign-in return path, not an API call.
const inviteKey = (id) => `unison.invite.${id}`;

function takeInviteToken(id) {
  const fromLink = new URLSearchParams(window.location.hash.slice(1)).get("invite") || new URLSearchParams(window.location.search).get("invite");
  if (fromLink) {
    sessionStorage.setItem(inviteKey(id), fromLink);
    window.history.replaceState(null, "", window.location.pathname);
  }
  return fromLink || sessionStorage.getItem(inviteKey(id)) || "";
}

// JoinPage is where an invite link lands. Signed out, it asks you to sign
// in and brings you back; signed in, it joins you (with your account's
// first name) and opens the workspace, with no further clicks.
export function JoinPage({ id, account }) {
  const [invite] = useState(() => takeInviteToken(id));
  const { ready, user, providers } = account;
  const state = useAutoJoin(id, invite, user);

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
        <p>Sign in to accept: you'll join as your account, and go straight to the workspace.</p>
        <SignIn providers={providers} returnTo={`/w/${id}/join`} />
      </GateShell>
    );
  }
  if (state.status === "invalid") {
    return (
      <GateShell account={account} title="This invite link doesn't work">
        <p>It may have expired or been revoked. Ask a member for a new one.</p>
      </GateShell>
    );
  }
  if (state.status === "failed") {
    // Joining didn't go through (say, the name is taken): pick one.
    return (
      <GateShell account={account} title={`Join ${state.target.workspace_name}`}>
        <p role="alert" className="field-error">
          {state.error}
        </p>
        <JoinForm workspaceId={state.target.workspace_id} invite={invite} onJoined={() => openWorkspace(id, state.target.workspace_id)} />
      </GateShell>
    );
  }
  return (
    <GateShell account={account} title={state.target ? `Joining ${state.target.workspace_name}…` : "Invitation"}>
      <p className="muted" aria-live="polite">
        {state.target ? `Joining as ${user.display_name || primaryEmail(user)}…` : "Checking the invite…"}
      </p>
    </GateShell>
  );
}

// openWorkspace forgets the used invite token and opens the workspace in
// place of the join page (Back doesn't return to a spent link).
function openWorkspace(id, wsID) {
  sessionStorage.removeItem(inviteKey(id));
  window.history.replaceState(null, "", `/w/${wsID}`);
  window.dispatchEvent(new PopStateEvent("popstate"));
}

// useAutoJoin looks the invite up and joins as the signed-in account: with
// the account's first name, or (if that name is taken there) an assigned
// one. Members already in are simply let in. Without a token (say, Back to
// a used join page) it opens the workspace page, which lets members in.
function useAutoJoin(id, invite, user) {
  const [state, setState] = useState({ status: "checking", target: null, error: "" });
  useEffect(() => {
    if (!user) return;
    if (!invite) return openWorkspace(id, id);
    let cancelled = false;
    (async () => {
      let target;
      try {
        target = await api("POST", "/api/invites/lookup", { body: { token: invite } });
      } catch {
        sessionStorage.removeItem(inviteKey(id));
        if (!cancelled) setState({ status: "invalid", target: null, error: "" });
        return;
      }
      if (cancelled) return;
      setState({ status: "joining", target, error: "" });
      const join = (name) => api("POST", `/api/workspaces/${target.workspace_id}/join`, { body: { name, emoji: "", invite } });
      try {
        await join(participantName(user)).catch((e) => (e instanceof ApiError && e.status === 409 ? join("") : Promise.reject(e)));
        if (!cancelled) openWorkspace(id, target.workspace_id);
      } catch (e) {
        if (!cancelled) setState({ status: "failed", target, error: e.message });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [user, invite]);
  return state;
}
