import { useState } from "react";
import Button from "./ui/Button.jsx";
import Dialog from "./ui/Dialog.jsx";
import Field from "./ui/Field.jsx";
import { Input } from "./ui/Input.jsx";

const FIELDS = ["name", "emoji", "status"];

// MeDialog edits who you are in this workspace (#303): your name, emoji
// and status line. It saves with PATCH …/me, the endpoint behind `unison
// name` and `unison status`, and sends only what changed, so a status set
// from the CLI meanwhile isn't overwritten by a rename here. The server
// has the last word on names (one word, no @, nobody else's) and its
// refusal is shown in place.
export default function MeDialog({ me, call, onSaved, onClose }) {
  const [form, setForm] = useState({ name: me.name, emoji: me.emoji, status: me.status || "" });
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const field = (key) => ({ value: form[key], onChange: (e) => setForm({ ...form, [key]: e.target.value }) });
  const save = async (e) => {
    e.preventDefault();
    const patch = {};
    for (const key of FIELDS) {
      const value = form[key].trim();
      if (value !== (me[key] || "")) patch[key] = value;
    }
    if (Object.keys(patch).length === 0) return onClose();
    setError("");
    setBusy(true);
    try {
      onSaved(await call("PATCH", "/me", patch));
      onClose();
    } catch (err) {
      setBusy(false);
      setError(err.message);
    }
  };
  return (
    <Dialog title="You in this workspace" onClose={onClose} className="me-dialog">
      <form className="form" onSubmit={save}>
        {error && (
          <p role="alert" className="field-error">
            {error}
          </p>
        )}
        <div className="new-workspace-me">
          <Field label="Name">
            <Input required data-autofocus pattern="[^\s@]+" title="One word, without @" autoComplete="off" {...field("name")} />
          </Field>
          <Field label="Emoji">
            <Input required autoComplete="off" {...field("emoji")} />
          </Field>
        </div>
        <p className="field-description">One word, without @: it is how people mention you. Earlier mentions keep the name they were written with.</p>
        <Field label="Status (optional)">
          <Input autoComplete="off" placeholder="working on #12" {...field("status")} />
        </Field>
        <div className="actions-row dialog-actions">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={busy}>
            Save
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
