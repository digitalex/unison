# GUI — GraphicalUIKomore design language

This file contains the saved theme and instructions for using it. The **Theme values** sections are readable, editable JSON and can restore the theme in Graphical. The accompanying `gui/` references describe component structure, behavior, and resolved styles; `skills/` contains workflows for using them. The full-component selection also includes Graphical’s actual themed component source and runtime. The default theme-and-skills selection includes guidance only and adds no runtime source or dependencies.

## Start here

Keep `GUI.md` and `gui/` at the project root. The install command puts the three skills in `.agents/skills/`; for a ZIP, copy the folders from `skills/` there, preserving existing files and local edits. Add this pointer to the project’s active agent instructions without replacing them: “For UI work, follow GUI.md and use graphical-ui, graphical-convert, or graphical-audit as appropriate.” Agents with other conventions can read these files directly.

Inspect the app’s styling, shared components, and consumers before making changes. Honor the selected setup recorded below and any scope the user already gave. Otherwise ask once: **“How would you like to apply this theme?”**

- **Convert the whole app:** update shared components and their usage across screens.
- **Convert selected areas:** establish the pages or components and contain shared changes using the app’s existing mechanisms.
- **Keep the current UI:** install the guidance and instruction pointer, then stop without changing styles or runtime dependencies.

Wait for a missing scope choice before changing application styles. A component request does not authorize whole-app conversion. These rules apply equally to prompt, terminal, and ZIP setup.

## Apply the theme

Use `graphical-convert` for existing UI, including its shadcn mapping when relevant. Use `graphical-ui` for new UI and future interface edits, even when the user does not mention Graphical. New screens use the current design system by default.

Implement the chosen scope in the app’s existing styling system and shared components. Use the values below, [foundations](gui/foundations.md), and each relevant [component specification](gui/components.md). Preserve authored component edits across parts, variants, sizes, states, modes, and portals. Global tokens alone do not complete conversion; an approximate preset or unchanged library defaults do not replace the supplied specifications.

Preserve content, behavior, routing, public component APIs, and intentional local customizations. After deliberate customization, the app’s active definitions take precedence over this downloaded snapshot. Preserve dependency versions, ranges, overrides, and lockfiles. Add only missing packages using the project’s package manager and version policy; verify APIs against installed versions and report incompatibilities instead of silently migrating dependencies.

### Set up all Base UI components

An explicit request for all Base UI components, including a recorded selection below, authorizes theme conversion, asset setup, and reusable modules across the existing app. Proceed without another scope question, honoring any narrower scope already supplied.

