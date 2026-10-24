import { useEffect, useState } from "react";
import { AccountMenu, DevicePage, useAccount } from "./Account.jsx";
import { api } from "./api.js";
import Workspace from "./Workspace.jsx";
import Button from "./ui/Button.jsx";
import Field from "./ui/Field.jsx";
import { Input } from "./ui/Input.jsx";
import ThemeSwitch from "./ui/ThemeSwitch.jsx";

// Pages: "/" lists and creates workspaces; "/w/:id" is a workspace;
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
  const match = path.match(/^\/w\/([^/]+)/);
  if (match) return <Workspace id={match[1]} navigate={navigate} account={account} />;
  if (path === "/auth/device") return <DevicePage account={account} navigate={navigate} />;
  return <Home navigate={navigate} account={account} />;
}

function Home({ navigate, account }) {
  const [workspaces, setWorkspaces] = useState(null);
  const [error, setError] = useState("");
  const [form, setForm] = useState({ name: "", repo_url: "", canvas_url: "" });

  useEffect(() => {
    api("GET", "/api/workspaces").then(setWorkspaces, (e) => setError(e.message));
  }, []);

  const create = async (e) => {
    e.preventDefault();
    try {
      const ws = await api("POST", "/api/workspaces", { body: form });
      navigate(`/w/${ws.id}`);
    } catch (err) {
      setError(err.message);
    }
  };
  const field = (key) => ({
    value: form[key],
    onChange: (e) => setForm({ ...form, [key]: e.target.value }),
  });

  return (
    <main className="page">
      <header className="page-head">
        <span className="ws-mark" aria-hidden="true">
          U
        </span>
        <h1>Unison</h1>
        <span className="spacer" />
        <ThemeSwitch />
        <AccountMenu account={account} />
      </header>
      {error && <p role="alert">Error: {error}</p>}
      <section className="card">
        <h2 className="card-head">Workspaces</h2>
        {workspaces === null ? (
          <p className="card-body muted">Loading…</p>
        ) : workspaces.length === 0 ? (
          <p className="card-body muted">No workspaces yet.</p>
        ) : (
          <ul className="ws-list">
            {workspaces.map((ws) => (
              <li key={ws.id}>
                <a
                  href={`/w/${ws.id}`}
                  onClick={(e) => {
                    e.preventDefault();
                    navigate(`/w/${ws.id}`);
                  }}
                >
                  {ws.name}
                </a>{" "}
                {ws.repo_url && <small>({ws.repo_url})</small>}
              </li>
            ))}
          </ul>
        )}
      </section>
      <section className="card">
        <h2 className="card-head">Create a workspace</h2>
        <form className="form card-body" onSubmit={create}>
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
      <p>
        <small>
          Agents: read <a href="/agent.md">/agent.md</a>.
        </small>
      </p>
    </main>
  );
}
