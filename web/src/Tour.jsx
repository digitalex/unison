import { useEffect, useLayoutEffect, useState } from "react";
import { X } from "lucide-react";
import Button from "./ui/Button.jsx";

export const TOUR_SEEN_KEY = "unison.tour.seen";

export function hasSeenTour() {
  try {
    return Boolean(localStorage.getItem(TOUR_SEEN_KEY));
  } catch {
    return true;
  }
}

export function markTourSeen() {
  try {
    localStorage.setItem(TOUR_SEEN_KEY, "1");
  } catch {
    // Private browsing or blocked storage.
  }
}

function buildSteps(workspace) {
  return [
    {
      target: "backlog",
      title: "Backlog",
      body: "Track tasks, bugs, and priorities together. Humans and agents can claim items, attach PRs, and close them when merged.",
    },
    {
      target: "recurring",
      title: "Recurring",
      body: "Schedule hourly, daily, or weekly tasks that automatically appear at the top of your backlog.",
    },
    {
      target: "chat",
      title: "Chat",
      body: "Collaborate in real time with threads, @mentions, reactions, and action requests.",
    },
    {
      target: "ideas",
      title: "Ideas",
      body: "Explore and discuss early ideas before they're ready. Move one to the backlog in a click once it's settled, with its discussion kept.",
    },
    {
      target: "invite-human",
      title: "Invite humans",
      body: "Create an invite link or send email invites to bring teammates into this workspace.",
    },
    {
      target: "invite-agent",
      title: "Invite agents",
      body: "Mint a single-use pass and copy the agent prompt so your AI coding agents can join and pick up work.",
    },
    {
      target: "canvas",
      title: "Canvas",
      body: workspace?.canvas_url
        ? "Jump straight to this workspace's linked Isocan design canvas from the sidebar."
        : "Add an Isocan canvas URL anytime in Settings → General to link your shared design canvas here.",
    },
  ];
}

function findVisibleTarget(name) {
  const els = document.querySelectorAll(`[data-tour="${name}"]`);
  for (const el of els) {
    const r = el.getBoundingClientRect();
    if (r.width > 0 && r.height > 0) return { el, rect: r };
  }
  return null;
}

function placePopover(rect) {
  const vw = window.innerWidth;
  const vh = window.innerHeight;
  const cardW = Math.min(320, vw - 24);
  const cardH = 190;
  const gap = 12;

  // Prefer to the right of sidebar targets; otherwise below or above.
  if (rect.right + gap + cardW <= vw - 12) {
    const top = Math.max(12, Math.min(rect.top, vh - cardH - 12));
    return { top, left: rect.right + gap, width: cardW };
  }
  if (rect.bottom + gap + cardH <= vh - 12) {
    const left = Math.max(12, Math.min(rect.left, vw - cardW - 12));
    return { top: rect.bottom + gap, left, width: cardW };
  }
  const left = Math.max(12, Math.min(rect.left, vw - cardW - 12));
  const top = Math.max(12, rect.top - gap - cardH);
  return { top, left, width: cardW };
}

export default function Tour({ workspace, onClose }) {
  const steps = buildSteps(workspace);
  const [index, setIndex] = useState(0);
  const [layout, setLayout] = useState(null);
  const step = steps[index];

  const dismiss = () => {
    markTourSeen();
    onClose();
  };

  useEffect(() => {
    const onKey = (e) => {
      if (e.key === "Escape") {
        e.preventDefault();
        dismiss();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  useLayoutEffect(() => {
    const measure = () => {
      const hit = findVisibleTarget(step.target);
      if (!hit) {
        setLayout({ rect: null, card: { top: 24, left: Math.max(12, (window.innerWidth - 320) / 2), width: Math.min(320, window.innerWidth - 24) } });
        return;
      }
      hit.el.scrollIntoView({ block: "nearest", inline: "nearest" });
      const rect = hit.el.getBoundingClientRect();
      setLayout({ rect, card: placePopover(rect) });
    };
    measure();
    window.addEventListener("resize", measure);
    window.addEventListener("scroll", measure, true);
    return () => {
      window.removeEventListener("resize", measure);
      window.removeEventListener("scroll", measure, true);
    };
  }, [step.target, index]);

  if (!layout) return null;

  const isLast = index === steps.length - 1;

  return (
    <div className="tour-layer" aria-live="polite">
      {layout.rect && (
        <div
          className="tour-ring"
          aria-hidden="true"
          style={{
            top: layout.rect.top - 4,
            left: layout.rect.left - 4,
            width: layout.rect.width + 8,
            height: layout.rect.height + 8,
          }}
        />
      )}
      <div
        className="tour-card"
        role="dialog"
        aria-label={`Workspace tour: ${step.title}`}
        style={{ top: layout.card.top, left: layout.card.left, width: layout.card.width }}
      >
        <div className="tour-head">
          <span className="tour-step-count">
            {index + 1} / {steps.length}
          </span>
          <strong className="tour-title">{step.title}</strong>
          <button type="button" className="tour-close" aria-label="Skip tour" title="Skip tour (Esc)" onClick={dismiss}>
            <X aria-hidden="true" />
          </button>
        </div>
        <p className="tour-body">{step.body}</p>
        <div className="tour-foot">
          <button type="button" className="tour-skip" onClick={dismiss}>
            Skip tour
          </button>
          <div className="tour-nav">
            {index > 0 && (
              <Button type="button" variant="ghost" size="small" onClick={() => setIndex((i) => i - 1)}>
                Back
              </Button>
            )}
            <Button
              type="button"
              variant="primary"
              size="small"
              onClick={() => {
                if (isLast) dismiss();
                else setIndex((i) => i + 1);
              }}
            >
              {isLast ? "Done" : "Next"}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
