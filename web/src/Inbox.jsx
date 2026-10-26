import { useEffect, useState } from "react";
import { BellRing, Check, ExternalLink, ListChecks } from "lucide-react";
import { ago } from "./backlogFormat.js";
import Markdown from "./Markdown.jsx";
import { itemLink } from "./nav.js";
import Button from "./ui/Button.jsx";
import Dialog from "./ui/Dialog.jsx";
import Field from "./ui/Field.jsx";
import { Input, Textarea } from "./ui/Input.jsx";

// Inbox is "Needs your attention": what someone (usually one of your
// agents) needs you to act on, at the top of the board. Only your entries
// (your account's, on any device). It isn't there when there's nothing to
// do; dismissing is one click, with a moment to undo. Past a few entries
// the rest fold away, so the backlog stays in view (on phones too).
const FOLDED = 3;

export default function Inbox({ entries, participants, call, onError }) {
  const [undo, setUndo] = useState(null); // The entry just dismissed.
  const [showAll, setShowAll] = useState(false);
  const shown = showAll ? entries : entries.slice(0, FOLDED);
  useEffect(() => {
    if (!undo) return;
    const t = setTimeout(() => setUndo(null), 6000);
    return () => clearTimeout(t);
  }, [undo]);
  const act = async (e, verb) => {
    try {
      await call("POST", `/inbox/${e.id}/${verb}`);
      setUndo(verb === "dismiss" ? e : null);
    } catch (err) {
      onError(err.message);
    }
  };
  if (entries.length === 0 && !undo) return null;
  return (
    <section className="inbox" aria-labelledby="inbox-title">
      <h2 id="inbox-title" className="inbox-title">
        <BellRing aria-hidden="true" />
        Needs your attention
        <span className="count">{entries.length}</span>
      </h2>
      {entries.length > 0 && (
        <ul className="inbox-list">
          {shown.map((e) => (
            <InboxEntry key={e.id} entry={e} sender={participants.find((p) => p.id === e.sender_id)} onDismiss={() => act(e, "dismiss")} />
          ))}
        </ul>
      )}
      {entries.length > FOLDED && (
        <Button variant="ghost" className="inbox-more" aria-expanded={showAll} onClick={() => setShowAll(!showAll)}>
          {showAll ? "Show fewer" : `Show all ${entries.length}`}
        </Button>
      )}
      {undo && (
        <p className="inbox-undo" role="status">
          Dismissed “{firstLine(undo.body)}”.{" "}
          <Button variant="ghost" onClick={() => act(undo, "restore")}>
            Undo
          </Button>
        </p>
      )}
    </section>
  );
}

function firstLine(text) {
  const line = text.split("\n")[0];
  return line.length > 60 ? `${line.slice(0, 57)}…` : line;
}

function linkLabel(url) {
  try {
    const u = new URL(url);
    return (u.host + u.pathname).replace(/\/$/, "");
  } catch {
    return url;
  }
}

function InboxEntry({ entry: e, sender, onDismiss }) {
  const agentOf = sender?.sponsor ? ` · agent of ${sender.sponsor.display_name}` : "";
  return (
    <li className="inbox-entry">
      <span className="avatar" aria-hidden="true">
        {e.sender_emoji}
      </span>
      <div className="inbox-main">
        <div className="inbox-head">
          <span className="who">{e.sender_name}</span>
          {agentOf && <span className="muted">{agentOf}</span>}
          <span className="time" title={new Date(e.created_at).toLocaleString()}>
            {ago(e.created_at)}
          </span>
        </div>
        <div className="inbox-text">
          <Markdown text={e.body} />
        </div>
        {(e.item || e.url) && (
          <div className="inbox-links">
            {e.item && (
              <a className="chip msg-item" title={`Backlog item #${e.item.id}: ${e.item.title}`} {...itemLink(e.item.id)}>
                <ListChecks aria-hidden="true" />#{e.item.id} <span className="msg-item-title">{e.item.title}</span>
              </a>
            )}
            {e.url && (
              <a className="inbox-url" href={e.url} target="_blank" rel="noopener noreferrer">
                <ExternalLink aria-hidden="true" />
                <span>{linkLabel(e.url)}</span>
              </a>
            )}
          </div>
        )}
      </div>
      <Button className="inbox-dismiss" aria-label={`Dismiss: ${firstLine(e.body)}`} title="Dismiss" onClick={onDismiss}>
        <Check aria-hidden="true" />
        <span className="label">Dismiss</span>
      </Button>
    </li>
  );
}

// SendToInboxDialog puts something in a member's inbox (to: a name the
// server resolves to their account).
export function SendToInboxDialog({ to, label, call, onClose, onError }) {
  const [form, setForm] = useState({ body: "", item: "", url: "" });
  const [busy, setBusy] = useState(false);
  const field = (key) => ({ value: form[key], onChange: (e) => setForm({ ...form, [key]: e.target.value }) });
  const send = async (e) => {
    e.preventDefault();
    setBusy(true);
    try {
      await call("POST", "/inbox", { to, body: form.body, url: form.url, item_id: form.item ? Number(form.item.replace("#", "")) : undefined });
      onClose();
    } catch (err) {
      setBusy(false);
      onError(err.message);
    }
  };
  return (
    <Dialog title={`Send to ${label}'s inbox`} onClose={onClose} className="new-workspace-dialog">
      <form className="form" onSubmit={send}>
        <p className="field-description">For something they need to act on. Status updates belong in chat.</p>
        <Field label="What needs doing">
          <Textarea required data-autofocus rows={4} placeholder="e.g. Please review the PR for the landing page" {...field("body")} />
        </Field>
        <div className="new-workspace-me">
          <Field label="Link (optional)">
            <Input type="url" placeholder="https://github.com/…/pull/7" {...field("url")} />
          </Field>
          <Field label="Item #">
            <Input inputMode="numeric" placeholder="12" {...field("item")} />
          </Field>
        </div>
        <div className="actions-row dialog-actions">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={busy}>
            Send
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
