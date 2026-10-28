# GraphicalUIKomore

Generated from the current project, including edits awaiting autosave. Return to the [theme index](../themes.md). Install the selected fonts and icons using the setup below; assets are acquired separately from their official sources.

## Font and icon setup

Follow [asset installation](../assets.md) when implementing this theme. Download and configure the selected free assets from their official sources; no font files or icon artwork are bundled. Weights below are the authored requests: load matching static faces or a variable range and report any unavailable weight. Register the exact family aliases below, or map the role tokens to the loader’s actual family.

| Selected font | Roles | Weights | Family stack | Setup |
| --- | --- | --- | --- | --- |
| <code>Geist</code> (free) | brand, ui, editorial | 300, 400, 600 | <code>"Sidebar Geist", sans-serif</code> | Download and load [Geist](https://vercel.com/font) in the app. |
| <code>Geist Mono</code> (free) | data | 300, 400, 600 | <code>"Sidebar Geist Mono", monospace</code> | Download and load [Geist Mono](https://vercel.com/font) in the app. |

### Icons

Selected: [Lucide](https://lucide.dev/guide/react/getting-started), outlined. Install and wire the selected free pack (React: <code>lucide-react</code>); reuse it if already installed. Use the official adapter for other frameworks.

Preserve icon size, color, weight, style, and accessible names. Match the selected style only where the pack supports it; Lucide’s official pack supplies outlines, so report a filled-style gap rather than claiming exact filled artwork. Keep existing package versions and add only missing packages using the receiving project’s package manager.

## Foundations

These foundations describe the captured theme resolved by the bundled GraphicalProvider. The name below is document.theme.name, an internal style identity used by the components; displayName is the project’s visible name. Preserve it and the provider’s resolved --theme-name even when they differ from each other or from the project’s display name. Use document.name for visible labels.

```json
{
  "name": "Grove",
  "text": {
    "l": {
      "size": 24,
      "lineHeight": 32
    },
    "m": {
      "size": 16,
      "lineHeight": 24
    },
    "s": {
      "size": 14,
      "lineHeight": 20
    },
    "xl": {
      "size": 36,
      "lineHeight": 39
    },
    "xs": {
      "size": 12,
      "lineHeight": 16
    },
    "xxl": {
      "size": 47,
      "lineHeight": 51
    },
    "xxs": {
      "size": 10,
      "lineHeight": 14
    }
  },
  "fonts": {
    "ui": {
      "family": "\"Sidebar Geist\", sans-serif",
      "weights": {
        "heavy": 600,
        "medium": 400,
        "regular": 300
      }
    },
    "data": {
      "family": "\"Sidebar Geist Mono\", monospace",
      "weights": {
        "heavy": 600,
        "medium": 400,
        "regular": 300
      }
    },
    "brand": {
      "family": "\"Sidebar Geist\", sans-serif",
      "weights": {
        "heavy": 600,
        "medium": 400,
        "regular": 300
      }
    },
    "editorial": {
      "family": "\"Sidebar Geist\", sans-serif",
      "weights": {
        "heavy": 600,
        "medium": 400,
        "regular": 300
      }
    }
  },
  "border": {
    "l": 2,
    "m": 1,
    "s": 1,
    "none": 0
  },
  "radius": {
    "l": 22,
    "m": 11,
    "s": 8,
    "xl": 33,
    "xs": 4,
    "full": 9999,
    "zero": 0
  },
  "shadows": {
    "l": {
      "x": 0,
      "y": 12,
      "blur": 30,
      "color": {
        "dark": "neutral-1",
        "light": "neutral-10"
      },
      "spread": 0,
      "opacity": 0
    },
    "m": {
      "x": 0,
      "y": 8,
      "blur": 24,
      "color": {
        "dark": "neutral-1",
        "light": "neutral-10"
      },
      "spread": 0,
      "opacity": 0
    },
    "s": {
      "x": 0,
      "y": 1,
      "blur": 2,
      "color": {
        "dark": "neutral-1",
        "light": "neutral-10"
      },
      "spread": 0,
      "opacity": 0
    }
  },
  "spacing": {
    "l": 23,
    "m": 15,
    "s": 12,
    "xl": 31,
    "xs": 8,
    "xxl": 46,
    "xxs": 4,
    "zero": 0
  },
  "animation": {
    "type": "easing",
    "large": {
      "type": "easing",
      "bounce": 0.3,
      "easing": [
        0.16,
        1,
        0.3,
        1
      ],
      "duration": 280,
      "visualDuration": 0.36
    },
    "bounce": 0.2,
    "easing": [
      0.16,
      1,
      0.3,
      1
    ],
    "duration": 160,
    "popupScale": 0.96,
    "pressDistance": 1,
    "visualDuration": 0.2
  },
  "iconStyle": "outlined",
  "iconFamily": "Lucide",
  "neutralTone": "warm",
  "colorEmphasis": 19.1943359375,
  "letterSpacing": {
    "l": 0.01,
    "m": 0,
    "s": -0.01,
    "xl": 0.02,
    "xs": -0.02
  },
  "primaryForeground": {
    "dark": "neutral-2",
    "light": "neutral-2"
  },
  "primaryActionColor": "neutral-10",
  "displayName": "GraphicalUIKomore"
}
```

## light CSS variables

GraphicalProvider already defines these variables for this mode and carries them into portalled components. Use this table to verify the integration. Preserve its internal --theme-name value; do not rename it to match the project title. Keep application styles linked to these variables.

| Variable | Value |
| --- | --- |
| `--theme-name` | Canvas |
| `--theme-icon-family` | Lucide |
| `--theme-icon-style` | outlined |
| `--theme-hugeicons-variant` | stroke-standard |
| `--theme-icon-stroke-width` | initial |
| `--theme-icon-thin-display` | initial |
| `--theme-icon-light-display` | initial |
| `--theme-icon-regular-display` | initial |
| `--theme-icon-bold-display` | initial |
| `--toolbar-divider-bleed` | 1 |
| `--focus-ring-outline` | 2px solid color-mix(in srgb, #000000 50%, transparent) |
| `--icon-stroke-width` | var(--theme-icon-stroke-width, 1.5) |
| `--icon-light-display` | var(--theme-icon-light-display, inline) |
| `--icon-regular-display` | var(--theme-icon-regular-display, none) |
| `--icon-bold-display` | var(--theme-icon-bold-display, none) |
| `--motion-duration` | 160ms |
| `--motion-easing` | cubic-bezier(0.16, 1, 0.3, 1) |
| `--motion-type` | easing |
| `--motion-visual-duration` | 0.16 |
| `--motion-bounce` | 0.2 |
| `--motion-enabled` | 1 |
| `--motion-small-iterations` | infinite |
| `--motion-large-duration` | 280ms |
| `--motion-large-easing` | cubic-bezier(0.16, 1, 0.3, 1) |
| `--motion-large-type` | easing |
| `--motion-large-visual-duration` | 0.28 |
| `--motion-large-bounce` | 0.3 |
| `--motion-large-iterations` | infinite |
| `--motion-popup-scale` | 0.96 |
| `--motion-press-distance` | 1px |
| `--option-badge-background` | #f2f0ec |
| `--option-badge-foreground` | #000000 |
| `--navigation-active-foreground` | #000000 |
| `--emphasis-chart-fill` | transparent |
| `--emphasis-balance-background` | #f2f0ec |
| `--emphasis-rewards-background` | #f2f0ec |
| `--emphasis-icon-background` | #f2f0ec |
| `--emphasis-icon-foreground` | #000000 |
| `--emphasis-type-background` | #f2f0ec |
| `--emphasis-type-foreground` | #000000 |
| `--navigation-active-background` | #f2f0ec |
| `--surface-raised-image` | none |
| `--surface-raised-shadow` | 0 0 0 0 transparent |
| `--surface-recessed-image` | none |
| `--surface-recessed-shadow` | 0 0 0 0 transparent |
| `--space-zero` | 0px |
| `--space-xxs` | 4px |
| `--space-xs` | 8px |
| `--space-s` | 12px |
| `--space-m` | 15px |
| `--space-l` | 23px |
| `--space-xl` | 31px |
| `--space-xxl` | 46px |
| `--tracking-xs` | -0.02em |
| `--tracking-s` | -0.01em |
| `--tracking-m` | 0em |
| `--tracking-l` | 0.01em |
| `--tracking-xl` | 0.02em |
| `--size-xxs` | 10px |
| `--line-xxs` | 14px |
| `--letter-spacing-xxs` | 0.02em |
| `--size-xs` | 12px |
| `--line-xs` | 16px |
| `--letter-spacing-xs` | 0.01em |
| `--size-s` | 14px |
| `--line-s` | 20px |
| `--letter-spacing-s` | 0.01em |
| `--size-m` | 16px |
| `--line-m` | 24px |
| `--letter-spacing-m` | 0em |
| `--size-l` | 24px |
| `--line-l` | 32px |
| `--letter-spacing-l` | -0.01em |
| `--size-xl` | 36px |
| `--line-xl` | 39px |
| `--letter-spacing-xl` | -0.01em |
| `--size-xxl` | 47px |
| `--line-xxl` | 51px |
| `--letter-spacing-xxl` | -0.02em |
| `--radius-zero` | 0px |
| `--radius-xs` | 4px |
| `--radius-s` | 8px |
| `--radius-m` | 11px |
| `--radius-l` | 22px |
| `--radius-xl` | 33px |
| `--radius-full` | 9999px |
| `--border-none` | 0px |
| `--border-s` | 1px |
| `--border-m` | 1px |
| `--border-l` | 2px |
| `--border-default-color` | rgb(0 0 0 / 0.1) |
| `--border-shadow-none` | 0 0 0 0 transparent |
| `--border-shadow-s` | 0 0 0 1px rgb(0 0 0 / 0.1) |
| `--border-shadow-m` | 0 0 0 1px rgb(0 0 0 / 0.1) |
| `--border-shadow-l` | 0 0 0 2px rgb(0 0 0 / 0.1) |
| `--font-ui` | "Sidebar Geist", sans-serif |
| `--weight-ui-regular` | 300 |
| `--weight-ui-medium` | 400 |
| `--weight-ui-heavy` | 600 |
| `--font-brand` | "Sidebar Geist", sans-serif |
| `--weight-brand-regular` | 300 |
| `--weight-brand-medium` | 400 |
| `--weight-brand-heavy` | 600 |
| `--font-editorial` | "Sidebar Geist", sans-serif |
| `--weight-editorial-regular` | 300 |
| `--weight-editorial-medium` | 400 |
| `--weight-editorial-heavy` | 600 |
| `--font-data` | "Sidebar Geist Mono", monospace |
| `--weight-data-regular` | 300 |
| `--weight-data-medium` | 400 |
| `--weight-data-heavy` | 600 |
| `--color-none` | transparent |
| `--color-1` | #262626 |
| `--color-1-transparent` | #26262633 |
| `--color-2` | #707070 |
| `--color-2-transparent` | #70707033 |
| `--color-3` | #b6b6b6 |
| `--color-3-transparent` | #b6b6b633 |
| `--color-4` | #e8e8e8 |
| `--color-4-transparent` | #e8e8e833 |
| `--neutral-1` | #ffffff |
| `--neutral-1-transparent` | #ffffff33 |
| `--neutral-2` | #fafaf9 |
| `--neutral-2-transparent` | #fafaf933 |
| `--neutral-3` | #f2f0ec |
| `--neutral-3-transparent` | #f2f0ec33 |
| `--neutral-4` | #e1ddd5 |
| `--neutral-4-transparent` | #e1ddd533 |
| `--neutral-5` | #cbc5b9 |
| `--neutral-5-transparent` | #cbc5b933 |
| `--neutral-6` | #9b968a |
| `--neutral-6-transparent` | #9b968a33 |
| `--neutral-7` | #716c61 |
| `--neutral-7-transparent` | #716c6133 |
| `--neutral-8` | #4a453b |
| `--neutral-8-transparent` | #4a453b33 |
| `--neutral-9` | #29241b |
| `--neutral-9-transparent` | #29241b33 |
| `--neutral-10` | #000000 |
| `--neutral-10-transparent` | #00000033 |
| `--success` | #76ef6b |
| `--success-transparent` | #76ef6b33 |
| `--warning` | #ffa344 |
| `--warning-transparent` | #ffa34433 |
| `--error` | #ff5263 |
| `--error-transparent` | #ff526333 |
| `--shadow-none` | none |
| `--shadow-s` | 0px 1px 2px 0px #00000000 |
| `--shadow-m` | 0px 8px 24px 0px #00000000 |
| `--shadow-l` | 0px 12px 30px 0px #00000000 |
| `--cte-canvas` | #ffffff |
| `--cte-surface` | #fafaf9 |
| `--cte-surface-muted` | #f2f0ec |
| `--cte-text` | #000000 |
| `--cte-text-muted` | #716c61 |
| `--cte-border` | rgb(0 0 0 / 0.1) |
| `--cte-accent` | #262626 |
| `--cte-accent-text` | #fafaf9 |
| `--cte-danger` | #ff5263 |
| `--cte-focus` | #262626 |
| `--cte-font` | "Sidebar Geist", sans-serif |
| `--cte-font-size` | 16px |
| `--cte-font-weight` | 300 |
| `--cte-line-height` | 24px |
| `--cte-letter-spacing` | 0em |
| `--cte-detail-font-size` | 16px |
| `--cte-detail-line-height` | 24px |
| `--cte-detail-letter-spacing` | 0em |

## dark CSS variables

GraphicalProvider already defines these variables for this mode and carries them into portalled components. Use this table to verify the integration. Preserve its internal --theme-name value; do not rename it to match the project title. Keep application styles linked to these variables.

| Variable | Value |
| --- | --- |
| `--theme-name` | Canvas |
| `--theme-icon-family` | Lucide |
| `--theme-icon-style` | outlined |
| `--theme-hugeicons-variant` | stroke-standard |
| `--theme-icon-stroke-width` | initial |
| `--theme-icon-thin-display` | initial |
| `--theme-icon-light-display` | initial |
| `--theme-icon-regular-display` | initial |
| `--theme-icon-bold-display` | initial |
| `--toolbar-divider-bleed` | 1 |
| `--focus-ring-outline` | 2px solid color-mix(in srgb, #ffffff 50%, transparent) |
| `--icon-stroke-width` | var(--theme-icon-stroke-width, 1.5) |
| `--icon-light-display` | var(--theme-icon-light-display, inline) |
| `--icon-regular-display` | var(--theme-icon-regular-display, none) |
| `--icon-bold-display` | var(--theme-icon-bold-display, none) |
| `--motion-duration` | 160ms |
| `--motion-easing` | cubic-bezier(0.16, 1, 0.3, 1) |
| `--motion-type` | easing |
| `--motion-visual-duration` | 0.16 |
| `--motion-bounce` | 0.2 |
| `--motion-enabled` | 1 |
| `--motion-small-iterations` | infinite |
| `--motion-large-duration` | 280ms |
| `--motion-large-easing` | cubic-bezier(0.16, 1, 0.3, 1) |
| `--motion-large-type` | easing |
| `--motion-large-visual-duration` | 0.28 |
| `--motion-large-bounce` | 0.3 |
| `--motion-large-iterations` | infinite |
| `--motion-popup-scale` | 0.96 |
| `--motion-press-distance` | 1px |
| `--option-badge-background` | #2c2b28 |
| `--option-badge-foreground` | #ffffff |
| `--navigation-active-foreground` | #ffffff |
| `--emphasis-chart-fill` | transparent |
| `--emphasis-balance-background` | #2c2b28 |
| `--emphasis-rewards-background` | #2c2b28 |
| `--emphasis-icon-background` | #2c2b28 |
| `--emphasis-icon-foreground` | #ffffff |
| `--emphasis-type-background` | #2c2b28 |
| `--emphasis-type-foreground` | #ffffff |
| `--navigation-active-background` | #2c2b28 |
| `--surface-raised-image` | none |
| `--surface-raised-shadow` | 0 0 0 0 transparent |
| `--surface-recessed-image` | none |
| `--surface-recessed-shadow` | 0 0 0 0 transparent |
| `--space-zero` | 0px |
| `--space-xxs` | 4px |
| `--space-xs` | 8px |
| `--space-s` | 12px |
| `--space-m` | 15px |
| `--space-l` | 23px |
| `--space-xl` | 31px |
| `--space-xxl` | 46px |
| `--tracking-xs` | -0.02em |
| `--tracking-s` | -0.01em |
| `--tracking-m` | 0em |
| `--tracking-l` | 0.01em |
| `--tracking-xl` | 0.02em |
| `--size-xxs` | 10px |
| `--line-xxs` | 14px |
| `--letter-spacing-xxs` | 0.02em |
| `--size-xs` | 12px |
| `--line-xs` | 16px |
| `--letter-spacing-xs` | 0.01em |
| `--size-s` | 14px |
| `--line-s` | 20px |
| `--letter-spacing-s` | 0.01em |
| `--size-m` | 16px |
| `--line-m` | 24px |
| `--letter-spacing-m` | 0em |
| `--size-l` | 24px |
| `--line-l` | 32px |
| `--letter-spacing-l` | -0.01em |
| `--size-xl` | 36px |
| `--line-xl` | 39px |
| `--letter-spacing-xl` | -0.01em |
| `--size-xxl` | 47px |
| `--line-xxl` | 51px |
| `--letter-spacing-xxl` | -0.02em |
| `--radius-zero` | 0px |
| `--radius-xs` | 4px |
| `--radius-s` | 8px |
| `--radius-m` | 11px |
| `--radius-l` | 22px |
| `--radius-xl` | 33px |
| `--radius-full` | 9999px |
| `--border-none` | 0px |
| `--border-s` | 1px |
| `--border-m` | 1px |
| `--border-l` | 2px |
| `--border-default-color` | rgb(0 0 0 / 0.1) |
| `--border-shadow-none` | 0 0 0 0 transparent |
| `--border-shadow-s` | 0 0 0 1px rgb(0 0 0 / 0.1) |
| `--border-shadow-m` | 0 0 0 1px rgb(0 0 0 / 0.1) |
| `--border-shadow-l` | 0 0 0 2px rgb(0 0 0 / 0.1) |
| `--font-ui` | "Sidebar Geist", sans-serif |
| `--weight-ui-regular` | 300 |
| `--weight-ui-medium` | 400 |
| `--weight-ui-heavy` | 600 |
| `--font-brand` | "Sidebar Geist", sans-serif |
| `--weight-brand-regular` | 300 |
| `--weight-brand-medium` | 400 |
| `--weight-brand-heavy` | 600 |
| `--font-editorial` | "Sidebar Geist", sans-serif |
| `--weight-editorial-regular` | 300 |
| `--weight-editorial-medium` | 400 |
| `--weight-editorial-heavy` | 600 |
| `--font-data` | "Sidebar Geist Mono", monospace |
| `--weight-data-regular` | 300 |
| `--weight-data-medium` | 400 |
| `--weight-data-heavy` | 600 |
| `--color-none` | transparent |
| `--color-1` | #e5e5e5 |
| `--color-1-transparent` | #e5e5e533 |
| `--color-2` | #b1b1b1 |
| `--color-2-transparent` | #b1b1b133 |
| `--color-3` | #777777 |
| `--color-3-transparent` | #77777733 |
| `--color-4` | #424242 |
| `--color-4-transparent` | #42424233 |
| `--neutral-1` | #000000 |
| `--neutral-1-transparent` | #00000033 |
| `--neutral-2` | #21201d |
| `--neutral-2-transparent` | #21201d33 |
| `--neutral-3` | #2c2b28 |
| `--neutral-3-transparent` | #2c2b2833 |
| `--neutral-4` | #393835 |
| `--neutral-4-transparent` | #39383533 |
| `--neutral-5` | #52504c |
| `--neutral-5-transparent` | #52504c33 |
| `--neutral-6` | #797773 |
| `--neutral-6-transparent` | #79777333 |
| `--neutral-7` | #a5a39f |
| `--neutral-7-transparent` | #a5a39f33 |
| `--neutral-8` | #c8c6c2 |
| `--neutral-8-transparent` | #c8c6c233 |
| `--neutral-9` | #e6e5e3 |
| `--neutral-9-transparent` | #e6e5e333 |
| `--neutral-10` | #ffffff |
| `--neutral-10-transparent` | #ffffff33 |
| `--success` | #76ef6b |
| `--success-transparent` | #76ef6b33 |
| `--warning` | #ffa344 |
| `--warning-transparent` | #ffa34433 |
| `--error` | #ff5263 |
| `--error-transparent` | #ff526333 |
| `--shadow-none` | none |
| `--shadow-s` | 0px 1px 2px 0px #00000000 |
| `--shadow-m` | 0px 8px 24px 0px #00000000 |
| `--shadow-l` | 0px 12px 30px 0px #00000000 |
| `--cte-canvas` | #000000 |
| `--cte-surface` | #21201d |
| `--cte-surface-muted` | #2c2b28 |
| `--cte-text` | #ffffff |
| `--cte-text-muted` | #a5a39f |
| `--cte-border` | rgb(0 0 0 / 0.1) |
| `--cte-accent` | #e5e5e5 |
| `--cte-accent-text` | #21201d |
| `--cte-danger` | #ff5263 |
| `--cte-focus` | #e5e5e5 |
| `--cte-font` | "Sidebar Geist", sans-serif |
| `--cte-font-size` | 16px |
| `--cte-font-weight` | 300 |
| `--cte-line-height` | 24px |
| `--cte-letter-spacing` | 0em |
| `--cte-detail-font-size` | 16px |
| `--cte-detail-line-height` | 24px |
| `--cte-detail-letter-spacing` | 0em |

## Authored component assignments

These are project edits. The [component reference](graphicaluikomore-components.md) includes the effective assignments with defaults and shared parts resolved.

```json
{
  "componentTokens": {
    "switch:default:part:row:rest": {
      "gap": "s"
    },
    "switch:default:part:label:rest": {
      "textSize": "m"
    },
    "checkbox:default:part:label:rest": {
      "textSize": "m"
    },
    "switch:default:part:control:rest": {
      "controlSize": "xl"
    }
  },
  "componentVariants": {}
}
```
