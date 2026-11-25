import { useEffect } from "react";
import Dialog from "./ui/Dialog.jsx";

export function isEditableTarget(target) {
  if (!target || typeof target.closest !== "function") return false;
  if (target.isContentEditable) return true;
  return Boolean(target.closest('input, textarea, select, [contenteditable="true"]'));
}

// shouldIgnoreShortcut suppresses single-key shortcuts inside inputs, IME
// composition, modifier chords, key repeat, open overlays, and active drags (#314).
export function shouldIgnoreShortcut(e) {
  if (e.defaultPrevented || e.repeat || e.isComposing || e.keyCode === 229) return true;
  if (e.metaKey || e.ctrlKey || e.altKey) return true;
  if (isEditableTarget(e.target)) return true;
  if (document.querySelector('[role="dialog"], .popover, [role="menu"], .tour-popover')) return true;
  if (document.querySelector('.dragging, [data-dragging="true"]') || document.documentElement.classList.contains("resizing")) return true;
  return false;
}

export function useKeyShortcut(match, onTrigger) {
  useEffect(() => {
    const onKey = (e) => {
      if (shouldIgnoreShortcut(e) || !match(e)) return;
      e.preventDefault();
      onTrigger(e);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [match, onTrigger]);
}

const SHORTCUTS = [
  { keys: ["1"], label: "Go to Backlog" },
  { keys: ["2"], label: "Go to Recurring" },
  { keys: ["3"], label: "Go to Chat" },
  { keys: ["4"], label: "Go to TODOs" },
  { keys: ["5"], label: "Go to Artifacts" },
  { keys: ["6"], label: "Go to Ideas" },
  { keys: ["c"], label: "New item (Backlog), recurring task (Recurring), TODO (TODOs), upload artifact (Artifacts), or idea (Ideas)" },
  { keys: ["?"], label: "Show or close keyboard shortcuts" },
  { keys: ["Esc"], label: "Close open dialog or thread" },
];

export function ShortcutsDialog({ onClose }) {
  return (
    <Dialog title="Keyboard shortcuts" onClose={onClose}>
      <dl className="shortcuts-list">
        {SHORTCUTS.map(({ keys, label }) => (
          <div key={label} className="shortcut-row">
            <dt className="shortcut-keys">
              {keys.map((k) => (
                <kbd key={k} className="kbd">
                  {k}
                </kbd>
              ))}
            </dt>
            <dd>{label}</dd>
          </div>
        ))}
      </dl>
    </Dialog>
  );
}
