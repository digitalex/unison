import { useEffect, useMemo, useState } from "react";
import { AlertTriangle, BookOpen, FileText, Plus, Trash2 } from "lucide-react";
import { PageUnloadingError } from "../api.js";
import Button from "../ui/Button.jsx";
import Field from "../ui/Field.jsx";
import { Input, Textarea } from "../ui/Input.jsx";

// Maximum rune/char count inlined directly in the agent doorway.
export const INSTRUCTIONS_INLINE_LIMIT = 1200;

// Maximum UTF-8 byte size accepted by the server (32 KiB).
export const INSTRUCTIONS_MAX_BYTES = 32 * 1024;

const STARTER_SNIPPETS = [
  {
    id: "claim-etiquette",
    label: "Claim etiquette",
    markdown: [
      "## Claim etiquette",
      "",
      "- Claim one backlog item at a time; do not hog multiple claims.",
      "- Release items promptly (`unison backlog release <id>`) if you switch focus or get blocked.",
    ].join("\n"),
  },
  {
    id: "try-it",
    label: "Try it checklist",
    markdown: [
      "## Try it",
      "",
      "- Verify your change end-to-end before opening a PR.",
      "- For UI changes, capture and inspect a rendered screenshot (`unison image upload`).",
    ].join("\n"),
  },
];

