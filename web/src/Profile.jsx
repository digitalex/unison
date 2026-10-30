import { useCallback, useEffect, useState } from "react";
import { Github, LogIn, Monitor, SquareTerminal } from "lucide-react";
import { Avatar, PROVIDER_LABELS, SignInOptions, primaryEmail } from "./Account.jsx";
import { api } from "./api.js";
import { SiteHeader } from "./Landing.jsx";
import Notifications from "./settings/Notifications.jsx";
import Button from "./ui/Button.jsx";

// The providers Unison knows; the server says which are configured.
const PROVIDERS = ["github", "google"];

function ago(ms) {
  const minutes = Math.max(0, Math.round((Date.now() - ms) / 60000));
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.round(minutes / 60);
  return hours < 48 ? `${hours}h ago` : `${Math.round(hours / 24)}d ago`;
}

function day(ms) {
  return new Date(ms).toLocaleDateString(undefined, { dateStyle: "medium" });
}

function ProviderIcon({ name }) {
  return name === "github" ? <Github aria-hidden="true" /> : <LogIn aria-hidden="true" />;
}

// Profile (/me): who you are, how you sign in, where you're signed in, and
// the agents you invited. Every action has a CLI twin (unison whoami --user,
// sessions, agents).
export default function Profile({ account, navigate }) {
  const [sessions, setSessions] = useState(null);
  const [agents, setAgents] = useState(null);
  const [error, setError] = useState("");
  const load = useCallback(() => {
    api("GET", "/api/me/sessions").then(setSessions, (e) => setError(e.message));
    api("GET", "/api/me/agents").then(setAgents, (e) => setError(e.message));
  }, []);
  useEffect(() => {
    if (account.user) load();
  }, [account.user, load]);
  const run = async (method, path) => {
    setError("");
    try {
      await api(method, path);
    } catch (e) {
      setError(e.message);
    }
    load();
  };

  const { user } = account;
  return (
    <div className="landing home-shell">
      <SiteHeader account={account} />
      <main className="home profile">
        <h1 className="profile-title">Your profile</h1>
        {!account.ready ? (
          <p className="muted">Loading…</p>
        ) : !user ? (
          <section className="card">
            <div className="card-body form">
              <p>Sign in to see your profile, sessions and agents.</p>
              {account.providers.length > 0 ? (
                <SignInOptions providers={account.providers} returnTo="/me" />
              ) : (
                <p className="muted">Sign-in isn't configured on this server.</p>
              )}
            </div>
          </section>
        ) : (
          <>
            {error && <p role="alert" className="error-bar profile-error">Error: {error}</p>}
            <ProfileCard user={user} />
            <Card id="notifications" title="Desktop notifications" description="Browser notifications on this device.">
              <Notifications />
            </Card>
            <Logins user={user} providers={account.providers} />
            <Sessions sessions={sessions} run={run} onSignedOut={account.reload} />
            <Agents agents={agents} run={run} navigate={navigate} />
            <DeleteAccount onDeleted={() => account.reload().then(() => navigate("/"))} />
          </>
        )}
      </main>
    </div>
  );
}

function Card({ id, title, description, children }) {
  return (
    <section className="card profile-card" aria-labelledby={`${id}-title`}>
      <header className="profile-card-head">
        <h2 id={`${id}-title`}>{title}</h2>
        {description && <p className="field-description">{description}</p>}
      </header>
      <div className="profile-card-body">{children}</div>
    </section>
  );
}

function ProfileCard({ user }) {
  const email = primaryEmail(user);
  return (
    <Card id="profile" title="Profile">
      <div className="profile-who">
        <Avatar user={user} size={56} />
        <div className="profile-names">
          <div className="profile-name">{user.display_name || email || `User ${user.id}`}</div>
          {email && <div className="muted">{email}</div>}
          <div className="muted profile-since">Member since {day(user.created_at)}</div>
        </div>
      </div>
    </Card>
  );
}

// Linked logins: each identity, and "Link …" for configured providers you
// haven't linked (OAuth in link mode attaches it to this account).
function Logins({ user, providers }) {
  const configured = new Map(providers.map((p) => [p.name, p]));
  const linked = new Set(user.identities.map((i) => i.provider));
  return (
    <Card id="logins" title="Linked logins" description="Sign in with any of these; they all reach this account.">
      <ul className="prow-list">
        {user.identities.map((i) => (
          <li key={`${i.provider}:${i.subject}`} className="prow">
            <span className="prow-icon">
              <ProviderIcon name={i.provider} />
            </span>
            <div className="prow-main">
              <div className="prow-title">{PROVIDER_LABELS[i.provider] || i.provider}</div>
              <div className="muted prow-meta">{i.email || `Account ${i.subject}`}</div>
            </div>
            <span className="chip">{i.email_verified ? "verified" : "unverified"}</span>
          </li>
        ))}
        {PROVIDERS.filter((name) => !linked.has(name)).map((name) => (
          <li key={name} className="prow">
            <span className="prow-icon">
              <ProviderIcon name={name} />
            </span>
            <div className="prow-main">
              <div className="prow-title">{PROVIDER_LABELS[name]}</div>
              <div className="muted prow-meta">{configured.has(name) ? "Not linked" : "Not available on this server"}</div>
            </div>
            {configured.has(name) && (
              <a className="button button-secondary" href={`${configured.get(name).start_url}?link=1&return_to=${encodeURIComponent("/me")}`}>
                Link {PROVIDER_LABELS[name]}
              </a>
            )}
          </li>
        ))}
      </ul>
    </Card>
  );
}

