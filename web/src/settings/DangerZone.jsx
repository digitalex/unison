import { useState } from "react";
import { setToken } from "../api.js";
import Button from "../ui/Button.jsx";
import Field from "../ui/Field.jsx";
import { Input } from "../ui/Input.jsx";

// Settings → Danger zone: delete the workspace permanently.
export default function DangerZone({ workspace, call, onError, navigate }) {
  const [confirmName, setConfirmName] = useState("");
  const [deleting, setDeleting] = useState(false);

  const confirmed = confirmName === workspace.name;

  const remove = async (e) => {
    e.preventDefault();
    if (!confirmed || deleting) return;
    setDeleting(true);
    try {
      await call("DELETE");
      setToken(workspace.id, "");
      localStorage.removeItem(`unison.chat.seen.${workspace.id}`);
      navigate("/");
    } catch (err) {
      setDeleting(false);
      onError(err.message);
    }
  };

  return (
    <div className="danger-zone">
      <p className="field-description">
        Deleting this workspace is permanent and cannot be undone. All chats, backlog items, repositories, webhooks, and related data will be permanently deleted.
      </p>
      <form className="form" onSubmit={remove}>
        <Field label={`Type "${workspace.name}"`}>
          <Input
            value={confirmName}
            onChange={(e) => setConfirmName(e.target.value)}
            placeholder={workspace.name}
            autoCapitalize="none"
            autoCorrect="off"
            spellCheck="false"
          />
        </Field>
        <div className="actions-row">
          <Button variant="danger" type="submit" disabled={!confirmed || deleting}>
            {deleting ? "Deleting…" : "Delete workspace"}
          </Button>
        </div>
      </form>
    </div>
  );
}
