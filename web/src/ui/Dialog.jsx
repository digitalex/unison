import { useEffect, useId, useRef } from "react";
import { X } from "lucide-react";
import Button from "./Button.jsx";

// Unison Dialog: a modal on the native <dialog> (showModal), so the page
// behind is inert (focus stays inside), Escape closes it, and it sits in the
// top layer. Mount it to open, unmount to close: onClose asks the owner to
// unmount it. A click on the backdrop closes too; focus returns to whatever
// opened it; the page behind doesn't scroll. Full-screen on phones.
//
// Mark the field to focus first with data-autofocus. title is the heading (the dialog's accessible name); actions go in the
// head next to the close button; children are the body.
export default function Dialog({ title, onClose, head, className = "", children }) {
  const ref = useRef(null);
  const titleId = useId();
  const pressedOutside = useRef(false);
  const close = useRef(onClose);
  close.current = onClose;

  useEffect(() => {
    const dialog = ref.current;
    const opener = document.activeElement;
    dialog.showModal();
    // showModal focuses the first focusable element (the close button);
    // a field marked data-autofocus takes focus instead.
    dialog.querySelector("[data-autofocus]")?.focus();
    document.documentElement.classList.add("dialog-open");
    return () => {
      dialog.close();
      if (!document.querySelector("dialog[open]")) document.documentElement.classList.remove("dialog-open");
      if (opener instanceof HTMLElement && opener.isConnected) opener.focus();
    };
  }, []);

  // A press that starts and ends outside the panel (on the backdrop) closes;
  // selecting text from inside and releasing outside doesn't.
  const outside = (e) => {
    const r = ref.current.getBoundingClientRect();
    return e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom;
  };
  return (
    <dialog
      ref={ref}
      className={`dialog ${className}`.trim()}
      aria-labelledby={titleId}
      onCancel={(e) => {
        e.preventDefault();
        close.current();
      }}
      onPointerDown={(e) => {
        pressedOutside.current = e.target === ref.current && outside(e);
      }}
      onClick={(e) => {
        if (pressedOutside.current && e.target === ref.current && outside(e)) close.current();
        pressedOutside.current = false;
      }}
    >
      <header className="dialog-head">
        <h2 id={titleId} className="dialog-title">
          {title}
        </h2>
        {head}
        <Button iconOnly variant="ghost" aria-label="Close" title="Close (Esc)" onClick={() => close.current()}>
          <X aria-hidden="true" />
        </Button>
      </header>
      <div className="dialog-body">{children}</div>
    </dialog>
  );
}
