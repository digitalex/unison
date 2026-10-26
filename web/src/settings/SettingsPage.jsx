import { useEffect, useRef } from "react";
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
  { id: "invites", title: "Invites", description: "The workspace is private: people join with an invite, by email or by link. Every invite works for 48 hours. Agents join with a pass from the Invite button." },
  { id: "repositories", title: "Repositories", description: "Code repositories for this workspace. The first one is shown as the main repository." },
  { id: "webhooks", title: "Webhooks", description: "Inbound webhooks (e.g. from GitHub). Each delivery wakes agents with your instructions." },
  { id: "agents", title: "Agents", description: "Agents that joined with a pass, and the member each one works for." },
];

// SettingsPage is the full-page workspace settings (/w/{ws}/settings): one
// card per section, scrolled through (the sidebar or tab bar leads back to
// the workspace). The data and live updates come from Workspace.
export default function SettingsPage({ workspace, members, invites, participants, webhooks, account, call, error, onError }) {
  const scroller = useRef(null);

  // A deep link (#invites) pins its section: the page scrolls to it, and
  // keeps it in place while lists load and cards grow, until the reader
  // scrolls on their own.
  const pinned = useRef(null);
  useEffect(() => {
    const content = scroller.current.querySelector(".settings-content");
    const scrollToPinned = () => pinned.current && document.getElementById(pinned.current)?.scrollIntoView({ block: "start" });
    const follow = () => {
      const id = window.location.hash.slice(1);
      pinned.current = SECTIONS.some((s) => s.id === id) ? id : null;
      scrollToPinned();
    };
    const unpin = () => {
      pinned.current = null;
    };
    const grown = new ResizeObserver(scrollToPinned);
    grown.observe(content);
    follow();
    window.addEventListener("hashchange", follow);
    const userScroll = ["wheel", "touchstart", "keydown"];
    userScroll.forEach((t) => window.addEventListener(t, unpin, { passive: true }));
    return () => {
      grown.disconnect();
      window.removeEventListener("hashchange", follow);
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
