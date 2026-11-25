import { Bot, Check, CheckSquare, Github, ListChecks, LogIn, PenTool, Plus, Repeat, SlidersHorizontal } from "lucide-react";
import { useEffect, useState } from "react";
import { AccountMenu, AppleLogo } from "./Account.jsx";
import { api } from "./api.js";
import HeroShot from "./HeroShot.jsx";
import { LegalLinks } from "./Legal.jsx";
import { repoName } from "./repos.js";
import Button from "./ui/Button.jsx";
import ThemeSwitch from "./ui/ThemeSwitch.jsx";
import { NewWorkspaceDialog } from "./WorkspaceSwitcher.jsx";

const REPO_URL = "https://github.com/digitalex/unison";

// SiteHeader is the slim bar on Home (signed in or out): the mark, the
// theme switch and the account menu.
export function SiteHeader({ account }) {
  return (
    <header className="site-head">
      <a className="site-brand" href="/">
        <span className="ws-mark" aria-hidden="true">
          U
        </span>
        Unison
      </a>
      <span className="spacer" />
      <ThemeSwitch />
      <AccountMenu account={account} />
    </header>
  );
}

// The five things Unison is for (#132): the first two cards are wide.
const FEATURES = [
  {
    icon: Bot,
    title: "Any model, any harness",
    text: "Invite any coding agent with a one-time pass from the sidebar; paste the prompt into your agent and it gets right to work.",
  },
  {
    icon: PenTool,
    title: "Design and code, in one place",
    text: "Keep an infinite Isocan (isocan.io) canvas right next to your backlog, linked GitHub repositories, and pull requests.",
  },
  {
    icon: Repeat,
    title: "Recurring tasks, handled",
    text: "Hand off scheduled chores like log checks, CI audits, triage sweeps, and release notes in one place.",
  },
  {
    icon: ListChecks,
    title: "You're in control",
    text: "Add and rank work in the backlog; agents claim the next item, attach pull requests, and flag you in chat when they need a decision.",
  },
  {
    icon: SlidersHorizontal,
    title: "You set the parallelism",
    text: "Run one agent or many alongside your teammates in a private shared workspace.",
  },
];

// Sign-in buttons: GitHub first and primary, other providers secondary.
function SignInButtons({ providers }) {
  if (providers.length === 0) return <p className="muted">Sign-in isn't configured on this server.</p>;
  const sorted = [...providers].sort((a, b) => (a.name === "github" ? -1 : b.name === "github" ? 1 : 0));
  return (
    <div className="hero-actions">
      {sorted.map((p, i) => {
        const Icon = { github: Github, apple: AppleLogo }[p.name] || LogIn;
        return (
          <a
            key={p.name}
            className={`button button-large ${i === 0 ? "button-primary" : "button-secondary"}`}
            href={`${p.start_url}?return_to=${encodeURIComponent("/")}`}
          >
            <Icon aria-hidden="true" />
            Sign in with {p.label}
          </a>
        );
      })}
    </div>
  );
}

// The public counters (#301), in the order they are shown. The windows are
// rolling: "today" is the past 24 hours.
const STATS = [
  { key: "total", label: "Total workspaces" },
  { key: "created_last_7_days", label: "Created in the last 7 days" },
  { key: "created_today", label: "Created today" },
];

// WorkspaceStats shows how many workspaces this server has: counts only,
// from GET /api/workspaces/stats, which needs no sign-in. The row holds its
// place while the numbers load, and goes away if they can't be had: the
// page reads fine without it.
function WorkspaceStats() {
  const [stats, setStats] = useState(null);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    let live = true;
    api("GET", "/api/workspaces/stats").then(
      (s) => live && setStats(s),
      () => live && setFailed(true),
    );
    return () => {
      live = false;
    };
  }, []);
  if (failed) return null;
  return (
    <dl className="stats" aria-busy={!stats}>
      {STATS.map(({ key, label }) => (
        <div key={key} className="stat">
          <dt>{label}</dt>
          <dd>{stats ? Number(stats[key] || 0).toLocaleString() : "\u2013"}</dd>
        </div>
      ))}
    </dl>
  );
}

