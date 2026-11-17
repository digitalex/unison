import { useEffect, useId, useLayoutEffect, useRef, useState } from "react";
import { Smile } from "lucide-react";
import Button from "./Button.jsx";
import { Input } from "./Input.jsx";
import { searchEmoji } from "./emoji.js";

// Structure of the panel, in px: its width and tallest height, the least
// room it wants below the field before opening upwards instead, and its
// distance from the field and from the window's edges.
const WIDTH = 320;
const MAX_HEIGHT = 360;
const MIN_ROOM = 240;
const GAP = 4;
const MARGIN = 12;

// EmojiPicker (#305) is a labelled emoji field: a text input, where any
// emoji can still be typed or pasted, with a button that opens a panel to
// pick one from. The panel has a search box (it filters by label and
// keywords; Enter takes the first match, ArrowDown moves into the grid)
// over a categorized grid walked with the arrow keys.
//
// label is the field's visible label; value and onChange(emoji) hold the
// text; other props (required, placeholder, …) go to the input.
//
// The panel is position: fixed and, where the browser has the Popover API,
// shown in the top layer, so a dialog's scrolling body doesn't clip it.
export default function EmojiPicker({ label, value, onChange, ...inputProps }) {
  const id = useId();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [place, setPlace] = useState(null);
  const anchor = useRef(null);
  const panel = useRef(null);
  const search = useRef(null);
  const groups = searchEmoji(query);
  const first = groups[0]?.emoji[0];

  const close = (refocus) => {
    setOpen(false);
    setQuery("");
    setPlace(null);
    if (refocus) anchor.current.querySelector(".emoji-trigger").focus();
  };
  const pick = (emoji) => {
    onChange(emoji);
    close(true);
  };

  // Keep the panel under the field (or over it, when there is little room
  // below), inside the window, for as long as it is open.
  useLayoutEffect(() => {
    if (!open) return;
    const position = () => {
      const r = anchor.current.getBoundingClientRect();
      const vw = document.documentElement.clientWidth;
      const vh = window.innerHeight;
      const width = Math.min(WIDTH, vw - 2 * MARGIN);
      const left = Math.max(MARGIN, Math.min(r.right - width, vw - width - MARGIN));
      const below = vh - r.bottom - GAP - MARGIN;
      const above = r.top - GAP - MARGIN;
      const up = below < MIN_ROOM && above > below;
      setPlace({
        left,
        width,
        maxHeight: Math.min(MAX_HEIGHT, up ? above : below),
        ...(up ? { bottom: vh - r.top + GAP } : { top: r.bottom + GAP }),
      });
    };
    position();
    // Scrolling the grid itself doesn't move the field.
    const onScroll = (e) => {
      if (!panel.current?.contains(e.target)) position();
    };
    window.addEventListener("resize", position);
    window.addEventListener("scroll", onScroll, true);
    return () => {
      window.removeEventListener("resize", position);
      window.removeEventListener("scroll", onScroll, true);
    };
  }, [open]);

  // Once it has a place it can be seen, and so focused: start in the search.
  const placed = Boolean(place);
  useEffect(() => {
    if (placed) search.current.focus();
  }, [placed]);

  useEffect(() => {
    if (!open) return;
    panel.current.showPopover?.();
    const onPointer = (e) => {
      if (!anchor.current.contains(e.target)) close(false);
    };
    document.addEventListener("pointerdown", onPointer);
    return () => document.removeEventListener("pointerdown", onPointer);
  }, [open]);

  const options = () => [...panel.current.querySelectorAll(".emoji-option")];
  const onSearchKey = (e) => {
    if (e.key === "Enter") {
      // Never the form around the field: Enter here takes the first match.
      e.preventDefault();
      if (first) pick(first.emoji);
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      options()[0]?.focus();
    }
  };
  const onGridKey = (e) => {
    if (!["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown", "Home", "End"].includes(e.key)) return;
    const all = options();
    const i = all.indexOf(document.activeElement);
    if (i < 0) return;
    e.preventDefault();
    if (e.key === "ArrowLeft") return all[i - 1]?.focus();
    if (e.key === "ArrowRight") return all[i + 1]?.focus();
    if (e.key === "Home") return all[0].focus();
    if (e.key === "End") return all[all.length - 1].focus();
    // Up and down go to the nearest row that way (across categories), to
    // the emoji closest to this column; up from the top row is the search.
    const down = e.key === "ArrowDown";
    const here = all[i].getBoundingClientRect();
    const beyond = all.map((el) => ({ el, r: el.getBoundingClientRect() })).filter(({ r }) => (down ? r.top > here.top + 1 : r.top < here.top - 1));
    if (beyond.length === 0) {
      if (!down) search.current.focus();
      return;
    }
    const tops = beyond.map(({ r }) => r.top);
    const row = down ? Math.min(...tops) : Math.max(...tops);
    beyond
      .filter(({ r }) => Math.abs(r.top - row) < 1)
      .sort((a, b) => Math.abs(a.r.left - here.left) - Math.abs(b.r.left - here.left))[0]
      .el.focus();
  };

  return (
    <div className="field">
      <label className="field-label" htmlFor={id}>
        {label}
      </label>
      <div
        className="emoji-picker"
        ref={anchor}
        onKeyDown={(e) => {
          if (!open || e.key !== "Escape") return;
          // Escape closes the panel only, not a dialog the field is in.
          e.preventDefault();
          e.stopPropagation();
          close(true);
        }}
        onBlur={(e) => {
          // Tabbing out of the picker closes the panel.
          if (open && e.relatedTarget && !anchor.current.contains(e.relatedTarget)) close(false);
        }}
      >
        <Input id={id} autoComplete="off" {...inputProps} className="emoji-input" value={value} onChange={(e) => onChange(e.target.value)} />
        <Button
          type="button"
          variant="ghost"
          size="small"
          iconOnly
          className="emoji-trigger"
          aria-label="Choose an emoji"
          title="Choose an emoji"
          aria-haspopup="dialog"
          aria-expanded={open}
          onClick={() => (open ? close(false) : setOpen(true))}
        >
          <Smile aria-hidden="true" />
        </Button>
        {open && (
          <div ref={panel} className="popover emoji-panel" popover="manual" role="dialog" aria-label="Choose an emoji" style={place || { visibility: "hidden" }}>
            <Input
              ref={search}
              type="search"
              className="emoji-search"
              placeholder="Search emoji"
              aria-label="Search emoji"
              autoComplete="off"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={onSearchKey}
            />
            <div className="emoji-groups" onKeyDown={onGridKey}>
              {groups.map((group) => (
                <EmojiGroup key={group.title} group={group} value={value} first={first} onPick={pick} />
              ))}
              {groups.length === 0 && <p className="field-description">No match. Any emoji can be typed or pasted into the field.</p>}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

// One category: its title over its emoji. Only the first emoji of the
// whole grid is a tab stop; the arrow keys move between the rest.
function EmojiGroup({ group, value, first, onPick }) {
  const titleId = useId();
  return (
    <div role="group" aria-labelledby={titleId}>
      <h3 id={titleId} className="emoji-group-title">
        {group.title}
      </h3>
      <div className="emoji-grid">
        {group.emoji.map((item) => (
          <button
            key={item.emoji}
            type="button"
            className="emoji-option"
            tabIndex={item === first ? 0 : -1}
            title={item.label}
            aria-label={item.label}
            aria-current={item.emoji === value ? "true" : undefined}
            onClick={() => onPick(item.emoji)}
          >
            {item.emoji}
          </button>
        ))}
      </div>
    </div>
  );
}
