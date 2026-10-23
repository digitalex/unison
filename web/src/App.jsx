import { useEffect, useState } from "react";
import { api } from "./api.js";
import Workspace from "./Workspace.jsx";

// Two pages: "/" lists and creates workspaces; "/w/:id" is a workspace.
export default function App() {
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
  if (match) return <Workspace id={match[1]} navigate={navigate} />;
  return <Home navigate={navigate} />;
}

function Home({ navigate }) {
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
    <main>
      <h1>Unison</h1>
      {error && <p role="alert">Error: {error}</p>}
      <h2>Workspaces</h2>
      {workspaces === null ? (
        <p>Loading…</p>
      ) : workspaces.length === 0 ? (
        <p>No workspaces yet.</p>
      ) : (
        <ul>
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
      <h2>Create a workspace</h2>
      <form onSubmit={create}>
        <p>
          <label>
            Name <input required {...field("name")} placeholder="Unison" />
          </label>
        </p>
        <p>
          <label>
            Repository URL{" "}
            <input {...field("repo_url")} placeholder="https://github.com/digitalex/unison" size={45} />
          </label>
        </p>
        <p>
          <label>
            Isocan canvas URL{" "}
            <input {...field("canvas_url")} placeholder="https://isocan.io/p/…" size={45} />
          </label>
        </p>
        <button type="submit">Create</button>
      </form>
      <p>
        <small>
          Agents: read <a href="/agent.md">/agent.md</a>.
        </small>
      </p>
    </main>
  );
}
