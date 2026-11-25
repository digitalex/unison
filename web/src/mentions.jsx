import { createContext, useContext, useEffect, useMemo, useRef, useState } from "react";
import { ImageTextarea } from "./images.jsx";

const MentionContext = createContext({ participants: [], me: null, onMentionClick: null });

export function MentionProvider({ participants, me, onMentionClick, children }) {
  const parent = useContext(MentionContext);
  const value = useMemo(
    () => ({
      participants: participants ?? parent.participants,
      me: me !== undefined ? me : parent.me,
      onMentionClick: onMentionClick !== undefined ? onMentionClick : parent.onMentionClick,
    }),
    [participants, me, onMentionClick, parent.participants, parent.me, parent.onMentionClick],
  );
  return <MentionContext.Provider value={value}>{children}</MentionContext.Provider>;
}

export function useMentions() {
  return useContext(MentionContext);
}

const WORD_CHAR = /[\p{L}\p{N}_]/u;
const DISALLOWED_BEFORE_AT = /[\p{L}\p{N}_./:@-]/u;

export function participantTooltip(p) {
  const parts = [p.name];
  if (p.model) parts[0] += ` (${p.model})`;
  if (p.sponsor) parts.push(`agent of ${p.sponsor.display_name}`);
  if (p.status) return `${parts.join(" · ")} — ${p.status}`;
  return parts.join(" · ");
}

const sortedParticipantsCache = new WeakMap();

function sortedParticipants(participants) {
  let sorted = sortedParticipantsCache.get(participants);
  if (!sorted) {
    sorted = [...participants].sort((a, b) => (b.name?.length || 0) - (a.name?.length || 0));
    sortedParticipantsCache.set(participants, sorted);
  }
  return sorted;
}

// matchMention checks whether s[index] starts a workspace @mention or @all/@everyone broadcast.
export function matchMention(s, index, participants = []) {
  if (s[index] !== "@" || (index > 0 && WORD_CHAR.test(s[index - 1]))) return null;
  const tail = s.slice(index + 1);
  const sorted = sortedParticipants(participants);
  for (const p of sorted) {
    if (!p.name) continue;
    if (tail.slice(0, p.name.length).toLowerCase() === p.name.toLowerCase()) {
      const after = tail[p.name.length];
      if (after === undefined || !WORD_CHAR.test(after)) {
        return { kind: "participant", participant: p, length: p.name.length };
      }
    }
  }
  const broadcast = tail.match(/^(everyone|all)(?![\p{L}\p{N}_])/iu);
  if (broadcast) {
    return { kind: "broadcast", name: broadcast[1].toLowerCase(), length: broadcast[1].length };
  }
  return null;
}

