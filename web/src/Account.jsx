import { useCallback, useEffect, useState } from "react";
import { Check, LogIn, LogOut, SquareTerminal, UserRound, X } from "lucide-react";
import { api } from "./api.js";
import { spaLink } from "./nav.js";
import Button from "./ui/Button.jsx";
import Field from "./ui/Field.jsx";
import { Input } from "./ui/Input.jsx";
import Popover from "./ui/Popover.jsx";
import ThemeSwitch from "./ui/ThemeSwitch.jsx";

// Accounts: sign in with GitHub, Google or Apple. Workspaces are private to their
// members, so people sign in to open theirs or to accept an invite.

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

export function primaryEmail(user) {
  if (!user.identities) return "";
  const id = user.identities.find((i) => i.email_verified && i.email) || user.identities.find((i) => i.email);
  return id ? id.email : "";
}

// participantName is a participant name from an account name: its first
// word, without characters names can't have (whitespace, "@").
export function participantName(user) {
  return (user?.display_name || "").trim().split(/\s+/)[0].replace(/@/g, "");
}

export const PROVIDER_LABELS = { github: "GitHub", google: "Google", apple: "Apple" };

// AppleLogo is the Apple mark for Sign in with Apple buttons (#136); lucide
// has no brand icons. Padded like lucide's 24px icons.
export function AppleLogo(props) {
  return (
    <svg viewBox="-2 -2 28 28" fill="currentColor" width="24" height="24" {...props}>
      <path d="M12.152 6.896c-.948 0-2.415-1.078-3.96-1.04-2.04.027-3.91 1.183-4.961 3.014-2.117 3.675-.546 9.103 1.519 12.09 1.013 1.454 2.208 3.09 3.792 3.039 1.52-.065 2.09-.987 3.935-.987 1.831 0 2.35.987 3.96.948 1.637-.026 2.676-1.48 3.676-2.948 1.156-1.688 1.636-3.325 1.662-3.415-.039-.013-3.182-1.221-3.22-4.857-.026-3.04 2.48-4.494 2.597-4.559-1.429-2.09-3.623-2.324-4.39-2.376-2-.156-3.675 1.09-4.61 1.09zM15.53 3.83c.843-1.012 1.4-2.427 1.245-3.83-1.207.052-2.662.805-3.532 1.818-.78.896-1.454 2.338-1.273 3.714 1.338.104 2.715-.688 3.559-1.701" />
    </svg>
  );
}

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

// AccountMenu is the account control: "Sign in" (a popover with the
// providers) or the signed-in person's avatar menu. Hidden when the server
// has no providers configured. showName puts your name beside the avatar
// (the workspace sidebar); menuExtra(close) adds to the bottom of the menu.
export function AccountMenu({ account, showName = false, menuExtra }) {
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
            <p className="muted account-note">Workspaces are private: sign in to open yours or to accept an invite.</p>
            <SignInOptions providers={providers} />
          </>
        )}
      </Popover>
    );
  }
  const email = primaryEmail(user);
  const via = [...new Set(user.identities.map((i) => PROVIDER_LABELS[i.provider] || i.provider))].join(" · ");
  return (
    <Popover
      title={`Account: ${user.display_name || email}`}
      className={showName ? "button button-ghost account-trigger account-trigger-named" : "button button-ghost button-icon account-trigger"}
      trigger={
        <>
          <Avatar user={user} />
          {showName && <span className="account-trigger-name">{user.display_name || email}</span>}
        </>
      }
    >
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
            <a className="button button-secondary" {...spaLink("/me", close)}>
              <UserRound aria-hidden="true" />
              Your profile
            </a>
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
          {menuExtra?.(close)}
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
