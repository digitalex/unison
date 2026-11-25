import { useCallback, useState } from "react";
import { ArrowUpDown, ChevronDown, ChevronRight, Layers, ListTodo } from "lucide-react";
import { STATUS_LABELS } from "./backlogFormat.js";
import { SectionIcon } from "./settings/Sections.jsx";

export const SORT_KEY = "unison.backlog.sort";
export const GROUP_KEY = "unison.backlog.group";
export const COLLAPSED_KEY = "unison.backlog.collapsed";

export const SORT_OPTIONS = [
  { id: "priority", label: "Priority" },
  { id: "newest", label: "Newest" },
  { id: "oldest", label: "Oldest" },
  { id: "updated", label: "Updated" },
  { id: "title", label: "Title" },
];

export const GROUP_OPTIONS = [
  { id: "status", label: "Group: Status" },
  { id: "section", label: "Group: Section" },
];

const VALID_SORTS = new Set(SORT_OPTIONS.map((opt) => opt.id));
const VALID_GROUPS = new Set(GROUP_OPTIONS.map((opt) => opt.id));

function comparePriority(a, b) {
  const aDone = a.status === "done" ? 1 : 0;
  const bDone = b.status === "done" ? 1 : 0;
  if (aDone !== bDone) return aDone - bDone;
  if (aDone) return (b.closed_at || 0) - (a.closed_at || 0) || b.id - a.id;
  return (a.position || 0) - (b.position || 0) || a.id - b.id;
}

function compareNewest(a, b) {
  return (b.created_at || 0) - (a.created_at || 0) || b.id - a.id;
}

function compareOldest(a, b) {
  return (a.created_at || 0) - (b.created_at || 0) || a.id - b.id;
}

function compareUpdated(a, b) {
  return (b.updated_at || 0) - (a.updated_at || 0) || b.id - a.id;
}

function compareTitle(a, b) {
  const at = (a.title || "").toLowerCase();
  const bt = (b.title || "").toLowerCase();
  if (at !== bt) return at < bt ? -1 : 1;
  return (a.position || 0) - (b.position || 0) || a.id - b.id;
}

function comparatorForSort(sortBy) {
  switch (sortBy) {
    case "newest":
      return compareNewest;
    case "oldest":
      return compareOldest;
    case "updated":
      return compareUpdated;
    case "title":
      return compareTitle;
    default:
      return comparePriority;
  }
}

export function sortItems(list, sortBy = "priority") {
  return [...list].sort(comparatorForSort(sortBy));
}

export function formatStatusLabel(status) {
  if (STATUS_LABELS[status]) return STATUS_LABELS[status];
  if (!status) return "";
  const spaced = String(status).replace(/_/g, " ");
  return spaced.charAt(0).toUpperCase() + spaced.slice(1);
}

function extractExtraStatuses(queued) {
  const seen = new Set();
  for (const it of queued) {
    if (it.status && it.status !== "in_progress" && it.status !== "open" && it.status !== "done") {
      seen.add(it.status);
    }
  }
  return [...seen].sort((a, b) => {
    if (a === "paused") return -1;
    if (b === "paused") return 1;
    return a < b ? -1 : a > b ? 1 : 0;
  });
}

export function buildBacklogStatusView(items, sortBy = "priority") {
  const queued = sortItems(
    items.filter((it) => it.status !== "done"),
    sortBy,
  );
  const extraStatuses = extractExtraStatuses(queued);
  const groups = {
    in_progress: queued.filter((it) => it.status === "in_progress"),
    open: queued.filter((it) => it.status === "open"),
  };
  for (const st of extraStatuses) {
    groups[st] = queued.filter((it) => it.status === st);
  }
  const done = sortItems(
    items.filter((it) => it.status === "done"),
    sortBy,
  );
  return { queued, groups, extraStatuses, done };
}

export function buildBacklogSectionView(sourceItems, sections, sortBy = "priority") {
  const activeAll = sourceItems.filter((it) => it.status !== "done");
  const knownIds = new Set(sections.map((sec) => sec.id));
  const sectionGroups = [
    {
      key: "section:backlog",
      sectionId: 0,
      label: "Backlog",
      icon: null,
      isBacklog: true,
      notify: false,
      items: sortItems(
        activeAll.filter((it) => !it.section_id || !knownIds.has(it.section_id)),
        sortBy,
      ),
    },
    ...sections.map((sec) => ({
      key: `section:${sec.id}`,
      sectionId: sec.id,
      label: sec.name,
      icon: sec.icon,
      isBacklog: false,
      notify: Boolean(sec.notify),
      items: sortItems(
        activeAll.filter((it) => it.section_id === sec.id),
        sortBy,
      ),
    })),
  ];
  const done = sortItems(
    sourceItems.filter((it) => it.status === "done"),
    sortBy,
  );
  return { activeCount: activeAll.length, sectionGroups, done };
}

function readStoredChoice(key, validSet, fallback) {
  try {
    const raw = localStorage.getItem(key);
    return raw && validSet.has(raw) ? raw : fallback;
  } catch {
    return fallback;
  }
}

