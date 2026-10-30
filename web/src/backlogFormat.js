// Formatting shared by the backlog list and the item dialog.

export function name(participants, id) {
  const p = participants.find((p) => p.id === id);
  return p ? `${p.emoji} ${p.name}` : `participant ${id}`;
}

export function hoursLeft(ms) {
  return Math.max(0, (ms - Date.now()) / 3600000).toFixed(1);
}

export function ago(ms) {
  const minutes = Math.max(0, Math.round((Date.now() - ms) / 60000));
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.round(minutes / 60);
  return hours < 48 ? `${hours}h ago` : `${Math.round(hours / 24)}d ago`;
}

export function meta(item) {
  if (item.status === "in_progress" && item.lease_expires_at) return `lease ${hoursLeft(item.lease_expires_at)}h left`;
  if (item.status === "open") return `added ${ago(item.created_at)}`;
  return item.close_note || "done";
}

export const STATUS_LABELS = { open: "Open", in_progress: "In progress", done: "Done" };

const GITHUB_PR = /^https?:\/\/github\.com\/([^/]+)\/([^/]+)\/pull\/(\d+)/i;
const GITHUB_REPO = /github\.com\/([^/]+)\/([^/#?]+?)(?:\.git)?\/?$/i;

// prLabel shortens a PR URL. In a workspace repository it is "#4" when that
// is the only repository, else "repo#4"; in another GitHub repository
// "owner/repo#4"; otherwise the host name.
export function prLabel(url, repos) {
  const pr = url.match(GITHUB_PR);
  if (!pr) {
    try {
      return new URL(url).hostname;
    } catch {
      return url;
    }
  }
  const [, owner, name, number] = pr;
  const same = (r) => {
    const m = r.url.match(GITHUB_REPO);
    return m && m[1].toLowerCase() === owner.toLowerCase() && m[2].toLowerCase() === name.toLowerCase();
  };
  const repo = repos.find(same);
  if (!repo) return `${owner}/${name}#${number}`;
  return repos.length === 1 ? `#${number}` : `${name}#${number}`;
}

// submitOnModEnter makes ⌘/Ctrl+Enter submit the surrounding form.
export function submitOnModEnter(e) {
  if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
    e.preventDefault();
    e.currentTarget.closest("form")?.requestSubmit();
  }
}
