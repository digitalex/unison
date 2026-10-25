import { useState } from "react";
import { X } from "lucide-react";
import Button from "../ui/Button.jsx";
import { Input } from "../ui/Input.jsx";

// Settings → Repositories. Changes apply immediately (each is its own API
// call).
export default function Repositories({ repos, call, onError }) {
  const [url, setUrl] = useState("");
  const [label, setLabel] = useState("");
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
    if (await run("POST", "/repos", { url, label })) {
      setUrl("");
      setLabel("");
    }
  };
  return (
    <>
      {repos.length === 0 && <p className="field-description">No repositories yet.</p>}
      {repos.map((r) => (
        <RepoRow key={r.id} repo={r} run={run} />
      ))}
      <form className="repo-form" onSubmit={add}>
        <Input className="repo-label-input" aria-label="Label (optional)" value={label} onChange={(e) => setLabel(e.target.value)} placeholder="Label" />
        <Input type="url" required aria-label="Repository URL" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://github.com/owner/repo" />
        <Button type="submit">Add</Button>
      </form>
    </>
  );
}

function RepoRow({ repo, run }) {
  const [label, setLabel] = useState(repo.label);
  const [url, setUrl] = useState(repo.url);
  const dirty = label !== repo.label || url !== repo.url;
  const save = (e) => {
    e.preventDefault();
    run("PATCH", `/repos/${repo.id}`, { label, url });
  };
  return (
    <form className="repo-form" onSubmit={save}>
      <Input className="repo-label-input" aria-label={`Label for ${repo.url}`} value={label} onChange={(e) => setLabel(e.target.value)} placeholder="Label" />
      <Input type="url" required aria-label="Repository URL" value={url} onChange={(e) => setUrl(e.target.value)} />
      {dirty && <Button type="submit">Save</Button>}
      <Button
        type="button"
        iconOnly
        variant="ghost"
        className="button-danger"
        aria-label={`Remove ${repo.url}`}
        title="Remove"
        onClick={() => window.confirm(`Remove ${repo.url} from this workspace?`) && run("DELETE", `/repos/${repo.id}`)}
      >
        <X aria-hidden="true" />
      </Button>
    </form>
  );
}

