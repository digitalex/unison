import { useState } from "react";
import Button from "../ui/Button.jsx";
import { notifySupported, useNotifyPref } from "../notify.js";

// What to check when the browser shows a notification that never reaches
// the screen: the operating system decides last.
const OS_HINT =
  "If you didn't see it, your system is hiding your browser's notifications: on a Mac, open System Settings → Notifications → (your browser) and allow them, and turn off Focus / Do Not Disturb. On Windows, check Settings → System → Notifications.";

// showTestNotification shows a test notification and resolves to what
// happened, as the browser reports it: shown, failed, or no answer. Each test
// gets its own tag, so a second click alerts again instead of silently
// replacing the first.
function showTestNotification() {
  return new Promise((resolve) => {
    const n = new Notification("Unison test notification", {
      body: "Desktop notifications are working!",
      tag: `test-notification-${Date.now()}`,
      icon: "/apple-touch-icon.png",
    });
    const timer = setTimeout(() => resolve(`Sent, but the browser hasn't confirmed it was shown. ${OS_HINT}`), 4000);
    n.onshow = () => {
      clearTimeout(timer);
      resolve(`The browser shows it now. ${OS_HINT}`);
    };
    n.onerror = () => {
      clearTimeout(timer);
      resolve("The browser couldn't show it. Check that notifications are allowed for this site in the browser's site settings.");
    };
  });
}

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
      setTestStatus("Sending…");
      setTestStatus(await showTestNotification());
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
