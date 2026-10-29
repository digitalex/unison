import { Fragment } from "react";
import { itemLink } from "./nav.js";

// A small Markdown renderer for chat. It builds React elements only — no
// HTML strings, no dangerouslySetInnerHTML — so raw HTML in a message stays
// literal text, and link hrefs are restricted to http(s) and mailto.
//
// Blocks: paragraphs (single newlines are line breaks, as in chat), fenced
// code, > quotes, - * + and 1. lists (one nested level), # to ### headings
// (rendered small), and --- rules. Inline: **bold** __bold__ *italic*
// _italic_ ~~strike~~ `code` [text](url), bare URLs, @mentions, #N backlog
// item references (links that open the item, #98) and backslash escapes. Unclosed markers render literally. Every scan is linear-time
// (closing-marker searches are memoized), and nesting depth is capped.

const MAX_DEPTH = 4;

export default function Markdown({ text }) {
  return <div className="md">{renderBlocks(parseBlocks(String(text ?? "").replace(/\r\n?/g, "\n").split("\n"), 0))}</div>;
}

// ---- Blocks ----

const FENCE = /^ {0,3}(`{3,}|~{3,})(.*)$/;
const HEADING = /^ {0,3}(#{1,3})(?:[ \t]+|$)/;
const QUOTE = /^ {0,3}> ?/;
const LIST_ITEM = /^( *)([-*+]|\d{1,9}[.)])[ \t]+/;

function isRule(line) {
  const s = line.replace(/[ \t]/g, "");
  return s.length >= 3 && (/^-+$/.test(s) || /^\*+$/.test(s) || /^_+$/.test(s)) && /^ {0,3}[-*_]/.test(line);
}

function startsBlock(line) {
  return FENCE.test(line) || HEADING.test(line) || QUOTE.test(line) || isRule(line) || LIST_ITEM.test(line);
}

// parseBlocks turns lines into a block tree:
// { type: "p" | "code" | "quote" | "list" | "heading" | "hr", ... }
function parseBlocks(lines, depth) {
  const blocks = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    if (line.trim() === "") {
      i++;
      continue;
    }
    const fence = line.match(FENCE);
    if (fence && !(fence[1][0] === "`" && fence[2].includes("`"))) {
      const marker = fence[1];
      let end = i + 1;
      while (end < lines.length && !closesFence(lines[end], marker)) end++;
      if (end < lines.length) {
        blocks.push({ type: "code", text: lines.slice(i + 1, end).join("\n") });
        i = end + 1;
        continue;
      }
      // Unclosed fence: fall through and treat the line as text.
    } else if (isRule(line)) {
      blocks.push({ type: "hr" });
      i++;
      continue;
    } else if (HEADING.test(line)) {
      const level = line.match(HEADING)[1].length;
      const content = stripClosingHashes(line.replace(HEADING, "").trim());
      blocks.push({ type: "heading", level, text: content });
      i++;
      continue;
    } else if (QUOTE.test(line)) {
      const inner = [];
      while (i < lines.length && QUOTE.test(lines[i])) inner.push(lines[i++].replace(QUOTE, ""));
      blocks.push(depth < MAX_DEPTH ? { type: "quote", blocks: parseBlocks(inner, depth + 1) } : { type: "p", text: inner.join("\n") });
      continue;
    } else if (LIST_ITEM.test(line)) {
      const [list, next] = parseList(lines, i, depth);
      blocks.push(list);
      i = next;
      continue;
    }
    // Paragraph: until a blank line or the start of another block.
    const para = [line];
    i++;
    while (i < lines.length && lines[i].trim() !== "" && !startsBlock(lines[i])) para.push(lines[i++]);
    blocks.push({ type: "p", text: para.join("\n") });
  }
  return blocks;
}

// stripClosingHashes removes an optional closing "###" sequence (preceded by
// whitespace) from a heading, without a backtracking regex.
function stripClosingHashes(s) {
  let end = s.length;
  while (end > 0 && s[end - 1] === "#") end--;
  if (end === s.length || (end > 0 && !/[ \t]/.test(s[end - 1]))) return s;
  return s.slice(0, end).trimEnd();
}

function closesFence(line, marker) {
  const s = line.trim();
  return s.length >= marker.length && s[0] === marker[0] && [...s].every((c) => c === marker[0]);
}

// parseList reads consecutive items of one list starting at lines[start].
// Items indented at least two spaces more than the first become a nested
// list under the previous item; indented plain lines continue an item.
function parseList(lines, start, depth) {
  const first = lines[start].match(LIST_ITEM);
  const indent = first[1].length;
  const ordered = /\d/.test(first[2]);
  const list = { type: "list", ordered, start: ordered ? parseInt(first[2], 10) : 1, items: [] };
  let i = start;
  while (i < lines.length) {
    const line = lines[i];
    if (line.trim() === "") break;
    const m = line.match(LIST_ITEM);
    if (m && m[1].length < indent + 2) {
      if (/\d/.test(m[2]) !== ordered || m[1].length < indent) break;
      list.items.push({ text: line.slice(m[0].length), children: null });
      i++;
      continue;
    }
    const item = list.items[list.items.length - 1];
    if (m && depth < MAX_DEPTH) {
      const [child, next] = parseList(lines, i, depth + 1);
      if (item.children) item.children.items.push(...child.items);
      else item.children = child;
      i = next;
      continue;
    }
    if (/^\s/.test(line) || !startsBlock(line)) {
      item.text += "\n" + line.trim();
      i++;
      continue;
    }
    break;
  }
  return [list, i];
}

function renderBlocks(blocks) {
  return blocks.map((b, i) => {
    switch (b.type) {
      case "code":
        return (
          <pre key={i} className="md-pre">
            <code>{b.text}</code>
          </pre>
        );
      case "hr":
        return <hr key={i} className="md-hr" />;
      case "heading":
        return (
          <p key={i} className={`md-heading md-h${b.level}`}>
            {renderInline(b.text)}
          </p>
        );
      case "quote":
        return (
          <blockquote key={i} className="md-quote">
            {renderBlocks(b.blocks)}
          </blockquote>
        );
      case "list":
        return renderList(b, i);
      default:
        return (
          <p key={i} className="md-p">
            {renderInline(b.text)}
          </p>
        );
    }
  });
}

function renderList(list, key) {
  const items = list.items.map((item, i) => (
    <li key={i}>
      {renderInline(item.text)}
      {item.children && renderList(item.children, "c")}
    </li>
  ));
  return list.ordered ? (
    <ol key={key} className="md-list" start={list.start !== 1 ? list.start : undefined}>
      {items}
    </ol>
  ) : (
    <ul key={key} className="md-list">
      {items}
    </ul>
  );
}

// ---- Inline ----

const PUNCTUATION = "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~";
const WORD = /[\p{L}\p{N}_]/u;
const URL_START = /^(?:https?:\/\/|www\.)/i;
const URL_CHARS = /[^\s<>"'`]+/y;
// Punctuation that usually ends a sentence rather than the URL.
const TRAILING_PUNCTUATION = /[.,;:!?'")\]}*_~]+$/;

// trimUrl drops trailing punctuation, but keeps a closing ")" that balances
// an opening "(" inside the URL (e.g. Wikipedia links).
function trimUrl(url) {
  const trimmed = url.replace(TRAILING_PUNCTUATION, "");
  const tail = url.slice(trimmed.length);
  const opens = (trimmed.match(/\(/g) || []).length;
  const closes = (trimmed.match(/\)/g) || []).length;
  if (opens > closes && tail.startsWith(")")) return trimmed + ")";
  return trimmed;
}

// refersToPR says whether the text before a "#N" makes it a pull request
// ("PR #82", "pull request #82"), not a backlog item.
export function refersToPR(before) {
  return /\b(?:PRs?|pull(?: request)?s?)\s*$/i.test(before);
}

// safeHref returns an allowed href for url, or null.
export function safeHref(url) {
  const u = url.trim();
  if (/[\s\u0000-\u001f]/.test(u)) return null;
  if (/^https?:\/\/[^/]/i.test(u) || /^mailto:[^@\s]+@[^@\s]+$/i.test(u)) return u;
  if (/^www\./i.test(u)) return `https://${u}`;
  return null;
}

function link(href, children, key) {
  return (
    <a key={key} href={href} target="_blank" rel="noopener noreferrer">
      {children}
    </a>
  );
}

// makeFinder memoizes "first valid position of marker at or after from" per
// marker. Validity depends only on the candidate position, so a cached
// answer stays correct for any later from that does not pass it: every
// search is amortized linear over the text.
function makeFinder(s) {
  const cache = new Map();
  return (marker, from, valid) => {
    const c = cache.get(marker);
    if (c && from >= c.from && (c.at === -1 || c.at >= from)) return c.at;
    let j = s.indexOf(marker, from);
    while (j !== -1 && !valid(j)) j = s.indexOf(marker, j + 1);
    cache.set(marker, { from, at: j });
    return j;
  };
}

function count(s, ch, from, to) {
  let n = 0;
  for (let k = from; k < to; k++) if (s[k] === ch) n++;
  return n;
}

const isSpace = (c) => c === undefined || /\s/.test(c);
const isWord = (c) => c !== undefined && WORD.test(c);

function renderInline(s, depth = 0) {
  const nodes = [];
  let text = "";
  const flush = () => {
    if (text) nodes.push(text);
    text = "";
  };
  const push = (node) => {
    flush();
    nodes.push(node);
  };
  const find = makeFinder(s);
  const inner = (content) => (depth < MAX_DEPTH ? renderInline(content, depth + 1) : content);

  // Closers for paired markers: not preceded by whitespace, and for
  // underscores not followed by a word character (so snake_case stays).
  const closer = (marker) => (j) => {
    if (isSpace(s[j - 1]) || s[j - 1] === "\\") return false;
    const after = s[j + marker.length];
    if (marker.length === 1 && (after === marker || s[j - 1] === marker)) return false;
    return marker[0] !== "_" || !isWord(after);
  };

  let i = 0;
  while (i < s.length) {
    const c = s[i];

    if (c === "\\" && i + 1 < s.length && PUNCTUATION.includes(s[i + 1])) {
      text += s[i + 1];
      i += 2;
      continue;
    }
    if (c === "\n") {
      push(<br key={i} />);
      i++;
      continue;
    }
    if (c === "`") {
      let n = 1;
      while (s[i + n] === "`") n++;
      const run = "`".repeat(n);
      const end = find(run, i + n, (j) => s[j - 1] !== "`" && s[j + n] !== "`");
      if (end !== -1) {
        let code = s.slice(i + n, end).replace(/\n/g, " ");
        if (code.length > 2 && code.startsWith(" ") && code.endsWith(" ")) code = code.slice(1, -1);
        push(<code key={i} className="md-code">{code}</code>);
        i = end + n;
      } else {
        text += run;
        i += n;
      }
      continue;
    }
    if ((c === "*" || c === "_" || c === "~") && !isSpace(s[i + 1])) {
      const double = s[i + 1] === c;
      const marker = double ? c + c : c;
      const opensWord = c !== "_" || !isWord(s[i - 1]);
      if ((c !== "~" || double) && opensWord && !isSpace(s[i + marker.length])) {
        const end = find(marker, i + marker.length + 1, closer(marker));
        if (end !== -1) {
          const content = inner(s.slice(i + marker.length, end));
          const Tag = c === "~" ? "del" : double ? "strong" : "em";
          push(<Tag key={i}>{content}</Tag>);
          i = end + marker.length;
          continue;
        }
      }
      text += marker;
      i += marker.length;
      continue;
    }
    if (c === "[") {
      const close = find("]", i + 1, (j) => s[j - 1] !== "\\");
      if (close !== -1 && s[close + 1] === "(") {
        let end = find(")", close + 2, () => true);
        // Allow balanced parentheses in the URL (e.g. Wikipedia links).
        while (end !== -1 && count(s, "(", close + 2, end) > count(s, ")", close + 2, end)) {
          const next = s.indexOf(")", end + 1);
          if (next === -1 || next - close > 2048) break;
          end = next;
        }
        const href = end !== -1 ? safeHref(s.slice(close + 2, end)) : null;
        const label = s.slice(i + 1, close);
        if (href && label.trim()) {
          push(link(href, depth < MAX_DEPTH ? renderInline(label, MAX_DEPTH) : label, i));
          i = end + 1;
          continue;
        }
      }
      text += c;
      i++;
      continue;
    }
    if ((c === "h" || c === "H" || c === "w" || c === "W") && !isWord(s[i - 1]) && depth < MAX_DEPTH && URL_START.test(s.slice(i, i + 8))) {
      URL_CHARS.lastIndex = i;
      const url = trimUrl(URL_CHARS.exec(s)[0]);
      const href = safeHref(url);
      if (href) {
        push(link(href, url, i));
        i += url.length;
        continue;
      }
    }
    if (c === "#" && depth < MAX_DEPTH && !isWord(s[i - 1]) && s[i - 1] !== "&" && s[i - 1] !== "/") {
      const m = s.slice(i + 1, i + 11).match(/^\d{1,9}(?![\p{L}\p{N}_])/u);
      if (m && !refersToPR(s.slice(Math.max(0, i - 16), i))) {
        push(
          <a key={i} className="md-item" title={`Open backlog item #${m[0]}`} {...itemLink(Number(m[0]))}>
            #{m[0]}
          </a>,
        );
        i += 1 + m[0].length;
        continue;
      }
    }
    if (c === "@" && !isWord(s[i - 1]) && /[\p{L}\p{N}_]/u.test(s[i + 1] ?? "")) {
      const m = s.slice(i + 1, i + 65).match(/^[\p{L}\p{N}_.-]*[\p{L}\p{N}_]/u);
      push(
        <span key={i} className="md-mention">
          @{m[0]}
        </span>,
      );
      i += 1 + m[0].length;
      continue;
    }
    text += c;
    i++;
  }
  flush();
  return nodes.map((n, k) => (typeof n === "string" ? <Fragment key={`t${k}`}>{n}</Fragment> : n));
}