// Extracts level-2 ("## ") Markdown headings, ignoring fenced code blocks.
export function extractInstructionHeadings(md) {
  const headings = [];
  let fenceChar = "";
  let fenceLen = 0;
  for (const line of md.split("\n")) {
    const trimmed = line.trim();
    if (trimmed.length >= 3 && (trimmed[0] === "`" || trimmed[0] === "~")) {
      const ch = trimmed[0];
      let n = 0;
      while (n < trimmed.length && trimmed[n] === ch) n++;
      if (n >= 3) {
        if (!fenceChar) {
          fenceChar = ch;
          fenceLen = n;
        } else if (ch === fenceChar && n >= fenceLen && trimmed.slice(n).trim() === "") {
          fenceChar = "";
          fenceLen = 0;
        }
        continue;
      }
    }
    if (fenceChar) continue;
    if (line.startsWith("## ") || line.startsWith("##\t")) {
      const text = line.slice(3).trim().replace(/#+$/, "").trim();
      if (text) headings.push(text);
    }
  }
  return headings;
}

function utf8ByteLength(s) {
  return new TextEncoder().encode(s).length;
}

function runeLength(s) {
  return Array.from(s).length;
}

// Settings → Instructions: opt-in per-workspace agent instructions editor with
// live context-budget indicator, optional repo-relative sync path, and starter
// snippets.
export default function Instructions({ workspace, call, onError }) {
  const [saved, setSaved] = useState({
    instructions: "",
    instructions_path: workspace.instructions_path || "",
  });
  const [form, setForm] = useState({
    instructions: "",
    instructions_path: workspace.instructions_path || "",
  });
  const [loaded, setLoaded] = useState(false);
  const [saving, setSaving] = useState(false);
  const [status, setStatus] = useState("");

  useEffect(() => {
    let active = true;
    const empty = {
      instructions: "",
      instructions_path: workspace.instructions_path || "",
    };
    if (!workspace.has_instructions && !workspace.instructions_path) {
      setSaved(empty);
      setForm(empty);
      setLoaded(true);
      return;
    }
    setLoaded(false);
    (async () => {
      try {
        const res = await call("GET", "/instructions");
        if (!active) return;
        const next = {
          instructions: typeof res?.instructions === "string" ? res.instructions : "",
          instructions_path: typeof res?.instructions_path === "string" ? res.instructions_path : workspace.instructions_path || "",
        };
        setSaved(next);
        setForm(next);
        setLoaded(true);
      } catch (err) {
        if (!active) return;
        setLoaded(true);
        if (!(err instanceof PageUnloadingError)) {
          onError(err instanceof Error ? err.message : String(err));
        }
      }
    })();
    return () => {
      active = false;
    };
  }, [workspace.id, workspace.has_instructions, workspace.instructions_path, workspace.instructions_updated_at, call]);

  const dirty = form.instructions !== saved.instructions || form.instructions_path !== saved.instructions_path;

  const trimmed = form.instructions.trim();
  const charCount = useMemo(() => runeLength(trimmed), [trimmed]);
  const byteCount = useMemo(() => utf8ByteLength(form.instructions), [form.instructions]);
  const headings = useMemo(() => extractInstructionHeadings(form.instructions), [form.instructions]);
  const overLimit = byteCount > INSTRUCTIONS_MAX_BYTES;

  const appendSnippet = (snippet) => {
    setStatus("");
    setForm((prev) => {
      const existing = prev.instructions.trimEnd();
      const next = existing ? `${existing}\n\n${snippet.markdown}\n` : `${snippet.markdown}\n`;
      return { ...prev, instructions: next };
    });
  };

  const save = async (e) => {
    e.preventDefault();
    if (saving || overLimit) return;
    setSaving(true);
    setStatus("");
    try {
      const res = await call("PUT", "/instructions", {
        instructions: form.instructions,
        instructions_path: form.instructions_path.trim(),
      });
      const next = {
        instructions: typeof res?.instructions === "string" ? res.instructions : form.instructions,
        instructions_path: typeof res?.instructions_path === "string" ? res.instructions_path : form.instructions_path.trim(),
      };
      setSaved(next);
      setForm(next);
      onError("");
      setStatus("Saved");
    } catch (err) {
      if (!(err instanceof PageUnloadingError)) {
        onError(err instanceof Error ? err.message : String(err));
      }
    } finally {
      setSaving(false);
    }
  };

  const clear = async () => {
    if (saving) return;
    setSaving(true);
    setStatus("");
    try {
      await call("PUT", "/instructions", {
        instructions: "",
        instructions_path: "",
      });
      const cleared = { instructions: "", instructions_path: "" };
      setSaved(cleared);
      setForm(cleared);
      onError("");
      setStatus("Cleared");
    } catch (err) {
      if (!(err instanceof PageUnloadingError)) {
        onError(err instanceof Error ? err.message : String(err));
      }
    } finally {
      setSaving(false);
    }
  };

  let budgetClass = "chip instructions-budget";
  let BudgetIcon = FileText;
  let budgetLabel = "Off (0 tokens on join)";
  if (overLimit) {
    budgetClass = "chip instructions-budget instructions-budget-danger";
    BudgetIcon = AlertTriangle;
    budgetLabel = `Too large (${byteCount.toLocaleString()} / ${INSTRUCTIONS_MAX_BYTES.toLocaleString()} bytes)`;
  } else if (charCount > 0 && charCount <= INSTRUCTIONS_INLINE_LIMIT) {
    budgetClass = "chip instructions-budget instructions-budget-ok";
    BudgetIcon = FileText;
    budgetLabel = `Inline on join (${charCount.toLocaleString()} / ${INSTRUCTIONS_INLINE_LIMIT.toLocaleString()} chars)`;
  } else if (charCount > INSTRUCTIONS_INLINE_LIMIT) {
    budgetClass = "chip chip-accent instructions-budget";
    BudgetIcon = BookOpen;
    if (headings.length > 0) {
      const noun = headings.length === 1 ? "section" : "sections";
      budgetLabel = `Indexed by headings (${headings.length} ${noun} — agents load on demand)`;
    } else {
      budgetLabel = `Indexed on demand (${charCount.toLocaleString()} chars — add ## headings to index)`;
    }
  }

  const hasAny =
    Boolean(form.instructions.trim()) ||
    Boolean(form.instructions_path.trim()) ||
    Boolean(saved.instructions.trim()) ||
    Boolean(saved.instructions_path.trim());

  return (
    <form className="form" onSubmit={save}>
      <Field label="Instructions" htmlFor="ws-instructions-body">
        <div className="instructions-editor">
          <Textarea
            id="ws-instructions-body"
            className="instructions-textarea"
            rows={10}
            disabled={!loaded || saving}
            placeholder="Optional Markdown instructions for agents joining this workspace. Use ## headings to organize longer guides."
            value={form.instructions}
            onChange={(e) => {
              setStatus("");
              setForm((prev) => ({ ...prev, instructions: e.target.value }));
            }}
          />
          <div className="instructions-meta">
            <span className={budgetClass} aria-label="Context budget">
              <BudgetIcon size={13} aria-hidden="true" />
              <span>{budgetLabel}</span>
            </span>
            <div className="instructions-snippets">
              <span className="muted">Starter snippets:</span>
              {STARTER_SNIPPETS.map((s) => (
                <Button
                  key={s.id}
                  type="button"
                  size="small"
                  disabled={!loaded || saving}
                  onClick={() => appendSnippet(s)}
                >
                  <Plus size={13} aria-hidden="true" />
                  {s.label}
                </Button>
              ))}
            </div>
          </div>
        </div>
      </Field>
      <Field label="Repo file path" htmlFor="ws-instructions-path">
        <Input
          id="ws-instructions-path"
          disabled={!loaded || saving}
          placeholder=".unison/instructions.md"
          value={form.instructions_path}
          onChange={(e) => {
            setStatus("");
            setForm((prev) => ({ ...prev, instructions_path: e.target.value }));
          }}
        />
      </Field>
      <p className="field-description">
        Optional repo-relative path. Sync changes from your checkout with <code>unison workspace instructions sync</code>.
      </p>
      <div className="actions-row">
        <Button variant="primary" type="submit" disabled={!loaded || !dirty || saving || overLimit}>
          {saving ? "Saving…" : "Save"}
        </Button>
        {hasAny && (
          <Button type="button" disabled={!loaded || saving} onClick={() => void clear()}>
            <Trash2 size={14} aria-hidden="true" />
            Clear
          </Button>
        )}
        {dirty && (
          <Button
            type="button"
            disabled={saving}
            onClick={() => {
              setStatus("");
              setForm(saved);
            }}
          >
            Reset
          </Button>
        )}
        {status && !dirty && (
          <span className="muted" role="status">
            {status}
          </span>
        )}
      </div>
    </form>
  );
}