function WorkspaceDock({ account, workspaces, workspacesError, showLists, navigate, onCreate }) {
  const [myTodos, setMyTodos] = useState([]);
  useEffect(() => {
    if (!account.user) {
      setMyTodos([]);
      return;
    }
    let live = true;
    const refresh = () =>
      api("GET", "/api/me/todos?status=active").then(
        (list) => live && setMyTodos(Array.isArray(list) ? list : []),
        () => {},
      );
    refresh();
    return () => {
      live = false;
    };
  }, [account.user?.id]);

  const refreshTodos = () =>
    api("GET", "/api/me/todos?status=active").then(
      (list) => setMyTodos(Array.isArray(list) ? list : []),
      () => {},
    );

  const sameEntry = (a, b) => (a.kind || "") === (b.kind || "") && a.workspace_id === b.workspace_id && a.id === b.id;

  const todoItemNum = (td) => td.item_number || td.item?.number || td.item_id || 0;

  const markTodoDone = (td) => {
    setMyTodos((list) => list.filter((x) => !sameEntry(x, td)));
    if (td.kind === "section_item") {
      api("POST", `/api/workspaces/${td.workspace_id}/backlog/${todoItemNum(td) || td.id}/close`, { body: { note: "" } }).catch(refreshTodos);
      return;
    }
    api("POST", `/api/workspaces/${td.workspace_id}/todos/${td.id}/done`, { body: { note: "" } }).catch(refreshTodos);
  };

  const answerSectionChoice = (td, choice) => {
    if (td.close_on_answer) {
      setMyTodos((list) => list.filter((x) => !sameEntry(x, td)));
    } else {
      setMyTodos((list) => list.map((x) => (sameEntry(x, td) ? { ...x, answered_choice: choice } : x)));
    }
    api("POST", `/api/workspaces/${td.workspace_id}/backlog/${todoItemNum(td) || td.id}/answer`, { body: { choice } }).catch(refreshTodos);
  };

  const snoozeTodo = (td, duration) => {
    setMyTodos((list) => list.filter((x) => !sameEntry(x, td)));
    api("POST", `/api/workspaces/${td.workspace_id}/todos/${td.id}/snooze`, { body: { duration } }).catch(refreshTodos);
  };

  const sorted = workspaces ? [...workspaces].sort((a, b) => a.name.localeCompare(b.name)) : null;
  const who = account.user?.display_name || account.user?.email || "";
  return (
    <aside className="ws-dock-card" aria-label="Your workspaces">
      <div className="ws-dock-head">
        <h2>Your workspaces</h2>
        {who && <span className="muted">{who}</span>}
      </div>
      {!showLists ? (
        <div className="ws-dock-body">
          <p className="muted">Sign in to view your workspaces, create a new one, or accept a teammate's invite link.</p>
          {account.ready && <SignInButtons providers={account.providers} />}
        </div>
      ) : workspacesError ? (
        <p role="alert" className="ws-dock-body">
          Error: {workspacesError}
        </p>
      ) : sorted === null ? (
        <p className="ws-dock-body muted">Loading…</p>
      ) : sorted.length === 0 ? (
        <p className="ws-dock-body muted">No workspaces yet. Create one, or open an invite link from a teammate.</p>
      ) : (
        <ul className="ws-dock-list">
          {sorted.map((ws) => (
            <li key={ws.id}>
              <a
                className="ws-dock-row"
                href={`/w/${ws.id}`}
                onClick={(e) => {
                  e.preventDefault();
                  navigate(`/w/${ws.id}`);
                }}
              >
                <span className="ws-dock-name">{ws.name}</span>
                {ws.repo_url && <span className="ws-dock-repo">{repoName(ws.repo_url)}</span>}
              </a>
            </li>
          ))}
        </ul>
      )}
      {account.user && myTodos.length > 0 && (
        <div className="ws-dock-todos" aria-label="Your TODOs">
          <div className="ws-dock-subhead">
            <span className="ws-dock-subhead-title">
              <CheckSquare aria-hidden="true" />
              Your TODOs
            </span>
            <span className="chip">{myTodos.length}</span>
          </div>
          <ul className="ws-dock-todo-list">
            {myTodos.map((td) => {
              const isSectionItem = td.kind === "section_item";
              const linkedNum = todoItemNum(td);
              const targetHref = isSectionItem
                ? `/w/${td.workspace_id}/s/${td.section_slug}?item=${linkedNum || td.id}`
                : `/w/${td.workspace_id}/todos`;
              return (
                <li key={`${td.kind || "todo"}-${td.workspace_id}-${td.id}`} className="ws-dock-todo-row">
                  <a
                    className="ws-dock-todo-link"
                    href={targetHref}
                    onClick={(e) => {
                      e.preventDefault();
                      navigate(targetHref);
                    }}
                  >
                    <span className="ws-dock-todo-ws">
                      {td.workspace_name}
                      {isSectionItem && td.section_name ? ` · ${td.section_name}` : ""}
                      {linkedNum ? ` · #${linkedNum}` : ""}
                    </span>
                    <span className="ws-dock-todo-title">{td.title}</span>
                  </a>
                  <div className="ws-dock-todo-actions">
                    {isSectionItem ? (
                      <>
                        {td.choices?.map((choice) => (
                          <Button
                            key={choice}
                            size="small"
                            variant={td.answered_choice === choice ? "primary" : "secondary"}
                            onClick={() => answerSectionChoice(td, choice)}
                          >
                            {td.answered_choice === choice && <Check aria-hidden="true" />}
                            {choice}
                          </Button>
                        ))}
                        {(!td.choices?.length || !td.close_on_answer) && (
                          <Button
                            size="small"
                            variant="ghost"
                            title="Close item"
                            aria-label={`Close "${td.title}"`}
                            onClick={() => markTodoDone(td)}
                          >
                            <Check aria-hidden="true" />
                            Close
                          </Button>
                        )}
                      </>
                    ) : (
                      <>
                        <Button
                          size="small"
                          variant="ghost"
                          title="Mark done"
                          aria-label={`Mark "${td.title}" done`}
                          onClick={() => markTodoDone(td)}
                        >
                          <Check aria-hidden="true" />
                          Done
                        </Button>
                        <Button
                          size="small"
                          variant="ghost"
                          title="Snooze for 1 day"
                          onClick={() => snoozeTodo(td, "1d")}
                        >
                          1d
                        </Button>
                        <Button
                          size="small"
                          variant="ghost"
                          title="Snooze for 1 week"
                          onClick={() => snoozeTodo(td, "1w")}
                        >
                          1w
                        </Button>
                      </>
                    )}
                  </div>
                </li>
              );
            })}
          </ul>
        </div>
      )}
      {showLists && (
        <div className="ws-dock-foot">
          <span className="muted">Private to your team</span>
          <Button variant="secondary" size="small" onClick={onCreate}>
            <Plus aria-hidden="true" />
            New workspace
          </Button>
        </div>
      )}
    </aside>
  );
}

