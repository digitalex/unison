import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Check, CheckSquare, CircleAlert, Ellipsis, GitPullRequest, MessagesSquare, Pencil, Plus, Trash2, X } from "lucide-react";
import { ArtifactTypeIcon, UploadArtifactDialog, formatArtifactSize, withArtifact } from "./Artifacts.jsx";
import { STATUS_LABELS, ago, blockerTitle, blockersOf, hoursLeft, itemNum, name, submitOnModEnter, useStableParticipants } from "./backlogFormat.js";
import { ActionLine, REACTIONS, Reactions, withDone, withReaction } from "./Chat.jsx";
import { MemoMarkdown } from "./Markdown.jsx";
import { itemHref, itemLink, messageLink, spaLink } from "./nav.js";
import { RecurringMarker } from "./Recurring.jsx";
import { NewTodoDialog, withTodo } from "./Todos.jsx";
import Button from "./ui/Button.jsx";
import Checkbox from "./ui/Checkbox.jsx";
import CopyLinkButton from "./ui/CopyLinkButton.jsx";
import Dialog from "./ui/Dialog.jsx";
import Field from "./ui/Field.jsx";
import { MentionProvider, MentionTextarea } from "./mentions.jsx";
import { Input, Select } from "./ui/Input.jsx";

function ChoicesField({ choices, setChoices, closeOnAnswer, setCloseOnAnswer }) {
  if (choices.length === 0) {
    return (
      <div className="choices-add-row">
        <Button type="button" size="small" variant="ghost" onClick={() => setChoices(["", ""])}>
          <Plus aria-hidden="true" />
          Add choice
        </Button>
      </div>
    );
  }
  const updateAt = (idx, value) => setChoices(choices.map((c, i) => (i === idx ? value : c)));
  const removeAt = (idx) => {
    const next = choices.filter((_, i) => i !== idx);
    setChoices(next);
    if (next.length === 0) setCloseOnAnswer(false);
  };
  return (
    <div className="field choices-editor">
      <div className="detail-section-head">
        <span className="field-label">Decision choices</span>
        <Button
          type="button"
          size="small"
          variant="ghost"
          onClick={() => {
            setChoices([]);
            setCloseOnAnswer(false);
          }}
        >
          Clear choices
        </Button>
      </div>
      <div className="choices-inputs">
        {choices.map((val, idx) => (
          <div key={idx} className="choice-input-row">
            <Input
              maxLength={120}
              aria-label={`Choice ${idx + 1}`}
              value={val}
              onChange={(e) => updateAt(idx, e.target.value)}
              placeholder={`Option ${idx + 1} (e.g. ${idx === 0 ? "Approve" : "Request changes"})`}
            />
            <Button
              type="button"
              iconOnly
              variant="ghost"
              aria-label={`Remove choice ${idx + 1}`}
              title="Remove choice"
              onClick={() => removeAt(idx)}
            >
              <X aria-hidden="true" />
            </Button>
          </div>
        ))}
      </div>
      <div className="choices-editor-foot">
        {choices.length < 10 && (
          <Button type="button" size="small" variant="ghost" onClick={() => setChoices([...choices, ""])}>
            <Plus aria-hidden="true" />
            Add choice
          </Button>
        )}
        <Checkbox checked={closeOnAnswer} onChange={setCloseOnAnswer}>
          Close item automatically when answered
        </Checkbox>
      </div>
    </div>
  );
}

