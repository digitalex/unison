import { memo } from "react";

// Pure React MathML renderer for inline ($...$) and display ($$...$$) LaTeX
// expressions (#280). Builds MathML React elements directly with zero HTML
// strings, zero dangerouslySetInnerHTML, and zero external dependencies.
// Malformed or unclosed TeX degrades gracefully to partial MathML/text nodes
// without ever throwing.

const MAX_TEX_LENGTH = 4096;
const MAX_PARSE_DEPTH = 24;

const GREEK = {
  alpha: "α",
  beta: "β",
  gamma: "γ",
  delta: "δ",
  epsilon: "ϵ",
  varepsilon: "ε",
  zeta: "ζ",
  eta: "η",
  theta: "θ",
  vartheta: "ϑ",
  iota: "ι",
  kappa: "κ",
  lambda: "λ",
  mu: "μ",
  nu: "ν",
  xi: "ξ",
  pi: "π",
  varpi: "ϖ",
  rho: "ρ",
  varrho: "ϱ",
  sigma: "σ",
  varsigma: "ς",
  tau: "τ",
  upsilon: "υ",
  phi: "ϕ",
  varphi: "φ",
  chi: "χ",
  psi: "ψ",
  omega: "ω",
  ell: "ℓ",
  hbar: "ℏ",
  imath: "ı",
  jmath: "ȷ",
  wp: "℘",
  aleph: "ℵ",
};

const UPPER_GREEK = {
  Gamma: "Γ",
  Delta: "Δ",
  Theta: "Θ",
  Lambda: "Λ",
  Xi: "Ξ",
  Pi: "Π",
  Sigma: "Σ",
  Upsilon: "Υ",
  Phi: "Φ",
  Psi: "Ψ",
  Omega: "Ω",
  Re: "ℜ",
  Im: "ℑ",
};

const ORD_SYMBOLS = {
  infty: "∞",
  partial: "∂",
  nabla: "∇",
  forall: "∀",
  exists: "∃",
  nexists: "∄",
  emptyset: "∅",
  varnothing: "∅",
  neg: "¬",
  lnot: "¬",
  top: "⊤",
  bot: "⊥",
  angle: "∠",
  triangle: "△",
  prime: "′",
  backslash: "\\",
  dots: "…",
  ldots: "…",
  cdots: "⋯",
  vdots: "⋮",
  ddots: "⋱",
};

const OPERATORS = {
  in: "∈",
  notin: "∉",
  ni: "∋",
  subset: "⊂",
  subseteq: "⊆",
  subsetneq: "⊊",
  supset: "⊃",
  supseteq: "⊇",
  cup: "∪",
  cap: "∩",
  setminus: "∖",
  times: "×",
  cdot: "⋅",
  ast: "∗",
  star: "⋆",
  circ: "∘",
  bullet: "•",
  oplus: "⊕",
  ominus: "⊖",
  otimes: "⊗",
  oslash: "⊘",
  odot: "⊙",
  div: "÷",
  pm: "±",
  mp: "∓",
  land: "∧",
  wedge: "∧",
  lor: "∨",
  vee: "∨",
  le: "≤",
  leq: "≤",
  ge: "≥",
  geq: "≥",
  ll: "≪",
  gg: "≫",
  prec: "≺",
  preceq: "⪯",
  succ: "≻",
  succeq: "⪰",
  ne: "≠",
  neq: "≠",
  approx: "≈",
  equiv: "≡",
  sim: "∼",
  simeq: "≃",
  cong: "≅",
  propto: "∝",
  perp: "⊥",
  parallel: "∥",
  mid: "∣",
  nmid: "∤",
  vdash: "⊢",
  models: "⊨",
  to: "→",
  rightarrow: "→",
  gets: "←",
  leftarrow: "←",
  leftrightarrow: "↔",
  Rightarrow: "⇒",
  Leftarrow: "⇐",
  Leftrightarrow: "⇔",
  implies: "⟹",
  impliedby: "⟸",
  iff: "⟺",
  mapsto: "↦",
  longmapsto: "⟼",
  longrightarrow: "⟶",
  longleftarrow: "⟵",
  uparrow: "↑",
  downarrow: "↓",
  updownarrow: "↕",
  Uparrow: "⇑",
  Downarrow: "⇓",
  nearrow: "↗",
  searrow: "↘",
  hookleftarrow: "↩",
  hookrightarrow: "↪",
  langle: "⟨",
  rangle: "⟩",
  lceil: "⌈",
  rceil: "⌉",
  lfloor: "⌊",
  rfloor: "⌋",
  Vert: "‖",
  vert: "|",
  colon: ":",
  bmod: "mod",
};

