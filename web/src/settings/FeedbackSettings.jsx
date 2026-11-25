import { useState } from "react";
import { spaLink } from "../nav.js";

// Settings → Feedback (#281): turns the workspace's Send feedback dialog and
// Feedback triage view on or off.
export default function FeedbackSettings({ workspace, call, onError }) {
  const [busy, setBusy] = useState(false);
  const enabled = Boolean(workspace.feedback_enabled);

  const toggle = async (next) => {
    if (busy || next === enabled) return;
    setBusy(true);
    try {
      await call("PATCH", "", { feedback_enabled: next });
    } catch (err) {
      onError(err.message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="notify-settings">
      <div className="account-theme">
        <span className="muted">In-app Send feedback &amp; triage</span>
        <div className="seg" role="group" aria-label="In-app Send feedback and triage">
          <button type="button" aria-pressed={enabled} disabled={busy} onClick={() => toggle(true)}>
            On
          </button>
          <button type="button" aria-pressed={!enabled} disabled={busy} onClick={() => toggle(false)}>
            Off
          </button>
        </div>
      </div>
      <p className="field-description">
        {enabled ? (
          <>
            Enabled — members can open <strong>Send feedback</strong> from the sidebar to capture and crop a screenshot, and triage reports on the{" "}
            <a {...spaLink(`/w/${workspace.id}/feedback`)}>Feedback</a> screen.
          </>
        ) : (
          "Off by default — turn on to add a Send feedback button (with tab screenshot capture & crop) and a Feedback triage view in the sidebar."
        )}
      </p>
    </div>
  );
}
