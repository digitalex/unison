import { createContext, useContext, useEffect, useMemo, useRef, useState } from "react";
import { itemNum } from "./backlogFormat.js";
import { ImageTextarea } from "./images.jsx";

const EMPTY_ITEMS = [];
const EMPTY_ITEMS_MAP = new Map();

function buildItemsByNumber(items) {
  if (!items || items.length === 0) return EMPTY_ITEMS_MAP;
  const map = new Map();
  for (const it of items) {
    if (!it) continue;
    if (it.id > 0 && !map.has(it.id)) map.set(it.id, it);
    const num = itemNum(it);
    if (num > 0) map.set(num, it);
  }
  return map;
}

const MentionContext = createContext({
  participants: [],
  items: EMPTY_ITEMS,
  itemsByNumber: EMPTY_ITEMS_MAP,
  me: null,
  onMentionClick: null,
});

export function MentionProvider({ participants, items, me, onMentionClick, children }) {
  const parent = useContext(MentionContext);
  const resolvedItems = items ?? parent.items;
  const itemsByNumber = useMemo(
    () => (items === undefined ? parent.itemsByNumber : buildItemsByNumber(items)),
    [items, parent.itemsByNumber],
  );
  const value = useMemo(
    () => ({
      participants: participants ?? parent.participants,
      items: resolvedItems,
      itemsByNumber,
      me: me !== undefined ? me : parent.me,
      onMentionClick: onMentionClick !== undefined ? onMentionClick : parent.onMentionClick,
    }),
    [
      participants,
      resolvedItems,
      itemsByNumber,
      me,
      onMentionClick,
      parent.participants,
      parent.me,
      parent.onMentionClick,
    ],
  );
  return <MentionContext.Provider value={value}>{children}</MentionContext.Provider>;
}

export function useMentions() {
  return useContext(MentionContext);
}

