import { useEffect, useState } from "react";
import { ArrowUpRight, CircleAlert, CircleCheck, CircleDashed, CircleX, GitMerge, GitPullRequest } from "lucide-react";
import { ago } from "./backlogFormat.js";

// Pending review (#127): open pull requests in the workspace's GitHub
// repositories that aren't set to auto-merge, i.e. that wait for a person.
// The server reads them with the workspace's GitHub token (Settings →
// Repositories) and caches them for a minute.

const POLL_MS = 60 * 1000;

// usePendingReviews loads the list when the workspace has a token, again
// every minute while the page is visible, and when the repositories or the
// token change. null until loaded (or without a token).
export function usePendingReviews(call, workspace) {
  const [reviews, setReviews] = useState(null);
  const enabled = Boolean(workspace?.has_github_token);
  const repoKey = (workspace?.repos || []).map((r) => r.url).join(" ");
  useEffect(() => {
    if (!enabled) {
      setReviews(null);
      return;
    }
    let stale = false;
    const load = () => {
      if (document.visibilityState === "hidden") return;
      call("GET", "/reviews").then(
        (r) => !stale && setReviews(r),
        () => {}, // Keep the last list; the next poll tries again.
      );
    };
    load();
    const timer = setInterval(load, POLL_MS);
    document.addEventListener("visibilitychange", load);
    return () => {
      stale = true;
      clearInterval(timer);
      document.removeEventListener("visibilitychange", load);
    };
  }, [call, enabled, repoKey]);
  return reviews;
}

// MERGE_STATES (#134): whether a pull request can be merged, as its icon:
// green when it's ready, red when a conflict or failing checks block it.
const MERGE_STATES = {
  ready: { icon: CircleCheck, label: "Ready to merge", tone: "ok" },
  conflict: { icon: GitMerge, label: "Merge conflict", tone: "bad" },
  checks_failing: { icon: CircleX, label: "Checks failing", tone: "bad" },
  checks_pending: { icon: CircleDashed, label: "Checks running", tone: "" },
};
const UNKNOWN_STATE = { icon: GitPullRequest, label: "", tone: "" };

// PendingReviewList is the sidebar section's body: a two-line row per pull
// request (title; repository#number and author), opening it on GitHub, then
// any repository that couldn't be read.
export function PendingReviewList({ reviews, id }) {
  const repoLabel = (slug) => slug.split("/").pop();
  return (
    <ul id={id} className="nav-list nav-list-small pending-reviews">
      {reviews.pulls.length === 0 && reviews.errors.length === 0 && <li className="nav-empty">Nothing waiting for review</li>}
      {reviews.pulls.map((p) => {
        const state = MERGE_STATES[p.merge_state] || UNKNOWN_STATE;
        const Icon = state.icon;
        return (
          <li key={p.url}>
            <a
              className={p.draft ? "nav-link pending-pr draft" : "nav-link pending-pr"}
              href={p.url}
              target="_blank"
              rel="noreferrer"
              title={`${p.repo}#${p.number}: ${p.title}\nby ${p.author}, opened ${ago(p.created_at)}${p.draft ? " · draft" : ""}${state.label ? `\n${state.label}` : ""}`}
            >
              <Icon
                className={state.tone && `merge-${state.tone}`}
                aria-label={state.label || undefined}
                aria-hidden={state.label ? undefined : "true"}
              />
              <span className="pending-pr-text">
                <span className="nav-text">{p.title}</span>
                <span className="pending-pr-ref">
                  {repoLabel(p.repo)}#{p.number} · {p.author}
                  {p.draft && " · draft"}
                </span>
              </span>
              <ArrowUpRight className="nav-ext" aria-label="(opens in a new tab)" />
            </a>
          </li>
        );
      })}
      {reviews.errors.map((e) => (
        <li key={e.repo} className="pending-error" title={e.error}>
          <CircleAlert aria-hidden="true" />
          <span className="nav-text">
            {repoLabel(e.repo)}: {e.error}
          </span>
        </li>
      ))}
    </ul>
  );
}
