import { useEffect, useId, useRef, useState } from "react";
import { Check, ChevronsUpDown, Plus } from "lucide-react";
import { participantName } from "./Account.jsx";
import { ApiError, api } from "./api.js";
import Button from "./ui/Button.jsx";
import Dialog from "./ui/Dialog.jsx";
import Field from "./ui/Field.jsx";
import { Input } from "./ui/Input.jsx";

// WorkspaceSwitcher is the top bar's workspace control: the current
// workspace's name, opening a menu of your workspaces (fetched on open, so
// new memberships show up) and "New workspace". Arrow keys, Home and End
// move through the menu; Escape or a click outside closes it.
export default function WorkspaceSwitcher({ workspace, account, navigate }) {
  const [open, setOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const anchor = useRef(null);
  const button = useRef(null);
  const menuId = useId();
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
  return (
    <div className="popover-anchor switcher" ref={anchor}>
      <button
        ref={button}
        type="button"
        className="button button-ghost switcher-trigger"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? menuId : undefined}
        title="Switch workspace"
        onClick={() => setOpen(!open)}
        onKeyDown={opener}
      >
        <span className="switcher-name">{workspace.name}</span>
        <ChevronsUpDown aria-hidden="true" />
      </button>
      {open && (
        <SwitcherMenu
          id={menuId}
          current={workspace}
          signedIn={Boolean(account.user)}
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

function SwitcherMenu({ id, current, signedIn, onPick, onCreate, onClose }) {
  const [list, setList] = useState(null); // null while loading
  const menu = useRef(null);
  useEffect(() => {
    if (!signedIn) return setList([]);
    api("GET", "/api/workspaces").then(
      (all) => setList([...all].sort((a, b) => a.name.localeCompare(b.name))),
      () => setList([]),
    );
  }, []);
  // Focus the current workspace (or the first item) once the list is in.
  useEffect(() => {
    const items = [...menu.current.querySelectorAll('[role^="menuitem"]')];
    (items.find((el) => el.getAttribute("aria-checked") === "true") || items[0])?.focus();
  }, [list]);

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
// account (with the name and emoji given here), then opens it.
function NewWorkspaceDialog({ account, navigate, onClose }) {
  const [form, setForm] = useState({ name: "", repo_url: "", canvas_url: "", me: participantName(account.user), emoji: "" });
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const field = (key) => ({ value: form[key], onChange: (e) => setForm({ ...form, [key]: e.target.value }) });
  const create = async (e) => {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      const ws = await api("POST", "/api/workspaces", { body: { name: form.name, repo_url: form.repo_url, canvas_url: form.canvas_url } });
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
        <Field label="Isocan canvas URL (optional)">
          <Input type="url" placeholder="https://isocan.io/p/…" {...field("canvas_url")} />
        </Field>
        <div className="new-workspace-me">
          <Field label="Your name in it">
            <Input placeholder="one word" {...field("me")} />
          </Field>
          <Field label="Emoji">
            <Input placeholder="🦊" {...field("emoji")} />
          </Field>
        </div>
        <p className="field-description">You join it right away; leave either blank to be assigned one.</p>
        <div className="actions-row dialog-actions">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={busy}>
            Create workspace
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
