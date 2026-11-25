import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { ChevronDown, ChevronUp } from "lucide-react";

// MessageFold (#280) collapses very long chat messages while keeping shorter
// ones untouched. Hysteresis (threshold 380px -> folded 260px) prevents
// folding a message just to hide a couple of trailing lines.
//
// A single module-level `selectionchange` listener is attached on `document`
// ONLY while at least one folded message is mounted, and automatically expands
// a folded message when a non-collapsed selection (drag-select or Find in
// Page) lands inside it.

export const FOLD_THRESHOLD = 380;
export const FOLD_HEIGHT = 260;

const activeFolds = new Set();
let selectionListenerAttached = false;

export function handleFoldSelectionChange(
  doc = typeof document !== "undefined" ? document : null,
  win = typeof window !== "undefined" ? window : null,
) {
  if (!doc || !win || typeof win.getSelection !== "function" || activeFolds.size === 0) return;
  const sel = win.getSelection();
  if (!sel || sel.isCollapsed) return;
  const anchor = sel.anchorNode;
  const focus = sel.focusNode;
  for (const entry of [...activeFolds]) {
    const el = entry.getElement();
    if (!el) continue;
    if ((anchor && el.contains(anchor)) || (focus && el.contains(focus))) {
      entry.expand();
    }
  }
}

function onDocumentSelectionChange() {
  handleFoldSelectionChange();
}

export function registerActiveFold(entry, doc = typeof document !== "undefined" ? document : null) {
  activeFolds.add(entry);
  if (!selectionListenerAttached && doc && typeof doc.addEventListener === "function") {
    doc.addEventListener("selectionchange", onDocumentSelectionChange);
    selectionListenerAttached = true;
  }
  return () => {
    activeFolds.delete(entry);
    if (activeFolds.size === 0 && selectionListenerAttached && doc && typeof doc.removeEventListener === "function") {
      doc.removeEventListener("selectionchange", onDocumentSelectionChange);
      selectionListenerAttached = false;
    }
  };
}

export function activeFoldCount() {
  return activeFolds.size;
}

export function isSelectionListenerAttached() {
  return selectionListenerAttached;
}

function countLineBreaks(s) {
  let n = 0;
  for (let i = 0; i < s.length; i++) {
    if (s[i] === "\n") n++;
  }
  return n;
}

export function canSkipFoldMeasure(text) {
  if (text === undefined || text === null) return false;
  const raw = String(text);
  return (
    raw.length < 280 &&
    countLineBreaks(raw) < 7 &&
    !raw.includes("![") &&
    !raw.includes("$$") &&
    !raw.includes("\\begin{")
  );
}

const useIsomorphicLayoutEffect = typeof window !== "undefined" ? useLayoutEffect : useEffect;

export default function MessageFold({ text, forceExpand = false, children }) {
  const clipRef = useRef(null);
  const contentRef = useRef(null);
  const [foldable, setFoldable] = useState(false);
  const [expanded, setExpanded] = useState(() => Boolean(forceExpand));
  const skipMeasure = canSkipFoldMeasure(text);

  useEffect(() => {
    if (forceExpand) setExpanded(true);
  }, [forceExpand]);

  useIsomorphicLayoutEffect(() => {
    if (skipMeasure) {
      setFoldable(false);
      return undefined;
    }
    const el = contentRef.current;
    if (!el) return undefined;
    const measure = () => {
      const height = el.scrollHeight || el.getBoundingClientRect().height || 0;
      setFoldable(height > FOLD_THRESHOLD);
    };
    measure();
    if (typeof ResizeObserver === "undefined") return undefined;
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, [skipMeasure, text]);

  const isExpanded = expanded || Boolean(forceExpand);
  const collapsed = foldable && !isExpanded;

  useEffect(() => {
    if (!collapsed) return undefined;
    return registerActiveFold({
      getElement: () => clipRef.current,
      expand: () => setExpanded(true),
    });
  }, [collapsed]);

  if (skipMeasure) {
    return children;
  }

  return (
    <div className={collapsed ? "msg-fold is-collapsed" : "msg-fold"}>
      <div
        ref={clipRef}
        className={collapsed ? "msg-fold-clip msg-fold-collapsed" : "msg-fold-clip"}
        style={collapsed ? { maxHeight: `${FOLD_HEIGHT}px` } : undefined}
      >
        <div ref={contentRef} className="msg-fold-content">
          {children}
        </div>
      </div>
      {foldable && (
        <button
          type="button"
          className="msg-fold-toggle"
          aria-expanded={isExpanded}
          onClick={() => setExpanded(!isExpanded)}
        >
          {isExpanded ? (
            <>
              <ChevronUp aria-hidden="true" />
              Show less
            </>
          ) : (
            <>
              <ChevronDown aria-hidden="true" />
              Show more
            </>
          )}
        </button>
      )}
    </div>
  );
}
