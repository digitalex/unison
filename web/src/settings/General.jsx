import { useEffect, useState } from "react";
import Button from "../ui/Button.jsx";
import Field from "../ui/Field.jsx";
import { Input } from "../ui/Input.jsx";

// Settings → General: the workspace's name and canvas URL.
export default function General({ workspace, call, onError }) {
  const saved = { name: workspace.name, canvas_url: workspace.canvas_url };
  const [form, setForm] = useState(saved);
  const [status, setStatus] = useState("");
  const dirty = form.name !== saved.name || form.canvas_url !== saved.canvas_url;
  // Follow edits made elsewhere (live updates) while nothing is typed here.
  useEffect(() => {
    if (!dirty) setForm(saved);
  }, [workspace.name, workspace.canvas_url]);
  const save = async (e) => {
    e.preventDefault();
    try {
      await call("PATCH", "", form);
      setStatus("Saved");
    } catch (err) {
      onError(err.message);
    }
  };
  const field = (key) => ({
    value: form[key],
    onChange: (e) => {
      setStatus("");
      setForm({ ...form, [key]: e.target.value });
    },
  });
  return (
    <form className="form" onSubmit={save}>
      <Field label="Name">
        <Input required {...field("name")} />
      </Field>
      <Field label="Canvas URL">
        <Input placeholder="https://isocan.io/p/…" {...field("canvas_url")} />
      </Field>
      <div className="actions-row">
        <Button variant="primary" type="submit" disabled={!dirty}>
          Save
        </Button>
        {dirty && (
          <Button type="button" onClick={() => setForm(saved)}>
            Reset
          </Button>
        )}
        {status && !dirty && (
          <span className="muted" role="status">
            {status}
          </span>
        )}
      </div>
    </form>
  );
}
