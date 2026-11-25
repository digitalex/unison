import { useEffect, useState } from "react";
import { DevicePage, useAccount } from "./Account.jsx";
import { ApiError, api } from "./api.js";
import { JoinPage } from "./Gate.jsx";
import { LEGAL_PATHS } from "./Legal.jsx";
import Landing, { SiteHeader } from "./Landing.jsx";
import Profile from "./Profile.jsx";
import Workspace from "./Workspace.jsx";
import Button from "./ui/Button.jsx";
import Field from "./ui/Field.jsx";
import { Input } from "./ui/Input.jsx";

// Pages: "/" is Home (the landing page, or the workspace list when signed
// in); "/w/:id" is a workspace's backlog, "/w/:id/recurring" its recurring
// tasks, "/w/:id/ideas" its Ideas board, "/w/:id/chat" its chat, "/w/:id/settings" its settings,
// "/w/:id/join?invite=…" where an invite link lands; "/me" is your profile;
// "/auth/device" approves a `unison login`; "/privacy-policy" and
// "/terms-of-service" are the legal pages.
export default function App() {
  const account = useAccount();
  const [path, setPath] = useState(window.location.pathname);
  useEffect(() => {
    const onPop = () => setPath(window.location.pathname);
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, []);
  const navigate = (to) => {
    window.history.pushState(null, "", to);
    setPath(window.location.pathname);
  };
  const artifactMatch = path.match(/^\/w\/([^/]+)\/artifacts\/([^/]+)\/?$/);
  if (artifactMatch) {
    let artifactFile = artifactMatch[2];
    try {
      artifactFile = decodeURIComponent(artifactFile);
    } catch {}
    return (
      <Workspace
        key={artifactMatch[1]}
        id={artifactMatch[1]}
        view="artifacts"
        artifactFile={artifactFile}
        navigate={navigate}
        account={account}
      />
    );
  }
  const sectionMatch = path.match(/^\/w\/([^/]+)\/s\/([^/]+)\/?$/);
  if (sectionMatch) {
    let sectionSlug = sectionMatch[2];
    try {
      sectionSlug = decodeURIComponent(sectionSlug);
    } catch {}
    return (
      <Workspace
        key={sectionMatch[1]}
        id={sectionMatch[1]}
        view="section"
        sectionSlug={sectionSlug}
        navigate={navigate}
        account={account}
      />
    );
  }
  const match = path.match(/^\/w\/([^/]+)(?:\/(settings|join|recurring|ideas|feedback|chat|todos|artifacts))?/);
  if (match && match[2] === "join") return <JoinPage id={match[1]} account={account} navigate={navigate} />;
  // Keyed by id: switching workspaces remounts the page, so its data, live
  // stream and dialogs start fresh for the new one.
  if (match) return <Workspace key={match[1]} id={match[1]} view={match[2] || "backlog"} navigate={navigate} account={account} />;
  if (path === "/auth/device") return <DevicePage account={account} navigate={navigate} />;
  if (path === "/me") return <Profile account={account} navigate={navigate} />;
  const Legal = LEGAL_PATHS[path];
  if (Legal) return <Legal account={account} />;
  return <Home navigate={navigate} account={account} />;
}

// Home: a landing page for signed-out visitors, your workspaces and the
// create form once signed in. Workspaces are private, so signed-out visitors
// see no lists, unless the server has no sign-in configured at all: then the
// list is fetched, and works only with privacy off (UNISON_AUTH=off).
function Home({ navigate, account }) {
  const [workspaces, setWorkspaces] = useState(null);
  const [error, setError] = useState("");
  const noSignIn = account.ready && account.providers.length === 0;
  const showLists = Boolean(account.user) || noSignIn;

  useEffect(() => {
    if (!showLists) return;
    api("GET", "/api/workspaces").then(setWorkspaces, (e) =>
      setError(
        noSignIn && e instanceof ApiError && e.status === 401
          ? "Workspaces are private, and sign-in isn't configured on this server. Whoever runs it can add GitHub, Google or Apple sign-in (or set UNISON_AUTH=off for local development)."
          : e.message,
      ),
    );
  }, [showLists]);

  if (!account.ready) {
    return (
      <div className="landing">
        <SiteHeader account={account} />
      </div>
    );
  }
  return (
    <Landing
      account={account}
      workspaces={workspaces}
      workspacesError={error}
      showLists={showLists}
      navigate={navigate}
    />
  );
}