// NewItemDialog writes a backlog item: a title, description, and optional
// assignee (#146), plus custom section & structured decision choices (#919)
// when sections are configured.
export function NewItemDialog({ initial, section = null, sections = [], participants = [], onClose, onCreate }) {
  const [title, setTitle] = useState(initial?.title || "");
  const [body, setBody] = useState(initial?.body || "");
  const [assigneeId, setAssigneeId] = useState(initial?.assignee_id || 0);
  const [sectionId, setSectionId] = useState(() => initial?.section_id ?? (section?.id || 0));
  const [choices, setChoices] = useState(() => (initial?.choices ? [...initial.choices] : []));
  const [closeOnAnswer, setCloseOnAnswer] = useState(() => Boolean(initial?.close_on_answer));
  const create = (e) => {
    e.preventDefault();
    const cleanedChoices = choices.map((c) => c.trim()).filter(Boolean);
    onCreate({
      title,
      body,
      assignee_id: assigneeId,
      section_id: sectionId,
      choices: cleanedChoices,
      close_on_answer: closeOnAnswer && cleanedChoices.length > 0,
    });
  };
  const dialogTitle = section ? `New ${section.name} item` : "New backlog item";
  return (
    <Dialog title={dialogTitle} onClose={onClose} className="item-dialog">
      <form className="form item-form" onSubmit={create} onKeyDown={submitOnModEnter}>
        <Field label="Title">
          <Input required data-autofocus value={title} onChange={(e) => setTitle(e.target.value)} placeholder="What needs doing?" />
        </Field>
        <Field label="Description">
          <MentionTextarea rows={10} value={body} onChange={(e) => setBody(e.target.value)} placeholder="Details, acceptance criteria, links, screenshots… (Markdown)" />
        </Field>
        {sections.length > 0 && (
          <Field label="Section">
            <Select value={sectionId} onChange={(e) => setSectionId(Number(e.target.value))}>
              <option value={0}>Backlog</option>
              {sections.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
            </Select>
          </Field>
        )}
        <Field label="Assignee">
          <Select value={assigneeId} onChange={(e) => setAssigneeId(Number(e.target.value))}>
            <option value={0}>Unassigned</option>
            {participants.map((p) => (
              <option key={p.id} value={p.id}>
                {p.emoji} {p.name}
              </option>
            ))}
          </Select>
        </Field>
        {(sections.length > 0 || choices.length > 0) && (
          <ChoicesField
            choices={choices}
            setChoices={setChoices}
            closeOnAnswer={closeOnAnswer}
            setCloseOnAnswer={setCloseOnAnswer}
          />
        )}
        <div className="actions-row dialog-actions">
          <span className="muted dialog-hint">⌘/Ctrl+Enter to create</span>
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary">
            Create item
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// ItemDialog shows one item with everything you can do to it. It renders
// whatever the live list holds, so other people's changes show up here.
export function ItemDialog({ item, items = [], sections = [], messages = [], updateMessages, todos = [], setTodos, artifacts = [], setArtifacts, workspace, token = "", onReloadMe, participants, me, call, run, onError, onClose }) {
  const num = itemNum(item);
  const path = `/backlog/${num}`;
  const [editing, setEditing] = useState(false);
  const stableParticipants = useStableParticipants(participants);
  return (
    <Dialog
      title={
        <>
          <span className={`status ${item.status}`} title={STATUS_LABELS[item.status]} />
          <span className="item-id">#{num}</span>
          <RecurringMarker item={item} />
          <span className="dialog-title-text">{item.title}</span>
        </>
      }
      head={
        <>
          <CopyLinkButton
            href={itemHref(num, workspace?.id || item.workspace_id)}
            label={`Copy link to #${num}`}
            title="Copy link to item"
          />
          {!editing && (
            <Button iconOnly variant="ghost" aria-label="Edit title and description" title="Edit" onClick={() => setEditing(true)}>
              <Pencil aria-hidden="true" />
            </Button>
          )}
        </>
      }
      onClose={onClose}
      className="item-dialog"
    >
      {editing ? (
        <EditForm item={item} sections={sections} path={path} run={run} onReloadMe={onReloadMe} onDone={() => setEditing(false)} />
      ) : (
        <>
          <div className="item-description">{item.body ? <MemoMarkdown text={item.body} /> : <p className="muted">No description.</p>}</div>
          {item.choices?.length > 0 && (
            <DecisionCard item={item} path={path} participants={stableParticipants} run={run} onReloadMe={onReloadMe} />
          )}
        </>
      )}
      <ItemFacts item={item} participants={stableParticipants} />
      <ItemActions item={item} path={path} participants={stableParticipants} me={me} run={run} onDeleted={onClose} />
      <NeedsList key={`needs-${path}`} item={item} items={items} path={path} run={run} />
      <PRList item={item} path={path} run={run} />
      <ItemArtifacts
        item={item}
        items={items}
        artifacts={artifacts}
        setArtifacts={setArtifacts}
        workspaceId={workspace?.id || item.workspace_id}
        token={token}
      />
      <ItemTodos
        item={item}
        items={items}
        todos={todos}
        setTodos={setTodos}
        onReloadMe={onReloadMe}
        participants={stableParticipants}
        me={me}
        run={run}
        onNavigate={onClose}
      />
      <ItemThread
        key={path}
        item={item}
        path={path}
        messages={messages}
        updateMessages={updateMessages}
        participants={stableParticipants}
        me={me}
        call={call}
        run={run}
        onError={onError}
      />
    </Dialog>
  );
}

function DecisionCard({ item, path, participants, run, onReloadMe }) {
  const [busyChoice, setBusyChoice] = useState(null);
  const choose = async (choice) => {
    setBusyChoice(choice);
    const updated = await run("POST", `${path}/answer`, { choice });
    setBusyChoice(null);
    if (updated) onReloadMe?.();
  };
  const isDone = item.status === "done";
  return (
    <section className="decision-card" aria-label="Decision choices">
      <div className="decision-card-head">
        <span className="decision-card-title">Decision</span>
        {item.close_on_answer && !isDone && <span className="muted decision-card-hint">Resolves & closes item on click</span>}
      </div>
      <div className="decision-choices">
        {item.choices.map((choice) => {
          const selected = item.answered_choice === choice;
          return (
            <button
              key={choice}
              type="button"
              className={selected ? "decision-choice selected" : "decision-choice"}
              aria-pressed={selected}
              disabled={busyChoice !== null || isDone}
              onClick={() => choose(choice)}
            >
              {selected && <Check aria-hidden="true" />}
              <span>{choice}</span>
            </button>
          );
        })}
      </div>
      {item.answered_choice && (
        <p className="decision-summary">
          Selected <b>{item.answered_choice}</b>
          {item.answered_by_id ? ` by ${name(participants, item.answered_by_id)}` : ""}
          {item.answered_at ? ` · ${ago(item.answered_at)}` : ""}
        </p>
      )}
    </section>
  );
}

function EditForm({ item, sections = [], path, run, onReloadMe, onDone }) {
  const [title, setTitle] = useState(item.title);
  const [body, setBody] = useState(item.body);
  const [sectionId, setSectionId] = useState(item.section_id || 0);
  const [choices, setChoices] = useState(() => (item.choices ? [...item.choices] : []));
  const [closeOnAnswer, setCloseOnAnswer] = useState(Boolean(item.close_on_answer));
  const save = async (e) => {
    e.preventDefault();
    const cleanedChoices = choices.map((c) => c.trim()).filter(Boolean);
    const payload = {
      title,
      body,
      choices: cleanedChoices,
      close_on_answer: closeOnAnswer && cleanedChoices.length > 0,
    };
    if (sections.length > 0 || item.section_id) {
      payload.section_id = sectionId;
    }
    if (await run("PATCH", path, payload)) {
      onReloadMe?.();
      onDone();
    }
  };
  return (
    <form className="form item-form" onSubmit={save} onKeyDown={submitOnModEnter}>
      <Field label="Title">
        <Input required autoFocus value={title} onChange={(e) => setTitle(e.target.value)} />
      </Field>
      <Field label="Description">
        <MentionTextarea rows={10} value={body} onChange={(e) => setBody(e.target.value)} />
      </Field>
      {(sections.length > 0 || item.section_id) && (
        <Field label="Section">
          <Select value={sectionId} onChange={(e) => setSectionId(Number(e.target.value))}>
            <option value={0}>Backlog</option>
            {sections.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </Select>
        </Field>
      )}
      <ChoicesField
        choices={choices}
        setChoices={setChoices}
        closeOnAnswer={closeOnAnswer}
        setCloseOnAnswer={setCloseOnAnswer}
      />
      <div className="actions-row dialog-actions">
        <Button type="button" variant="ghost" onClick={onDone}>
          Cancel
        </Button>
        <Button type="submit" variant="primary">
          Save
        </Button>
      </div>
    </form>
  );
}

function ItemFacts({ item, participants }) {
  const blockers = blockersOf(item);
  const facts = [["Status", STATUS_LABELS[item.status] || item.status]];
  if (item.section_name) facts.push(["Section", item.section_name]);
  if (blockers.length > 0) {
    facts.push([
      "Blocked by",
      blockers.map((n, idx) => (
        <span key={n.id}>
          {idx > 0 ? ", " : ""}
          <a {...itemLink(itemNum(n))}>#{itemNum(n)}</a>
        </span>
      )),
    ]);
  } else if (item.status !== "done" && item.needs?.length > 0 && item.ready_at) {
    facts.push(["Ready", ago(item.ready_at)]);
  }
  if (item.assignee_id) facts.push([item.status === "done" ? "Done by" : "Assignee", name(participants, item.assignee_id)]);
  if (item.status === "in_progress" && item.lease_expires_at) facts.push(["Lease", `${hoursLeft(item.lease_expires_at)}h left`]);
  if (item.status === "done" && item.close_note) facts.push(["Note", item.close_note]);
  facts.push(["Created", `${ago(item.created_at)} by ${name(participants, item.created_by)}`]);
  return (
    <dl className="item-facts">
      {facts.map(([k, v]) => (
        <div key={k}>
          <dt>{k}</dt>
          <dd>{v}</dd>
        </div>
      ))}
    </dl>
  );
}

function ItemActions({ item, path, participants, me, run, onDeleted }) {
  const [closing, setClosing] = useState(false);
  const blockers = blockersOf(item);
  const canStart = item.status === "open" && me && (!item.assignee_id || item.assignee_id === me.id);
  const changeAssignee = (e) => {
    const id = Number(e.target.value);
    if (id !== (item.assignee_id || 0)) run("POST", `${path}/assign`, { assignee_id: id });
  };
  const remove = async () => {
    if (window.confirm(`Delete #${itemNum(item)}? This can't be undone.`) && (await run("DELETE", path))) onDeleted();
  };
  if (closing) return <CloseForm path={path} run={run} onDone={() => setClosing(false)} />;
  return (
    <div className="item-actions">
      {canStart && (
        <Button
          variant={blockers.length > 0 ? "secondary" : "primary"}
          title={blockers.length > 0 ? blockerTitle(blockers) : undefined}
          onClick={() => run("POST", `${path}/start`)}
        >
          Start
        </Button>
      )}
      {item.status === "paused" && (
        <Button variant="primary" onClick={() => run("POST", `${path}/resume`)}>
          Resume
        </Button>
      )}
      {(item.status === "open" || item.status === "in_progress") && (
        <Button onClick={() => run("POST", `${path}/pause`)}>Pause</Button>
      )}
      {item.status === "in_progress" && <Button onClick={() => run("POST", `${path}/stop`)}>Move to open</Button>}
      {(item.status === "in_progress" || (item.status === "paused" && item.assignee_id)) && (
        <Button onClick={() => run("POST", `${path}/release`)}>Release</Button>
      )}
      {item.status !== "done" && (
        <Select value={item.assignee_id || 0} onChange={changeAssignee} aria-label="Assignee">
          <option value={0}>Unassigned</option>
          {participants.map((p) => (
            <option key={p.id} value={p.id}>
              {p.emoji} {p.name}
            </option>
          ))}
        </Select>
      )}
      {item.status !== "done" ? (
        <Button onClick={() => setClosing(true)}>Close…</Button>
      ) : (
        <Button onClick={() => run("POST", `${path}/reopen`)}>Reopen</Button>
      )}
      <span className="spacer" />
      <Button variant="ghost" className="button-danger" onClick={remove}>
        <Trash2 aria-hidden="true" />
        Delete
      </Button>
    </div>
  );
}

function wouldCycle(byId, targetId, candidateId) {
  const seen = new Set([candidateId]);
  const queue = [candidateId];
  while (queue.length > 0) {
    const cur = queue.shift();
    const it = byId.get(cur);
    for (const n of it?.needs || []) {
      if (n.id === targetId) return true;
      if (!seen.has(n.id)) {
        seen.add(n.id);
        queue.push(n.id);
      }
    }
  }
  return false;
}

function NeedsList({ item, items, path, run }) {
  const [adding, setAdding] = useState(false);
  const [query, setQuery] = useState("");
  const needs = item.needs || [];

  const candidates = useMemo(() => {
    const linked = new Set(needs.map((n) => n.id));
    const byId = new Map(items.map((it) => [it.id, it]));
    return items.filter((it) => it.id !== item.id && !linked.has(it.id) && !wouldCycle(byId, item.id, it.id));
  }, [items, item.id, needs]);

  const matches = useMemo(() => {
    const q = query.trim().toLowerCase();
    const qNum = q.startsWith("#") ? q.slice(1) : q;
    const filtered = q
      ? candidates.filter((it) => {
          const numStr = String(itemNum(it));
          return numStr.includes(qNum) || it.title.toLowerCase().includes(q);
        })
      : candidates;
    return [...filtered]
      .sort((a, b) => {
        const aDone = a.status === "done" ? 1 : 0;
        const bDone = b.status === "done" ? 1 : 0;
        if (aDone !== bDone) return aDone - bDone;
        return (a.position || 999999) - (b.position || 999999) || itemNum(a) - itemNum(b);
      })
      .slice(0, 8);
  }, [candidates, query]);

  const addNeed = async (target) => {
    if (!target) return;
    const ok = await run("POST", `${path}/needs`, { needs: [itemNum(target)] });
    if (ok) {
      setQuery("");
      setAdding(false);
    }
  };

  if (needs.length === 0 && candidates.length === 0) return null;

  return (
    <section className="detail-section">
      <div className="detail-section-head">
        <h3>
          Needs {needs.length > 0 && <span className="muted">({needs.length})</span>}
        </h3>
        {!adding && candidates.length > 0 && (
          <Button type="button" size="small" variant="ghost" onClick={() => setAdding(true)}>
            <Plus aria-hidden="true" />
            Add prerequisite
          </Button>
        )}
      </div>
      {needs.map((n) => {
        const nNum = itemNum(n);
        return (
          <div key={n.id} className="pr-line need-line">
            <span className={`status ${n.status}`} title={STATUS_LABELS[n.status] || n.status} />
            <a {...itemLink(nNum)}>
              #{nNum} {n.title}
            </a>
            <span className={n.status === "paused" ? "chip status-chip-paused" : "chip"}>{STATUS_LABELS[n.status] || n.status}</span>
            <Button
              iconOnly
              variant="ghost"
              aria-label={`Remove prerequisite #${nNum}`}
              title="Remove prerequisite"
              onClick={() => run("DELETE", `${path}/needs`, { needs_item_id: nNum })}
            >
              <X aria-hidden="true" />
            </Button>
          </div>
        );
      })}
      {adding && (
        <div className="needs-picker">
          <div className="detail-form">
            <Input
              autoFocus
              aria-label="Search items to add as prerequisite"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Escape") {
                  e.preventDefault();
                  e.stopPropagation();
                  setQuery("");
                  setAdding(false);
                } else if (e.key === "Enter") {
                  e.preventDefault();
                  if (matches[0]) addNeed(matches[0]);
                }
              }}
              placeholder="Filter by #number or title…"
            />
            <Button
              type="button"
              variant="ghost"
              onClick={() => {
                setQuery("");
                setAdding(false);
              }}
            >
              Cancel
            </Button>
          </div>
          <div className="needs-picker-list" role="listbox" aria-label="Matching items">
            {matches.length === 0 ? (
              <p className="muted needs-picker-empty">No matching items.</p>
            ) : (
              matches.map((c) => (
                <button key={c.id} type="button" className="needs-picker-option" onClick={() => addNeed(c)}>
                  <span className={`status ${c.status}`} aria-hidden="true" />
                  <span className="item-id">#{itemNum(c)}</span>
                  <span className="needs-picker-title">{c.title}</span>
                  <span className={c.status === "paused" ? "chip status-chip-paused" : "chip"}>{STATUS_LABELS[c.status] || c.status}</span>
                </button>
              ))
            )}
          </div>
        </div>
      )}
    </section>
  );
}

function CloseForm({ path, run, onDone }) {
  const [note, setNote] = useState("");
  const close = async (e) => {
    e.preventDefault();
    if (await run("POST", `${path}/close`, { note })) onDone();
  };
  return (
    <form className="item-actions" onSubmit={close}>
      <Input
        autoFocus
        aria-label="Closing note"
        value={note}
        onChange={(e) => setNote(e.target.value)}
        placeholder="Closing note (GitHub PR URLs are linked to the item)"
      />
      <Button type="submit" variant="primary">
        Close item
      </Button>
      <Button type="button" variant="ghost" onClick={onDone}>
        Cancel
      </Button>
    </form>
  );
}

function PRList({ item, path, run }) {
  const [url, setUrl] = useState("");
  // Not `required`: after a successful submit clears the field, it would
  // show as invalid.
  const link = async (e) => {
    e.preventDefault();
    if (url.trim() && (await run("POST", `${path}/prs`, { url }))) setUrl("");
  };
  return (
    <section className="detail-section">
      <h3>Pull requests</h3>
      {item.prs.map((pr) => (
        <div key={pr.id} className="pr-line">
          <GitPullRequest aria-hidden="true" className="muted" />
          <a href={pr.url} target="_blank" rel="noopener noreferrer">
            {pr.url}
          </a>
          <Button
            iconOnly
            variant="ghost"
            aria-label={`Unlink ${pr.url}`}
            title="Unlink"
            onClick={() => run("DELETE", `${path}/prs`, { url: pr.url })}
          >
            <X aria-hidden="true" />
          </Button>
        </div>
      ))}
      <form className="detail-form" onSubmit={link}>
        <Input
          type="url"
          aria-label="Pull request URL"
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          placeholder="https://github.com/owner/repo/pull/123"
        />
        <Button type="submit">Link PR</Button>
      </form>
    </section>
  );
}

function ItemArtifacts({ item, items, artifacts, setArtifacts, workspaceId, token }) {
  const [uploading, setUploading] = useState(false);
  const linkedArtifacts = useMemo(() => artifacts.filter((a) => a.item_id === item.id), [artifacts, item.id]);

  return (
    <section className="detail-section">
      <div className="detail-section-head">
        <h3>
          Artifacts {linkedArtifacts.length > 0 && <span className="muted">({linkedArtifacts.length})</span>}
        </h3>
        <Button type="button" size="small" variant="ghost" onClick={() => setUploading(true)}>
          <Plus aria-hidden="true" />
          Upload artifact
        </Button>
      </div>
      {linkedArtifacts.map((art) => (
        <div key={art.id} className="pr-line">
          <span className="muted artifact-inline-icon">
            <ArtifactTypeIcon artifact={art} />
          </span>
          <a {...spaLink(art.url)}>
            {art.title && art.title !== art.name ? `${art.title} (${art.name})` : art.name}
          </a>
          <span className="chip">v{art.version}</span>
          <span className="muted">{formatArtifactSize(art.size)}</span>
        </div>
      ))}
      {uploading && (
        <UploadArtifactDialog
          workspaceId={workspaceId}
          token={token}
          items={items.length ? items : [item]}
          initialItemId={itemNum(item)}
          onClose={() => setUploading(false)}
          onUploaded={(created) => setArtifacts?.((list) => withArtifact(list, created))}
        />
      )}
    </section>
  );
}

function ItemTodos({ item, items, todos, setTodos, onReloadMe, participants, me, run, onNavigate }) {
  const [adding, setAdding] = useState(false);
  const humans = useMemo(() => participants.filter((p) => !p.sponsor && !p.revoked_at), [participants]);
  const linkedTodos = useMemo(() => todos.filter((t) => t.item_id === item.id), [todos, item.id]);

  if (linkedTodos.length === 0 && humans.length === 0) return null;

  const create = async (payload) => {
    const td = await run("POST", "/todos", payload);
    if (!td) return false;
    setTodos?.((list) => withTodo(list, td));
    onReloadMe?.();
    return true;
  };

  return (
    <section className="detail-section">
      <div className="detail-section-head">
        <h3>
          TODOs {linkedTodos.length > 0 && <span className="muted">({linkedTodos.length})</span>}
        </h3>
        {humans.length > 0 && (
          <Button type="button" size="small" variant="ghost" onClick={() => setAdding(true)}>
            <Plus aria-hidden="true" />
            Add TODO
          </Button>
        )}
      </div>
      {linkedTodos.map((td) => (
        <div key={td.id} className="pr-line">
          <CheckSquare aria-hidden="true" className="muted" />
          <a {...spaLink(`/w/${item.workspace_id}/todos`, onNavigate)}>
            #{td.id} {td.title}
          </a>
          <span className="chip">{td.assignee_emoji ? `${td.assignee_emoji} ${td.assignee_name}` : name(participants, td.assignee_id)}</span>
          {td.status === "done" && <span className="muted">done{td.done_note ? `: ${td.done_note}` : ""}</span>}
        </div>
      ))}
      {adding && (
        <NewTodoDialog
          initial={{ item_id: itemNum(item), pr_url: item.prs?.[0]?.url || "" }}
          humans={humans}
          items={items.length ? items : [item]}
          me={me}
          onClose={() => setAdding(false)}
          onCreate={create}
        />
      )}
    </section>
  );
}

function entryKey(m) {
  return m.pending ? m.id : m.legacy ? `legacy-${m.id}` : `msg-${m.id}`;
}

function compareEntries(a, b) {
  if (a.created_at !== b.created_at) return a.created_at - b.created_at;
  if (Boolean(a.legacy) !== Boolean(b.legacy)) return a.legacy ? -1 : 1;
  return Number(a.id) - Number(b.id);
}

// withEntry returns list (chronological) with entry m inserted or updated.
function withEntry(list, m) {
  const base = list || [];
  const k = entryKey(m);
  if (base.some((x) => entryKey(x) === k)) {
    return base.map((x) => (entryKey(x) === k ? m : x));
  }
  return [...base, m].sort(compareEntries);
}

// mergeAddedEntries keeps entries just returned by POST visible if an earlier
// in-flight GET finishes before the live-update refetch.
function mergeAddedEntries(fetched, addedMap) {
  let out = fetched;
  for (const [k, m] of addedMap) {
    if (out.some((x) => entryKey(x) === k)) addedMap.delete(k);
    else out = withEntry(out, m);
  }
  return out;
}

const ThreadEntryRow = memo(function ThreadEntryRow({
  entry: m,
  author,
  authorName,
  participants,
  me,
  canDelete,
  hasMixedLegacy,
  actionsOpen,
  onToggleActions,
  onReact,
  onDone,
  onDelete,
}) {
  const isChat = !m.legacy && !m.pending;
  const asksMe = Boolean(isChat && me && (m.action_for || []).includes(me.id));
  const needsMe = Boolean(asksMe && !(m.done_by || []).includes(me.id));
  return (
    <div
      className={[
        "comment",
        "thread-entry",
        m.pending && "comment-pending",
        needsMe && "needs-me",
        actionsOpen && "msg-actions-open",
      ]
        .filter(Boolean)
        .join(" ")}
      aria-busy={m.pending || undefined}
    >
      <div className="comment-head">
        <span className="who">{authorName}</span>
        {author?.sponsor && <span className="agent-of">· agent of {author.sponsor.display_name}</span>}
        <span className="time" title={m.pending ? undefined : new Date(m.created_at).toLocaleString()}>
          {m.pending ? "sending…" : ago(m.created_at)}
        </span>
        {isChat && (
          <a className="thread-entry-id" {...messageLink(m.id)} title="Show in chat">
            #{m.id}
          </a>
        )}
        {m.legacy && hasMixedLegacy && (
          <span className="item-thread-legacy" title="Historical comment from before unified threads">
            legacy
          </span>
        )}
        {m.legacy && canDelete && (
          <Button
            iconOnly
            variant="ghost"
            className="comment-delete"
            aria-label="Delete comment"
            title="Delete comment"
            onClick={() => onDelete(m)}
          >
            <X aria-hidden="true" />
          </Button>
        )}
      </div>
      <div className="comment-body">
        <MemoMarkdown text={m.body} />
      </div>
      {isChat && m.action_required && (
        <ActionLine message={m} participants={participants} me={me} asksMe={asksMe} needsMe={needsMe} onDone={(done) => onDone(m, done)} />
      )}
      {isChat && (
        <Reactions reactions={m.reactions || []} participants={participants} me={me} onReact={(emoji) => onReact(m, emoji)} />
      )}
      {isChat && (
        <>
          <div className="msg-actions" role="toolbar" aria-label={`Message #${m.id}`}>
            {REACTIONS.map(({ emoji, label }) => {
              const mine = m.reactions?.some((r) => r.emoji === emoji && r.by.includes(me?.id));
              return (
                <button
                  key={emoji}
                  type="button"
                  className="msg-action msg-emoji"
                  aria-label={`React ${label}`}
                  aria-pressed={Boolean(mine)}
                  title={label}
                  onClick={() => onReact(m, emoji)}
                >
                  {emoji}
                </button>
              );
            })}
            {canDelete && (
              <button
                type="button"
                className="msg-action"
                aria-label="Delete comment"
                title="Delete comment"
                onClick={() => onDelete(m)}
              >
                <Trash2 aria-hidden="true" />
              </button>
            )}
          </div>
          <button
            type="button"
            className="msg-more"
            aria-label="Message actions"
            aria-expanded={actionsOpen}
            onClick={() => onToggleActions(m.id)}
          >
            <Ellipsis aria-hidden="true" />
          </button>
        </>
      )}
    </div>
  );
});

// Isolated from ItemThread so typing in the box never re-renders the thread
// list or re-parses Markdown on every keystroke (#181).
function ItemThreadComposer({ onSend, mentionPrefill, onMentionConsumed }) {
  const [text, setText] = useState("");
  const [needsAction, setNeedsAction] = useState(false);
  const inputRef = useRef(null);
  const canAsk = /@\S/.test(text);

  useEffect(() => {
    if (!mentionPrefill) return;
    const prefix = `@${mentionPrefill.name} `;
    setText((t) => (t.startsWith(prefix) ? t : prefix + t));
    onMentionConsumed?.();
    requestAnimationFrame(() => {
      const el = inputRef.current;
      el?.focus();
      el?.setSelectionRange(el.value.length, el.value.length);
    });
  }, [mentionPrefill?.at]);

  const send = async (e) => {
    e.preventDefault();
    if (!text.trim()) return;
    const sent = text;
    const ask = needsAction && canAsk;
    setText("");
    setNeedsAction(false);
    const ok = await onSend(sent, ask);
    if (!ok) {
      setText((t) => t || sent);
      setNeedsAction(ask);
    }
  };
  return (
    <form className="item-thread-composer" onSubmit={send} onKeyDown={submitOnModEnter}>
      <MentionTextarea
        ref={inputRef}
        rows={2}
        aria-label="Comment"
        value={text}
        onChange={(e) => setText(e.target.value)}
        placeholder="Add to thread…  @Name to mention, paste/drop images, ⌘/Ctrl+Enter to send"
      />
      <div className="item-thread-composer-foot">
        {canAsk ? (
          <button
            type="button"
            className={needsAction ? "chip needs-action-toggle on" : "chip needs-action-toggle"}
            aria-pressed={needsAction}
            title="Ask the people you @mention to act: it stands out for them until they're done"
            onClick={() => {
              setNeedsAction(!needsAction);
              inputRef.current?.focus();
            }}
          >
            <CircleAlert aria-hidden="true" />
            Needs action
          </button>
        ) : (
          <span className="muted item-thread-hint">⌘/Ctrl+Enter to send · paste or drop images</span>
        )}
        <Button type="submit" variant="primary">
          Comment
        </Button>
      </div>
    </form>
  );
}

// ItemThread renders the item's unified conversation thread (#916): chat
// messages tied to the item (with @mentions, reactions, Needs action, and
// image attachments) alongside any historical legacy comments, oldest first.
function ItemThread({ item, path, messages = [], updateMessages, participants, me, call, run, onError }) {
  const [entries, setEntries] = useState(null);
  const [pending, setPending] = useState([]);
  const [removing, setRemoving] = useState([]);
  const [actionsFor, setActionsFor] = useState(null);
  const [mentionPrefill, setMentionPrefill] = useState(null);
  const added = useRef(new Map());
  const seq = useRef(0);

  // Refetch when the unified count changes (live updates reload the item).
  useEffect(() => {
    let current = true;
    call("GET", `${path}/comments`).then(
      (list) => current && setEntries(mergeAddedEntries(list, added.current)),
      () => current && setEntries((prev) => prev || []),
    );
    return () => {
      current = false;
    };
  }, [call, path, item.comment_count, item.message_count]);

  const participantById = useMemo(() => new Map(participants.map((p) => [p.id, p])), [participants]);

  // Overlay live workspace messages (reactions, action done state, newly
  // received item messages) so changes from Chat or other tabs appear at once.
  const merged = useMemo(() => {
    const base = entries || [];
    const liveById = new Map();
    const liveForItem = [];
    for (const m of messages) {
      liveById.set(m.id, m);
      if (m.item_id === item.id) liveForItem.push(m);
    }
    const seenMsgIds = new Set();
    const out = base.map((e) => {
      if (e.legacy) return e;
      seenMsgIds.add(e.id);
      return liveById.get(e.id) || e;
    });
    for (const m of liveForItem) {
      if (!seenMsgIds.has(m.id)) out.push(m);
    }
    return out.sort(compareEntries);
  }, [entries, messages, item.id]);

  const sendComment = useCallback(
    async (sent, actionRequired) => {
      const temp = {
        id: `pending-${++seq.current}`,
        pending: true,
        author_id: me?.id,
        body: sent,
        created_at: Date.now(),
        reactions: [],
      };
      setPending((list) => [...list, temp]);
      const payload = actionRequired ? { body: sent, action_required: true } : { body: sent };
      const m = await run("POST", `${path}/comments`, payload);
      setPending((list) => list.filter((x) => x !== temp));
      if (m) {
        added.current.set(entryKey(m), m);
        setEntries((list) => withEntry(list, m));
        updateMessages?.((list) => (list.some((x) => x.id === m.id) ? list : [...list, m].sort((a, b) => a.id - b.id)));
        return true;
      }
      return false;
    },
    [me?.id, path, run, updateMessages],
  );

  const react = useCallback(
    async (m, emoji) => {
      const mine = m.reactions?.some((r) => r.emoji === emoji && r.by.includes(me?.id));
      const apply = (on) => {
        const patch = (x) => (x.id === m.id && !x.legacy ? { ...x, reactions: withReaction(x.reactions || [], emoji, me?.id, on) } : x);
        setEntries((list) => list?.map(patch) ?? null);
        updateMessages?.((list) => list.map((x) => (x.id === m.id ? { ...x, reactions: withReaction(x.reactions || [], emoji, me?.id, on) } : x)));
      };
      apply(!mine);
      try {
        await call(mine ? "DELETE" : "PUT", `/messages/${m.id}/reactions/${encodeURIComponent(emoji)}`);
      } catch (err) {
        apply(mine);
        onError?.(err.message);
      }
    },
    [call, me?.id, onError, updateMessages],
  );

  const setDone = useCallback(
    async (m, done) => {
      const apply = (on) => {
        const patch = (x) => (x.id === m.id && !x.legacy ? { ...x, done_by: withDone(x, me?.id, on) } : x);
        setEntries((list) => list?.map(patch) ?? null);
        updateMessages?.((list) => list.map((x) => (x.id === m.id ? { ...x, done_by: withDone(x, me?.id, on) } : x)));
      };
      apply(done);
      try {
        await call(done ? "POST" : "DELETE", `/messages/${m.id}/done`);
      } catch (err) {
        apply(!done);
        onError?.(err.message);
      }
    },
    [call, me?.id, onError, updateMessages],
  );

  const remove = useCallback(
    async (m) => {
      if (!window.confirm("Delete this comment?")) return;
      const k = entryKey(m);
      setRemoving((keys) => [...keys, k]);
      const suffix = m.legacy ? `${path}/comments/${m.id}?legacy=1` : `${path}/comments/${m.id}`;
      const ok = await run("DELETE", suffix);
      if (ok) {
        added.current.delete(k);
        setEntries((list) => list?.filter((x) => entryKey(x) !== k) ?? null);
        if (!m.legacy) {
          updateMessages?.((list) => list.filter((x) => x.id !== m.id));
        }
      }
      setRemoving((keys) => keys.filter((x) => x !== k));
    },
    [path, run, updateMessages],
  );

  const toggleActions = useCallback((id) => {
    setActionsFor((cur) => (cur === id ? null : id));
  }, []);

  const insertMention = useCallback((who) => {
    setMentionPrefill({ name: who, at: Date.now() });
  }, []);

  const confirmed = merged.filter((m) => !removing.includes(entryKey(m)));
  const shown = pending.length ? [...confirmed, ...pending] : confirmed;
  const rootMessage = confirmed.find((m) => !m.legacy && !m.parent_id) || confirmed.find((m) => !m.legacy) || null;
  const hasMixedLegacy = confirmed.some((m) => m.legacy) && confirmed.some((m) => !m.legacy);

  return (
    <MentionProvider onMentionClick={insertMention}>
      <section className="detail-section item-thread">
        <div className="item-thread-head">
          <h3>
            Thread {shown.length > 0 && <span className="muted">({shown.length})</span>}
          </h3>
          {rootMessage && (
            <a className="item-thread-open-chat" {...messageLink(rootMessage.id)} title="Open this thread in Chat">
              <MessagesSquare aria-hidden="true" />
              Open in chat
            </a>
          )}
        </div>
        {entries !== null && shown.length === 0 && <p className="muted">No comments yet.</p>}
        <div className="item-thread-list">
          {shown.map((m) => (
            <ThreadEntryRow
              key={entryKey(m)}
              entry={m}
              author={participantById.get(m.author_id)}
              authorName={name(participants, m.author_id)}
              participants={participants}
              me={me}
              canDelete={!m.pending && me?.id === m.author_id}
              hasMixedLegacy={hasMixedLegacy}
              actionsOpen={actionsFor === m.id}
              onToggleActions={toggleActions}
              onReact={react}
              onDone={setDone}
              onDelete={remove}
            />
          ))}
        </div>
        <ItemThreadComposer
          onSend={sendComment}
          mentionPrefill={mentionPrefill}
          onMentionConsumed={() => setMentionPrefill(null)}
        />
      </section>
    </MentionProvider>
  );
}

const CommentRow = memo(function CommentRow({ comment: c, authorName, canDelete, onDelete }) {
  return (
    <div className={c.pending ? "comment comment-pending" : "comment"} aria-busy={c.pending || undefined}>
      <div className="comment-head">
        <span className="who">{authorName}</span>
        <span className="time" title={c.pending ? undefined : new Date(c.created_at).toLocaleString()}>
          {c.pending ? "sending…" : ago(c.created_at)}
        </span>
        {canDelete && (
          <Button
            iconOnly
            variant="ghost"
            className="comment-delete"
            aria-label="Delete comment"
            title="Delete comment"
            onClick={() => onDelete(c)}
          >
            <X aria-hidden="true" />
          </Button>
        )}
      </div>
      <div className="comment-body">
        <MemoMarkdown text={c.body} />
      </div>
    </div>
  );
});

function CommentComposer({ onSend }) {
  const [text, setText] = useState("");
  const send = async (e) => {
    e.preventDefault();
    if (!text.trim()) return;
    const sent = text;
    setText("");
    const ok = await onSend(sent);
    if (!ok) setText((t) => t || sent);
  };
  return (
    <form className="detail-form comment-form" onSubmit={send} onKeyDown={submitOnModEnter}>
      <MentionTextarea
        rows={2}
        aria-label="Comment"
        value={text}
        onChange={(e) => setText(e.target.value)}
        placeholder="Add a comment…  @Name to mention, ⌘/Ctrl+Enter to send"
      />
      <Button type="submit">Comment</Button>
    </form>
  );
}

// Comments lists and edits comments on a non-chat-threaded resource such as an
// idea's discussion (#917).
export function Comments({ item, path, participants, me, call, run, title = "Comments" }) {
  const [comments, setComments] = useState(null);
  const [pending, setPending] = useState([]);
  const [removing, setRemoving] = useState([]);
  const added = useRef(new Map());
  const seq = useRef(0);

  useEffect(() => {
    let current = true;
    call("GET", `${path}/comments`).then(
      (list) => {
        if (!current) return;
        let out = list;
        for (const [id, c] of added.current) {
          if (out.some((x) => x.id === id)) added.current.delete(id);
          else out = [...out, c].sort((a, b) => a.id - b.id);
        }
        setComments(out);
      },
      () => current && setComments((prev) => prev || []),
    );
    return () => {
      current = false;
    };
  }, [call, path, item.comment_count]);

  const sendComment = useCallback(
    async (sent) => {
      const temp = {
        id: `pending-${++seq.current}`,
        pending: true,
        author_id: me?.id,
        body: sent,
        created_at: Date.now(),
      };
      setPending((list) => [...list, temp]);
      const c = await run("POST", `${path}/comments`, { body: sent });
      setPending((list) => list.filter((x) => x !== temp));
      if (c) {
        added.current.set(c.id, c);
        setComments((list) => {
          const base = list || [];
          return base.some((x) => x.id === c.id) ? base : [...base, c].sort((a, b) => a.id - b.id);
        });
        return true;
      }
      return false;
    },
    [me?.id, path, run],
  );

  const remove = useCallback(
    async (c) => {
      if (!window.confirm("Delete this comment?")) return;
      setRemoving((ids) => [...ids, c.id]);
      const ok = await run("DELETE", `${path}/comments/${c.id}`);
      if (ok) {
        added.current.delete(c.id);
        setComments((list) => list?.filter((x) => x.id !== c.id) ?? null);
      }
      setRemoving((ids) => ids.filter((id) => id !== c.id));
    },
    [path, run],
  );

  const confirmed = comments ? comments.filter((c) => !removing.includes(c.id)) : [];
  const shown = pending.length ? [...confirmed, ...pending] : confirmed;

  return (
    <section className="detail-section">
      <h3>{title}</h3>
      {comments !== null && shown.length === 0 && <p className="muted">No comments yet.</p>}
      {shown.map((c) => (
        <CommentRow
          key={c.id}
          comment={c}
          authorName={name(participants, c.author_id)}
          canDelete={!c.pending && me?.id === c.author_id}
          onDelete={remove}
        />
      ))}
      <CommentComposer onSend={sendComment} />
    </section>
  );
}