const BIG_OPS = {
  sum: { glyph: "∑", movable: true },
  prod: { glyph: "∏", movable: true },
  coprod: { glyph: "∐", movable: true },
  bigcup: { glyph: "⋃", movable: true },
  bigcap: { glyph: "⋂", movable: true },
  bigoplus: { glyph: "⊕", movable: true },
  bigotimes: { glyph: "⊗", movable: true },
  bigsqcup: { glyph: "⊔", movable: true },
  bigvee: { glyph: "∨", movable: true },
  bigwedge: { glyph: "∧", movable: true },
  biguplus: { glyph: "⊎", movable: true },
  int: { glyph: "∫", movable: false },
  iint: { glyph: "∬", movable: false },
  iiint: { glyph: "∭", movable: false },
  oint: { glyph: "∮", movable: false },
};

const FUNCTIONS = {
  lim: { label: "lim", movable: true },
  limsup: { label: "lim sup", movable: true },
  liminf: { label: "lim inf", movable: true },
  min: { label: "min", movable: true },
  max: { label: "max", movable: true },
  sup: { label: "sup", movable: true },
  inf: { label: "inf", movable: true },
  det: { label: "det", movable: true },
  gcd: { label: "gcd", movable: true },
  Pr: { label: "Pr", movable: true },
  argmin: { label: "arg min", movable: true },
  argmax: { label: "arg max", movable: true },
  sin: { label: "sin", movable: false },
  cos: { label: "cos", movable: false },
  tan: { label: "tan", movable: false },
  cot: { label: "cot", movable: false },
  sec: { label: "sec", movable: false },
  csc: { label: "csc", movable: false },
  arcsin: { label: "arcsin", movable: false },
  arccos: { label: "arccos", movable: false },
  arctan: { label: "arctan", movable: false },
  sinh: { label: "sinh", movable: false },
  cosh: { label: "cosh", movable: false },
  tanh: { label: "tanh", movable: false },
  coth: { label: "coth", movable: false },
  log: { label: "log", movable: false },
  ln: { label: "ln", movable: false },
  lg: { label: "lg", movable: false },
  exp: { label: "exp", movable: false },
  dim: { label: "dim", movable: false },
  ker: { label: "ker", movable: false },
  hom: { label: "hom", movable: false },
  deg: { label: "deg", movable: false },
  arg: { label: "arg", movable: false },
  mod: { label: "mod", movable: false },
};

const ACCENTS = {
  hat: { glyph: "^", stretchy: false },
  widehat: { glyph: "^", stretchy: true },
  bar: { glyph: "¯", stretchy: false },
  overline: { glyph: "¯", stretchy: true },
  vec: { glyph: "→", stretchy: false },
  overrightarrow: { glyph: "→", stretchy: true },
  tilde: { glyph: "~", stretchy: false },
  widetilde: { glyph: "~", stretchy: true },
  dot: { glyph: "˙", stretchy: false },
  ddot: { glyph: "¨", stretchy: false },
};

const SPACES = {
  ",": "0.17em",
  thinspace: "0.17em",
  ":": "0.22em",
  ">": "0.22em",
  ";": "0.28em",
  " ": "0.33em",
  quad: "1em",
  qquad: "2em",
  "!": "-0.17em",
};

const ESCAPED_SYMBOLS = {
  "{": "{",
  "}": "}",
  "|": "‖",
  $: "$",
  "%": "%",
  "&": "&",
  "#": "#",
  _: "_",
};

const DELIMITER_COMMANDS = {
  "{": "{",
  "}": "}",
  "|": "‖",
  Vert: "‖",
  vert: "|",
  langle: "⟨",
  rangle: "⟩",
  lceil: "⌈",
  rceil: "⌉",
  lfloor: "⌊",
  rfloor: "⌋",
  backslash: "\\",
  uparrow: "↑",
  downarrow: "↓",
  updownarrow: "↕",
  Uparrow: "⇑",
  Downarrow: "⇓",
};

