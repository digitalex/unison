import { useState } from "react";
import { ChevronDown, ChevronRight, Pencil, Plus, Trash2, TrendingDown, TrendingUp } from "lucide-react";
import Button from "./ui/Button.jsx";
import { Input, Select } from "./ui/Input.jsx";

// What the workspace is trying to optimize. Adding a metric also queues a
// "Track metric" backlog item (done by the server, in the same transaction).
export default function Metrics({ metrics, items, call, onError }) {
  const [open, setOpen] = useState(true);
  const [editing, setEditing] = useState(null);
  const itemsById = new Map(items.map((it) => [it.id, it]));
  const run = (method, suffix, payload) =>
    call(method, suffix, payload).then(
      () => true,
      (e) => {
        onError(e.message);
        return false;
      },
    );

  return (
    <section aria-label="Metrics" className="metrics">
      <div className="toolbar">
        <h2>
          <button type="button" className="section-toggle" aria-expanded={open} onClick={() => setOpen(!open)}>
            {open ? <ChevronDown aria-hidden="true" /> : <ChevronRight aria-hidden="true" />}
            Metrics
          </button>
        </h2>
        <span className="muted">{metrics.length}</span>
      </div>
      {open && (
        <>
          {metrics.map((m) =>
            editing === m.id ? (
              <MetricForm
                key={m.id}
                initial={m}
                onSubmit={async (fields) => (await run("PATCH", `/metrics/${m.id}`, fields)) && setEditing(null)}
                onCancel={() => setEditing(null)}
              />
            ) : (
              <MetricRow
                key={m.id}
                metric={m}
                item={itemsById.get(m.item_id)}
                onEdit={() => setEditing(m.id)}
                onDelete={() => run("DELETE", `/metrics/${m.id}`)}
              />
            ),
          )}
          <MetricForm onSubmit={(fields) => run("POST", "/metrics", fields)} />
        </>
      )}
    </section>
  );
}

function formatTarget(m) {
  if (m.target === null || m.target === undefined) return m.unit ? `in ${m.unit}` : "";
  return `target ${m.target}${m.unit ? ` ${m.unit}` : ""}`;
}

function MetricRow({ metric: m, item, onEdit, onDelete }) {
  const Arrow = m.direction === "decrease" ? TrendingDown : TrendingUp;
  const remove = () => {
    const kept = m.item_id ? ` Its tracking item #${m.item_id} stays in the backlog.` : "";
    if (window.confirm(`Delete metric "${m.name}"?${kept}`)) onDelete();
  };
  const jump = (e) => {
    e.preventDefault();
    document.getElementById(`item-${m.item_id}`)?.scrollIntoView({ block: "center", behavior: "smooth" });
  };
  return (
    <div className="row metric-row">
      <Arrow className="metric-arrow" role="img" aria-label={m.direction} />
      <span className="metric-name">{m.name}</span>
      <span className="metric-description">{m.description}</span>
      <span className="meta">{formatTarget(m)}</span>
      {m.item_id && (
        <a className="chip item-link" href={`#item-${m.item_id}`} onClick={jump} title={item ? `Tracking: ${item.title}` : "Tracking item"}>
          <span className={`status ${item?.status || "open"}`} aria-hidden="true" />#{m.item_id}
        </a>
      )}
      <span className="row-actions">
        <Button iconOnly variant="ghost" aria-label={`Edit ${m.name}`} title="Edit" onClick={onEdit}>
          <Pencil aria-hidden="true" />
        </Button>
        <Button iconOnly variant="ghost" className="button-danger" aria-label={`Delete ${m.name}`} title="Delete" onClick={remove}>
          <Trash2 aria-hidden="true" />
        </Button>
      </span>
    </div>
  );
}

const EMPTY = { name: "", description: "", direction: "increase", unit: "", target: null };

// MetricForm adds a metric (no initial) or edits one in place.
function MetricForm({ initial, onSubmit, onCancel }) {
  const start = initial || EMPTY;
  const [name, setName] = useState(start.name);
  const [description, setDescription] = useState(start.description);
  const [direction, setDirection] = useState(start.direction);
  const [unit, setUnit] = useState(start.unit);
  const [target, setTarget] = useState(start.target ?? "");
  const submit = async (e) => {
    e.preventDefault();
    const fields = { name, description, direction, unit, target: String(target).trim() === "" ? null : Number(target) };
    if ((await onSubmit(fields)) && !initial) {
      setName("");
      setDescription("");
      setDirection("increase");
      setUnit("");
      setTarget("");
    }
  };
  return (
    <form className={initial ? "metric-form editing" : "metric-form"} onSubmit={submit}>
      {initial ? <Pencil aria-hidden="true" className="muted" /> : <Plus aria-hidden="true" className="muted" />}
      <input
        className="bare"
        required
        aria-label="Metric name"
        value={name}
        onChange={(e) => setName(e.target.value)}
        placeholder="Add a metric…"
      />
      <input
        className="bare"
        aria-label="Description (optional)"
        value={description}
        onChange={(e) => setDescription(e.target.value)}
        placeholder="Description (optional)"
      />
      <Select aria-label="Direction" value={direction} onChange={(e) => setDirection(e.target.value)}>
        <option value="increase">Increase</option>
        <option value="decrease">Decrease</option>
      </Select>
      <Input className="compact" aria-label="Unit (optional)" value={unit} onChange={(e) => setUnit(e.target.value)} placeholder="Unit" />
      <Input
        className="compact"
        type="number"
        step="any"
        aria-label="Target (optional)"
        value={target}
        onChange={(e) => setTarget(e.target.value)}
        placeholder="Target"
      />
      <Button type="submit">{initial ? "Save" : "Add"}</Button>
      {onCancel && (
        <Button type="button" variant="ghost" onClick={onCancel}>
          Cancel
        </Button>
      )}
    </form>
  );
}
