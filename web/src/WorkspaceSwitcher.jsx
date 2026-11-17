import { useEffect, useId, useRef, useState } from "react";
import { Check, ChevronsUpDown, Plus } from "lucide-react";
import { participantName } from "./Account.jsx";
import { hasUnseen, markSeen, useSeen } from "./activity.js";
import { ApiError, api } from "./api.js";
import { createCanvas } from "./isocan.js";
import Button from "./ui/Button.jsx";
import Checkbox from "./ui/Checkbox.jsx";
import Dialog from "./ui/Dialog.jsx";
import EmojiPicker from "./ui/EmojiPicker.jsx";
import Field from "./ui/Field.jsx";
import { Input } from "./ui/Input.jsx";

// How often the list of workspaces is fetched again while the page shows.
const POLL_MS = 30000;

// WorkspaceSwitcher is the sidebar's workspace control: the current
// workspace's name, opening a menu of your workspaces and "New workspace".
// Arrow keys, Home and End move through the menu; Escape or a click outside
// closes it.
//
// It also says when another of your workspaces has something new (#300): a
// dot on the control, and one on each such workspace in the menu. "New" is
// a workspace's activity_seq (its latest event by somebody else) being past
// what this account has seen of it (activity.js). Being in a workspace
// counts as seeing it.
export default function WorkspaceSwitcher({ workspace, account, navigate }) {
  const [open, setOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const anchor = useRef(null);
  const button = useRef(null);
  const menuId = useId();
  const signedIn = Boolean(account.user);
  const userId = account.user?.id;
  const [list, reload] = useWorkspaceList(signedIn);
  const seen = useSeen(userId);
  // Opening the menu fetches again, so new memberships show up.
  useEffect(() => {
    if (open) reload();
  }, [open]);
  useEffect(() => {
    for (const ws of list || []) markSeen(userId, ws.id, ws.activity_seq || 0, { baseline: ws.id !== workspace.id });
  }, [list, workspace.id, userId]);
  const unseen = (list || []).filter((ws) => ws.id !== workspace.id && hasUnseen(ws, seen));

  const close = (refocus = true) => {
    setOpen(false);
    if (refocus) button.current?.focus();
  };
  useDismiss(open, anchor, close);
  const opener = (e) => {
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      setOpen(true);
    }
  };
  const news = unseen.length > 0 ? `New activity in ${unseen.map((ws) => ws.name).join(", ")}` : "";
  return (
    <div className="popover-anchor switcher" ref={anchor}>
      <button
        ref={button}
        type="button"
        className="button button-ghost switcher-trigger"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? menuId : undefined}
        title={news ? `Switch workspace · ${news}` : "Switch workspace"}
        onClick={() => setOpen(!open)}
        onKeyDown={opener}
      >
        <span className="switcher-name">{workspace.name}</span>
        {news && <span className="switcher-activity" role="img" aria-label={news} />}
        <ChevronsUpDown aria-hidden="true" />
      </button>
      {open && (
        <SwitcherMenu
          id={menuId}
          current={workspace}
          list={signedIn ? list : []}
          unseen={unseen}
          signedIn={signedIn}
          onPick={(ws) => {
            close(false);
            if (ws.id !== workspace.id) navigate(`/w/${ws.id}`);
          }}
          onCreate={() => {
            // Focus the trigger first: the dialog returns focus to it.
            close();
            setCreating(true);
          }}
          onClose={close}
        />
      )}
      {creating && <NewWorkspaceDialog account={account} navigate={navigate} onClose={() => setCreating(false)} />}
    </div>
  );
}

// useWorkspaceList is your workspaces, sorted by name (null until the first
// answer; [] if it can't be had), and a function that fetches them again.
// It fetches when the switcher appears, every POLL_MS after that, and when
// the page comes back into view or focus; never while the page is hidden.
function useWorkspaceList(signedIn) {
  const [list, setList] = useState(null);
  const load = useRef(() => {});
  useEffect(() => {
    if (!signedIn) return;
    let gone = false;
    load.current = () => {
      if (document.visibilityState !== "visible") return;
      api("GET", "/api/workspaces").then(
        (all) => gone || setList([...all].sort((a, b) => a.name.localeCompare(b.name))),
        () => gone || setList((old) => old || []),
      );
    };
    const again = () => load.current();
    again();
    const timer = setInterval(again, POLL_MS);
    document.addEventListener("visibilitychange", again);
    window.addEventListener("focus", again);
    return () => {
      gone = true;
      load.current = () => {};
      clearInterval(timer);
      document.removeEventListener("visibilitychange", again);
      window.removeEventListener("focus", again);
    };
  }, [signedIn]);
  return [list, () => load.current()];
}

// useDismiss closes the menu on a press outside it.
function useDismiss(open, anchor, close) {
  useEffect(() => {
    if (!open) return;
    const onPointer = (e) => {
      if (!anchor.current.contains(e.target)) close(false);
    };
    document.addEventListener("pointerdown", onPointer);
    return () => document.removeEventListener("pointerdown", onPointer);
  }, [open]);
}

