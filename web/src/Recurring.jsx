import { useEffect, useState } from "react";
import { Play, Plus, Repeat, Trash2 } from "lucide-react";
import { submitOnModEnter } from "./backlogFormat.js";
import { itemLink } from "./nav.js";
import Button from "./ui/Button.jsx";
import Dialog from "./ui/Dialog.jsx";
import Field from "./ui/Field.jsx";
import { Input, Select, Textarea } from "./ui/Input.jsx";
import ScreenHead from "./ui/ScreenHead.jsx";

// Recurring tasks (#113, #120): a top-level screen listing the schedules that
// add a backlog item at the top of the queue every day, or every week on a
// given day, at 00:00 server time (the server makes the runs, #112). Clicking
// a row opens the schedule to edit, run now, or delete it.

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

export default function Recurring({ schedules, call, onError, head, error }) {
  const [editing, setEditing] = useState(null); // A schedule id, or "new".
  const [ran, setRan] = useState(null); // Last Run now result: { item, added }.
  const run = (method, suffix, payload) => call(method, suffix, payload).catch((e) => onError(e.message));
  const activeSchedule = editing && editing !== "new" ? schedules.find((s) => s.id === editing) : null;

  useEffect(() => {
    if (editing !== null && editing !== "new" && !activeSchedule) setEditing(null);
  }, [editing, activeSchedule]);

  useEffect(() => {
    if (!ran) return;
    const t = setTimeout(() => setRan(null), 6000);
    return () => clearTimeout(t);
  }, [ran]);

  return (
    <section className="screen" aria-label="Recurring">
      <ScreenHead title="Recurring">
        {head}
        <Button variant="primary" size="small" onClick={() => setEditing("new")} aria-haspopup="dialog">
          <Plus aria-hidden="true" />
          <span className="label">New task</span>
        </Button>
      </ScreenHead>
      {error}
      <div className="screen-body">
        {ran && (
          <p role="status" className="error-bar notice-bar">
            <span>
              {ran.added ? (
                <>
                  Added <a {...itemLink(ran.item.id)}>#{ran.item.id}</a> to the top of the backlog.
                </>
              ) : (
                <>
                  <a {...itemLink(ran.item.id)}>#{ran.item.id}</a> from this recurring task is still open, so the run was noted on it.
                </>
              )}
            </span>
            <Button variant="ghost" onClick={() => setRan(null)}>
              dismiss
            </Button>
          </p>
        )}
        {schedules.length === 0 ? (
          <p className="recurring-empty">No recurring tasks yet.</p>
        ) : (
          <div className="recurring-list">
            {schedules.map((s) => (
              <div key={s.id} className="item recurring">
                <div
                  className="row"
                  onClick={(e) => {
                    e.currentTarget.querySelector(".row-title").focus();
                    setEditing(s.id);
                  }}
                >
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
          </div>
        )}
      </div>
      {(editing === "new" || activeSchedule) && (
        <ScheduleDialog key={editing} schedule={activeSchedule} run={run} onRan={setRan} onClose={() => setEditing(null)} />
      )}
    </section>
  );
}

// ScheduleDialog adds a schedule (schedule null) or edits/deletes one, or
// runs it now (#116): exactly what a due run does, without moving the next
// run. onRan gets the server's {item, added}.
function ScheduleDialog({ schedule, run, onRan, onClose }) {
  const [title, setTitle] = useState(schedule?.title || "");
  const [body, setBody] = useState(schedule?.body || "");
  const [cadence, setCadence] = useState(schedule?.cadence || "daily");
  const [weekday, setWeekday] = useState(schedule?.weekday || "mon");
  const [busy, setBusy] = useState(false);
  const path = schedule ? `/schedules/${schedule.id}` : "/schedules";
  const save = async (e) => {
    e.preventDefault();
    setBusy(true);
    const payload = { title, body, cadence, weekday: cadence === "weekly" ? weekday : "" };
    const ok = await run(schedule ? "PATCH" : "POST", path, payload);
    setBusy(false);
    if (ok) onClose();
  };
  const runNow = async () => {
    setBusy(true);
    const result = await run("POST", `${path}/run`);
    setBusy(false);
    if (result) {
      onClose();
      onRan(result);
    }
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
        <div className="actions-row dialog-actions">
          {schedule && (
            <Button type="button" variant="ghost" className="button-danger" onClick={remove}>
              <Trash2 aria-hidden="true" />
              Delete
            </Button>
          )}
          <span className="spacer" />
          {schedule && (
            <Button type="button" onClick={runNow} disabled={busy} title="Add this task to the top of the backlog now; the schedule stays as it is">
              <Play aria-hidden="true" />
              Run now
            </Button>
          )}
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