const SIZED_DELIM_COMMANDS = new Set([
  "big",
  "Big",
  "bigg",
  "Bigg",
  "bigl",
  "Bigl",
  "biggl",
  "Biggl",
  "bigr",
  "Bigr",
  "biggr",
  "Biggr",
  "bigm",
  "Bigm",
  "biggm",
  "Biggm",
]);

const TEXT_COMMANDS = {
  text: null,
  textrm: null,
  mbox: null,
  textbf: "math-bf",
  textit: "math-it",
  texttt: "math-tt",
};

const FONT_COMMANDS = new Set([
  "mathrm",
  "mathbf",
  "boldsymbol",
  "bm",
  "mathit",
  "mathsf",
  "mathtt",
  "mathcal",
  "mathbb",
]);

const MATRIX_DELIMS = {
  matrix: [null, null],
  pmatrix: ["(", ")"],
  bmatrix: ["[", "]"],
  Bmatrix: ["{", "}"],
  vmatrix: ["|", "|"],
  Vmatrix: ["‖", "‖"],
  cases: ["{", null],
  aligned: [null, null],
  align: [null, null],
  "align*": [null, null],
  array: [null, null],
};

const BLACKBOARD_SPECIAL = {
  C: "ℂ",
  H: "ℍ",
  N: "ℕ",
  P: "ℙ",
  Q: "ℚ",
  R: "ℝ",
  Z: "ℤ",
};

const SCRIPT_SPECIAL = {
  B: "ℬ",
  E: "ℰ",
  F: "ℱ",
  H: "ℋ",
  I: "ℐ",
  L: "ℒ",
  M: "ℳ",
  R: "ℛ",
};

const NEGATED_RELATIONS = {
  "=": "≠",
  in: "∉",
  equiv: "≢",
  subset: "⊄",
  subseteq: "⊈",
  supset: "⊅",
  supseteq: "⊉",
  sim: "≁",
  approx: "≉",
  mid: "∤",
};

function toBlackboardBold(str) {
  let out = "";
  for (const ch of str) {
    if (BLACKBOARD_SPECIAL[ch]) {
      out += BLACKBOARD_SPECIAL[ch];
      continue;
    }
    const code = ch.charCodeAt(0);
    if (code >= 65 && code <= 90) out += String.fromCodePoint(0x1d538 + (code - 65));
    else if (code >= 97 && code <= 122) out += String.fromCodePoint(0x1d552 + (code - 97));
    else if (code >= 48 && code <= 57) out += String.fromCodePoint(0x1d7d8 + (code - 48));
    else out += ch;
  }
  return out;
}

function toCalligraphic(str) {
  let out = "";
  for (const ch of str) {
    if (SCRIPT_SPECIAL[ch]) {
      out += SCRIPT_SPECIAL[ch];
      continue;
    }
    const code = ch.charCodeAt(0);
    if (code >= 65 && code <= 90) out += String.fromCodePoint(0x1d49c + (code - 65));
    else out += ch;
  }
  return out;
}

function skipWhitespace(cur) {
  while (cur.pos < cur.len && /\s/.test(cur.src[cur.pos])) cur.pos++;
}

function peekCommandName(cur) {
  if (cur.src[cur.pos] !== "\\") return null;
  let end = cur.pos + 1;
  if (end >= cur.len) return "";
  if (/[a-zA-Z]/.test(cur.src[end])) {
    while (end < cur.len && /[a-zA-Z]/.test(cur.src[end])) end++;
    return cur.src.slice(cur.pos + 1, end);
  }
  return cur.src[end];
}

function readCommandName(cur) {
  const name = peekCommandName(cur);
  if (name === null) return null;
  cur.pos += 1 + name.length;
  return name;
}

function readRawBracedText(cur) {
  skipWhitespace(cur);
  if (cur.pos >= cur.len) return "";
  if (cur.src[cur.pos] !== "{") {
    const ch = cur.src[cur.pos];
    cur.pos++;
    return ch;
  }
  cur.pos++;
  let depth = 1;
  let out = "";
  while (cur.pos < cur.len && depth > 0) {
    const ch = cur.src[cur.pos++];
    if (ch === "\\" && cur.pos < cur.len) {
      const next = cur.src[cur.pos++];
      out += next === "{" || next === "}" || next === "\\" || next === "$" || next === "%" || next === "&" || next === "#" || next === "_" ? next : `\\${next}`;
      continue;
    }
    if (ch === "{") depth++;
    else if (ch === "}") {
      depth--;
      if (depth === 0) break;
    }
    out += ch;
  }
  return out;
}

