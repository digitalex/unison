import { useState } from "react";
import { ChevronDown, ChevronRight, Play, Plus, Repeat, Trash2 } from "lucide-react";
import { submitOnModEnter } from "./backlogFormat.js";
import Button from "./ui/Button.jsx";
import Dialog from "./ui/Dialog.jsx";
import Field from "./ui/Field.jsx";
import { Input, Select, Textarea } from "./ui/Input.jsx";

// Recurring tasks (#113): schedules that add a backlog item at the top of the
// queue every day, or every week on a given day, at 00:00 server time (the
// server makes the runs, #112). The section lists them with their next run;
// a row opens the schedule to edit or delete it.

const WEEKDAYS = [
  ["mon", "Monday"],
  ["tue", "Tuesday"],
  ["wed", "Wednesday"],
  ["thu", "Thursday"],
  ["fri", "Friday"],
  ["sat", "Saturday"],
  ["sun", "Sunday"],
];

// cadenceLabel is "Daily" or "Weekly on Mon".
export function cadenceLabel(s) {
  if (s.cadence !== "weekly") return "Daily";
  const day = WEEKDAYS.find(([id]) => id === s.weekday);
  return `Weekly on ${day ? day[1].slice(0, 3) : s.weekday}`;
}

function nextRun(ms) {
  return new Date(ms).toLocaleString([], { weekday: "short", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

// RecurringMarker marks a backlog item that a schedule added.
export function RecurringMarker({ item }) {
  if (!item.schedule_id) return null;
  return (
    <span className="recurring-marker" title="Added by a recurring task" aria-label="Recurring">
      <Repeat aria-hidden="true" />
    </span>
  );
}

// RecurringSection is the backlog's Recurring group: collapsible, like Done.
export default function RecurringSection({ schedules, run }) {
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState(null); // A schedule, or "new".
  return (
    <>
      <button type="button" className="group-head" aria-expanded={open} onClick={() => setOpen(!open)}>
        {open ? <ChevronDown aria-hidden="true" /> : <ChevronRight aria-hidden="true" />}
        <Repeat aria-hidden="true" className="group-icon" />
        Recurring
        <span className="count">{schedules.length}</span>
      </button>
      {open && (
        <>
          {schedules.map((s) => (
            <div key={s.id} className="item recurring">
              <div className="row" onClick={() => setEditing(s)}>
                <span className="rank" />
                <span className="recurring-marker" aria-hidden="true">
                  <Repeat />
                </span>
                <button type="button" className="row-title" aria-haspopup="dialog">
                  {s.title}
                </button>
                <span className="chip">{cadenceLabel(s)}</span>
                <span className="meta" title={new Date(s.next_due_at).toLocaleString()}>
                  next {nextRun(s.next_due_at)}
                </span>
              </div>
            </div>
          ))}
          <button type="button" className="add-row" onClick={() => setEditing("new")}>
            <Plus aria-hidden="true" />
            Add recurring task
          </button>
        </>
      )}
      {editing && <ScheduleDialog schedule={editing === "new" ? null : editing} run={run} onClose={() => setEditing(null)} />}
    </>
  );
}

// ScheduleDialog adds a schedule (schedule null) or edits/deletes one.
function ScheduleDialog({ schedule, run, onClose }) {
  const [title, setTitle] = useState(schedule?.title || "");
  const [body, setBody] = useState(schedule?.body || "");
  const [cadence, setCadence] = useState(schedule?.cadence || "daily");
  const [weekday, setWeekday] = useState(schedule?.weekday || "mon");
  const [busy, setBusy] = useState(false);
  const [ran, setRan] = useState(null);
  const path = schedule ? `/schedules/${schedule.id}` : "/schedules";
  const save = async (e) => {
    e.preventDefault();
    setBusy(true);
    const payload = { title, body, cadence, weekday: cadence === "weekly" ? weekday : "" };
    const ok = await run(schedule ? "PATCH" : "POST", path, payload);
    setBusy(false);
    if (ok) onClose();
  };
  // Run now (#116): exactly what a due run does; the next run stays.
  const runNow = async () => {
    setBusy(true);
    const res = await run("POST", `${path}/run`);
    setBusy(false);
    if (res?.item)
      setRan(
        res.added
          ? `Added #${res.item.id} to the top of the backlog.`
          : `#${res.item.id} is still open, so no new item was added; the run is noted on it.`,
      );
  };
  const remove = async () => {
    if (window.confirm(`Delete the recurring task "${schedule.title}"? Items it already added stay.`) && (await run("DELETE", path)) !== undefined) onClose();
  };
  return (
    <Dialog title={schedule ? "Recurring task" : "New recurring task"} onClose={onClose} className="item-dialog">
      <form className="form item-form" onSubmit={save} onKeyDown={submitOnModEnter}>
        <Field label="Title">
          <Input required data-autofocus value={title} onChange={(e) => setTitle(e.target.value)} placeholder="What needs doing each time?" />
        </Field>
        <Field label="Description">
          <Textarea rows={6} value={body} onChange={(e) => setBody(e.target.value)} placeholder="Instructions for whoever picks it up (Markdown)" />
        </Field>
        <div className="schedule-when">
          <Field label="Repeats">
            <Select value={cadence} onChange={(e) => setCadence(e.target.value)}>
              <option value="daily">Daily</option>
              <option value="weekly">Weekly</option>
            </Select>
          </Field>
          {cadence === "weekly" && (
            <Field label="On">
              <Select value={weekday} onChange={(e) => setWeekday(e.target.value)}>
                {WEEKDAYS.map(([id, label]) => (
                  <option key={id} value={id}>
                    {label}
                  </option>
                ))}
              </Select>
            </Field>
          )}
        </div>
        <p className="muted field-description">
          Each run adds this as a new item at the top of the backlog at 00:00 server time. If the last run is still open, it's noted on that item instead.
          {schedule && ` Next run: ${nextRun(schedule.next_due_at)}.`}
        </p>
        {ran && (
          <p className="field-description" role="status">
            {ran}
          </p>
        )}
        <div className="actions-row dialog-actions">
          {schedule && (
            <Button type="button" variant="ghost" className="button-danger" onClick={remove}>
              <Trash2 aria-hidden="true" />
              Delete
            </Button>
          )}
          {schedule && (
            <Button type="button" variant="ghost" onClick={runNow} disabled={busy} title="Do now what a due run does; the next scheduled run stays">
              <Play aria-hidden="true" />
              Run now
            </Button>
          )}
          <span className="spacer" />
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={busy}>
            {schedule ? "Save" : "Add recurring task"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