// SwitcherMenu lists the workspaces (list: null while loading) with the
// current one checked and the ones in unseen marked.
function SwitcherMenu({ id, current, list, unseen, signedIn, onPick, onCreate, onClose }) {
  const menu = useRef(null);
  // Focus the current workspace (or the first item) once the list is in;
  // not again when a later fetch replaces it, which would move the focus
  // under someone walking the menu.
  const loaded = list !== null;
  useEffect(() => {
    const items = [...menu.current.querySelectorAll('[role^="menuitem"]')];
    (items.find((el) => el.getAttribute("aria-checked") === "true") || items[0])?.focus();
  }, [loaded]);

  const onKeyDown = (e) => {
    const items = [...menu.current.querySelectorAll('[role^="menuitem"]')];
    const i = items.indexOf(document.activeElement);
    const go = (n) => {
      e.preventDefault();
      items[(n + items.length) % items.length]?.focus();
    };
    if (e.key === "ArrowDown") go(i + 1);
    else if (e.key === "ArrowUp") go(i - 1);
    else if (e.key === "Home") go(0);
    else if (e.key === "End") go(items.length - 1);
    else if (e.key === "Escape") {
      e.preventDefault();
      onClose();
    } else if (e.key === "Tab") onClose(false);
  };
  const workspaces = list && list.length > 0 ? list : [current];
  return (
    <div className="popover popover-start switcher-menu" role="menu" id={id} aria-label="Workspaces" ref={menu} onKeyDown={onKeyDown}>
      <div className="switcher-heading" role="presentation">
        Your workspaces
      </div>
      {list === null && <p className="muted switcher-loading">Loading…</p>}
      {list !== null &&
        workspaces.map((ws) => (
          <button key={ws.id} type="button" role="menuitemradio" aria-checked={ws.id === current.id} className="switcher-item" tabIndex={-1} onClick={() => onPick(ws)}>
            <Check aria-hidden="true" className="switcher-check" />
            <span className="switcher-item-text">
              <span className="switcher-item-name">{ws.name}</span>
              {mainRepo(ws) && <span className="switcher-item-repo">{mainRepo(ws).replace(/^https?:\/\//, "")}</span>}
            </span>
            {unseen.some((u) => u.id === ws.id) && <span className="switcher-activity" role="img" aria-label="New activity" />}
          </button>
        ))}
      {signedIn && (
        <>
          <div className="switcher-sep" role="separator" />
          <button type="button" role="menuitem" className="switcher-item switcher-new" tabIndex={-1} onClick={onCreate}>
            <Plus aria-hidden="true" />
            New workspace
          </button>
        </>
      )}
    </div>
  );
}

function mainRepo(ws) {
  return ws.repos?.[0]?.url || ws.repo_url || "";
}

// NewWorkspaceDialog creates a workspace and joins it as the signed-in
// account (with the name and emoji given here), then opens it. Its canvas is
// a URL you paste, or with "Create one" a new isocan canvas named after the
// workspace, made in an isocan popup when you submit (#297).
function NewWorkspaceDialog({ account, navigate, onClose }) {
  const [form, setForm] = useState({ name: "", repo_url: "", canvas_url: "", me: participantName(account.user), emoji: "" });
  const [newCanvas, setNewCanvas] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const field = (key) => ({ value: form[key], onChange: (e) => setForm({ ...form, [key]: e.target.value }) });
  const create = async (e) => {
    e.preventDefault();
    // Before any await: the popup must open while this is still the submit.
    const canvas = newCanvas ? createCanvas(form.name) : Promise.resolve(form.canvas_url);
    setError("");
    setBusy(true);
    try {
      const ws = await api("POST", "/api/workspaces", { body: { name: form.name, repo_url: form.repo_url, canvas_url: await canvas } });
      // Creating made you a member; joining gives you a participant there.
      // If it fails, the workspace page offers the join form instead.
      await api("POST", `/api/workspaces/${ws.id}/join`, { body: { name: form.me, emoji: form.emoji } }).catch(() => {});
      onClose();
      navigate(`/w/${ws.id}`);
    } catch (err) {
      setBusy(false);
      setError(createError(err));
    }
  };
  return (
    <Dialog title="New workspace" onClose={onClose} className="new-workspace-dialog">
      <form className="form" onSubmit={create}>
        {error && (
          <p role="alert" className="field-error">
            {error}
          </p>
        )}
        <Field label="Name">
          <Input required data-autofocus placeholder="Unison" {...field("name")} />
        </Field>
        <Field label="Repository URL (optional)">
          <Input type="url" placeholder="https://github.com/owner/repo" {...field("repo_url")} />
        </Field>
        <div className="new-workspace-canvas">
          <Field label="Isocan canvas URL (optional)">
            <Input
              type="url"
              placeholder="https://isocan.io/p/…"
              value={form.canvas_url}
              onChange={(e) => {
                setNewCanvas(false);
                setForm({ ...form, canvas_url: e.target.value });
              }}
            />
          </Field>
          <Checkbox
            checked={newCanvas}
            onChange={(checked) => {
              setNewCanvas(checked);
              if (checked) setForm({ ...form, canvas_url: "" });
            }}
          >
            Create one
          </Checkbox>
        </div>
        <div className="new-workspace-me">
          <Field label="Your name in it">
            <Input placeholder="one word" {...field("me")} />
          </Field>
          <EmojiPicker label="Emoji" placeholder="🦊" value={form.emoji} onChange={(emoji) => setForm({ ...form, emoji })} />
        </div>
        <p className="field-description">You join it right away; leave either blank to be assigned one.</p>
        <div className="actions-row dialog-actions">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={busy}>
            {busy && newCanvas ? "Waiting for isocan…" : "Create workspace"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function createError(err) {
  // UNISON_CREATORS limits creation to listed accounts.
  if (err instanceof ApiError && err.status === 403) {
    return "This server only lets approved accounts create workspaces. Ask whoever runs it to add your email.";
  }
  if (err instanceof ApiError && err.status === 401) return "Sign in to create a workspace.";
  return err.message;
}
