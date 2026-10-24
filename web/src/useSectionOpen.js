import { useState } from "react";

// Whether a collapsible section (metrics, backlog, chat) is open. The choice
// is remembered per browser so the layout stays the way the person left it.
export default function useSectionOpen(key, defaultOpen) {
  const storageKey = `unison.section.${key}`;
  const [open, setOpenState] = useState(() => {
    const saved = localStorage.getItem(storageKey);
    return saved === null ? defaultOpen : saved === "1";
  });
  const setOpen = (next) => {
    localStorage.setItem(storageKey, next ? "1" : "0");
    setOpenState(next);
  };
  return [open, setOpen];
}
