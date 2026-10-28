# Component specifications

Each theme below includes a focused specification for every component in Graphical's preview catalog. Specifications combine the preview's resolved values with anatomy, geometry, state, and composition rules. Authored refinements are part of the target, not optional suggestions. Read the relevant files when building individual components; read every entry when setting up the full library.

Both setup choices include all specifications. When the full library is selected, components/graphical/ also contains the actual preview components, their shared styling runtime, styles.css, and the current theme.ts snapshot. Use that implementation directly and follow the component setup in base-ui.md; specifications explain and verify it, not replace it. The theme-and-skills choice includes no component source or component dependency installation; build or style components as needed. Follow the selected setup in GUI.md when present.

For a full installation, track every component below as implemented, reused and verified, or blocked with a concrete reason, and provide its import path. Also check the receiving version's [official Base UI catalog](https://base-ui.com/llms.txt) for supported additions; disclose components without a Graphical preview specification instead of claiming a verified visual match. Utility providers and hooks are integration tools, not standalone visual components.

Preserve the supplied assets and resolved styles. Free fallbacks can change appearance and must be disclosed. Honor the receiving app's dependency and review policies; only claim visual parity for what was actually verified.

## GraphicalUIKomore

[Theme foundations](themes/graphicaluikomore.md) · [Effective assignment tables](themes/graphicaluikomore-components.md)

- [Accordion](components/graphicaluikomore/accordion.md)
- [Alert dialog](components/graphicaluikomore/alert-dialog.md)
- [Autocomplete](components/graphicaluikomore/autocomplete.md)
- [Avatar](components/graphicaluikomore/avatar.md)
- [Button](components/graphicaluikomore/button.md)
- [Checkbox](components/graphicaluikomore/checkbox.md)
- [Checkbox group](components/graphicaluikomore/checkbox-group.md)
- [Collapsible](components/graphicaluikomore/collapsible.md)
- [Combobox](components/graphicaluikomore/combobox.md)
- [Context menu](components/graphicaluikomore/context-menu.md)
- [Dialog](components/graphicaluikomore/dialog.md)
- [Drawer](components/graphicaluikomore/drawer.md)
- [Field](components/graphicaluikomore/field.md)
- [Fieldset](components/graphicaluikomore/fieldset.md)
- [Form](components/graphicaluikomore/form.md)
- [Menu](components/graphicaluikomore/menu.md)
- [Menubar](components/graphicaluikomore/menubar.md)
- [Meter](components/graphicaluikomore/meter.md)
- [Navigation menu](components/graphicaluikomore/navigation-menu.md)
- [Number field](components/graphicaluikomore/number-field.md)
- [OTP field](components/graphicaluikomore/otp-field.md)
- [Popover](components/graphicaluikomore/popover.md)
- [Preview card](components/graphicaluikomore/preview-card.md)
- [Progress](components/graphicaluikomore/progress.md)
- [Radio](components/graphicaluikomore/radio.md)
- [Radio group](components/graphicaluikomore/radio-group.md)
- [Scroll area](components/graphicaluikomore/scroll-area.md)
- [Select](components/graphicaluikomore/select.md)
- [Separator](components/graphicaluikomore/separator.md)
- [Slider](components/graphicaluikomore/slider.md)
- [Switch](components/graphicaluikomore/switch.md)
- [Tabs](components/graphicaluikomore/tabs.md)
- [Text field](components/graphicaluikomore/input.md)
- [Toast](components/graphicaluikomore/toast.md)
- [Toggle](components/graphicaluikomore/toggle.md)
- [Toggle group](components/graphicaluikomore/toggle-group.md)
- [Toolbar](components/graphicaluikomore/toolbar.md)
- [Tooltip](components/graphicaluikomore/tooltip.md)
