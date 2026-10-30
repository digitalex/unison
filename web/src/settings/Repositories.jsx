import { useState } from "react";
import { KeyRound, X } from "lucide-react";
import { repoName } from "../repos.js";
import Button from "../ui/Button.jsx";
import { Input } from "../ui/Input.jsx";

// Settings → Repositories, each known by the name in its URL. Changes apply
// immediately (each is its own API call).
export default function Repositories({ repos, hasGitHubToken, call, onError }) {
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
      <GitHubToken hasToken={hasGitHubToken} run={run} />
    </>
  );
}

// GitHubToken sets the token the server lists pull requests pending review
// with (#127). It is write-only: once saved, only whether one is set shows.
function GitHubToken({ hasToken, run }) {
  const [token, setToken] = useState("");
  const [replacing, setReplacing] = useState(false);
  const save = async (e) => {
    e.preventDefault();
    if (await run("PATCH", "", { github_token: token })) {
      setToken("");
      setReplacing(false);
    }
  };
  const remove = () => window.confirm("Remove the GitHub token? Pending review will stop showing pull requests.") && run("PATCH", "", { github_token: "" });
  return (
    <div className="github-token">
      <h3 className="github-token-title">GitHub token</h3>
      <p className="field-description">
        Generate a PAT with PR read access to all the repos in the workspace, and paste it here. Pending review in the sidebar uses it to list open pull requests that aren't set to auto-merge. It's
        stored on the server and never shown again.
      </p>
      {hasToken && !replacing ? (
        <div className="repo-form">
          <span className="github-token-set">
            <KeyRound aria-hidden="true" />A token is set.
          </span>
          <Button type="button" onClick={() => setReplacing(true)}>
            Replace
          </Button>
          <Button type="button" variant="ghost" className="button-danger" onClick={remove}>
            Remove
          </Button>
        </div>
      ) : (
        <form className="repo-form" onSubmit={save}>
          <Input
            type="password"
            required
            autoComplete="off"
            spellCheck={false}
            aria-label="GitHub personal access token"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            placeholder="github_pat_… or ghp_…"
          />
          <Button type="submit">Save</Button>
          {replacing && (
            <Button type="button" variant="ghost" onClick={() => setReplacing(false)}>
              Cancel
            </Button>
          )}
        </form>
      )}
    </div>
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