const WORD_CHAR = /[\p{L}\p{N}_]/u;
const ALNUM_CHAR = /[\p{L}\p{N}]/u;
const DISALLOWED_BEFORE_AT = /[\p{L}\p{N}_./:@-]/u;
const DISALLOWED_BEFORE_HASH = /[\p{L}\p{N}_./:@&#-]/u;
const MAX_ITEM_QUERY_LEN = 32;

// refersToPR says whether the text before a "#N" makes it a pull request
// ("PR #82", "pull request #82"), not a backlog item.
export function refersToPR(before) {
  return /\b(?:PRs?|pull(?: request)?s?)\s*$/i.test(before);
}

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

function countSpaces(s) {
  let n = 0;
  for (let i = 0; i < s.length; i++) {
    if (s[i] === " ") n++;
  }
  return n;
}

function isValidItemQuery(query) {
  if (query.length > MAX_ITEM_QUERY_LEN) return false;
  if (/[\r\n\t#@/`]/.test(query)) return false;
  if (query === "") return true;
  if (/^\s/.test(query) || /^\d+\s/.test(query)) return false;
  if (!/\s/.test(query)) return true;
  if (/^\d/.test(query) || query.endsWith("  ")) return false;
  return countSpaces(query) <= 2;
}

// findItemQuery detects an active "#query" backlog item reference before cursor,
// ignoring headings, PR references, URL fragments, entities, and code spans/blocks.
export function findItemQuery(text, cursor) {
  if (cursor == null || cursor < 0 || cursor > text.length) return null;
  const minPos = Math.max(0, cursor - (MAX_ITEM_QUERY_LEN + 1));
  for (let i = cursor - 1; i >= minPos; i--) {
    const ch = text[i];
    if (ch === "\n" || ch === "\r") return null;
    if (ch === "#") {
      if (i > 0 && DISALLOWED_BEFORE_HASH.test(text[i - 1])) return null;
      const query = text.slice(i + 1, cursor);
      if (!isValidItemQuery(query)) return null;
      if (refersToPR(text.slice(Math.max(0, i - 16), i))) return null;
      if (isInsideFencedCode(text, i) || isInsideInlineCode(text, i)) return null;
      return { start: i, end: cursor, query };
    }
  }
  return null;
}

export function findAutocompleteQuery(text, cursor) {
  const mention = findMentionQuery(text, cursor);
  const item = findItemQuery(text, cursor);
  if (mention && item) {
    return mention.start >= item.start
      ? { kind: "mention", ...mention }
      : { kind: "item", ...item };
  }
  if (mention) return { kind: "mention", ...mention };
  if (item) return { kind: "item", ...item };
  return null;
}

const BROADCASTS = [
  { kind: "mention", id: "broadcast-all", name: "all", emoji: "📢", hint: "Notify everyone", broadcast: true },
  { kind: "mention", id: "broadcast-everyone", name: "everyone", emoji: "📢", hint: "Notify everyone", broadcast: true },
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
        kind: "mention",
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

function insertTopK(top, candidate, limit, compare) {
  if (limit <= 0) return;
  if (top.length === limit && compare(candidate, top[limit - 1]) >= 0) {
    return;
  }
  let pos = top.length;
  while (pos > 0 && compare(candidate, top[pos - 1]) < 0) {
    pos--;
  }
  if (top.length < limit) {
    top.splice(pos, 0, candidate);
  } else {
    for (let j = limit - 1; j > pos; j--) {
      top[j] = top[j - 1];
    }
    top[pos] = candidate;
  }
}

function hasWordPrefix(titleLower, prefix) {
  let idx = titleLower.indexOf(prefix);
  while (idx !== -1) {
    if (idx === 0 || !ALNUM_CHAR.test(titleLower[idx - 1])) return true;
    idx = titleLower.indexOf(prefix, idx + 1);
  }
  return false;
}

function matchItemTier(q, tokens, numStr, titleLower, status) {
  if (!q) {
    if (status === "in_progress") return 0;
    return status !== "done" ? 1 : 2;
  }
  for (let i = 0; i < tokens.length; i++) {
    const tok = tokens[i];
    if (!numStr.includes(tok) && !titleLower.includes(tok)) return -1;
  }
  if (numStr === q) return 0;
  if (numStr.startsWith(q)) return 1;
  if (titleLower.startsWith(q)) return 2;
  if (hasWordPrefix(titleLower, tokens[0])) return 3;
  return 4;
}

function makeItemCandidateComparator(emptyQuery) {
  return (a, b) => {
    if (a.tier !== b.tier) return a.tier - b.tier;
    const aDone = a.it.status === "done" ? 1 : 0;
    const bDone = b.it.status === "done" ? 1 : 0;
    if (aDone !== bDone) return aDone - bDone;
    if (aDone === 0) {
      const posDiff = (a.it.position ?? 0) - (b.it.position ?? 0);
      if (posDiff !== 0) return posDiff;
      return emptyQuery
        ? (a.it.id || 0) - (b.it.id || 0) || a.num - b.num
        : a.num - b.num || (a.it.id || 0) - (b.it.id || 0);
    }
    const closedDiff = (b.it.closed_at || 0) - (a.it.closed_at || 0);
    if (closedDiff !== 0) return closedDiff;
    return b.num - a.num;
  };
}

function toItemSuggestion({ it, num }) {
  return {
    kind: "item",
    id: `item-${it.id}`,
    number: num,
    title: it.title || "",
    status: it.status || "open",
    sectionName: it.section_name || "",
  };
}

export function rankItemSuggestions(query, items = EMPTY_ITEMS, limit = 8) {
  if (!items || items.length === 0 || limit <= 0) return EMPTY_SUGGESTIONS;
  const q = query.trim().toLowerCase();
  const tokens = q ? q.split(/\s+/).filter(Boolean) : EMPTY_ITEMS;
  const compare = makeItemCandidateComparator(q === "");
  const top = [];
  for (let i = 0; i < items.length; i++) {
    const it = items[i];
    if (!it) continue;
    const num = it.number > 0 ? it.number : it.id;
    if (!num) continue;
    const numStr = q ? String(num) : "";
    const titleLower = q ? (it.title || "").toLowerCase() : "";
    const tier = matchItemTier(q, tokens, numStr, titleLower, it.status);
    if (tier === -1) continue;
    insertTopK(top, { it, num, tier }, limit, compare);
  }
  return top.map(toItemSuggestion);
}

const EMPTY_SUGGESTIONS = [];

export function useMentionAutocomplete({ value, onChange, textareaRef, participants, items = EMPTY_ITEMS }) {
  const [cursor, setCursor] = useState(null);
  const [activeIndex, setActiveIndex] = useState(0);
  const [dismissedKey, setDismissedKey] = useState(null);

  const match = useMemo(() => findAutocompleteQuery(value || "", cursor), [value, cursor]);
  const suggestions = useMemo(() => {
    if (!match) return EMPTY_SUGGESTIONS;
    return match.kind === "item"
      ? rankItemSuggestions(match.query, items)
      : rankMentionSuggestions(match.query, participants);
  }, [match?.kind, match?.query, participants, items]);
  const matchKey = match ? `${match.kind}:${match.start}` : null;
  const open = Boolean(match && suggestions.length > 0 && dismissedKey !== matchKey);

  useEffect(() => {
    setActiveIndex(0);
  }, [match?.kind, match?.start, match?.query]);

  useEffect(() => {
    if (!match) setDismissedKey(null);
  }, [Boolean(match)]);

  const syncCursor = (el) => {
    if (!el) return;
    if (typeof el.value === "string" && !el.value.includes("@") && !el.value.includes("#")) return;
    setCursor(el.selectionStart ?? null);
  };

  const accept = (suggestion) => {
    if (!match || !suggestion) return;
    const cur = value || "";
    const inserted =
      suggestion.kind === "item" || match.kind === "item"
        ? `#${suggestion.number} `
        : `@${suggestion.name} `;
    const next = cur.slice(0, match.start) + inserted + cur.slice(match.end);
    const nextPos = match.start + inserted.length;
    onChange({ target: { value: next } });
    setCursor(nextPos);
    setDismissedKey(null);
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
      setDismissedKey(matchKey);
      return true;
    }
    return false;
  };

  return {
    open,
    kind: match?.kind || "mention",
    suggestions,
    activeIndex: Math.min(activeIndex, Math.max(0, suggestions.length - 1)),
    syncCursor,
    accept,
    handleKeyDown,
  };
}

function ItemOptionContent({ item }) {
  const hint = item.sectionName || (item.status === "done" ? "done" : "");
  return (
    <>
      <span className={`status ${item.status}`} aria-hidden="true" />
      <span className="mention-option-num">#{item.number}</span>
      <span className="mention-option-title">{item.title}</span>
      {hint && <span className="mention-option-hint">{hint}</span>}
    </>
  );
}

function MentionOptionContent({ item }) {
  return (
    <>
      {item.emoji && (
        <span className="mention-option-emoji" aria-hidden="true">
          {item.emoji}
        </span>
      )}
      <span className="mention-option-name">@{item.name}</span>
      {item.hint && <span className="mention-option-hint">{item.hint}</span>}
    </>
  );
}

export function MentionTextarea({ value, onChange, onKeyDown, onClick, onKeyUp, onSelect, ref: externalRef, ...props }) {
  const { participants, items } = useMentions();
  const internalRef = useRef(null);
  const menuRef = useRef(null);
  const textareaRef = externalRef || internalRef;
  const { open, kind, suggestions, activeIndex, syncCursor, accept, handleKeyDown } = useMentionAutocomplete({
    value,
    onChange,
    textareaRef,
    participants,
    items,
  });

  useEffect(() => {
    if (!open) return;
    const activeEl = menuRef.current?.querySelector(".mention-option.active");
    activeEl?.scrollIntoView?.({ block: "nearest" });
  }, [open, activeIndex]);

  return (
    <div className="mention-field">
      {open && (
        <div
          ref={menuRef}
          className="mention-menu"
          role="listbox"
          aria-label={kind === "item" ? "Reference a backlog item" : "Mention a participant"}
        >
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
              {item.kind === "item" ? <ItemOptionContent item={item} /> : <MentionOptionContent item={item} />}
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