function readOptionalBracket(cur, display) {
  skipWhitespace(cur);
  if (cur.src[cur.pos] !== "[") return null;
  cur.pos++;
  const nodes = parseSequence(cur, display, { stopBracket: true });
  if (cur.src[cur.pos] === "]") cur.pos++;
  return wrapNodes(nodes);
}

function readDelimiter(cur) {
  skipWhitespace(cur);
  if (cur.pos >= cur.len) return null;
  const ch = cur.src[cur.pos];
  if (ch === "\\") {
    const cmd = readCommandName(cur);
    return DELIMITER_COMMANDS[cmd] ?? cmd;
  }
  cur.pos++;
  if (ch === ".") return null;
  return ch;
}

function wrapNodes(nodes) {
  if (nodes.length === 0) return { type: "mrow", children: [] };
  if (nodes.length === 1) return nodes[0];
  return { type: "mrow", children: nodes };
}

function readRequiredArg(cur, display) {
  skipWhitespace(cur);
  if (cur.pos >= cur.len || cur.src[cur.pos] === "}") {
    return { type: "mrow", children: [] };
  }
  if (cur.src[cur.pos] === "{") {
    cur.pos++;
    const children = cur.depth < MAX_PARSE_DEPTH ? parseSequenceWithDepth(cur, display, { stopBrace: true }) : [];
    if (cur.src[cur.pos] === "}") cur.pos++;
    return wrapNodes(children);
  }
  const atom = parseAtom(cur, display);
  return atom || { type: "mrow", children: [] };
}

function parseSequenceWithDepth(cur, display, stops) {
  cur.depth++;
  const nodes = parseSequence(cur, display, stops);
  cur.depth--;
  return nodes;
}

function shouldStopSequence(cur, stops) {
  const ch = cur.src[cur.pos];
  if (stops.stopBrace && ch === "}") return true;
  if (stops.stopBracket && ch === "]") return true;
  if (stops.stopCell && ch === "&") return true;
  if (ch === "\\") {
    const next = peekCommandName(cur);
    if (stops.stopRight && next === "right") return true;
    if (stops.stopCell && (next === "\\" || next === "cr" || next === "end")) return true;
  }
  return false;
}

function parseSequence(cur, display, stops = {}) {
  const nodes = [];
  while (cur.pos < cur.len) {
    skipWhitespace(cur);
    if (cur.pos >= cur.len || shouldStopSequence(cur, stops)) break;
    const item = parseScriptedItem(cur, display, stops);
    if (item) nodes.push(item);
  }
  return nodes;
}

function parseScriptedItem(cur, display, stops) {
  const ch = cur.src[cur.pos];
  let base = null;
  if (ch === "^" || ch === "_") {
    base = { type: "mrow", children: [] };
  } else {
    base = parseAtom(cur, display, stops);
    if (!base) return null;
  }
  return attachScripts(cur, base, display);
}

function readLimitModifier(cur) {
  let limitMode = null;
  while (cur.pos < cur.len) {
    skipWhitespace(cur);
    const cmd = peekCommandName(cur);
    if (cmd === "limits") {
      readCommandName(cur);
      limitMode = "over";
    } else if (cmd === "nolimits") {
      readCommandName(cur);
      limitMode = "scripts";
    } else {
      break;
    }
  }
  return limitMode;
}

function readPrimeRun(cur) {
  let count = 0;
  while (cur.pos < cur.len && cur.src[cur.pos] === "'") {
    count++;
    cur.pos++;
  }
  if (count === 0) return null;
  const glyph = count === 1 ? "′" : count === 2 ? "″" : count === 3 ? "‴" : "′".repeat(count);
  return { type: "mo", value: glyph };
}

function combineSuperscript(primeNode, supNode) {
  if (!primeNode) return supNode;
  if (!supNode) return primeNode;
  return { type: "mrow", children: [primeNode, supNode] };
}

