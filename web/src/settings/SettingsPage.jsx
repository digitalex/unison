import { useEffect, useRef } from "react";
import ScreenHead from "../ui/ScreenHead.jsx";
import Agents from "./Agents.jsx";
import DangerZone from "./DangerZone.jsx";
import General from "./General.jsx";
import Invites from "./Invites.jsx";
import Members from "./Members.jsx";
import Repositories from "./Repositories.jsx";
import Webhooks from "./Webhooks.jsx";

// The sections, in order. Deep links: /w/{ws}/settings#<id>. Only the
// workspace's own settings: personal ones (theme, desktop notifications)
// are in your profile (/me) and the account menu (#131).
const SECTIONS = [
  { id: "general", title: "General", description: "The workspace's name and its isocan design canvas." },
  { id: "members", title: "Members", description: "Accounts in this workspace, each with the participants linked to it. Every member is equal." },
  { id: "invites", title: "Invites", description: "The workspace is private: people join with an invite, by email or by link. Every invite works for 48 hours. Agents join with a pass from the Invite button." },
  { id: "repositories", title: "Repositories", description: "Code repositories for this workspace. The first one is shown as the main repository." },
  { id: "webhooks", title: "Webhooks", description: "Inbound webhooks (e.g. from GitHub). Each delivery wakes agents with your instructions." },
  { id: "agents", title: "Agents", description: "Agents that joined with a pass, and the member each one works for." },
  { id: "danger", title: "Danger zone", description: "Irreversible actions for this workspace." },
];

// SettingsPage is the workspace settings screen (/w/{ws}/settings), laid out
// like the other screens: the screen header, then one section per topic,
// each with a head like the backlog's groups, scrolled through. head is the
// header's shared controls, error the workspace's error bar. The data and
// live updates come from Workspace.
export default function SettingsPage({ workspace, members, invites, participants, webhooks, account, call, error, onError, head, navigate }) {
  const scroller = useRef(null);

  // A deep link (#invites) pins its section: the page scrolls to it, and
  // keeps it in place while lists load and cards grow, until the reader
  // scrolls on their own.
  const pinned = useRef(null);
  useEffect(() => {
    const content = scroller.current.firstElementChild;
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
    repositories: <Repositories repos={workspace.repos || []} hasGitHubToken={Boolean(workspace.has_github_token)} call={call} onError={onError} />,
    webhooks: <Webhooks webhooks={webhooks} call={call} onError={onError} />,
    agents: <Agents participants={participants} />,
    danger: <DangerZone workspace={workspace} call={call} onError={onError} navigate={navigate} />,
  };

  return (
    <section className="screen settings" aria-label="Settings">
      <ScreenHead title="Settings" note={workspace.name}>
        {head}
      </ScreenHead>
      {error}
      <div className="screen-body" ref={scroller}>
        <div className="settings-sections">
          {SECTIONS.map((s) => (
            <section key={s.id} id={s.id} className="settings-section" aria-labelledby={`${s.id}-title`}>
              <header className="settings-head">
                <h3 id={`${s.id}-title`}>{s.title}</h3>
                <p>{s.description}</p>
              </header>
              <div className="settings-body">{body[s.id]}</div>
            </section>
          ))}
        </div>
      </div>
    </section>
  );
}
