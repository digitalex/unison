import test, { after, before } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { createServer } from "vite";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const webRoot = path.resolve(__dirname, "..");
const srcRoot = path.resolve(webRoot, "src");

let vite;
let GraphicalProvider;
let graphicalTheme;
let LatexMath;
let Markdown;
let MessageFoldModule;
let CopyLinkModule;
let WorkspaceModule;
let BacklogDialogModule;
let IdeasModule;
let ChatModule;
let MentionModule;

function collectSourceFiles(dir) {
  const out = [];
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) out.push(...collectSourceFiles(full));
    else if (/\.(?:js|jsx|ts|tsx)$/.test(entry.name)) out.push(full);
  }
  return out;
}

function renderWithTheme(element) {
  return renderToStaticMarkup(
    React.createElement(GraphicalProvider, { document: graphicalTheme, colorMode: "light" }, element),
  );
}

before(async () => {
  vite = await createServer({
    root: webRoot,
    logLevel: "error",
    server: { middlewareMode: true },
    appType: "custom",
  });
  const graphical = await vite.ssrLoadModule("components/graphical");
  GraphicalProvider = graphical.GraphicalProvider;
  graphicalTheme = graphical.theme;
  LatexMath = (await vite.ssrLoadModule("/src/math.jsx")).default;
  Markdown = (await vite.ssrLoadModule("/src/Markdown.jsx")).default;
  MessageFoldModule = await vite.ssrLoadModule("/src/MessageFold.jsx");
  CopyLinkModule = await vite.ssrLoadModule("/src/ui/CopyLinkButton.jsx");
  WorkspaceModule = await vite.ssrLoadModule("/src/Workspace.jsx");
  BacklogDialogModule = await vite.ssrLoadModule("/src/BacklogItemDialog.jsx");
  IdeasModule = await vite.ssrLoadModule("/src/Ideas.jsx");
  ChatModule = await vite.ssrLoadModule("/src/Chat.jsx");
  MentionModule = await vite.ssrLoadModule("/src/mentions.jsx");
});

after(async () => {
  await vite?.close();
});

