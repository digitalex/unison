import { useEffect, useState } from "react";
import { Copy } from "lucide-react";
import Button from "../ui/Button.jsx";
import { Input, Textarea } from "../ui/Input.jsx";
import { ago } from "./format.js";

// Settings → Webhooks: inbound webhooks whose deliveries wake agents with
// the webhook's plain-text instructions.
export default function Webhooks({ webhooks, call, onError }) {
  const [created, setCreated] = useState(null);
  const [form, setForm] = useState({ name: "", instructions: "", signing_secret: "" });
  const run = (method, suffix, body) =>
    call(method, suffix, body).catch((e) => {
      onError(e.message);
      return null;
    });
  const add = async (e) => {
    e.preventDefault();
    const h = await run("POST", "/webhooks", form);
    if (h) {
      setCreated(h);
      setForm({ name: "", instructions: "", signing_secret: "" });
    }
  };
  const field = (key) => ({ value: form[key], onChange: (e) => setForm({ ...form, [key]: e.target.value }) });

  return (
    <>
      {webhooks.length === 0 && <p className="field-description">No webhooks yet.</p>}
      {webhooks.map((h) => (
        <WebhookRow key={h.id} hook={h} call={call} run={run} />
      ))}
      {created && (
        <div className="webhook-created">
          <p className="field-description">
            Receive URL for <b>{created.name}</b>. Keep it secret. In GitHub use content type application/json
            {created.has_signing_secret ? " and your signing secret." : "."}
          </p>
          <UrlBox url={created.url} />
          <Button type="button" variant="ghost" onClick={() => setCreated(null)}>
            Done
          </Button>
        </div>
      )}
      <SetupGuide open={webhooks.length === 0} />
      <form className="webhook-form" onSubmit={add}>
        <Input required aria-label="Webhook name" placeholder="Name, e.g. GitHub" {...field("name")} />
        <Textarea
          aria-label="Instructions for agents"
          rows={2}
          placeholder="Instructions for agents, e.g. When a PR is opened, review it."
          {...field("instructions")}
        />
        <div className="webhook-form-row">
          <Input type="password" autoComplete="new-password" aria-label="Signing secret (optional)" placeholder="Signing secret (optional)" {...field("signing_secret")} />
          <Button type="submit">Add webhook</Button>
        </div>
      </form>
    </>
  );
}

function SetupGuide({ open }) {
  return (
    <details className="webhook-guide" open={open}>
      <summary>How to set up a webhook</summary>
      <ol>
        <li>
          Add a webhook below. Give it a name and tell agents what to do when a delivery arrives, e.g. "When a PR is
          opened, review it."
        </li>
        <li>
          Copy the <b>receive URL</b>. It's shown once you add the webhook, and later under <b>URL</b>. Anyone with the URL
          can post to it, so keep it secret.
        </li>
        <li>
          In the sender (on GitHub: repo <b>Settings → Webhooks → Add webhook</b>), set:
          <ul>
            <li>
              <b>Payload URL</b>: the receive URL
            </li>
            <li>
              <b>Content type</b>: <code>application/json</code>
            </li>
            <li>
              <b>Secret</b> (optional): the same signing secret you enter here. Unison then rejects deliveries without
              a valid <code>X-Hub-Signature-256</code>.
            </li>
            <li>
              <b>Events</b>: only the ones your instructions cover, e.g. pull requests
            </li>
          </ul>
        </li>
        <li>
          Use <b>Deliveries</b> to check what arrived. Each one wakes the workspace's agents with your instructions.
        </li>
      </ol>
    </details>
  );
}

export function UrlBox({ url, label = "Receive URL" }) {
  const [copied, setCopied] = useState(false);
  const copy = (e) => {
    const input = e.currentTarget.parentElement.querySelector("input");
    input.select();
    navigator.clipboard?.writeText(url).then(() => setCopied(true), () => {});
  };
  return (
    <div className="url-box">
      <Input readOnly aria-label={label} value={url} onFocus={(e) => e.target.select()} />
      <Button type="button" iconOnly variant="ghost" aria-label={`Copy ${label}`} title={copied ? "Copied" : `Copy ${label}`} onClick={copy}>
        <Copy aria-hidden="true" />
      </Button>
    </div>
  );
}

