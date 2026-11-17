import { Checkbox as GraphicalCheckbox } from "components/graphical";

// Unison checkbox (gui/components/graphicaluikomore/checkbox.md): a themed
// box with its label beside it. `onChange` gets the new checked state.
export default function Checkbox({ checked, onChange, disabled, children }) {
  return (
    <GraphicalCheckbox.Row>
      <GraphicalCheckbox.Root checked={checked} onCheckedChange={(next) => onChange(next)} disabled={disabled}>
        <GraphicalCheckbox.Indicator />
      </GraphicalCheckbox.Root>
      <GraphicalCheckbox.Label>{children}</GraphicalCheckbox.Label>
    </GraphicalCheckbox.Row>
  );
}
