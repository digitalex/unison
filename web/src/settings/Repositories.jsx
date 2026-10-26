import { useState } from "react";
import { X } from "lucide-react";
import { repoName } from "../repos.js";
import Button from "../ui/Button.jsx";
import { Input } from "../ui/Input.jsx";

// Settings → Repositories, each known by the name in its URL. Changes apply
// immediately (each is its own API call).
export default function Repositories({ repos, call, onError }) {
  const [url, setUrl] = useState("");
  const run = (method, suffix, body) =>
    call(method, suffix, body).then(
      () => true,
      (e) => {
        onError(e.message);
        return false;
      },
    );
  const add = async (e) => {
    e.preventDefault();
    if (await run("POST", "/repos", { url })) setUrl("");
  };
  return (
    <>
      {repos.length === 0 && <p className="field-description">No repositories yet.</p>}
      {repos.map((r) => (
        <RepoRow key={r.id} repo={r} run={run} />
      ))}
      <form className="repo-form" onSubmit={add}>
        <Input type="url" required aria-label="Repository URL" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://github.com/owner/repo" />
        <Button type="submit">Add</Button>
      </form>
    </>
  );
}

function RepoRow({ repo, run }) {
  const [url, setUrl] = useState(repo.url);
  const name = repoName(repo.url);
  const save = (e) => {
    e.preventDefault();
    run("PATCH", `/repos/${repo.id}`, { url });
  };
  return (
    <form className="repo-form" onSubmit={save}>
      <span className="repo-name" title={repo.url}>
        {name}
      </span>
      <Input type="url" required aria-label={`URL of ${name}`} value={url} onChange={(e) => setUrl(e.target.value)} />
      {url !== repo.url && <Button type="submit">Save</Button>}
      <Button
        type="button"
        iconOnly
        variant="ghost"
        className="button-danger"
        aria-label={`Remove ${name}`}
        title="Remove"
        onClick={() => window.confirm(`Remove ${repo.url} from this workspace?`) && run("DELETE", `/repos/${repo.id}`)}
      >
        <X aria-hidden="true" />
      </Button>
    </form>
  );
}
