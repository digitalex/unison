import { useCallback, useEffect, useState } from "react";
import { Check, Link2, LogIn, LogOut, SquareTerminal, X } from "lucide-react";
import { api } from "./api.js";
import Button from "./ui/Button.jsx";
import Field from "./ui/Field.jsx";
import { Input } from "./ui/Input.jsx";
import Popover from "./ui/Popover.jsx";
import ThemeSwitch from "./ui/ThemeSwitch.jsx";

// Accounts (optional): sign in with GitHub or Google. Nothing in a
// workspace needs it yet; participants still join with a name.

// useAccount loads the configured providers and the signed-in user.
export function useAccount() {
  const [state, setState] = useState({ ready: false, providers: [], user: null });
  const load = useCallback(async () => {
    const [providers, user] = await Promise.all([
      api("GET", "/api/auth/providers").catch(() => []),
      api("GET", "/api/me/user").catch(() => null),
    ]);
    setState({ ready: true, providers, user });
  }, []);
  useEffect(() => {
    load();
  }, [load]);
  const signOut = async () => {
    await api("POST", "/api/auth/logout").catch(() => {});
    setState((s) => ({ ...s, user: null }));
  };
  return { ...state, reload: load, signOut };
}

function currentPath() {
  return window.location.pathname + window.location.search;
}

function primaryEmail(user) {
  if (!user.identities) return "";
  const id = user.identities.find((i) => i.email_verified && i.email) || user.identities.find((i) => i.email);
  return id ? id.email : "";
}

const PROVIDER_LABELS = { github: "GitHub", google: "Google" };

export function SignInOptions({ providers, returnTo = currentPath() }) {
  return (
    <div className="provider-list">
      {providers.map((p) => (
        <a key={p.name} className="button button-secondary" href={`${p.start_url}?return_to=${encodeURIComponent(returnTo)}`}>
          Continue with {p.label}
        </a>
      ))}
    </div>
  );
}

// Avatar shows an account's picture, or its initial.
export function Avatar({ user, size = 24 }) {
  const initial = (user.display_name || primaryEmail(user) || "?").trim().charAt(0).toUpperCase();
  return user.avatar_url ? (
    <img className="account-avatar" src={user.avatar_url} alt="" width={size} height={size} referrerPolicy="no-referrer" />
  ) : (
    <span className="account-avatar account-initial" style={{ width: size, height: size }} aria-hidden="true">
      {initial}
    </span>
  );
}

// AccountMenu is the top-bar control: "Sign in" (a popover with the
// providers) or the signed-in person's avatar menu. Hidden when the server
// has no providers configured.
export function AccountMenu({ account }) {
  const { ready, providers, user } = account;
  if (!ready || (!user && providers.length === 0)) return null;
  if (!user) {
    return (
      <Popover
        title="Sign in"
        trigger={
          <>
            <LogIn aria-hidden="true" />
            <span className="label">Sign in</span>
          </>
        }
      >
        {() => (
          <>
            <div className="popover-title">Sign in to Unison</div>
            <p className="muted account-note">Optional for now: workspaces still work by joining with a name.</p>
            <SignInOptions providers={providers} />
          </>
        )}
      </Popover>
    );
  }
  const email = primaryEmail(user);
  const via = [...new Set(user.identities.map((i) => PROVIDER_LABELS[i.provider] || i.provider))].join(" · ");
  return (
    <Popover title={`Account: ${user.display_name || email}`} className="button button-ghost button-icon account-trigger" trigger={<Avatar user={user} />}>
      {(close) => (
        <div className="account-menu">
          <div className="account-who">
            <Avatar user={user} size={36} />
            <div className="account-names">
              <div className="account-name">{user.display_name || email || `User ${user.id}`}</div>
              {email && <div className="muted account-email">{email}</div>}
              {via && <div className="muted account-via">Signed in with {via}</div>}
            </div>
          </div>
          <div className="actions-row">
            <Button
              variant="secondary"
              onClick={async () => {
                close();
                await account.signOut();
              }}
            >
              <LogOut aria-hidden="true" />
              Sign out
            </Button>
          </div>
        </div>
      )}
    </Popover>
  );
}

function formatCode(raw) {
  const c = raw.toUpperCase().replace(/[^A-Z]/g, "").slice(0, 8);
  return c.length > 4 ? `${c.slice(0, 4)}-${c.slice(4)}` : c;
}