function attachScripts(cur, initialBase, display) {
  let node = initialBase;
  const explicitLimits = readLimitModifier(cur);
  const primes = readPrimeRun(cur);
  let sub = null;
  let sup = null;

  while (cur.pos < cur.len) {
    skipWhitespace(cur);
    const marker = cur.src[cur.pos];
    if (marker === "_" && !sub) {
      cur.pos++;
      sub = readRequiredArg(cur, false);
    } else if (marker === "^" && !sup) {
      cur.pos++;
      sup = readRequiredArg(cur, false);
    } else {
      break;
    }
  }

  const fullSup = combineSuperscript(primes, sup);
  if (!sub && !fullSup) return node;

  const useUnderOver =
    explicitLimits === "over" ||
    (explicitLimits !== "scripts" && Boolean(display && node.movable));

  if (useUnderOver) {
    if (sub && fullSup) node = { type: "munderover", base: node, under: sub, over: fullSup };
    else if (sub) node = { type: "munder", base: node, under: sub };
    else node = { type: "mover", base: node, over: fullSup };
  } else {
    if (sub && fullSup) node = { type: "msubsup", base: node, sub, sup: fullSup };
    else if (sub) node = { type: "msub", base: node, sub };
    else node = { type: "msup", base: node, sup: fullSup };
  }

  skipWhitespace(cur);
  if (cur.pos < cur.len && (cur.src[cur.pos] === "_" || cur.src[cur.pos] === "^")) {
    return attachScripts(cur, node, display);
  }
  return node;
}

function parseNumber(cur) {
  const start = cur.pos;
  while (cur.pos < cur.len && /[0-9]/.test(cur.src[cur.pos])) cur.pos++;
  if (cur.src[cur.pos] === "." && /[0-9]/.test(cur.src[cur.pos + 1] ?? "")) {
    cur.pos++;
    while (cur.pos < cur.len && /[0-9]/.test(cur.src[cur.pos])) cur.pos++;
  }
  return { type: "mn", value: cur.src.slice(start, cur.pos) };
}

function parseAtom(cur, display, stops = {}) {
  skipWhitespace(cur);
  if (cur.pos >= cur.len) return null;
  const ch = cur.src[cur.pos];

  if (ch === "{") {
    cur.pos++;
    const children = cur.depth < MAX_PARSE_DEPTH ? parseSequenceWithDepth(cur, display, { stopBrace: true }) : [];
    if (cur.src[cur.pos] === "}") cur.pos++;
    return wrapNodes(children);
  }
  if (ch === "}" || (stops.stopBracket && ch === "]")) {
    cur.pos++;
    return null;
  }
  if (ch === "\\") {
    return parseCommand(cur, display);
  }
  if (/[0-9]/.test(ch)) {
    return parseNumber(cur);
  }
  if (/[a-zA-Z]/.test(ch)) {
    cur.pos++;
    return { type: "mi", value: ch };
  }
  if (ch === "~") {
    cur.pos++;
    return { type: "mspace", width: "0.33em" };
  }
  if (ch === "'") {
    return readPrimeRun(cur);
  }
  cur.pos++;
  if (ch === "-") return { type: "mo", value: "−" };
  return { type: "mo", value: ch };
}

function applyFontVariant(node, variant) {
  if (!node) return node;
  if (node.type === "mi" || node.type === "mn") {
    if (variant === "mathbb") {
      return { ...node, type: "mi", value: toBlackboardBold(node.value), mathvariant: "normal" };
    }
    if (variant === "mathcal") {
      return { ...node, type: "mi", value: toCalligraphic(node.value), mathvariant: "normal" };
    }
    if (variant === "mathrm") {
      return { ...node, mathvariant: "normal" };
    }
    if (variant === "mathbf" || variant === "boldsymbol" || variant === "bm") {
      return { ...node, mathvariant: "normal", className: "math-bf" };
    }
    if (variant === "mathit") {
      return { ...node, className: "math-it" };
    }
    if (variant === "mathtt") {
      return { ...node, mathvariant: "normal", className: "math-tt" };
    }
    if (variant === "mathsf") {
      return { ...node, mathvariant: "normal", className: "math-sf" };
    }
    return node;
  }
  if (node.children) {
    return { ...node, children: node.children.map((c) => applyFontVariant(c, variant)) };
  }
  return node;
}

