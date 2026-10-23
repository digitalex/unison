# Unison

Generated from the current project, including edits awaiting autosave. Return to the [theme index](../themes.md). Install the selected fonts and icons using the setup below; assets are acquired separately from their official sources.

## Font and icon setup

Follow [asset installation](../assets.md) when implementing this theme. Download and configure the selected free assets from their official sources; no font files or icon artwork are bundled. Weights below are the authored requests: load matching static faces or a variable range and report any unavailable weight. Register the exact family aliases below, or map the role tokens to the loader’s actual family.

| Selected font | Roles | Weights | Family stack | Setup |
| --- | --- | --- | --- | --- |
| <code>Inter</code> (free) | brand, ui, editorial, data | 400, 500, 700 | <code>"Sidebar Inter", sans-serif</code> | Download and load [Inter](https://rsms.me/inter/) in the app. |

### Icons

Selected: [Lucide](https://lucide.dev/guide/react/getting-started), outlined. Install and wire the selected free pack (React: <code>lucide-react</code>); reuse it if already installed. Use the official adapter for other frameworks.

Preserve icon size, color, weight, style, and accessible names. Match the selected style only where the pack supports it; Lucide’s official pack supplies outlines, so report a filled-style gap rather than claiming exact filled artwork. Keep existing package versions and add only missing packages using the receiving project’s package manager.

## Foundations

```json
{
  "name": "Unison",
  "iconStyle": "outlined",
  "animation": {
    "duration": 150,
    "easing": [
      0.2,
      0.8,
      0.2,
      1
    ],
    "large": {
      "duration": 300,
      "easing": [
        0.16,
        1,
        0.3,
        1
      ]
    },
    "popupScale": 0.96,
    "pressDistance": 1
  },
  "spacing": {
    "zero": 0,
    "xxs": 4,
    "xs": 8,
    "s": 12,
    "m": 16,
    "l": 24,
    "xl": 32,
    "xxl": 48
  },
  "letterSpacing": {
    "xs": -0.02,
    "s": -0.01,
    "m": 0,
    "l": 0.01,
    "xl": 0.02
  },
  "text": {
    "xxs": {
      "size": 10,
      "lineHeight": 14
    },
    "xs": {
      "size": 12,
      "lineHeight": 16
    },
    "s": {
      "size": 14,
      "lineHeight": 20
    },
    "m": {
      "size": 16,
      "lineHeight": 24
    },
    "l": {
      "size": 24,
      "lineHeight": 32
    },
    "xl": {
      "size": 36,
      "lineHeight": 40
    },
    "xxl": {
      "size": 48,
      "lineHeight": 52
    }
  },
  "fonts": {
    "ui": {
      "family": "\"Sidebar Inter\", sans-serif",
      "weights": {
        "regular": 400,
        "medium": 500,
        "heavy": 700
      }
    },
    "brand": {
      "family": "\"Sidebar Inter\", sans-serif",
      "weights": {
        "regular": 400,
        "medium": 500,
        "heavy": 700
      }
    },
    "editorial": {
      "family": "\"Sidebar Inter\", sans-serif",
      "weights": {
        "regular": 400,
        "medium": 500,
        "heavy": 700
      }
    },
    "data": {
      "family": "\"Sidebar Inter\", sans-serif",
      "weights": {
        "regular": 400,
        "medium": 500,
        "heavy": 700
      }
    }
  },
  "radius": {
    "zero": 0,
    "xs": 4,
    "s": 8,
    "m": 12,
    "l": 24,
    "xl": 36,
    "full": 9999
  },
  "border": {
    "none": 0,
    "s": 1,
    "m": 1,
    "l": 2
  },
  "shadows": {
    "s": {
      "x": 0,
      "y": 1,
      "blur": 2,
      "spread": 0,
      "opacity": 0,
      "color": {
        "light": "neutral-10",
        "dark": "neutral-1"
      }
    },
    "m": {
      "x": 0,
      "y": 8,
      "blur": 24,
      "spread": 0,
      "opacity": 0,
      "color": {
        "light": "neutral-10",
        "dark": "neutral-1"
      }
    },
    "l": {
      "x": 0,
      "y": 12,
      "blur": 30,
      "spread": 0,
      "opacity": 0,
      "color": {
        "light": "neutral-10",
        "dark": "neutral-1"
      }
    }
  },
  "primaryForeground": {
    "light": "neutral-2",
    "dark": "neutral-2"
  },
  "primaryActionColor": "neutral-10",
  "neutralTone": "neutral",
  "colorEmphasis": 50,
  "iconFamily": "Lucide",
  "buttonRadius": "s"
}
```

## light CSS variables

Define these in the app’s existing theme scope for this mode. Keep component styles linked to the variables.

| Variable | Value |
| --- | --- |
| `--theme-name` | Unison |
| `--theme-icon-family` | Lucide |
| `--theme-icon-style` | outlined |
| `--theme-hugeicons-variant` | stroke-standard |
| `--theme-icon-stroke-width` | initial |
| `--theme-icon-thin-display` | initial |
| `--theme-icon-light-display` | initial |
| `--theme-icon-regular-display` | initial |
| `--theme-icon-bold-display` | initial |
| `--toolbar-divider-bleed` | 0 |
| `--focus-ring-outline` | initial |
| `--icon-stroke-width` | var(--theme-icon-stroke-width, 2) |
| `--icon-light-display` | var(--theme-icon-light-display, none) |
| `--icon-regular-display` | var(--theme-icon-regular-display, inline) |
| `--icon-bold-display` | var(--theme-icon-bold-display, none) |
| `--motion-duration` | 150ms |
| `--motion-easing` | cubic-bezier(0.2, 0.8, 0.2, 1) |
| `--motion-type` | easing |
| `--motion-visual-duration` | 0.15 |
| `--motion-bounce` | 0.2 |
| `--motion-enabled` | 1 |
| `--motion-small-iterations` | infinite |
| `--motion-large-duration` | 300ms |
| `--motion-large-easing` | cubic-bezier(0.16, 1, 0.3, 1) |
| `--motion-large-type` | easing |
| `--motion-large-visual-duration` | 0.3 |
| `--motion-large-bounce` | 0.2 |
| `--motion-large-iterations` | infinite |
| `--motion-popup-scale` | 0.96 |
| `--motion-press-distance` | 1px |
| `--option-badge-background` | color-mix(in srgb, var(--color-1) 10%, transparent) |
| `--option-badge-foreground` | #00afe8 |
| `--navigation-active-foreground` | #000000 |
| `--emphasis-chart-fill` | #00afe833 |
| `--emphasis-balance-background` | #f0f0f0 |
| `--emphasis-rewards-background` | #f0f0f0 |
| `--emphasis-icon-background` | #f0f0f0 |
| `--emphasis-icon-foreground` | #000000 |
| `--emphasis-type-background` | #f0f0f0 |
| `--emphasis-type-foreground` | #000000 |
| `--navigation-active-background` | #00afe833 |
| `--surface-raised-image` | none |
| `--surface-raised-shadow` | 0 0 0 0 transparent |
| `--surface-recessed-image` | none |
| `--surface-recessed-shadow` | 0 0 0 0 transparent |
| `--space-zero` | 0px |
| `--space-xxs` | 4px |
| `--space-xs` | 8px |
| `--space-s` | 12px |
| `--space-m` | 16px |
| `--space-l` | 24px |
| `--space-xl` | 32px |
| `--space-xxl` | 48px |
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
| `--line-xl` | 40px |
| `--letter-spacing-xl` | -0.01em |
| `--size-xxl` | 48px |
| `--line-xxl` | 52px |
| `--letter-spacing-xxl` | -0.02em |
| `--radius-zero` | 0px |
| `--radius-xs` | 4px |
| `--radius-s` | 8px |
| `--radius-m` | 12px |
| `--radius-l` | 24px |
| `--radius-xl` | 36px |
| `--radius-full` | 9999px |
| `--border-none` | 0px |
| `--border-s` | 1px |
| `--border-m` | 1px |
| `--border-l` | 2px |
| `--border-default-color` | #dedede33 |
| `--border-shadow-none` | 0 0 0 0 transparent |
| `--border-shadow-s` | 0 0 0 1px #dedede33 |
| `--border-shadow-m` | 0 0 0 1px #dedede33 |
| `--border-shadow-l` | 0 0 0 2px #dedede33 |
| `--font-ui` | "Sidebar Inter", sans-serif |
| `--weight-ui-regular` | 400 |
| `--weight-ui-medium` | 500 |
| `--weight-ui-heavy` | 700 |
| `--font-brand` | "Sidebar Inter", sans-serif |
| `--weight-brand-regular` | 400 |
| `--weight-brand-medium` | 500 |
| `--weight-brand-heavy` | 700 |
| `--font-editorial` | "Sidebar Inter", sans-serif |
| `--weight-editorial-regular` | 400 |
| `--weight-editorial-medium` | 500 |
| `--weight-editorial-heavy` | 700 |
| `--font-data` | "Sidebar Inter", sans-serif |
| `--weight-data-regular` | 400 |
| `--weight-data-medium` | 500 |
| `--weight-data-heavy` | 700 |
| `--color-none` | transparent |
| `--color-1` | #00afe8 |
| `--color-1-transparent` | #00afe833 |
| `--color-2` | #085e55 |
| `--color-2-transparent` | #085e5533 |
| `--color-3` | #ffed9b |
| `--color-3-transparent` | #ffed9b33 |
| `--color-4` | #ffb5aa |
| `--color-4-transparent` | #ffb5aa33 |
| `--neutral-1` | #ffffff |
| `--neutral-1-transparent` | #ffffff33 |
| `--neutral-2` | #fafafa |
| `--neutral-2-transparent` | #fafafa33 |
| `--neutral-3` | #f0f0f0 |
| `--neutral-3-transparent` | #f0f0f033 |
| `--neutral-4` | #dedede |
| `--neutral-4-transparent` | #dedede33 |
| `--neutral-5` | #c5c5c5 |
| `--neutral-5-transparent` | #c5c5c533 |
| `--neutral-6` | #969696 |
| `--neutral-6-transparent` | #96969633 |
| `--neutral-7` | #6c6c6c |
| `--neutral-7-transparent` | #6c6c6c33 |
| `--neutral-8` | #454545 |
| `--neutral-8-transparent` | #45454533 |
| `--neutral-9` | #252525 |
| `--neutral-9-transparent` | #25252533 |
| `--neutral-10` | #000000 |
| `--neutral-10-transparent` | #00000033 |
| `--success` | #000000 |
| `--success-transparent` | #00000033 |
| `--warning` | #000000 |
| `--warning-transparent` | #00000033 |
| `--error` | #000000 |
| `--error-transparent` | #00000033 |
| `--shadow-none` | none |
| `--shadow-s` | 0px 1px 2px 0px #00000000 |
| `--shadow-m` | 0px 8px 24px 0px #00000000 |
| `--shadow-l` | 0px 12px 30px 0px #00000000 |
| `--cte-canvas` | #ffffff |
| `--cte-surface` | #fafafa |
| `--cte-surface-muted` | #f0f0f0 |
| `--cte-text` | #000000 |
| `--cte-text-muted` | #6c6c6c |
| `--cte-border` | #dedede33 |
| `--cte-accent` | #00afe8 |
| `--cte-accent-text` | #fafafa |
| `--cte-danger` | #000000 |
| `--cte-focus` | #00afe8 |
| `--cte-font` | "Sidebar Inter", sans-serif |
| `--cte-font-size` | 14px |
| `--cte-font-weight` | 400 |
| `--cte-line-height` | 20px |
| `--cte-letter-spacing` | 0.01em |
| `--cte-detail-font-size` | 12px |
| `--cte-detail-line-height` | 16px |
| `--cte-detail-letter-spacing` | 0.01em |

## dark CSS variables

Define these in the app’s existing theme scope for this mode. Keep component styles linked to the variables.

| Variable | Value |
| --- | --- |
| `--theme-name` | Unison |
| `--theme-icon-family` | Lucide |
| `--theme-icon-style` | outlined |
| `--theme-hugeicons-variant` | stroke-standard |
| `--theme-icon-stroke-width` | initial |
| `--theme-icon-thin-display` | initial |
| `--theme-icon-light-display` | initial |
| `--theme-icon-regular-display` | initial |
| `--theme-icon-bold-display` | initial |
| `--toolbar-divider-bleed` | 0 |
| `--focus-ring-outline` | initial |
| `--icon-stroke-width` | var(--theme-icon-stroke-width, 2) |
| `--icon-light-display` | var(--theme-icon-light-display, none) |
| `--icon-regular-display` | var(--theme-icon-regular-display, inline) |
| `--icon-bold-display` | var(--theme-icon-bold-display, none) |
| `--motion-duration` | 150ms |
| `--motion-easing` | cubic-bezier(0.2, 0.8, 0.2, 1) |
| `--motion-type` | easing |
| `--motion-visual-duration` | 0.15 |
| `--motion-bounce` | 0.2 |
| `--motion-enabled` | 1 |
| `--motion-small-iterations` | infinite |
| `--motion-large-duration` | 300ms |
| `--motion-large-easing` | cubic-bezier(0.16, 1, 0.3, 1) |
| `--motion-large-type` | easing |
| `--motion-large-visual-duration` | 0.3 |
| `--motion-large-bounce` | 0.2 |
| `--motion-large-iterations` | infinite |
| `--motion-popup-scale` | 0.96 |
| `--motion-press-distance` | 1px |
| `--option-badge-background` | color-mix(in srgb, var(--color-1) 10%, transparent) |
| `--option-badge-foreground` | #00afe8 |
| `--navigation-active-foreground` | #ffffff |
| `--emphasis-chart-fill` | #00afe833 |
| `--emphasis-balance-background` | #2b2b2b |
| `--emphasis-rewards-background` | #2b2b2b |
| `--emphasis-icon-background` | #2b2b2b |
| `--emphasis-icon-foreground` | #ffffff |
| `--emphasis-type-background` | #2b2b2b |
| `--emphasis-type-foreground` | #ffffff |
| `--navigation-active-background` | #00afe833 |
| `--surface-raised-image` | none |
| `--surface-raised-shadow` | 0 0 0 0 transparent |
| `--surface-recessed-image` | none |
| `--surface-recessed-shadow` | 0 0 0 0 transparent |
| `--space-zero` | 0px |
| `--space-xxs` | 4px |
| `--space-xs` | 8px |
| `--space-s` | 12px |
| `--space-m` | 16px |
| `--space-l` | 24px |
| `--space-xl` | 32px |
| `--space-xxl` | 48px |
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
| `--line-xl` | 40px |
| `--letter-spacing-xl` | -0.01em |
| `--size-xxl` | 48px |
| `--line-xxl` | 52px |
| `--letter-spacing-xxl` | -0.02em |
| `--radius-zero` | 0px |
| `--radius-xs` | 4px |
| `--radius-s` | 8px |
| `--radius-m` | 12px |
| `--radius-l` | 24px |
| `--radius-xl` | 36px |
| `--radius-full` | 9999px |
| `--border-none` | 0px |
| `--border-s` | 1px |
| `--border-m` | 1px |
| `--border-l` | 2px |
| `--border-default-color` | #38383833 |
| `--border-shadow-none` | 0 0 0 0 transparent |
| `--border-shadow-s` | 0 0 0 1px #38383833 |
| `--border-shadow-m` | 0 0 0 1px #38383833 |
| `--border-shadow-l` | 0 0 0 2px #38383833 |
| `--font-ui` | "Sidebar Inter", sans-serif |
| `--weight-ui-regular` | 400 |
| `--weight-ui-medium` | 500 |
| `--weight-ui-heavy` | 700 |
| `--font-brand` | "Sidebar Inter", sans-serif |
| `--weight-brand-regular` | 400 |
| `--weight-brand-medium` | 500 |
| `--weight-brand-heavy` | 700 |
| `--font-editorial` | "Sidebar Inter", sans-serif |
| `--weight-editorial-regular` | 400 |
| `--weight-editorial-medium` | 500 |
| `--weight-editorial-heavy` | 700 |
| `--font-data` | "Sidebar Inter", sans-serif |
| `--weight-data-regular` | 400 |
| `--weight-data-medium` | 500 |
| `--weight-data-heavy` | 700 |
| `--color-none` | transparent |
| `--color-1` | #00afe8 |
| `--color-1-transparent` | #00afe833 |
| `--color-2` | #085e55 |
| `--color-2-transparent` | #085e5533 |
| `--color-3` | #ffed9b |
| `--color-3-transparent` | #ffed9b33 |
| `--color-4` | #ffb5aa |
| `--color-4-transparent` | #ffb5aa33 |
| `--neutral-1` | #000000 |
| `--neutral-1-transparent` | #00000033 |
| `--neutral-2` | #202020 |
| `--neutral-2-transparent` | #20202033 |
| `--neutral-3` | #2b2b2b |
| `--neutral-3-transparent` | #2b2b2b33 |
| `--neutral-4` | #383838 |
| `--neutral-4-transparent` | #38383833 |
| `--neutral-5` | #505050 |
| `--neutral-5-transparent` | #50505033 |
| `--neutral-6` | #777777 |
| `--neutral-6-transparent` | #77777733 |
| `--neutral-7` | #a3a3a3 |
| `--neutral-7-transparent` | #a3a3a333 |
| `--neutral-8` | #c6c6c6 |
| `--neutral-8-transparent` | #c6c6c633 |
| `--neutral-9` | #e5e5e5 |
| `--neutral-9-transparent` | #e5e5e533 |
| `--neutral-10` | #ffffff |
| `--neutral-10-transparent` | #ffffff33 |
| `--success` | #ffffff |
| `--success-transparent` | #ffffff33 |
| `--warning` | #ffffff |
| `--warning-transparent` | #ffffff33 |
| `--error` | #ffffff |
| `--error-transparent` | #ffffff33 |
| `--shadow-none` | none |
| `--shadow-s` | 0px 1px 2px 0px #00000000 |
| `--shadow-m` | 0px 8px 24px 0px #00000000 |
| `--shadow-l` | 0px 12px 30px 0px #00000000 |
| `--cte-canvas` | #000000 |
| `--cte-surface` | #202020 |
| `--cte-surface-muted` | #2b2b2b |
| `--cte-text` | #ffffff |
| `--cte-text-muted` | #a3a3a3 |
| `--cte-border` | #38383833 |
| `--cte-accent` | #00afe8 |
| `--cte-accent-text` | #202020 |
| `--cte-danger` | #ffffff |
| `--cte-focus` | #00afe8 |
| `--cte-font` | "Sidebar Inter", sans-serif |
| `--cte-font-size` | 14px |
| `--cte-font-weight` | 400 |
| `--cte-line-height` | 20px |
| `--cte-letter-spacing` | 0.01em |
| `--cte-detail-font-size` | 12px |
| `--cte-detail-line-height` | 16px |
| `--cte-detail-letter-spacing` | 0.01em |

## Authored component assignments

These are project edits. The [component reference](unison-components.md) includes the effective assignments with defaults and shared parts resolved.

```json
{
  "componentTokens": {},
  "componentVariants": {}
}
```
