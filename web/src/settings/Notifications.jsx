import { useState } from "react";
import Button from "../ui/Button.jsx";
import { notifySupported, useNotifyPref } from "../notify.js";

// Settings → Notifications: desktop notifications for mentions, actions, and joins.
export default function Notifications() {
  const [on, turnOn, turnOff] = useNotifyPref();
  const [testStatus, setTestStatus] = useState("");
  const blocked = notifySupported && Notification.permission === "denied";

  const sendTest = async () => {
    if (!notifySupported) return;
    setTestStatus("");
    try {
      if (Notification.permission !== "granted") {
        const p = await Notification.requestPermission();
        if (p !== "granted") {
          setTestStatus("Browser permission was not granted.");
          return;
        }
      }
      const n = new Notification("Unison test notification", {
        body: "Desktop notifications are working!",
        tag: "test-notification",
        icon: "/apple-touch-icon.png",
      });
      setTestStatus("Test notification sent!");
    } catch (err) {
      setTestStatus(`Failed to show notification: ${err.message}`);
    }
  };

  if (!notifySupported) {
    return (
      <p className="field-description">
        Desktop notifications are not supported in this browser.
      </p>
    );
  }

  return (
    <div className="notify-settings">
      <div className="account-theme">
        <span className="muted">Desktop notifications</span>
        {blocked ? (
          <span className="muted" title="Allow notifications for this site in your browser's settings">
            Blocked
          </span>
        ) : (
          <div className="seg" role="group" aria-label="Desktop notifications">
            <button type="button" aria-pressed={on} onClick={turnOn}>
              On
            </button>
            <button type="button" aria-pressed={!on} onClick={turnOff}>
              Off
            </button>
          </div>
        )}
      </div>
      <p className="field-description">
        Notifications appear while this tab is in the background for @mentions, messages asking you to act, and when someone joins.
      </p>
      {blocked && (
        <p className="field-description muted">
          Notifications are currently blocked. To enable them, open your browser's site settings or permissions for this site and allow notifications.
        </p>
      )}
      <div className="actions-row" style={{ marginTop: "0.75rem" }}>
        <Button type="button" variant="secondary" onClick={sendTest} disabled={blocked}>
          Send test notification
        </Button>
        {testStatus && (
          <span className="muted" role="status">
            {testStatus}
          </span>
        )}
      </div>
    </div>
  );
}

export function NotifyRow() {
  const [on, turnOn, turnOff] = useNotifyPref();
  const blocked = notifySupported && Notification.permission === "denied";
  return (
    <div className="account-theme">
      <span className="muted">Desktop notifications</span>
      {blocked ? (
        <span className="muted" title="Allow notifications for this site in your browser's settings">
          Blocked
        </span>
      ) : (
        <div className="seg" role="group" aria-label="Desktop notifications">
          <button type="button" aria-pressed={on} onClick={turnOn}>
            On
          </button>
          <button type="button" aria-pressed={!on} onClick={turnOff}>
            Off
          </button>
        </div>
      )}
    </div>
  );
}