function parseLeftRight(cur, display) {
  const openDelim = readDelimiter(cur);
  const children = cur.depth < MAX_PARSE_DEPTH ? parseSequenceWithDepth(cur, display, { stopRight: true }) : [];
  let closeDelim = null;
  skipWhitespace(cur);
  if (peekCommandName(cur) === "right") {
    readCommandName(cur);
    closeDelim = readDelimiter(cur);
  }
  const wrapped = [];
  if (openDelim) wrapped.push({ type: "mo", value: openDelim, stretchy: true });
  wrapped.push(...children);
  if (closeDelim) wrapped.push({ type: "mo", value: closeDelim, stretchy: true });
  return { type: "mrow", children: wrapped };
}

function parseEnvironment(cur, display) {
  const envName = readRawBracedText(cur).trim();
  if (envName === "array") {
    skipWhitespace(cur);
    if (cur.src[cur.pos] === "{") readRawBracedText(cur);
  }
  const rows = [];
  if (cur.depth < MAX_PARSE_DEPTH) {
    cur.depth++;
    while (cur.pos < cur.len) {
      skipWhitespace(cur);
      if (peekCommandName(cur) === "end") break;
      const row = [];
      while (cur.pos < cur.len) {
        const cellNodes = parseSequence(cur, false, { stopCell: true });
        row.push(wrapNodes(cellNodes));
        skipWhitespace(cur);
        if (cur.src[cur.pos] === "&") {
          cur.pos++;
          continue;
        }
        break;
      }
      rows.push(row);
      skipWhitespace(cur);
      const nextCmd = peekCommandName(cur);
      if (nextCmd === "\\" || nextCmd === "cr") {
        readCommandName(cur);
        skipWhitespace(cur);
        if (cur.src[cur.pos] === "[") readOptionalBracket(cur, false);
        skipWhitespace(cur);
        if (peekCommandName(cur) === "end" || cur.pos >= cur.len) break;
        continue;
      }
      break;
    }
    cur.depth--;
  }
  skipWhitespace(cur);
  if (peekCommandName(cur) === "end") {
    readCommandName(cur);
    skipWhitespace(cur);
    if (cur.src[cur.pos] === "{") readRawBracedText(cur);
  }
  const table = {
    type: "mtable",
    className: envName === "cases" ? "math-cases" : undefined,
    rows,
  };
  const [openDelim, closeDelim] = MATRIX_DELIMS[envName] || [null, null];
  if (!openDelim && !closeDelim) return table;
  const children = [];
  if (openDelim) children.push({ type: "mo", value: openDelim, stretchy: true });
  children.push(table);
  if (closeDelim) children.push({ type: "mo", value: closeDelim, stretchy: true });
  return { type: "mrow", children };
}

function parseNotRelation(cur, display) {
  skipWhitespace(cur);
  if (cur.pos >= cur.len) return { type: "mo", value: "¬" };
  if (cur.src[cur.pos] === "=") {
    cur.pos++;
    return { type: "mo", value: "≠" };
  }
  if (cur.src[cur.pos] === "\\") {
    const next = peekCommandName(cur);
    if (next && NEGATED_RELATIONS[next]) {
      readCommandName(cur);
      return { type: "mo", value: NEGATED_RELATIONS[next] };
    }
  }
  const target = parseAtom(cur, display);
  if (!target) return { type: "mo", value: "¬" };
  return { type: "mrow", children: [{ type: "mo", value: "¬" }, target] };
}

