// Unison Field (gui/components/unison/field.md): a visible label stacked above
// its control. The wrapping <label> keeps the control's accessible name.
export default function Field({ label, children }) {
  return (
    <label className="field">
      <span className="field-label">{label}</span>
      {children}
    </label>
  );
}
