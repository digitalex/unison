import { useEffect, useRef, useState } from "react";

// A trigger button with a small panel below it. Escape or a click outside
// closes the panel; `children` receives a `close` callback.
export default function Popover({ trigger, title, className = "button button-ghost", children }) {
  const [open, setOpen] = useState(false);
  const anchor = useRef(null);
  const button = useRef(null);

  useEffect(() => {
    if (!open) return;
    const onPointer = (e) => {
      if (!anchor.current.contains(e.target)) setOpen(false);
    };
    const onKey = (e) => {
      if (e.key !== "Escape") return;
      setOpen(false);
      button.current.focus();
    };
    document.addEventListener("pointerdown", onPointer);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("pointerdown", onPointer);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  return (
    <div className="popover-anchor" ref={anchor}>
      <button
        ref={button}
        type="button"
        className={className}
        title={title}
        aria-expanded={open}
        aria-haspopup="dialog"
        onClick={() => setOpen(!open)}
      >
        {trigger}
      </button>
      {open && (
        <div className="popover" role="dialog" aria-label={title}>
          {children(() => setOpen(false))}
        </div>
      )}
    </div>
  );
}
