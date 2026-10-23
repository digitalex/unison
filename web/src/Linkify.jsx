// Renders plain text with http(s) and www. URLs turned into links. Text stays
// text (React escapes it), and only http/https hrefs are ever produced.
const URL_PATTERN = /\b(?:https?:\/\/|www\.)[^\s<>"']+/gi;
// Punctuation that usually ends a sentence rather than the URL.
const TRAILING_PUNCTUATION = /[.,;:!?'")\]}]+$/;

export default function Linkify({ text }) {
  const parts = [];
  let last = 0;
  for (const match of text.matchAll(URL_PATTERN)) {
    const url = trimUrl(match[0]);
    const start = match.index;
    if (start > last) parts.push(text.slice(last, start));
    const href = url.toLowerCase().startsWith("www.") ? `https://${url}` : url;
    parts.push(
      <a key={start} href={href} target="_blank" rel="noopener noreferrer">
        {url}
      </a>,
    );
    last = start + url.length;
  }
  if (last < text.length) parts.push(text.slice(last));
  return parts;
}

// Drops trailing punctuation, but keeps a closing ")" that balances an
// opening "(" inside the URL (e.g. Wikipedia links).
function trimUrl(url) {
  const trimmed = url.replace(TRAILING_PUNCTUATION, "");
  const tail = url.slice(trimmed.length);
  const opens = (trimmed.match(/\(/g) || []).length;
  const closes = (trimmed.match(/\)/g) || []).length;
  if (opens > closes && tail.startsWith(")")) return trimmed + ")";
  return trimmed;
}