// DevicePage (/auth/device) approves a `unison login` from the terminal.
export function DevicePage({ account, navigate }) {
  const [code, setCode] = useState(() => formatCode(new URLSearchParams(window.location.search).get("code") || ""));
  const [lookup, setLookup] = useState(null); // {user_code, pending}
  const [result, setResult] = useState(""); // approved | denied
  const [error, setError] = useState("");
  const { ready, user, providers } = account;

  useEffect(() => {
    setLookup(null);
    if (!user || code.length !== 9) return;
    api("GET", `/api/auth/device/lookup?code=${encodeURIComponent(code)}`).then(setLookup, (e) => setError(e.message));
  }, [user, code]);

  const decide = async (approve) => {
    setError("");
    try {
      const r = await api("POST", "/api/auth/device/approve", { body: { user_code: code, approve } });
      setResult(r.status);
    } catch (e) {
      setError(e.message);
    }
  };

  return (
    <main className="page">
      <header className="page-head">
        <a
          className="ws-mark"
          href="/"
          aria-label="All workspaces"
          onClick={(e) => {
            e.preventDefault();
            navigate("/");
          }}
        >
          U
        </a>
        <h1>Unison</h1>
        <span className="spacer" />
        <ThemeSwitch />
        <AccountMenu account={account} />
      </header>
      <section className="card">
        <h2 className="card-head device-head">
          <SquareTerminal aria-hidden="true" />
          Sign in the Unison CLI
        </h2>
        <div className="card-body form device-body">
          {!ready ? (
            <p className="muted">Loading…</p>
          ) : !user ? (
            <>
              <p>
                Sign in to approve <code>unison login</code> on your computer.
              </p>
              {providers.length > 0 ? (
                <SignInOptions providers={providers} />
              ) : (
                <p className="muted">Sign-in is not configured on this server.</p>
              )}
            </>
          ) : result ? (
            <DeviceResult result={result} />
          ) : (
            <>
              <Field label="Code shown in your terminal">
                <Input
                  className="device-input"
                  value={code}
                  onChange={(e) => setCode(formatCode(e.target.value))}
                  placeholder="BCDF-GHJK"
                  autoComplete="off"
                  spellCheck={false}
                  inputMode="text"
                  aria-describedby="device-help"
                />
              </Field>
              <p id="device-help" className="muted">
                Approve only if you just ran <code>unison login</code> yourself and this code matches your terminal. The CLI will act as{" "}
                <strong>{user.display_name || primaryEmail(user)}</strong>.
              </p>
              {lookup && !lookup.pending && <p role="alert">This code is unknown or has expired. Run <code>unison login</code> again.</p>}
              {error && <p role="alert">Error: {error}</p>}
              <div className="actions-row">
                <Button variant="primary" disabled={!lookup?.pending} onClick={() => decide(true)}>
                  <Check aria-hidden="true" />
                  Approve
                </Button>
                <Button variant="secondary" disabled={!lookup?.pending} onClick={() => decide(false)}>
                  <X aria-hidden="true" />
                  Deny
                </Button>
              </div>
            </>
          )}
        </div>
      </section>
    </main>
  );
}

function DeviceResult({ result }) {
  return result === "approved" ? (
    <p className="device-result">
      <Check aria-hidden="true" /> Approved. You can return to your terminal.
    </p>
  ) : (
    <p className="device-result">
      <X aria-hidden="true" /> Denied. The terminal was not signed in.
    </p>
  );
}

// ClaimPrompt offers to link this browser's participant to the signed-in
// account ("This is you?"). Links are per browser: each device's
// participant is linked by signing in on it. Dismissal is remembered here.
export function ClaimPrompt({ me, account, workspaceId, call, onClaimed, onError }) {
  const key = `unison.claimDismissed.${workspaceId}.${me.id}`;
  const [dismissed, setDismissed] = useState(() => localStorage.getItem(key) === "1");
  if (!account.user || me.user_id || dismissed) return null;
  const link = async () => {
    try {
      const p = await call("POST", "/claim");
      onClaimed({ ...me, user_id: p.user_id, user: { id: account.user.id, display_name: account.user.display_name } });
    } catch (e) {
      onError(e.message);
    }
  };
  const dismiss = () => {
    localStorage.setItem(key, "1");
    setDismissed(true);
  };
  return (
    <div className="claim-prompt" role="status">
      <Link2 aria-hidden="true" />
      <span className="claim-text">
        This is you? Link{" "}
        <strong>
          {me.emoji} {me.name}
        </strong>{" "}
        to your account.
      </span>
      <Button variant="primary" onClick={link}>
        Link
      </Button>
      <Button variant="ghost" iconOnly aria-label="Dismiss" title="Dismiss" onClick={dismiss}>
        <X aria-hidden="true" />
      </Button>
    </div>
  );
}
