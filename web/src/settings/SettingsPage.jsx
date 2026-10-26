import { useEffect, useRef, useState } from "react";
import { ArrowLeft } from "lucide-react";
import Button from "../ui/Button.jsx";
import Agents from "./Agents.jsx";
import General from "./General.jsx";
import Invites from "./Invites.jsx";
import Members from "./Members.jsx";
import Repositories from "./Repositories.jsx";
import Webhooks from "./Webhooks.jsx";

// The sections, in order. Deep links: /w/{ws}/settings#<id>.
const SECTIONS = [
  { id: "general", title: "General", description: "The workspace's name and its isocan design canvas." },
  { id: "members", title: "Members", description: "Accounts in this workspace, each with the participants linked to it. Every member is equal." },
  { id: "invites", title: "Invites", description: "The workspace is private: people join with an invite, by email or by link. Agents join with a pass from the top bar." },
  { id: "repositories", title: "Repositories", description: "Code repositories for this workspace. The first one is shown as the main repository." },
  { id: "webhooks", title: "Webhooks", description: "Inbound webhooks (e.g. from GitHub). Each delivery wakes agents with your instructions." },
  { id: "agents", title: "Agents", description: "Agents that joined with a pass, and the member each one works for." },
];

function currentHash() {
  const id = window.location.hash.slice(1);
  return SECTIONS.some((s) => s.id === id) ? id : SECTIONS[0].id;
}

// SettingsPage is the full-page workspace settings (/w/{ws}/settings): a
// section nav (sticky on wide screens, a scroller on phones) and one card
// per section. The data and live updates come from Workspace.
export default function SettingsPage({ workspace, members, invites, participants, webhooks, account, call, error, onError, navigate }) {
  const [active, setActive] = useState(currentHash);
  const scroller = useRef(null);
  const back = `/w/${workspace.id}`;

  // A deep link (or a nav click) pins its section: the page scrolls to it,
  // and keeps it in place while lists load and cards grow, until the reader
  // scrolls on their own. Meanwhile the highlight follows the scroll.
  const pinned = useRef(null);
  useEffect(() => {
    const root = scroller.current;
    const content = root.querySelector(".settings-content");
    const scrollToPinned = () => pinned.current && document.getElementById(pinned.current)?.scrollIntoView({ block: "start" });
    const follow = () => {
      const id = currentHash();
      setActive(id);
      pinned.current = window.location.hash ? id : null;
      scrollToPinned();
    };
    const unpin = () => {
      pinned.current = null;
    };
    const onScroll = () => {
      if (pinned.current) return setActive(pinned.current);
      const line = Math.max(root.getBoundingClientRect().top, 0) + 120;
      let current = SECTIONS[0].id;
      for (const s of SECTIONS) {
        const el = document.getElementById(s.id);
        if (el && el.getBoundingClientRect().top <= line) current = s.id;
      }
      // At the very end the last cards can't reach the top: pick the last.
      const doc = document.documentElement;
      const own = root.scrollHeight > root.clientHeight;
      const scrolled = own ? root.scrollTop > 0 : window.scrollY > 0;
      const atEnd = own ? root.scrollTop + root.clientHeight >= root.scrollHeight - 2 : window.scrollY + window.innerHeight >= doc.scrollHeight - 2;
      if (scrolled && atEnd) current = SECTIONS[SECTIONS.length - 1].id;
      setActive(current);
    };
    const grown = new ResizeObserver(scrollToPinned);
    grown.observe(content);
    follow();
    window.addEventListener("hashchange", follow);
    root.addEventListener("scroll", onScroll, { passive: true });
    window.addEventListener("scroll", onScroll, { passive: true });
    const userScroll = ["wheel", "touchstart", "keydown"];
    userScroll.forEach((t) => window.addEventListener(t, unpin, { passive: true }));
    return () => {
      grown.disconnect();
      window.removeEventListener("hashchange", follow);
      root.removeEventListener("scroll", onScroll);
      window.removeEventListener("scroll", onScroll);
      userScroll.forEach((t) => window.removeEventListener(t, unpin));
    };
  }, []);

  const body = {
    general: <General workspace={workspace} call={call} onError={onError} />,
    members: <Members members={members} invites={invites} account={account} call={call} onError={onError} />,
    invites: <Invites invites={invites} account={account} call={call} onError={onError} />,
    repositories: <Repositories repos={workspace.repos || []} call={call} onError={onError} />,
    webhooks: <Webhooks webhooks={webhooks} call={call} onError={onError} />,
    agents: <Agents participants={participants} />,
  };

  return (
    <div className="settings-page" ref={scroller}>
      <div className="settings-layout">
        <nav className="settings-nav" aria-label="Settings sections">
          <a
            className="settings-back"
            href={back}
            onClick={(e) => {
              e.preventDefault();
              navigate(back);
            }}
          >
            <ArrowLeft aria-hidden="true" />
            Back to workspace
          </a>
          <ul>
            {SECTIONS.map((s) => (
              <li key={s.id}>
                <a href={`#${s.id}`} aria-current={active === s.id ? "location" : undefined}>
                  {s.title}
                </a>
              </li>
            ))}
          </ul>
        </nav>
        <div className="settings-content">
          <h2 className="settings-title">Settings</h2>
          {error && (
            <p role="alert" className="error-bar settings-error">
              Error: {error}{" "}
              <Button variant="ghost" onClick={() => onError("")}>
                dismiss
              </Button>
            </p>
          )}
          {SECTIONS.map((s) => (
            <section key={s.id} id={s.id} className="card settings-card" aria-labelledby={`${s.id}-title`}>
              <header className="settings-card-head">
                <h3 id={`${s.id}-title`}>{s.title}</h3>
                <p className="field-description">{s.description}</p>
              </header>
              <div className="settings-card-body">{body[s.id]}</div>
            </section>
          ))}
        </div>
      </div>
    </div>
  );
}