Integrate the bundled Graphical components and theme runtime; use their actual source rather than reconstructing the library from specifications. Import the bundled stylesheet, connect the theme provider to the supplied document and the app’s active color mode, and expose usable component imports. Reuse or adapt compatible existing shared components without discarding their public APIs or behavior. Use the [component specifications](gui/components.md) to verify the selected edits and [Base UI guidance](gui/base-ui.md) for integration. Check the receiving version’s [official catalog](https://base-ui.com/llms.txt) for additional primitives; use the same foundations where source is not supplied and disclose that coverage gap. Installing source files or dependencies alone does not complete setup until the components and existing app use the selected theme.

The source lives in `components/graphical/`. Import components, `GraphicalProvider`, and `theme` through its `index.ts`; `theme.ts` contains the captured document with the user’s component customizations. Import `components/graphical/styles.css` once at the framework’s supported stylesheet entry. Wrap the intended scope with `GraphicalProvider document={theme} colorMode={colorMode}`, using the app’s real light/dark state. Keep the bundled shared runtime with the components so their resolver, motion, and portal behavior remain connected. Configure the selected icons with `IconRendererProvider` under [Base UI integration](gui/base-ui.md), and fonts with the app’s loader; artwork and font files are not included. Do not silently leave Lucide active when a different free pack was selected.

Add only missing `@base-ui/react`, `motion`, and `lucide-react` packages required by the bundled source, using the receiving project’s package manager and version policy. Preserve its installed framework, React, dependency ranges, overrides, and lockfile decisions. Adapt source to supported APIs or report concrete incompatibilities; do not silently replace package versions. Install the selected fonts and icon pack separately under the asset instructions below. Guidance-only installation adds none of these runtime dependencies.

Before running an install command, check the receiving app’s installed APIs and dependency constraints. The supplied runtime uses React 19 context/provider and ref APIs and is verified against Base UI 1.7; this is a compatibility baseline, not a version-pinning requirement. Existing React 18 apps need compatible context/provider and ref-forwarding adaptations, or a concrete blocker report. Never upgrade existing dependencies automatically to make the source work.

## Rules for UI work

- Locate the active theme, mode, shared components, and nearby patterns first. Use existing tokens or verified aliases; report missing choices instead of inventing theme values. Keep named references at use sites and literals in token definitions. Typography includes family, weight, size, line height, and letter spacing.
- Reuse components and variants, then compose available primitives. New components fill missing capabilities using existing tokens. Consolidate equivalent roles without changing established values unless requested.
- Keep interfaces minimal: useful titles, content, and actions; no decorative eyebrows, filler subtitles, slogans, or redundant explanations. Default wrapping text to `text-wrap: pretty`.
- Treat grid tracks, breakpoints, content widths, aspect ratios, and positioning as structural choices. Preserve meaningful data encodings and external widgets in context.
- Preserve keyboard focus, labels, form values, interaction states, and theme propagation into portals. Use composed box shadows for edges with zero native border width, retaining opacity and the theme’s stroke placement.
- Fonts and icon artwork are acquired separately. Follow the selected assets and official links below. Preserve font names, weights, fallback stacks, and icon style/weight. Use licensed files when supplied; ask for missing files or their location before a fallback decision, honoring an existing answer. Use the documented free fallback only for unavailable paid selections, disclose substitutions, and explain how to restore licensed assets. System fonts need no download; report unresolved or gated sources without silently substituting.

## Verify and hand off

Finish conversion with `graphical-audit`: find and fix gaps within the agreed scope. Report each component as converted, already matches, intentionally preserved, or blocked, with evidence or a reason. For full library setup, also list module import paths and catalog coverage. Report actual checks, substitutions, and remaining gaps; source inspection alone does not establish rendered equivalence. Follow the receiving project’s testing and browser-review policy. A small edit does not trigger an app-wide audit.

## Focused references

| Need                                                   | Read                                                                      |
| ------------------------------------------------------ | ------------------------------------------------------------------------- |
| Token ownership, aliases, and deliberate theme changes | [Theme contract](gui/theme-contract.md)                                   |
| Hierarchy, composition, navigation, and controls       | [Interface judgment](gui/interface-judgment.md)                           |
| Colors, typography, spacing, effects, and motion       | [Foundations](gui/foundations.md)                                         |
| Asset installation and licensed fallbacks              | [Font and icon setup](gui/assets.md)                                      |
| Component anatomy, assignments, states, and modes      | [Component specifications](gui/components.md)                             |
| Primitives and existing libraries                      | [Base UI](gui/base-ui.md) · [Custom components](gui/custom-components.md) |
| Resolved CSS variables and component assignments       | [Theme references](gui/themes.md)                                         |
| Proportional checks                                    | [Verification](gui/verification.md)                                       |

| Task                                    | Skill               |
| --------------------------------------- | ------------------- |
| Build or edit UI with the current theme | `graphical-ui`      |
| Apply this theme to existing UI         | `graphical-convert` |
| Find and repair theme drift             | `graphical-audit`   |


## Selected setup for this download

The user selected **Install theme, skills, and all Base UI components**. The actual preview components, shared styling runtime, stylesheet, and current theme snapshot are included in `components/graphical/`. Use these files directly; do not recreate them from specifications. Follow [the component setup guide](gui/base-ui.md): import `components/graphical/styles.css` once, wrap the app with `GraphicalProvider` using the exported `theme`, and use the public components from `components/graphical/index.ts`. Apply the theme across existing app styles and components, preserving behavior and authored refinements. This is an explicit setup request: proceed without another conversion-scope question, honoring any narrower scope already supplied. Add only missing dependencies with the receiving package manager; preserve existing versions and report API incompatibilities. Install the chosen fonts and icons separately. Report component coverage, import paths, actual verification, and any visual or asset gaps. Copying files or installing Base UI alone is not completion.

## Fonts and icons

Follow [asset installation](gui/assets.md) when implementing this theme. Download and configure the selected free assets from their official sources; no font files or icon artwork are bundled. Weights below are the authored requests: load matching static faces or a variable range and report any unavailable weight. Register the exact family aliases below, or map the role tokens to the loader’s actual family.

| Selected font | Roles | Weights | Family stack | Setup |
| --- | --- | --- | --- | --- |
| <code>Geist</code> (free) | brand, ui, editorial | 300, 400, 600 | <code>"Sidebar Geist", sans-serif</code> | Download and load [Geist](https://vercel.com/font) in the app. |
| <code>Geist Mono</code> (free) | data | 300, 400, 600 | <code>"Sidebar Geist Mono", monospace</code> | Download and load [Geist Mono](https://vercel.com/font) in the app. |

### Icons

Selected: [Lucide](https://lucide.dev/guide/react/getting-started), outlined. Install and wire the selected free pack (React: <code>lucide-react</code>); reuse it if already installed. Use the official adapter for other frameworks.

Preserve icon size, color, weight, style, and accessible names. Match the selected style only where the pack supports it; Lucide’s official pack supplies outlines, so report a filled-style gap rather than claiming exact filled artwork. Keep existing package versions and add only missing packages using the receiving project’s package manager.

## Theme values

These sections contain the complete saved theme: both color modes, foundation values, and authored component edits. Import this GUI.md from Theme → Edit → Import theme to restore it. Shared palette catalogs and editor preferences are not part of the theme.

Lengths are pixels, letter spacing is em, motion durations are milliseconds, and opacity values are percentages. Text sizes, line heights, spacing, and radii render at the nearest whole pixel; the saved precision below preserves editing values. Color translucency defaults to 20% when omitted. Component edits use named tokens; the companion component specifications resolve inherited styles and behavior.

<!-- graphical-theme:start -->

### Identity

```json
{
  "format": "graphical-theme",
  "version": 1,
  "preset": "grove",
  "schemaVersion": 2,
  "name": "GraphicalUIKomore"
}
```

### Colors

```json
{
  "colors": {
    "dark": {
      "error": "#ff5263",
      "color-1": "#e5e5e5",
      "color-2": "#b1b1b1",
      "color-3": "#777777",
      "color-4": "#424242",
      "success": "#76ef6b",
      "warning": "#ffa344",
      "neutral-1": "#000000",
      "neutral-2": "#21201d",
      "neutral-3": "#2c2b28",
      "neutral-4": "#393835",
      "neutral-5": "#52504c",
      "neutral-6": "#797773",
      "neutral-7": "#a5a39f",
      "neutral-8": "#c8c6c2",
      "neutral-9": "#e6e5e3",
      "neutral-10": "#ffffff"
    },
    "light": {
      "error": "#ff5263",
      "color-1": "#262626",
      "color-2": "#707070",
      "color-3": "#b6b6b6",
      "color-4": "#e8e8e8",
      "success": "#76ef6b",
      "warning": "#ffa344",
      "neutral-1": "#ffffff",
      "neutral-2": "#fafaf9",
      "neutral-3": "#f2f0ec",
      "neutral-4": "#e1ddd5",
      "neutral-5": "#cbc5b9",
      "neutral-6": "#9b968a",
      "neutral-7": "#716c61",
      "neutral-8": "#4a453b",
      "neutral-9": "#29241b",
      "neutral-10": "#000000"
    }
  },
  "primaryForeground": { "dark": "neutral-2", "light": "neutral-2" },
  "primaryActionColor": "neutral-10",
  "neutralTone": "warm",
  "colorEmphasis": 19.1943359375,
  "keyColorPalette": [
    "#76ef6b",
    "#E0E709",
    "#00c9bb",
    "#009ff0",
    "#e864ff",
    "#262626",
    "#ffa344"
  ]
}
```

### Typography

```json
{
  "fonts": {
    "ui": {
      "family": "\"Sidebar Geist\", sans-serif",
      "weights": { "heavy": 600, "medium": 400, "regular": 300 }
    },
    "data": {
      "family": "\"Sidebar Geist Mono\", monospace",
      "weights": { "heavy": 600, "medium": 400, "regular": 300 }
    },
    "brand": {
      "family": "\"Sidebar Geist\", sans-serif",
      "weights": { "heavy": 600, "medium": 400, "regular": 300 }
    },
    "editorial": {
      "family": "\"Sidebar Geist\", sans-serif",
      "weights": { "heavy": 600, "medium": 400, "regular": 300 }
    }
  },
  "text": {
    "l": { "size": 23.697, "lineHeight": 31.596 },
    "m": { "size": 15.798, "lineHeight": 23.697 },
    "s": { "size": 13.823, "lineHeight": 19.748 },
    "xl": { "size": 35.546, "lineHeight": 39.495 },
    "xs": { "size": 11.848, "lineHeight": 15.798 },
    "xxl": { "size": 47.394, "lineHeight": 51.343 },
    "xxs": { "size": 9.874, "lineHeight": 13.823 }
  },
  "letterSpacing": { "l": 0.01, "m": 0, "s": -0.01, "xl": 0.02, "xs": -0.02 }
}
```

### Spacing and corners

```json
{
  "spacing": {
    "l": 23.199,
    "m": 15.466,
    "s": 11.599,
    "xl": 30.933,
    "xs": 7.733,
    "xxl": 46.399,
    "xxs": 3.866,
    "zero": 0
  },
  "radius": {
    "l": 22.13,
    "m": 11.33,
    "s": 7.54,
    "xl": 33.26,
    "xs": 3.75,
    "full": 9999,
    "zero": 0
  }
}
```

### Borders and effects

```json
{
  "border": { "l": 2, "m": 1, "s": 1, "none": 0 },
  "shadows": {
    "l": {
      "x": 0,
      "y": 12,
      "blur": 30,
      "color": { "dark": "neutral-1", "light": "neutral-10" },
      "spread": 0,
      "opacity": 0
    },
    "m": {
      "x": 0,
      "y": 8,
      "blur": 24,
      "color": { "dark": "neutral-1", "light": "neutral-10" },
      "spread": 0,
      "opacity": 0
    },
    "s": {
      "x": 0,
      "y": 1,
      "blur": 2,
      "color": { "dark": "neutral-1", "light": "neutral-10" },
      "spread": 0,
      "opacity": 0
    }
  }
}
```

### Motion

```json
{
  "animation": {
    "type": "easing",
    "large": {
      "type": "easing",
      "bounce": 0.3,
      "easing": [ 0.16, 1, 0.3, 1 ],
      "duration": 280,
      "visualDuration": 0.36
    },
    "bounce": 0.2,
    "easing": [ 0.16, 1, 0.3, 1 ],
    "duration": 160,
    "popupScale": 0.96,
    "pressDistance": 1,
    "visualDuration": 0.2
  }
}
```

### Icons

No separate icon weight is set; icons follow the surrounding text weight.

```json
{ "iconFamily": "Lucide", "iconStyle": "outlined" }
```

### Component edits

```json
{
  "componentTokens": {
    "switch:default:part:row:rest": { "gap": "s" },
    "switch:default:part:label:rest": { "textSize": "m" },
    "checkbox:default:part:label:rest": { "textSize": "m" },
    "switch:default:part:control:rest": { "controlSize": "xl" }
  },
  "componentVariants": {}
}
```

<!-- graphical-theme:end -->