function isInsideFencedCode(text, pos) {
  const lines = text.slice(0, pos).split("\n");
  let fence = null;
  for (let i = 0; i < lines.length - 1; i++) {
    const line = lines[i];
    if (!fence) {
      const m = line.match(/^ {0,3}(`{3,}|~{3,})(.*)$/);
      if (m && !(m[1][0] === "`" && m[2].includes("`"))) {
        fence = { char: m[1][0], len: m[1].length };
      }
    } else {
      const trimmed = line.replace(/^ {0,3}/, "").replace(/[ \t]+$/, "");
      if (trimmed.length >= fence.len && [...trimmed].every((c) => c === fence.char)) {
        fence = null;
      }
    }
  }
  return Boolean(fence);
}

function isInsideInlineCode(text, pos) {
  const lineStart = text.lastIndexOf("\n", pos - 1) + 1;
  const before = text.slice(lineStart, pos);
  let open = false;
  let openLen = 0;
  for (let i = 0; i < before.length; ) {
    if (before[i] === "\\" && i + 1 < before.length) {
      i += 2;
      continue;
    }
    if (before[i] === "`") {
      let n = 1;
      while (before[i + n] === "`") n++;
      if (!open) {
        open = true;
        openLen = n;
      } else if (n === openLen) {
        open = false;
        openLen = 0;
      }
      i += n;
      continue;
    }
    i++;
  }
  return open;
}

// findMentionQuery detects an active "@query" before cursor, ignoring emails,
// URLs, inline code spans, and fenced code blocks.
export function findMentionQuery(text, cursor) {
  if (cursor == null || cursor < 0 || cursor > text.length) return null;
  let i = cursor - 1;
  while (i >= 0 && !/\s/.test(text[i])) {
    if (text[i] === "@") {
      if (i > 0 && DISALLOWED_BEFORE_AT.test(text[i - 1])) return null;
      const query = text.slice(i + 1, cursor);
      if (query.includes("@") || query.includes("/")) return null;
      if (isInsideFencedCode(text, i) || isInsideInlineCode(text, i)) return null;
      return { start: i, end: cursor, query };
    }
    i--;
  }
  return null;
}

const BROADCASTS = [
  { id: "broadcast-all", name: "all", emoji: "📢", hint: "Notify everyone", broadcast: true },
  { id: "broadcast-everyone", name: "everyone", emoji: "📢", hint: "Notify everyone", broadcast: true },
];

export function rankMentionSuggestions(query, participants = []) {
  const q = query.trim().toLowerCase();
  const scored = [];
  for (const p of participants) {
    if (!p.name) continue;
    const name = p.name.toLowerCase();
    const sponsor = (p.sponsor?.display_name || "").toLowerCase();
    let tier = -1;
    if (!q || name.startsWith(q)) tier = 0;
    else if (name.includes(q)) tier = 1;
    else if (sponsor.includes(q)) tier = 2;
    if (tier !== -1) {
      scored.push({
        id: `p-${p.id}`,
        name: p.name,
        emoji: p.emoji || "",
        hint: p.sponsor ? `agent of ${p.sponsor.display_name}` : p.status || "",
        tier,
      });
    }
  }
  for (const b of BROADCASTS) {
    if (!q) {
      scored.push({ ...b, tier: 3 });
    } else if (b.name.startsWith(q)) {
      scored.push({ ...b, tier: 0 });
    } else if (b.name.includes(q)) {
      scored.push({ ...b, tier: 1 });
    }
  }
  scored.sort((a, b) => a.tier - b.tier || a.name.localeCompare(b.name));
  return scored.slice(0, 8);
}

const EMPTY_SUGGESTIONS = [];

export function useMentionAutocomplete({ value, onChange, textareaRef, participants }) {
  const [cursor, setCursor] = useState(null);
  const [activeIndex, setActiveIndex] = useState(0);
  const [dismissedAt, setDismissedAt] = useState(null);

  const match = useMemo(() => findMentionQuery(value || "", cursor), [value, cursor]);
  const suggestions = useMemo(
    () => (match ? rankMentionSuggestions(match.query, participants) : EMPTY_SUGGESTIONS),
    [match?.query, participants],
  );
  const open = Boolean(match && suggestions.length > 0 && dismissedAt !== match.start);

  useEffect(() => {
    setActiveIndex(0);
  }, [match?.start, match?.query]);

  useEffect(() => {
    if (!match) setDismissedAt(null);
  }, [Boolean(match)]);

  const syncCursor = (el) => {
    if (!el) return;
    if (typeof el.value === "string" && !el.value.includes("@")) return;
    setCursor(el.selectionStart ?? null);
  };

  const accept = (item) => {
    if (!match || !item) return;
    const cur = value || "";
    const inserted = `@${item.name} `;
    const next = cur.slice(0, match.start) + inserted + cur.slice(match.end);
    const nextPos = match.start + inserted.length;
    onChange({ target: { value: next } });
    setCursor(nextPos);
    setDismissedAt(null);
    requestAnimationFrame(() => {
      const el = textareaRef.current;
      if (!el) return;
      el.focus();
      el.setSelectionRange(nextPos, nextPos);
    });
  };

  const handleKeyDown = (e) => {
    if (!open) return false;
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActiveIndex((i) => (i + 1) % suggestions.length);
      return true;
    }
    if (e.key === "ArrowUp") {
      e.preventDefault();
      setActiveIndex((i) => (i - 1 + suggestions.length) % suggestions.length);
      return true;
    }
    if ((e.key === "Enter" && !e.shiftKey) || e.key === "Tab") {
      e.preventDefault();
      e.stopPropagation();
      e.nativeEvent?.stopImmediatePropagation?.();
      accept(suggestions[Math.min(activeIndex, suggestions.length - 1)]);
      return true;
    }
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      e.nativeEvent?.stopImmediatePropagation?.();
      setDismissedAt(match.start);
      return true;
    }
    return false;
  };

  return {
    open,
    suggestions,
    activeIndex: Math.min(activeIndex, Math.max(0, suggestions.length - 1)),
    syncCursor,
    accept,
    handleKeyDown,
  };
}

export function MentionTextarea({ value, onChange, onKeyDown, onClick, onKeyUp, onSelect, ref: externalRef, ...props }) {
  const { participants } = useMentions();
  const internalRef = useRef(null);
  const textareaRef = externalRef || internalRef;
  const { open, suggestions, activeIndex, syncCursor, accept, handleKeyDown } = useMentionAutocomplete({
    value,
    onChange,
    textareaRef,
    participants,
  });

  return (
    <div className="mention-field">
      {open && (
        <div className="mention-menu" role="listbox" aria-label="Mention a participant">
          {suggestions.map((item, i) => (
            <button
              key={item.id}
              type="button"
              role="option"
              aria-selected={i === activeIndex}
              className={i === activeIndex ? "mention-option active" : "mention-option"}
              onMouseDown={(e) => {
                e.preventDefault();
                accept(item);
              }}
            >
              {item.emoji && (
                <span className="mention-option-emoji" aria-hidden="true">
                  {item.emoji}
                </span>
              )}
              <span className="mention-option-name">@{item.name}</span>
              {item.hint && <span className="mention-option-hint">{item.hint}</span>}
            </button>
          ))}
        </div>
      )}
      <ImageTextarea
        {...props}
        ref={textareaRef}
        value={value}
        onChange={(e) => {
          onChange(e);
          syncCursor(e.target?.selectionStart !== undefined ? e.target : textareaRef.current);
        }}
        onKeyDown={(e) => {
          if (handleKeyDown(e)) return;
          onKeyDown?.(e);
        }}
        onClick={(e) => {
          syncCursor(e.currentTarget);
          onClick?.(e);
        }}
        onKeyUp={(e) => {
          syncCursor(e.currentTarget);
          onKeyUp?.(e);
        }}
        onSelect={(e) => {
          syncCursor(e.currentTarget);
          onSelect?.(e);
        }}
      />
    </div>
  );
}