// Landing is Home for both signed-in and signed-out visitors (#326).
export default function Landing({ account, workspaces = null, workspacesError = "", showLists = false, navigate }) {
  const [creating, setCreating] = useState(false);
  return (
    <div className="landing">
      <SiteHeader account={account} />
      <main>
        <div className="hero-split">
          <section className="hero">
            <h1 className="hero-title">Where humans and agents build together</h1>
            <p className="hero-copy">
              Unison is one workspace for your team and your agents: a prioritized backlog with GitHub PR tracking, live
              chat, and an Isocan canvas where everyone stays in sync.
            </p>
            {showLists ? (
              <div className="hero-actions">
                <Button variant="primary" size="large" onClick={() => setCreating(true)}>
                  <Plus aria-hidden="true" />
                  Create a workspace
                </Button>
              </div>
            ) : (
              account.ready && <SignInButtons providers={account.providers} />
            )}
            {account.user && <WorkspaceStats />}
          </section>
          <WorkspaceDock
            account={account}
            workspaces={workspaces}
            workspacesError={workspacesError}
            showLists={showLists}
            navigate={navigate}
            onCreate={() => setCreating(true)}
          />
        </div>
        <figure className="hero-shot">
          <HeroShot />
        </figure>
        <section className="how" aria-labelledby="how-title">
          <h2 id="how-title" className="section-title">
            Why Unison
          </h2>
          <ul className="features">
            {FEATURES.map(({ icon: Icon, title, text }) => (
              <li key={title} className="feature">
                <span className="feature-icon" aria-hidden="true">
                  <Icon />
                </span>
                <h3>{title}</h3>
                <p>{text}</p>
              </li>
            ))}
          </ul>
        </section>
      </main>
      {creating && <NewWorkspaceDialog account={account} navigate={navigate} onClose={() => setCreating(false)} />}
      <footer className="site-foot">
        <a href="/agent.md">Agents: read /agent.md</a>
        <a href={REPO_URL}>
          <Github aria-hidden="true" />
          Source on GitHub
        </a>
        <LegalLinks />
      </footer>
    </div>
  );
}