test("zero dangerouslySetInnerHTML across web/src", () => {
  const files = collectSourceFiles(srcRoot);
  assert.ok(files.length > 0);
  for (const file of files) {
    const src = fs.readFileSync(file, "utf8");
    const stripped = src.replace(/\/\/.*$/gm, "").replace(/\/\*[\s\S]*?\*\//g, "");
    assert.equal(
      stripped.includes("dangerouslySetInnerHTML"),
      false,
      `Forbidden dangerouslySetInnerHTML found in ${path.relative(webRoot, file)}`,
    );
  }
});

test("zero React hooks placed after conditional return statements across web/src", async () => {
  const esbuild = await import("esbuild");
  const files = collectSourceFiles(srcRoot);
  const hookCallPattern = /\buse[A-Z][A-Za-z0-9_]*\s*\(/g;

  // Positive control: verify the detector catches both single-line and block early returns.
  assert.deepEqual(
    findHookAfterReturnViolations("function Bad(p) { if (!p) return null; const [x] = useState(0); return x; }", hookCallPattern),
    ["useState"],
  );
  assert.deepEqual(
    findHookAfterReturnViolations("function Bad2(p) { if (!p) { return null; } useEffect(() => {}, []); return null; }", hookCallPattern),
    ["useEffect"],
  );

  for (const file of files) {
    const raw = fs.readFileSync(file, "utf8");
    const { code } = await esbuild.transform(raw, {
      loader: file.endsWith("tsx") ? "tsx" : file.endsWith("ts") ? "ts" : "jsx",
      jsx: "transform",
    });
    const violations = findHookAfterReturnViolations(code, hookCallPattern);
    assert.deepEqual(
      violations,
      [],
      `Hook called after conditional return in ${path.relative(webRoot, file)}: ${violations.join(", ")}`,
    );
  }
});

function findHookAfterReturnViolations(code, hookRegex) {
  const cleaned = stripStringsAndComments(code);
  const violations = [];
  const fnHeader = /\bfunction\s+([A-Za-z0-9_$]+)?\s*\([^)]*\)\s*\{|\([^)]*\)\s*=>\s*\{/g;
  let match;
  while ((match = fnHeader.exec(cleaned)) !== null) {
    const openBrace = cleaned.indexOf("{", match.index);
    if (openBrace === -1) continue;
    let depth = 1;
    let pos = openBrace + 1;
    let sawConditionalReturn = false;
    while (pos < cleaned.length && depth > 0) {
      const ch = cleaned[pos];
      if (ch === "{") {
        depth++;
        pos++;
        continue;
      }
      if (ch === "}") {
        depth--;
        pos++;
        continue;
      }
      if (depth === 2 && /\breturn\b/.test(cleaned.slice(pos, pos + 7)) && (pos === 0 || !/[A-Za-z0-9_$]/.test(cleaned[pos - 1]))) {
        const beforeBrace = cleaned.slice(openBrace + 1, pos);
        const lastOpen = beforeBrace.lastIndexOf("{");
        const headerBefore = beforeBrace.slice(Math.max(0, lastOpen - 80), lastOpen);
        if (/\b(?:if|switch|for|while)\s*\([^)]*\)\s*$/.test(headerBefore) || /\belse\s*$/.test(headerBefore)) {
          sawConditionalReturn = true;
        }
      } else if (depth === 1 && /\bif\s*\(/.test(cleaned.slice(pos, pos + 10)) && (pos === 0 || !/[A-Za-z0-9_$]/.test(cleaned[pos - 1]))) {
        const singleLineIfReturn = cleaned.slice(pos).match(/^if\s*\([^;{]*\)\s*return\b/);
        if (singleLineIfReturn) {
          sawConditionalReturn = true;
          pos += singleLineIfReturn[0].length;
          continue;
        }
      } else if (depth === 1 && sawConditionalReturn) {
        hookRegex.lastIndex = pos;
        const hm = hookRegex.exec(cleaned);
        if (hm && hm.index === pos && (pos === 0 || !/[A-Za-z0-9_$.]/.test(cleaned[pos - 1]))) {
          violations.push(hm[0].replace(/\s*\($/, ""));
        }
      }
      pos++;
    }
  }
  return violations;
}

function stripStringsAndComments(src) {
  let out = "";
  let i = 0;
  while (i < src.length) {
    const c = src[i];
    if (c === "/" && src[i + 1] === "/") {
      while (i < src.length && src[i] !== "\n") {
        out += " ";
        i++;
      }
      continue;
    }
    if (c === "/" && src[i + 1] === "*") {
      out += "  ";
      i += 2;
      while (i < src.length && !(src[i] === "*" && src[i + 1] === "/")) {
        out += src[i] === "\n" ? "\n" : " ";
        i++;
      }
      if (i < src.length) {
        out += "  ";
        i += 2;
      }
      continue;
    }
    if (c === '"' || c === "'" || c === "`") {
      const quote = c;
      out += quote;
      i++;
      while (i < src.length && src[i] !== quote) {
        if (src[i] === "\\") {
          out += "  ";
          i += 2;
          continue;
        }
        out += src[i] === "\n" ? "\n" : " ";
        i++;
      }
      if (i < src.length) {
        out += quote;
        i++;
      }
      continue;
    }
    out += c;
    i++;
  }
  return out;
}

test("LatexMath renders fractions, roots, Greek, scripts, limits, blackboard bold, and matrices", () => {
  const fracHtml = renderToStaticMarkup(React.createElement(LatexMath, { tex: "\\frac{a + 1}{b - 1}" }));
  assert.match(fracHtml, /<math><mfrac><mrow><mi>a<\/mi><mo>\+<\/mo><mn>1<\/mn><\/mrow><mrow><mi>b<\/mi><mo>−<\/mo><mn>1<\/mn><\/mrow><\/mfrac><\/math>/);

  const rootHtml = renderToStaticMarkup(React.createElement(LatexMath, { tex: "\\sqrt{x^2 + 1} + \\sqrt[3]{8}" }));
  assert.ok(rootHtml.includes("<msqrt>"));
  assert.ok(rootHtml.includes("<mroot>"));
  assert.ok(rootHtml.includes("<mn>3</mn>"));

  const greekHtml = renderToStaticMarkup(
    React.createElement(LatexMath, { tex: "\\alpha + \\varepsilon + \\varphi + \\Gamma + \\Omega" }),
  );
  for (const sym of ["α", "ε", "φ", "Γ", "Ω"]) {
    assert.ok(greekHtml.includes(sym), `Missing Greek symbol ${sym} in ${greekHtml}`);
  }

  const inlineSum = renderToStaticMarkup(React.createElement(LatexMath, { tex: "\\sum_{i=1}^{n} x_i^2" }));
  assert.ok(inlineSum.includes("<msubsup>"), `Inline sum should use msubsup: ${inlineSum}`);

  const displaySum = renderToStaticMarkup(
    React.createElement(LatexMath, { tex: "\\sum_{i=1}^{n} x_i^2 = \\int_0^1 f'(t) \\, \\mathrm{d}t", display: true }),
  );
  assert.ok(displaySum.includes('<math display="block">'));
  assert.ok(displaySum.includes("<munderover>"), `Display sum should use munderover: ${displaySum}`);
  assert.ok(displaySum.includes("<msubsup>"), `Integral should use msubsup: ${displaySum}`);
  assert.ok(displaySum.includes("′"), `Prime should render ′: ${displaySum}`);

  const setsHtml = renderToStaticMarkup(
    React.createElement(LatexMath, { tex: "x \\in \\mathbb{R}^n, \\mathbb{N}, \\mathbb{Z}, \\mathbb{Q}, \\mathbb{C}, \\mathbb{E}, \\mathbb{P}" }),
  );
  for (const bb of ["ℝ", "ℕ", "ℤ", "ℚ", "ℂ", "𝔼", "ℙ", "∈"]) {
    assert.ok(setsHtml.includes(bb), `Missing ${bb} in ${setsHtml}`);
  }

  const matrixHtml = renderToStaticMarkup(
    React.createElement(LatexMath, {
      tex: "\\begin{pmatrix} a & b \\\\ c & d \\end{pmatrix}",
      display: true,
    }),
  );
  assert.ok(matrixHtml.includes("<mtable>"));
  assert.ok(matrixHtml.includes("<mtr><mtd><mi>a</mi></mtd><mtd><mi>b</mi></mtd></mtr>"));
  assert.ok(matrixHtml.includes('<mo stretchy="true">(</mo>'));
  assert.ok(matrixHtml.includes('<mo stretchy="true">)</mo>'));

  const subSupOrderA = renderToStaticMarkup(React.createElement(LatexMath, { tex: "x_i^2" }));
  const subSupOrderB = renderToStaticMarkup(React.createElement(LatexMath, { tex: "x^2_i" }));
  assert.equal(subSupOrderA, subSupOrderB, "x_i^2 and x^2_i must produce identical msubsup order");
  assert.match(subSupOrderA, /<msubsup><mi>x<\/mi><mi>i<\/mi><mn>2<\/mn><\/msubsup>/);

  const accentsAndDelims = renderToStaticMarkup(
    React.createElement(LatexMath, {
      tex: "\\hat{x} + \\bar{y} + \\vec{v} + \\tilde{z} + \\dot{q} + \\ddot{q} + \\overline{AB} + \\widehat{xyz} + \\left\\langle u, \\|v\\| \\right\\rangle + \\left. f \\right|_0^1",
    }),
  );
  assert.ok(accentsAndDelims.includes("<mover"));
  assert.ok(accentsAndDelims.includes("⟨"));
  assert.ok(accentsAndDelims.includes("⟩"));
  assert.ok(accentsAndDelims.includes("‖"));

  const casesAndFonts = renderToStaticMarkup(
    React.createElement(LatexMath, {
      tex: "\\mathcal{L}(x) = \\begin{cases} \\mathbf{x} & \\text{if } x \\ge 0 \\\\ \\sin(x) + \\ln(1 + \\exp(x)) & \\text{otherwise} \\end{cases}",
      display: true,
    }),
  );
  assert.ok(casesAndFonts.includes("ℒ"));
  assert.ok(casesAndFonts.includes('class="math-cases"'));
  assert.ok(casesAndFonts.includes("<mtext>if </mtext>"));
  assert.ok(casesAndFonts.includes("≥"));
  assert.ok(casesAndFonts.includes('<mi mathvariant="normal">sin</mi>'));

  const bigOpsDisplay = renderToStaticMarkup(
    React.createElement(LatexMath, {
      tex: "\\prod_{k=1}^n a_k + \\bigcup_{i=1}^n A_i + \\bigcap_{i=1}^n B_i + \\lim_{x \\to \\infty} f(x)",
      display: true,
    }),
  );
  assert.equal((bigOpsDisplay.match(/<munderover>/g) || []).length, 3);
  assert.ok(bigOpsDisplay.includes("<munder>"));
});

test("Markdown renders inline $...$, display $$...$$, currency guards, and malformed LaTeX safely", () => {
  const inlineHtml = renderToStaticMarkup(
    React.createElement(Markdown, { text: "Let $x = 5$ and $y = 10$ so $x + y = 15$." }),
  );
  assert.equal((inlineHtml.match(/class="md-math"/g) || []).length, 3);
  assert.ok(inlineHtml.includes("<math>"));

  const currencyRange = renderToStaticMarkup(
    React.createElement(Markdown, { text: "Plan costs $5 to $10 per seat, or $100 flat." }),
  );
  assert.equal(currencyRange.includes("<math"), false, `Currency should not render as math: ${currencyRange}`);
  assert.ok(currencyRange.includes("Plan costs $5 to $10 per seat, or $100 flat."));

  const mixedCurrencyAndMath = renderToStaticMarkup(
    React.createElement(Markdown, { text: "It costs $100 and $x = 5$." }),
  );
  assert.equal((mixedCurrencyAndMath.match(/class="md-math"/g) || []).length, 1);
  assert.ok(mixedCurrencyAndMath.includes("It costs $100 and "));

  const escapedDollar = renderToStaticMarkup(
    React.createElement(Markdown, { text: "Escaped \\$100 and \\$200 stay text." }),
  );
  assert.equal(escapedDollar.includes("<math"), false);
  assert.ok(escapedDollar.includes("Escaped $100 and $200 stay text."));

  const proseDigits = renderToStaticMarkup(
    React.createElement(Markdown, { text: "Pay $50 total$ at checkout." }),
  );
  assert.equal(proseDigits.includes("<math"), false);

  const blockMath = renderToStaticMarkup(
    React.createElement(Markdown, {
      text: "Before:\n\n$$\n\\frac{-b \\pm \\sqrt{b^2 - 4ac}}{2a}\n$$\n\nAfter.",
    }),
  );
  assert.ok(blockMath.includes('class="md-math-block"'));
  assert.ok(blockMath.includes('<math display="block">'));
  assert.ok(blockMath.includes("±"));

  const singleLineBlock = renderToStaticMarkup(
    React.createElement(Markdown, { text: "$$E = mc^2$$" }),
  );
  assert.ok(singleLineBlock.includes('class="md-math-block"'));

  const dualInlineDouble = renderToStaticMarkup(
    React.createElement(Markdown, { text: "Compare $$a^2$$ and $$b^2$$ inline." }),
  );
  assert.equal(dualInlineDouble.includes('class="md-math-block"'), false);
  assert.equal((dualInlineDouble.match(/class="md-math"/g) || []).length, 2);

  const nthTerm = renderToStaticMarkup(
    React.createElement(Markdown, { text: "Compute the $n$th term after `echo $1` for $1,000.50 to $2,500.00." }),
  );
  assert.equal((nthTerm.match(/class="md-math"/g) || []).length, 1);
  assert.ok(nthTerm.includes('<code class="md-code">echo $1</code>'));
  assert.ok(nthTerm.includes("$1,000.50 to $2,500.00"));

  for (const broken of ["$x^$", "$\\frac{1}$", "$\\sqrt[3$", "$\\left( x$", "$\\begin{pmatrix} 1 & 2$", "$\\unknown{z}$"]) {
    const safeHtml = renderToStaticMarkup(React.createElement(Markdown, { text: broken }));
    assert.ok(safeHtml.includes("<math>"), `Malformed math ${broken} should degrade cleanly: ${safeHtml}`);
  }
});

test("MessageFold registers a single shared selectionchange listener and expands matching fold", () => {
  const {
    default: MessageFold,
    registerActiveFold,
    handleFoldSelectionChange,
    activeFoldCount,
    isSelectionListenerAttached,
  } = MessageFoldModule;

  let added = 0;
  let removed = 0;
  const handlers = new Set();
  const mockDoc = {
    addEventListener(type, fn) {
      if (type === "selectionchange") {
        added++;
        handlers.add(fn);
      }
    },
    removeEventListener(type, fn) {
      if (type === "selectionchange") {
        removed++;
        handlers.delete(fn);
      }
    },
  };

  assert.equal(activeFoldCount(), 0);
  assert.equal(isSelectionListenerAttached(), false);
  assert.equal(handlers.size, 0);

  const nodeA = { name: "a" };
  const nodeB = { name: "b" };
  const nodeC = { name: "c" };
  let expandedA = false;
  let expandedB = false;
  let expandedC = false;

  const unregA = registerActiveFold(
    { getElement: () => ({ contains: (n) => n === nodeA }), expand: () => (expandedA = true) },
    mockDoc,
  );
  assert.equal(activeFoldCount(), 1);
  assert.equal(isSelectionListenerAttached(), true);
  assert.equal(added, 1);
  assert.equal(handlers.size, 1);

  const unregB = registerActiveFold(
    { getElement: () => ({ contains: (n) => n === nodeB }), expand: () => (expandedB = true) },
    mockDoc,
  );
  const unregC = registerActiveFold(
    { getElement: () => ({ contains: (n) => n === nodeC }), expand: () => (expandedC = true) },
    mockDoc,
  );
  assert.equal(activeFoldCount(), 3);
  assert.equal(added, 1, "Must attach only ONE shared selectionchange listener for multiple folds");
  assert.equal(handlers.size, 1);

  handleFoldSelectionChange(mockDoc, {
    getSelection: () => ({ isCollapsed: true, anchorNode: nodeB, focusNode: nodeB }),
  });
  assert.equal(expandedB, false, "Collapsed selection must not expand folded message");

  handleFoldSelectionChange(mockDoc, {
    getSelection: () => ({ isCollapsed: false, anchorNode: null, focusNode: nodeB }),
  });
  assert.equal(expandedA, false);
  assert.equal(expandedB, true, "Non-collapsed selection inside fold B must auto-expand fold B");
  assert.equal(expandedC, false);

  unregA();
  unregB();
  assert.equal(activeFoldCount(), 1);
  assert.equal(removed, 0, "Listener stays active while 1 collapsed fold remains");

  unregC();
  assert.equal(activeFoldCount(), 0);
  assert.equal(isSelectionListenerAttached(), false);
  assert.equal(removed, 1, "Listener must be removed once activeFolds drops to 0");
  assert.equal(handlers.size, 0);

  const shortHtml = renderToStaticMarkup(
    React.createElement(MessageFold, { text: "Short note" }, React.createElement("p", null, "Short note")),
  );
  assert.equal(shortHtml, "<p>Short note</p>");

  const longBody = Array.from({ length: 20 }, (_, i) => `Line ${i + 1}`).join("\n");
  const longHtml = renderToStaticMarkup(
    React.createElement(MessageFold, { text: longBody }, React.createElement("p", null, longBody)),
  );
  assert.ok(longHtml.includes('class="msg-fold"'));
});

test("CopyLinkButton, SidebarResizer clamping, Chat, and ItemDialog render cleanly", async () => {
  const { default: CopyLinkButton, resolveAbsoluteUrl, copyTextToClipboard } = CopyLinkModule;
  const {
    SIDEBAR_MIN,
    SIDEBAR_MAX,
    DEFAULT_SIDEBAR_WIDTH,
    clampSidebarWidth,
    parseStoredSidebarWidth,
    SidebarResizer,
  } = WorkspaceModule;
  const { ItemDialog } = BacklogDialogModule;
  const { default: Chat } = ChatModule;
  const { MentionProvider } = MentionModule;

  assert.equal(
    resolveAbsoluteUrl("/w/demo/chat#msg-42", "https://unison.example"),
    "https://unison.example/w/demo/chat#msg-42",
  );
  assert.equal(
    resolveAbsoluteUrl("/w/demo?item=280", "https://unison.example"),
    "https://unison.example/w/demo?item=280",
  );

  let copiedValue = null;
  const prevNav = globalThis.navigator;
  Object.defineProperty(globalThis, "navigator", {
    configurable: true,
    value: {
      clipboard: {
        writeText: async (v) => {
          copiedValue = v;
        },
      },
    },
  });
  try {
    const ok = await copyTextToClipboard("https://unison.example/w/demo?item=280");
    assert.equal(ok, true);
    assert.equal(copiedValue, "https://unison.example/w/demo?item=280");
  } finally {
    Object.defineProperty(globalThis, "navigator", { configurable: true, value: prevNav });
  }

  assert.equal(clampSidebarWidth(120), SIDEBAR_MIN);
  assert.equal(clampSidebarWidth(600), SIDEBAR_MAX);
  assert.equal(clampSidebarWidth(400, 660), 300, "Must leave at least MAIN_MIN=360px for .shell-main");
  assert.equal(parseStoredSidebarWidth(null), null);
  assert.equal(parseStoredSidebarWidth("invalid"), null);
  assert.equal(parseStoredSidebarWidth("280"), 280);
  assert.equal(parseStoredSidebarWidth("999"), SIDEBAR_MAX);

  const resizerHtml = renderToStaticMarkup(
    React.createElement(SidebarResizer, { shellRef: { current: null }, width: null, onWidth: () => {} }),
  );
  assert.ok(resizerHtml.includes('role="separator"'));
  assert.ok(resizerHtml.includes('aria-label="Resize sidebar"'));
  assert.ok(resizerHtml.includes(`aria-valuemin="${SIDEBAR_MIN}"`));
  assert.ok(resizerHtml.includes(`aria-valuemax="${SIDEBAR_MAX}"`));
  assert.ok(resizerHtml.includes(`aria-valuenow="${DEFAULT_SIDEBAR_WIDTH}"`));

  const copyBtnHtml = renderWithTheme(
    React.createElement(CopyLinkButton, {
      href: "/w/demo?item=280",
      label: "Copy link to #280",
      title: "Copy link to item",
    }),
  );
  assert.ok(copyBtnHtml.includes('aria-label="Copy link to #280"'));
  assert.ok(copyBtnHtml.includes('title="Copy link to item"'));

  const item = {
    id: 280,
    number: 280,
    workspace_id: "demo",
    title: "UI & rendering polish",
    body: "Supports inline math $E = mc^2$ cleanly.",
    status: "in_progress",
    created_at: Date.now() - 60000,
    created_by: 1,
    prs: [],
    needs: [],
    message_count: 0,
    comment_count: 0,
  };
  const participants = [{ id: 1, name: "Alex", emoji: "🦊" }];
  const dialogHtml = renderWithTheme(
    React.createElement(
      MentionProvider,
      { participants, items: [item], me: participants[0] },
      React.createElement(ItemDialog, {
        item,
        items: [item],
        workspace: { id: "demo", name: "Demo" },
        participants,
        me: participants[0],
        call: () => Promise.resolve([]),
        run: () => Promise.resolve(null),
        onClose: () => {},
      }),
    ),
  );
  assert.ok(dialogHtml.includes("item-dialog"));
  assert.ok(dialogHtml.includes('aria-label="Copy link to #280"'));
  assert.ok(dialogHtml.includes("<math>"));

  const chatHtml = renderWithTheme(
    React.createElement(
      MentionProvider,
      { participants, items: [item], me: participants[0] },
      React.createElement(Chat, {
        workspaceId: "demo",
        messages: [
          {
            id: 99,
            author_id: 1,
            body: "Check $\\alpha + \\beta$!",
            created_at: Date.now(),
            reactions: [],
          },
        ],
        activity: [],
        updateMessages: () => {},
        newAfter: 0,
        mention: null,
        onMentioned: () => {},
        items: [item],
        participants,
        me: participants[0],
        call: () => Promise.resolve([]),
        onError: () => {},
        shown: null,
      }),
    ),
  );
  assert.ok(chatHtml.includes('aria-label="Copy link to message"'));
  assert.ok(chatHtml.includes("α"));

  const ideasHtml = renderWithTheme(
    React.createElement(
      MentionProvider,
      { participants, items: [item], me: participants[0] },
      React.createElement(IdeasModule.default, {
        ideas: [{ id: 7, workspace_id: "demo", title: "Math rendering", body: "Try $\\pi$", status: "open", created_at: Date.now(), created_by: 1, comment_count: 0 }],
        setIdeas: () => {},
        participants,
        me: participants[0],
        call: () => Promise.resolve([]),
        onError: () => {},
      }),
    ),
  );
  assert.ok(ideasHtml.includes("Math rendering"));
});