function parseCommand(cur, display) {
  const cmd = readCommandName(cur);
  if (!cmd) return { type: "mtext", value: "\\" };

  if (Object.hasOwn(ESCAPED_SYMBOLS, cmd)) {
    return { type: "mo", value: ESCAPED_SYMBOLS[cmd] };
  }
  if (Object.hasOwn(SPACES, cmd)) {
    return { type: "mspace", width: SPACES[cmd] };
  }
  if (Object.hasOwn(GREEK, cmd)) {
    return { type: "mi", value: GREEK[cmd] };
  }
  if (Object.hasOwn(UPPER_GREEK, cmd)) {
    return { type: "mi", value: UPPER_GREEK[cmd], mathvariant: "normal" };
  }
  if (Object.hasOwn(ORD_SYMBOLS, cmd)) {
    return { type: "mi", value: ORD_SYMBOLS[cmd], mathvariant: "normal" };
  }
  if (Object.hasOwn(OPERATORS, cmd)) {
    return { type: "mo", value: OPERATORS[cmd] };
  }
  if (Object.hasOwn(BIG_OPS, cmd)) {
    const op = BIG_OPS[cmd];
    return { type: "mo", value: op.glyph, movable: op.movable, largeop: true };
  }
  if (Object.hasOwn(FUNCTIONS, cmd)) {
    const fn = FUNCTIONS[cmd];
    return { type: "mi", value: fn.label, mathvariant: "normal", movable: fn.movable };
  }
  if (cmd === "frac" || cmd === "dfrac" || cmd === "tfrac" || cmd === "cfrac") {
    const subDisplay = cmd === "dfrac" ? true : cmd === "tfrac" ? false : display;
    const num = readRequiredArg(cur, subDisplay);
    const den = readRequiredArg(cur, subDisplay);
    return {
      type: "mfrac",
      num,
      den,
      className: cmd === "dfrac" ? "math-dfrac" : cmd === "tfrac" ? "math-tfrac" : undefined,
    };
  }
  if (cmd === "binom") {
    const top = readRequiredArg(cur, false);
    const bot = readRequiredArg(cur, false);
    return {
      type: "mrow",
      children: [
        { type: "mo", value: "(", stretchy: true },
        { type: "mfrac", num: top, den: bot, linethickness: "0" },
        { type: "mo", value: ")", stretchy: true },
      ],
    };
  }
  if (cmd === "sqrt") {
    const rootIndex = readOptionalBracket(cur, false);
    const radicand = readRequiredArg(cur, display);
    return rootIndex ? { type: "mroot", base: radicand, index: rootIndex } : { type: "msqrt", base: radicand };
  }
  if (Object.hasOwn(ACCENTS, cmd)) {
    const acc = ACCENTS[cmd];
    const base = readRequiredArg(cur, display);
    return {
      type: "mover",
      base,
      over: { type: "mo", value: acc.glyph, stretchy: acc.stretchy },
      accent: !acc.stretchy,
    };
  }
  if (cmd === "underline") {
    const base = readRequiredArg(cur, display);
    return { type: "munder", base, under: { type: "mo", value: "_", stretchy: true } };
  }
  if (cmd === "overbrace") {
    const base = readRequiredArg(cur, display);
    return { type: "mover", base, over: { type: "mo", value: "⏞", stretchy: true } };
  }
  if (cmd === "underbrace") {
    const base = readRequiredArg(cur, display);
    return { type: "munder", base, under: { type: "mo", value: "⏟", stretchy: true } };
  }
  if (cmd === "overset") {
    const over = readRequiredArg(cur, false);
    const base = readRequiredArg(cur, display);
    return { type: "mover", base, over };
  }
  if (cmd === "underset") {
    const under = readRequiredArg(cur, false);
    const base = readRequiredArg(cur, display);
    return { type: "munder", base, under };
  }
  if (cmd === "left") {
    return parseLeftRight(cur, display);
  }
  if (cmd === "middle") {
    const delim = readDelimiter(cur);
    return delim ? { type: "mo", value: delim, stretchy: true } : { type: "mrow", children: [] };
  }
  if (cmd === "right") {
    const delim = readDelimiter(cur);
    return delim ? { type: "mo", value: delim } : { type: "mrow", children: [] };
  }
  if (SIZED_DELIM_COMMANDS.has(cmd)) {
    const delim = readDelimiter(cur);
    return delim ? { type: "mo", value: delim } : { type: "mrow", children: [] };
  }
  if (Object.hasOwn(TEXT_COMMANDS, cmd)) {
    const value = readRawBracedText(cur);
    return { type: "mtext", value, className: TEXT_COMMANDS[cmd] || undefined };
  }
  if (cmd === "operatorname") {
    skipWhitespace(cur);
    let movable = false;
    if (cur.src[cur.pos] === "*") {
      movable = true;
      cur.pos++;
    }
    const label = readRawBracedText(cur).trim();
    return { type: "mi", value: label, mathvariant: "normal", movable };
  }
  if (FONT_COMMANDS.has(cmd)) {
    const arg = readRequiredArg(cur, display);
    return applyFontVariant(arg, cmd);
  }
  if (cmd === "begin") {
    return parseEnvironment(cur, display);
  }
  if (cmd === "not") {
    return parseNotRelation(cur, display);
  }
  if (cmd === "pmod") {
    const arg = readRequiredArg(cur, false);
    return {
      type: "mrow",
      children: [
        { type: "mspace", width: "0.33em" },
        { type: "mo", value: "(" },
        { type: "mi", value: "mod", mathvariant: "normal" },
        { type: "mspace", width: "0.22em" },
        arg,
        { type: "mo", value: ")" },
      ],
    };
  }
  if (cmd === "displaystyle" || cmd === "textstyle" || cmd === "scriptstyle" || cmd === "noindent") {
    return { type: "mrow", children: [] };
  }
  return { type: "mtext", value: `\\${cmd}`, className: "math-unknown" };
}