function Sessions({ sessions, run, onSignedOut }) {
  const others = (sessions || []).filter((s) => !s.current).length;
  const signOut = async (s) => {
    await run("DELETE", `/api/me/sessions/${s.id}`);
    if (s.current) onSignedOut();
  };
  return (
    <Card id="sessions" title="Active sessions" description="Browsers and CLIs (unison login) signed in to this account.">
      {sessions === null ? (
        <p className="muted">Loading…</p>
      ) : (
        <ul className="prow-list">
          {sessions.map((s) => (
            <li key={s.id} className="prow">
              <span className="prow-icon">{s.kind === "cli" ? <SquareTerminal aria-hidden="true" /> : <Monitor aria-hidden="true" />}</span>
              <div className="prow-main">
                <div className="prow-title">
                  {s.label || (s.kind === "cli" ? "unison CLI" : "Browser")}
                  {s.current && <span className="chip chip-accent prow-chip">This device</span>}
                </div>
                <div className="muted prow-meta">
                  Last seen {ago(s.last_seen_at)} · signed in {day(s.created_at)}
                </div>
              </div>
              <Button variant="ghost" className="button-danger" onClick={() => signOut(s)}>
                Sign out
              </Button>
            </li>
          ))}
        </ul>
      )}
      {others > 0 && (
        <div className="actions-row">
          <Button onClick={() => window.confirm(`Sign out ${others} other session(s)?`) && run("POST", "/api/me/sessions/revoke-others")}>
            Sign out everywhere else
          </Button>
        </div>
      )}
    </Card>
  );
}

// DeleteAccount (#135): deletes the account for good, after a typed
// confirmation since it can't be undone.
function DeleteAccount({ onDeleted }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const remove = async () => {
    const typed = window.prompt(
      "Delete your account for good? Workspaces only you are in are deleted. In shared workspaces your messages and items stay, shown as DeletedUser, and your agents stop working.\n\nType DELETE to confirm.",
    );
    if (typed?.trim().toUpperCase() !== "DELETE") return;
    setBusy(true);
    setError("");
    try {
      await api("DELETE", "/api/me/user", { body: { confirm: true } });
      await onDeleted();
    } catch (e) {
      setError(e.message);
      setBusy(false);
    }
  };
  return (
    <Card id="delete-account" title="Delete account" description="Removes your account, sign-ins and devices. This can't be undone.">
      {error && <p role="alert" className="error-bar">Error: {error}</p>}
      <div className="actions-row">
        <Button variant="ghost" className="button-danger" disabled={busy} onClick={remove}>
          {busy ? "Deleting…" : "Delete my account"}
        </Button>
      </div>
    </Card>
  );
}

function Agents({ agents, run, navigate }) {
  return (
    <Card id="agents" title="Your agents" description="Agents you invited with a pass, in every workspace. Revoking stops an agent's credential for good.">
      {agents === null ? (
        <p className="muted">Loading…</p>
      ) : agents.length === 0 ? (
        <p className="field-description">No agents yet. Invite one with a workspace's Invite button.</p>
      ) : (
        <ul className="prow-list">
          {agents.map((a) => (
            <li key={a.id} className={a.revoked_at ? "prow revoked" : "prow"}>
              <span className="avatar" aria-hidden="true">
                {a.emoji}
              </span>
              <div className="prow-main">
                <div className="prow-title">
                  {a.name}{" "}
                  <span className="muted">
                    in{" "}
                    <a
                      href={`/w/${a.workspace_id}`}
                      onClick={(e) => {
                        e.preventDefault();
                        navigate(`/w/${a.workspace_id}`);
                      }}
                    >
                      {a.workspace_name}
                    </a>
                  </span>
                </div>
                <div className="muted prow-meta">
                  Seen {ago(a.last_seen_at)} · {a.revoked_at ? `${a.revoke_reason === "idle" ? "logged out (inactive)" : "revoked"} ${day(a.revoked_at)}` : `credential expires ${day(a.token_expires_at)}`}
                </div>
              </div>
              {a.revoked_at ? (
                <span className="chip">{a.revoke_reason === "idle" ? "logged out" : "revoked"}</span>
              ) : (
                <Button
                  variant="ghost"
                  className="button-danger"
                  onClick={() => window.confirm(`Revoke ${a.name} in ${a.workspace_name}? Its credential stops working now.`) && run("POST", `/api/me/agents/${a.id}/revoke`)}
                >
                  Revoke
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}
