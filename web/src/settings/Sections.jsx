import { useState } from "react";
import {
  ArrowDown,
  ArrowUp,
  Bookmark,
  Bug,
  ClipboardCheck,
  Compass,
  Flag,
  FlaskConical,
  Folder,
  HelpCircle,
  Inbox,
  Layers,
  Lightbulb,
  Pencil,
  Plus,
  Scale,
  Shield,
  Sparkles,
  Trash2,
  Zap,
} from "lucide-react";
import Button from "../ui/Button.jsx";
import Checkbox from "../ui/Checkbox.jsx";
import Field from "../ui/Field.jsx";
import { Input } from "../ui/Input.jsx";

export const SECTION_ICONS = [
  { id: "inbox", label: "Inbox", Icon: Inbox },
  { id: "scale", label: "Scale", Icon: Scale },
  { id: "flask-conical", label: "Flask", Icon: FlaskConical },
  { id: "sparkles", label: "Sparkles", Icon: Sparkles },
  { id: "bug", label: "Bug", Icon: Bug },
  { id: "compass", label: "Compass", Icon: Compass },
  { id: "shield", label: "Shield", Icon: Shield },
  { id: "layers", label: "Layers", Icon: Layers },
  { id: "lightbulb", label: "Lightbulb", Icon: Lightbulb },
  { id: "bookmark", label: "Bookmark", Icon: Bookmark },
  { id: "flag", label: "Flag", Icon: Flag },
  { id: "help-circle", label: "Help", Icon: HelpCircle },
  { id: "folder", label: "Folder", Icon: Folder },
  { id: "zap", label: "Zap", Icon: Zap },
  { id: "clipboard-check", label: "Checklist", Icon: ClipboardCheck },
];

const ICON_MAP = Object.fromEntries(SECTION_ICONS.map((entry) => [entry.id, entry.Icon]));

export function SectionIcon({ icon, ...props }) {
  const Icon = ICON_MAP[icon] || Folder;
  return <Icon {...props} />;
}

export function withSection(list, sec) {
  const next = list.some((x) => x.id === sec.id) ? list.map((x) => (x.id === sec.id ? sec : x)) : [...list, sec];
  return [...next].sort((a, b) => (a.position || 0) - (b.position || 0) || a.id - b.id);
}

function IconPickerGrid({ value, onChange, labelId }) {
  return (
    <div className="section-icon-grid" role="group" aria-labelledby={labelId}>
      {SECTION_ICONS.map(({ id, label, Icon }) => (
        <button
          key={id}
          type="button"
          className={value === id ? "section-icon-option selected" : "section-icon-option"}
          aria-pressed={value === id}
          aria-label={label}
          title={`${label} (${id})`}
          onClick={() => onChange(id)}
        >
          <Icon aria-hidden="true" />
        </button>
      ))}
    </div>
  );
}

