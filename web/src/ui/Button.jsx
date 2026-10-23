// Unison Button: variants primary, secondary, outline, ghost, danger
// (gui/components/unison/button.md). Renders a native <button>; callers keep
// control of `type` and all other button props.
export default function Button({ variant = "secondary", className = "", ...props }) {
  return <button {...props} className={`button button-${variant} ${className}`.trim()} />;
}
