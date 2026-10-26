import { useEffect, useState } from "react";
import { DevicePage, useAccount } from "./Account.jsx";
import { ApiError, api } from "./api.js";
import { JoinPage } from "./Gate.jsx";
import Landing, { SiteHeader } from "./Landing.jsx";
import Profile from "./Profile.jsx";
import Workspace from "./Workspace.jsx";
import Button from "./ui/Button.jsx";
import Field from "./ui/Field.jsx";
import { Input } from "./ui/Input.jsx";

// Pages: "/" is Home (the landing page, or the workspace list when signed
// in); "/w/:id" is a workspace, "/w/:id/settings" its settings,
// "/w/:id/join?invite=…" where an invite link lands; "/me" is your profile;
// "/auth/device" approves a `unison login`.
export default function App() {
  const account = useAccount();
  const [path, setPath] = useState(window.location.pathname);
  useEffect(() => {
    const onPop = () => setPath(window.location.pathname);
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, []);
  const navigate = (to) => {
    window.history.pushState(null, "", to);
    setPath(to);
  };
  const match = path.match(/^\/w\/([^/]+)(\/settings|\/join)?/);
  if (match && match[2] === "/join") return <JoinPage id={match[1]} account={account} navigate={navigate} />;
  // Keyed by id: switching workspaces remounts the page, so its data, live
  // stream and dialogs start fresh for the new one.
  if (match) return <Workspace key={match[1]} id={match[1]} view={match[2] ? "settings" : "board"} navigate={navigate} account={account} />;
  if (path === "/auth/device") return <DevicePage account={account} navigate={navigate} />;
  if (path === "/me") return <Profile account={account} navigate={navigate} />;
  return <Home navigate={navigate} account={account} />;
}

// Home: a landing page for signed-out visitors, your workspaces and the
// create form once signed in. Workspaces are private, so signed-out visitors
// see no lists, unless the server has no sign-in configured at all: then the
// list is fetched, and works only with privacy off (UNISON_AUTH=off).
function Home({ navigate, account }) {
  const [workspaces, setWorkspaces] = useState(null);
  const [error, setError] = useState("");
  const noSignIn = account.ready && account.providers.length === 0;
  const showLists = Boolean(account.user) || noSignIn;

  useEffect(() => {
    if (!showLists) return;
    api("GET", "/api/workspaces").then(setWorkspaces, (e) =>
      setError(
        noSignIn && e instanceof ApiError && e.status === 401
          ? "Workspaces are private, and sign-in isn't configured on this server. Whoever runs it can add GitHub or Google sign-in (or set UNISON_AUTH=off for local development)."
          : e.message,
      ),
    );
  }, [showLists]);

  const lists = <WorkspaceLists workspaces={workspaces} error={error} navigate={navigate} />;
  if (!account.ready) {
    return (
      <div className="landing">
        <SiteHeader account={account} />
      </div>
    );
  }
  if (!account.user) {
    return (
      <Landing account={account}>
        {noSignIn && (
          <section className="landing-workspaces" aria-label="Workspaces">
            {lists}
            {/* Without sign-in there is no other way to start a workspace. */}
            {workspaces && <CreateWorkspace navigate={navigate} />}
          </section>
        )}
      </Landing>
    );
  }
  return (
    <div className="landing home-shell">
      <SiteHeader account={account} />
      <main className="home">
        {lists}
        <CreateWorkspace navigate={navigate} />
        <p className="muted home-foot">
          Agents: read <a href="/agent.md">/agent.md</a>.
        </p>
      </main>
    </div>
  );
}

function WorkspaceLink({ ws, navigate }) {
  return (
    <li>
      <a
        href={`/w/${ws.id}`}
        onClick={(e) => {
          e.preventDefault();
          navigate(`/w/${ws.id}`);
        }}
      >
        {ws.name}
      </a>
      {ws.repo_url && <small className="muted">{ws.repo_url.replace(/^https?:\/\//, "")}</small>}
    </li>
  );
}

// WorkspaceLists: the workspaces you're a member of.
function WorkspaceLists({ workspaces, error, navigate }) {
  if (error) return <p role="alert">Error: {error}</p>;
  return (
    <section className="card">
      <h2 className="card-head">Your workspaces</h2>
      {workspaces === null ? (
        <p className="card-body muted">Loading…</p>
      ) : workspaces.length === 0 ? (
        <p className="card-body muted">No workspaces yet. Create one, or open an invite link from a member.</p>
      ) : (
        <ul className="ws-list">
          {workspaces.map((ws) => (
            <WorkspaceLink key={ws.id} ws={ws} navigate={navigate} />
          ))}
        </ul>
      )}
    </section>
  );
}

function CreateWorkspace({ navigate }) {
  const [form, setForm] = useState({ name: "", repo_url: "", canvas_url: "" });
  const [error, setError] = useState("");
  const create = async (e) => {
    e.preventDefault();
    setError("");
    try {
      const ws = await api("POST", "/api/workspaces", { body: form });
      navigate(`/w/${ws.id}`);
    } catch (err) {
      // UNISON_CREATORS limits creation to listed accounts.
      if (err instanceof ApiError && err.status === 403) {
        setError("This server only lets approved accounts create workspaces. Ask whoever runs it to add your email, or ask a member of a workspace for an invite.");
      } else if (err instanceof ApiError && err.status === 401) {
        setError("Sign in to create a workspace on this server.");
      } else setError(err.message);
    }
  };
  const field = (key) => ({
    value: form[key],
    onChange: (e) => setForm({ ...form, [key]: e.target.value }),
  });
  return (
    <section className="card">
      <h2 className="card-head">Create a workspace</h2>
      <form className="form card-body" onSubmit={create}>
        {error && (
          <p role="alert" className="field-error">
            {error}
          </p>
        )}
        <Field label="Name">
          <Input required {...field("name")} placeholder="Unison" />
        </Field>
        <Field label="Repository URL">
          <Input {...field("repo_url")} placeholder="https://github.com/digitalex/unison" />
        </Field>
        <Field label="Isocan canvas URL">
          <Input {...field("canvas_url")} placeholder="https://isocan.io/p/…" />
        </Field>
        <div className="actions-row">
          <Button variant="primary" type="submit">
            Create
          </Button>
        </div>
      </form>
    </section>
  );
}
