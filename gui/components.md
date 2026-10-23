# Component specifications

Each theme below includes a focused specification for every component in Graphical's preview catalog. Specifications combine the preview's resolved values with anatomy, geometry, state, and composition rules. Authored refinements are part of the target, not optional suggestions. Read the relevant files when building individual components; read every entry when setting up the full library.

Both setup choices include all specifications. When the full library is selected, components/graphical/ also contains the actual preview components, their shared styling runtime, styles.css, and the current theme.ts snapshot. Use that implementation directly and follow the component setup in base-ui.md; specifications explain and verify it, not replace it. The theme-and-skills choice includes no component source or component dependency installation; build or style components as needed. Follow the selected setup in GUI.md when present.

For a full installation, track every component below as implemented, reused and verified, or blocked with a concrete reason, and provide its import path. Also check the receiving version's [official Base UI catalog](https://base-ui.com/llms.txt) for supported additions; disclose components without a Graphical preview specification instead of claiming a verified visual match. Utility providers and hooks are integration tools, not standalone visual components.

Preserve the supplied assets and resolved styles. Free fallbacks can change appearance and must be disclosed. Honor the receiving app's dependency and review policies; only claim visual parity for what was actually verified.

## Unison

[Theme foundations](themes/unison.md) · [Effective assignment tables](themes/unison-components.md)

- [Accordion](components/unison/accordion.md)
- [Alert dialog](components/unison/alert-dialog.md)
- [Autocomplete](components/unison/autocomplete.md)
- [Avatar](components/unison/avatar.md)
- [Button](components/unison/button.md)
- [Checkbox](components/unison/checkbox.md)
- [Checkbox group](components/unison/checkbox-group.md)
- [Collapsible](components/unison/collapsible.md)
- [Combobox](components/unison/combobox.md)
- [Context menu](components/unison/context-menu.md)
- [Dialog](components/unison/dialog.md)
- [Drawer](components/unison/drawer.md)
- [Field](components/unison/field.md)
- [Fieldset](components/unison/fieldset.md)
- [Form](components/unison/form.md)
- [Menu](components/unison/menu.md)
- [Menubar](components/unison/menubar.md)
- [Meter](components/unison/meter.md)
- [Navigation menu](components/unison/navigation-menu.md)
- [Number field](components/unison/number-field.md)
- [OTP field](components/unison/otp-field.md)
- [Popover](components/unison/popover.md)
- [Preview card](components/unison/preview-card.md)
- [Progress](components/unison/progress.md)
- [Radio](components/unison/radio.md)
- [Radio group](components/unison/radio-group.md)
- [Scroll area](components/unison/scroll-area.md)
- [Select](components/unison/select.md)
- [Separator](components/unison/separator.md)
- [Slider](components/unison/slider.md)
- [Switch](components/unison/switch.md)
- [Tabs](components/unison/tabs.md)
- [Text field](components/unison/input.md)
- [Toast](components/unison/toast.md)
- [Toggle](components/unison/toggle.md)
- [Toggle group](components/unison/toggle-group.md)
- [Toolbar](components/unison/toolbar.md)
- [Tooltip](components/unison/tooltip.md)
