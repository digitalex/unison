import { useEffect, useState } from "react";

const STORAGE_KEY = "unison.chatWidth";
export const CHAT_WIDTH_DEFAULT = 360;
const CHAT_WIDTH_MIN = 280;
const CHAT_WIDTH_MAX = 800;

// The chat pane's width on wide screens: remembered per browser, and kept
// within bounds that leave the backlog usable at the current window width.
export function useChatWidth() {
  const [saved, setSaved] = useState(() => Number(localStorage.getItem(STORAGE_KEY)) || CHAT_WIDTH_DEFAULT);
  const [viewport, setViewport] = useState(window.innerWidth);
  useEffect(() => {
    const onResize = () => setViewport(window.innerWidth);
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);
  const max = Math.max(CHAT_WIDTH_MIN, Math.min(CHAT_WIDTH_MAX, Math.round(viewport * 0.6)));
  const clamp = (w) => Math.min(max, Math.max(CHAT_WIDTH_MIN, Math.round(w)));
  const setWidth = (w) => {
    const next = clamp(w);
    localStorage.setItem(STORAGE_KEY, String(next));
    setSaved(next);
  };
  return { width: clamp(saved), min: CHAT_WIDTH_MIN, max, setWidth };
}

// A vertical divider between the backlog and the chat. Drag it, use the
// arrow keys when it is focused, or double-click to reset.
export default function Splitter({ width, min, max, onChange }) {
  const [dragging, setDragging] = useState(false);

  const onPointerDown = (e) => {
    if (e.button !== 0) return;
    e.preventDefault();
    e.currentTarget.setPointerCapture(e.pointerId);
    const startX = e.clientX;
    const startWidth = width;
    setDragging(true);
    const move = (ev) => onChange(startWidth - (ev.clientX - startX));
    const up = () => {
      setDragging(false);
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  };

  const onKeyDown = (e) => {
    const step = e.shiftKey ? 64 : 16;
    const moves = { ArrowLeft: width + step, ArrowRight: width - step, Home: max, End: min };
    if (!(e.key in moves)) return;
    e.preventDefault();
    onChange(moves[e.key]);
  };

  return (
    <div
      className={dragging ? "splitter dragging" : "splitter"}
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize chat"
      aria-valuenow={width}
      aria-valuemin={min}
      aria-valuemax={max}
      tabIndex={0}
      title="Drag to resize chat · double-click to reset"
      onPointerDown={onPointerDown}
      onKeyDown={onKeyDown}
      onDoubleClick={() => onChange(CHAT_WIDTH_DEFAULT)}
    />
  );
}
