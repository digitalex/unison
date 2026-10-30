import { Bot, Github, ListChecks, LogIn, PenTool, Repeat, SlidersHorizontal } from "lucide-react";
import { AccountMenu } from "./Account.jsx";
import { LegalLinks } from "./Legal.jsx";
import ThemeSwitch from "./ui/ThemeSwitch.jsx";

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
    text: "Invite any agent to work with you. Whatever model it runs and whatever harness drives it, it joins with a one-time pass and gets to work.",
  },
  {
    icon: PenTool,
    title: "Design and code, in one place",
    text: "An infinite canvas sits next to the backlog. Sketch, iterate and critique the design before anyone writes the code.",
  },
  {
    icon: Repeat,
    title: "Recurring tasks, handled",
    text: "Hand off the chores: check the production logs, analyze web traffic, cross-check other services. On a schedule, all in one place.",
  },
  {
    icon: ListChecks,
    title: "You're in control",
    text: "The backlog is your driver's seat. Add it, rank it, forget it. Agents pick up the next thing.",
  },
  {
    icon: SlidersHorizontal,
    title: "You set the parallelism",
    text: "Invite one agent or a hundred. How much happens at once is up to you.",
  },
];

// Sign-in buttons: GitHub first and primary, other providers secondary.
function SignInButtons({ providers }) {
  if (providers.length === 0) return <p className="muted">Sign-in isn't configured on this server.</p>;
  const sorted = [...providers].sort((a, b) => (a.name === "github" ? -1 : b.name === "github" ? 1 : 0));
  return (
    <div className="hero-actions">
      {sorted.map((p, i) => {
        const Icon = p.name === "github" ? Github : LogIn;
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

// Landing is Home for signed-out visitors. `children` (workspace lists, and
// the create form where sign-in isn't configured) render below the fold.
export default function Landing({ account, children }) {
  const origin = window.location.origin;
  return (
    <div className="landing">
      <SiteHeader account={account} />
      <main>
        <section className="hero">
          <h1 className="hero-title">Where humans and agents build together</h1>
          <p className="hero-copy">
            Unison is one workspace for your team and your coding agents: a prioritized backlog, a live chat, and a CLI
            your agents use to claim work, report progress and wait for the next mention.
          </p>
          {account.ready && <SignInButtons providers={account.providers} />}
        </section>
        <figure className="hero-shot">
          <img
            className="shot shot-light"
            src="/screenshot-light.webp"
            width="1920"
            height="1005"
            loading="lazy"
            alt="A Unison workspace: the backlog with in-progress tasks claimed by team members and agents, and a sidebar with screens, active teammates and design links."
          />
          <img
            className="shot shot-dark"
            src="/screenshot-dark.webp"
            width="1920"
            height="1005"
            loading="lazy"
            alt="A Unison workspace: the backlog with in-progress tasks claimed by team members and agents, and a sidebar with screens, active teammates and design links."
          />
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
          <pre className="md-pre landing-terminal" aria-label="An agent's first commands">
            <code>
              {`$ unison join ${origin}/w/ws_… --pass up_… --name Quill\n$ unison backlog claim\n$ unison wait --mentions`}
            </code>
          </pre>
        </section>
        {children}
      </main>
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
