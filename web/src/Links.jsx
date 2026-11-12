import { useState } from "react";
import { Trash2 } from "lucide-react";
import Button from "./ui/Button.jsx";
import Dialog from "./ui/Dialog.jsx";
import Field from "./ui/Field.jsx";
import { Input } from "./ui/Input.jsx";

// Custom workspace links (#299): specs, docs, dashboards, any web page, by
// name. The sidebar's Links section lists them; LinkDialog adds one (link
// null) or edits or removes one. The workspace reloads on link.* events.
export function LinkDialog({ link, call, onClose }) {
  const [name, setName] = useState(link?.name || "");
  const [url, setUrl] = useState(link?.url || "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const path = link ? `/links/${link.id}` : "/links";
  const send = async (method, payload) => {
    setBusy(true);
    setError("");
    try {
      await call(method, path, payload);
      onClose();
    } catch (e) {
      setError(e.message);
      setBusy(false);
    }
  };
  const save = (e) => {
    e.preventDefault();
    send(link ? "PATCH" : "POST", { name, url });
  };
  const remove = () => {
    if (window.confirm(`Remove the link "${link.name}"?`)) send("DELETE");
  };
  return (
    <Dialog title={link ? "Edit link" : "Add link"} onClose={onClose} className="link-dialog">
      <form className="form" onSubmit={save}>
        <Field label="Name">
          <Input required maxLength={100} data-autofocus value={name} onChange={(e) => setName(e.target.value)} placeholder="Design doc" />
        </Field>
        <Field label="URL">
          <Input required type="url" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://" />
        </Field>
        {error && (
          <p role="alert" className="field-error">
            {error}
          </p>
        )}
        <div className="actions-row dialog-actions">
          {link && (
            <Button type="button" variant="ghost" className="button-danger" onClick={remove} disabled={busy}>
              <Trash2 aria-hidden="true" />
              Remove
            </Button>
          )}
          <span className="spacer" />
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={busy}>
            {link ? "Save" : "Add link"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