export default function Sections({ sections = [], call, onError }) {
  const [name, setName] = useState("");
  const [icon, setIcon] = useState("inbox");
  const [notify, setNotify] = useState(false);
  const [editingId, setEditingId] = useState(null);

  const run = (method, suffix, body) =>
    call(method, suffix, body).then(
      (res) => res ?? true,
      (e) => {
        onError(e.message);
        return null;
      },
    );

  const add = async (e) => {
    e.preventDefault();
    if (!name.trim()) return;
    const created = await run("POST", "/sections", { name: name.trim(), icon, notify });
    if (created) {
      setName("");
      setIcon("inbox");
      setNotify(false);
    }
  };

  const move = (sec, delta) => {
    const nextPos = sec.position + delta;
    if (nextPos < 1 || nextPos > sections.length) return;
    run("POST", `/sections/${sec.id}/move`, { position: nextPos });
  };

  const toggleNotify = (sec, next) => {
    run("PATCH", `/sections/${sec.id}`, { notify: Boolean(next) });
  };

  const remove = (sec) => {
    if (!window.confirm(`Delete section "${sec.name}"? Any items in it will move back to Backlog.`)) return;
    run("DELETE", `/sections/${sec.id}`);
  };

  return (
    <div className="sections-settings">
      {sections.length === 0 ? (
        <p className="field-description">No custom sections configured. Workspaces without custom sections show only the main Backlog.</p>
      ) : (
        <div className="sections-list">
          {sections.map((sec, idx) =>
            editingId === sec.id ? (
              <SectionEditRow
                key={sec.id}
                section={sec}
                run={run}
                onDone={() => setEditingId(null)}
              />
            ) : (
              <div key={sec.id} className="section-settings-row">
                <span className="section-settings-icon" aria-hidden="true">
                  <SectionIcon icon={sec.icon} />
                </span>
                <div className="section-settings-main">
                  <div className="section-settings-title-row">
                    <span className="section-settings-name">{sec.name}</span>
                    <code className="artifact-filename">/s/{sec.slug}</code>
                  </div>
                  <div className="section-settings-notify">
                    <Checkbox checked={Boolean(sec.notify)} onChange={(next) => toggleNotify(sec, next)}>
                      Surface assigned items in Actionable (notify)
                    </Checkbox>
                  </div>
                </div>
                <div className="section-settings-actions">
                  <Button
                    type="button"
                    iconOnly
                    variant="ghost"
                    disabled={idx === 0}
                    aria-label={`Move ${sec.name} up`}
                    title="Move up"
                    onClick={() => move(sec, -1)}
                  >
                    <ArrowUp aria-hidden="true" />
                  </Button>
                  <Button
                    type="button"
                    iconOnly
                    variant="ghost"
                    disabled={idx === sections.length - 1}
                    aria-label={`Move ${sec.name} down`}
                    title="Move down"
                    onClick={() => move(sec, 1)}
                  >
                    <ArrowDown aria-hidden="true" />
                  </Button>
                  <Button
                    type="button"
                    iconOnly
                    variant="ghost"
                    aria-label={`Edit ${sec.name}`}
                    title="Edit section"
                    onClick={() => setEditingId(sec.id)}
                  >
                    <Pencil aria-hidden="true" />
                  </Button>
                  <Button
                    type="button"
                    iconOnly
                    variant="ghost"
                    className="button-danger"
                    aria-label={`Delete ${sec.name}`}
                    title="Delete section"
                    onClick={() => remove(sec)}
                  >
                    <Trash2 aria-hidden="true" />
                  </Button>
                </div>
              </div>
            ),
          )}
        </div>
      )}

      <form className="form section-create-form" onSubmit={add}>
        <Field label="New section">
          <Input
            required
            maxLength={60}
            aria-label="Section name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g. Triage, Decisions, Research"
          />
        </Field>
        <div className="field">
          <span id="new-section-icon-label" className="field-label">
            Icon
          </span>
          <IconPickerGrid value={icon} onChange={setIcon} labelId="new-section-icon-label" />
        </div>
        <Checkbox checked={notify} onChange={setNotify}>
          Surface assigned items in Actionable (notify)
        </Checkbox>
        <div className="actions-row">
          <Button type="submit" variant="primary" disabled={!name.trim()}>
            <Plus aria-hidden="true" />
            Add section
          </Button>
        </div>
      </form>
    </div>
  );
}

function SectionEditRow({ section, run, onDone }) {
  const [name, setName] = useState(section.name);
  const [slug, setSlug] = useState(section.slug);
  const [icon, setIcon] = useState(section.icon || "folder");
  const [notify, setNotify] = useState(Boolean(section.notify));

  const save = async (e) => {
    e.preventDefault();
    if (!name.trim()) return;
    const updated = await run("PATCH", `/sections/${section.id}`, {
      name: name.trim(),
      slug: slug.trim(),
      icon,
      notify,
    });
    if (updated) onDone();
  };

  const iconLabelId = `edit-section-icon-${section.id}`;

  return (
    <form className="form section-edit-card" onSubmit={save}>
      <div className="section-edit-fields">
        <Field label="Name">
          <Input required maxLength={60} value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label="URL slug">
          <Input required maxLength={60} value={slug} onChange={(e) => setSlug(e.target.value)} />
        </Field>
      </div>
      <div className="field">
        <span id={iconLabelId} className="field-label">
          Icon
        </span>
        <IconPickerGrid value={icon} onChange={setIcon} labelId={iconLabelId} />
      </div>
      <Checkbox checked={notify} onChange={setNotify}>
        Surface assigned items in Actionable (notify)
      </Checkbox>
      <div className="actions-row">
        <Button type="submit" variant="primary">
          Save
        </Button>
        <Button type="button" variant="ghost" onClick={onDone}>
          Cancel
        </Button>
      </div>
    </form>
  );
}