function writeStoredValue(key, value) {
  try {
    localStorage.setItem(key, value);
  } catch {
    // Storage blocked: the choice lasts until reload.
  }
}

export function useBacklogSort() {
  const [sort, setSort] = useState(() => readStoredChoice(SORT_KEY, VALID_SORTS, "priority"));
  const choose = useCallback((next) => {
    const normalized = VALID_SORTS.has(next) ? next : "priority";
    setSort(normalized);
    writeStoredValue(SORT_KEY, normalized);
  }, []);
  return [sort, choose];
}

export function useBacklogGroup() {
  const [groupBy, setGroupBy] = useState(() => readStoredChoice(GROUP_KEY, VALID_GROUPS, "status"));
  const choose = useCallback((next) => {
    const normalized = VALID_GROUPS.has(next) ? next : "status";
    setGroupBy(normalized);
    writeStoredValue(GROUP_KEY, normalized);
  }, []);
  return [groupBy, choose];
}

function readCollapsedMap() {
  try {
    const raw = localStorage.getItem(COLLAPSED_KEY);
    if (!raw) return {};
    const parsed = JSON.parse(raw);
    if (Array.isArray(parsed)) {
      const out = {};
      for (const k of parsed) {
        if (typeof k === "string") out[k] = true;
      }
      return out;
    }
    if (parsed && typeof parsed === "object") {
      const out = {};
      for (const [k, v] of Object.entries(parsed)) {
        out[k] = Boolean(v);
      }
      return out;
    }
    return {};
  } catch {
    return {};
  }
}

export function isGroupExpanded(collapsedMap, key) {
  if (Object.prototype.hasOwnProperty.call(collapsedMap, key)) {
    return !collapsedMap[key];
  }
  return key !== "done";
}

export function useCollapsedGroups() {
  const [collapsed, setCollapsed] = useState(readCollapsedMap);

  const isExpanded = useCallback((key) => isGroupExpanded(collapsed, key), [collapsed]);

  const toggleGroup = useCallback((key) => {
    setCollapsed((prev) => {
      const next = { ...prev, [key]: isGroupExpanded(prev, key) };
      writeStoredValue(COLLAPSED_KEY, JSON.stringify(next));
      return next;
    });
  }, []);

  const expandGroups = useCallback((keys) => {
    if (!keys || keys.length === 0) return;
    setCollapsed((prev) => {
      let changed = false;
      const next = { ...prev };
      for (const key of keys) {
        if (key && !isGroupExpanded(next, key)) {
          next[key] = false;
          changed = true;
        }
      }
      if (!changed) return prev;
      writeStoredValue(COLLAPSED_KEY, JSON.stringify(next));
      return next;
    });
  }, []);

  return { isExpanded, toggleGroup, expandGroups };
}

export function SortControl({ sort, onChange }) {
  return (
    <span className="backlog-select" title="Sort backlog">
      <ArrowUpDown className="backlog-select-icon" aria-hidden="true" />
      <select className="control" aria-label="Sort backlog" value={sort} onChange={(e) => onChange(e.target.value)}>
        {SORT_OPTIONS.map(({ id, label }) => (
          <option key={id} value={id}>
            {label}
          </option>
        ))}
      </select>
      <ChevronDown className="backlog-select-chevron" aria-hidden="true" />
    </span>
  );
}

export function GroupControl({ groupBy, onChange }) {
  return (
    <span className="backlog-select" title="Group backlog">
      <Layers className="backlog-select-icon" aria-hidden="true" />
      <select className="control" aria-label="Group backlog" value={groupBy} onChange={(e) => onChange(e.target.value)}>
        {GROUP_OPTIONS.map(({ id, label }) => (
          <option key={id} value={id}>
            {label}
          </option>
        ))}
      </select>
      <ChevronDown className="backlog-select-chevron" aria-hidden="true" />
    </span>
  );
}

export function BacklogToolbar({ viewSwitch, sort, onSortChange, showGroupControl, groupBy, onGroupChange }) {
  return (
    <div className="backlog-tools">
      {viewSwitch}
      <SortControl sort={sort} onChange={onSortChange} />
      {showGroupControl && <GroupControl groupBy={groupBy} onChange={onGroupChange} />}
    </div>
  );
}

export function CollapsibleGroupHead({ expanded, onToggle, status, sectionIcon, isBacklogSection = false, label, notify = false, count }) {
  return (
    <button type="button" className="group-head" aria-expanded={expanded} onClick={onToggle}>
      {expanded ? <ChevronDown aria-hidden="true" /> : <ChevronRight aria-hidden="true" />}
      {status ? (
        <span className={`status ${status}`} />
      ) : isBacklogSection ? (
        <ListTodo className="group-head-icon" aria-hidden="true" />
      ) : (
        <SectionIcon icon={sectionIcon} className="group-head-icon" aria-hidden="true" />
      )}
      <span className="group-head-label">{label}</span>
      {notify && <span className="chip section-notify-chip">Actionable</span>}
      <span className="count">{count}</span>
    </button>
  );
}