function WebhookRow({ hook: h, call, run }) {
  const [panel, setPanel] = useState(null); // "url" | "deliveries" | "edit"
  const [url, setUrl] = useState("");
  const toggle = async (name) => {
    if (panel === name) return setPanel(null);
    if (name === "url") {
      const s = await run("GET", `/webhooks/${h.id}/secret`);
      if (!s) return;
      setUrl(s.url);
    }
    setPanel(name);
  };
  const rotate = async () => {
    if (!window.confirm(`Issue a new receive URL for "${h.name}"? The current one stops working.`)) return;
    const s = await run("POST", `/webhooks/${h.id}/rotate`);
    if (s) {
      setUrl(s.url);
      setPanel("url");
    }
  };
  return (
    <div className="webhook">
      <div className="webhook-head">
        <span className="webhook-name">{h.name}</span>
        {!h.enabled && <span className="chip">disabled</span>}
        <span className="meta">{h.last_delivery_at ? `last ${ago(h.last_delivery_at)}` : "no deliveries"}</span>
      </div>
      {h.instructions && <p className="webhook-instructions">{h.instructions}</p>}
      <div className="webhook-actions">
        <Button type="button" variant="ghost" aria-expanded={panel === "url"} onClick={() => toggle("url")}>
          URL
        </Button>
        <Button type="button" variant="ghost" aria-expanded={panel === "deliveries"} onClick={() => toggle("deliveries")}>
          Deliveries{h.delivery_count ? ` (${h.delivery_count})` : ""}
        </Button>
        <Button type="button" variant="ghost" aria-expanded={panel === "edit"} onClick={() => toggle("edit")}>
          Edit
        </Button>
        <Button type="button" variant="ghost" onClick={() => run("PATCH", `/webhooks/${h.id}`, { enabled: !h.enabled })}>
          {h.enabled ? "Disable" : "Enable"}
        </Button>
        <Button type="button" variant="ghost" onClick={rotate}>
          Rotate
        </Button>
        <Button
          type="button"
          variant="ghost"
          className="button-danger"
          onClick={() => window.confirm(`Delete webhook "${h.name}" and its deliveries?`) && run("DELETE", `/webhooks/${h.id}`)}
        >
          Delete
        </Button>
      </div>
      {panel === "url" && <UrlBox url={url} />}
      {panel === "deliveries" && <Deliveries hook={h} call={call} run={run} />}
      {panel === "edit" && <EditWebhook hook={h} run={run} onDone={() => setPanel(null)} />}
    </div>
  );
}

function EditWebhook({ hook, run, onDone }) {
  const [name, setName] = useState(hook.name);
  const [instructions, setInstructions] = useState(hook.instructions);
  const [secret, setSecret] = useState("");
  const save = async (e) => {
    e.preventDefault();
    const patch = { name, instructions };
    if (secret) patch.signing_secret = secret;
    if (await run("PATCH", `/webhooks/${hook.id}`, patch)) onDone();
  };
  return (
    <form className="webhook-form" onSubmit={save}>
      <Input required aria-label="Webhook name" value={name} onChange={(e) => setName(e.target.value)} />
      <Textarea aria-label="Instructions for agents" rows={2} value={instructions} onChange={(e) => setInstructions(e.target.value)} />
      <Input
        type="password"
        autoComplete="new-password"
        aria-label="New signing secret"
        placeholder={hook.has_signing_secret ? "New signing secret (blank: keep)" : "Signing secret (optional)"}
        value={secret}
        onChange={(e) => setSecret(e.target.value)}
      />
      <div className="actions-row">
        <Button type="submit">Save</Button>
        {hook.has_signing_secret && (
          <Button type="button" variant="ghost" onClick={async () => (await run("PATCH", `/webhooks/${hook.id}`, { signing_secret: "" })) && onDone()}>
            Remove secret
          </Button>
        )}
        <Button type="button" variant="ghost" onClick={onDone}>
          Cancel
        </Button>
      </div>
    </form>
  );
}

function Deliveries({ hook, call, run }) {
  const [list, setList] = useState(null);
  const [open, setOpen] = useState(null); // The delivery being viewed, with payload.
  // Refetch when a new delivery arrives (live updates reload the webhooks).
  useEffect(() => {
    let current = true;
    call("GET", `/webhooks/${hook.id}/deliveries?limit=20`).then(
      (l) => current && setList(l),
      () => current && setList([]),
    );
    return () => {
      current = false;
    };
  }, [hook.id, hook.last_delivery_at]);
  const view = async (d) => {
    if (open?.id === d.id) return setOpen(null);
    const full = await run("GET", `/deliveries/${d.id}`);
    if (full) setOpen(full);
  };
  if (!list) return null;
  if (list.length === 0) return <p className="field-description">No deliveries yet.</p>;
  return (
    <ul className="delivery-list">
      {list.map((d) => (
        <li key={d.id}>
          <button type="button" className="delivery-line" aria-expanded={open?.id === d.id} onClick={() => view(d)}>
            <span className="time">{ago(d.received_at)}</span>
            <span className="delivery-summary">{d.summary}</span>
          </button>
          {open?.id === d.id && <pre className="delivery-payload">{formatDelivery(open)}</pre>}
        </li>
      ))}
    </ul>
  );
}

function formatDelivery(d) {
  const headers = Object.entries(d.headers || {})
    .map(([k, v]) => `${k}: ${v}`)
    .join("\n");
  let payload = d.payload || "";
  try {
    payload = JSON.stringify(JSON.parse(payload), null, 2);
  } catch {
    // Not JSON: show as is.
  }
  const note = d.truncated ? `\n(truncated: ${d.size} bytes received)` : "";
  return `${headers}\n\n${payload}${note}`;
}