export function parseLatex(tex, display = false) {
  const src = String(tex ?? "");
  if (src.length > MAX_TEX_LENGTH) {
    return [{ type: "mtext", value: src }];
  }
  const cur = { src, pos: 0, len: src.length, depth: 0 };
  return parseSequence(cur, display);
}

function renderMathNode(node, key) {
  switch (node.type) {
    case "mi":
      return (
        <mi key={key} mathvariant={node.mathvariant} className={node.className}>
          {node.value}
        </mi>
      );
    case "mn":
      return (
        <mn key={key} mathvariant={node.mathvariant} className={node.className}>
          {node.value}
        </mn>
      );
    case "mo":
      return (
        <mo
          key={key}
          stretchy={node.stretchy ? "true" : undefined}
          largeop={node.largeop ? "true" : undefined}
          className={node.className}
        >
          {node.value}
        </mo>
      );
    case "mtext":
      return (
        <mtext key={key} className={node.className}>
          {node.value}
        </mtext>
      );
    case "mspace":
      if (node.width?.startsWith("-")) {
        return <mspace key={key} style={{ marginLeft: node.width }} />;
      }
      return <mspace key={key} width={node.width} />;
    case "mrow":
      return (
        <mrow key={key} className={node.className}>
          {node.children.map((child, idx) => renderMathNode(child, idx))}
        </mrow>
      );
    case "msup":
      return (
        <msup key={key}>
          {renderMathNode(node.base, "b")}
          {renderMathNode(node.sup, "p")}
        </msup>
      );
    case "msub":
      return (
        <msub key={key}>
          {renderMathNode(node.base, "b")}
          {renderMathNode(node.sub, "s")}
        </msub>
      );
    case "msubsup":
      return (
        <msubsup key={key}>
          {renderMathNode(node.base, "b")}
          {renderMathNode(node.sub, "s")}
          {renderMathNode(node.sup, "p")}
        </msubsup>
      );
    case "mfrac":
      return (
        <mfrac key={key} linethickness={node.linethickness} className={node.className}>
          {renderMathNode(node.num, "n")}
          {renderMathNode(node.den, "d")}
        </mfrac>
      );
    case "msqrt":
      return <msqrt key={key}>{renderMathNode(node.base, "b")}</msqrt>;
    case "mroot":
      return (
        <mroot key={key}>
          {renderMathNode(node.base, "b")}
          {renderMathNode(node.index, "i")}
        </mroot>
      );
    case "mover":
      return (
        <mover key={key} accent={node.accent ? "true" : undefined}>
          {renderMathNode(node.base, "b")}
          {renderMathNode(node.over, "o")}
        </mover>
      );
    case "munder":
      return (
        <munder key={key}>
          {renderMathNode(node.base, "b")}
          {renderMathNode(node.under, "u")}
        </munder>
      );
    case "munderover":
      return (
        <munderover key={key}>
          {renderMathNode(node.base, "b")}
          {renderMathNode(node.under, "u")}
          {renderMathNode(node.over, "o")}
        </munderover>
      );
    case "mtable":
      return (
        <mtable key={key} className={node.className}>
          {node.rows.map((row, rIdx) => (
            <mtr key={rIdx}>
              {row.map((cell, cIdx) => (
                <mtd key={cIdx}>{renderMathNode(cell, "c")}</mtd>
              ))}
            </mtr>
          ))}
        </mtable>
      );
    default:
      return null;
  }
}

const LatexMath = memo(function LatexMath({ tex, display = false }) {
  const nodes = parseLatex(tex, display);
  return (
    <math display={display ? "block" : undefined}>
      {nodes.length === 1 ? renderMathNode(nodes[0], 0) : <mrow>{nodes.map((n, idx) => renderMathNode(n, idx))}</mrow>}
    </math>
  );
});

export default LatexMath;
