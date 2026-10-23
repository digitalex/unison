// Unison Button: variants primary, secondary (outline is an alias), ghost and
// danger; `iconOnly` makes a square button for a single icon (give it an
// aria-label). Renders a native <button>; callers keep control of `type`.
export default function Button({ variant = "secondary", iconOnly = false, className = "", ...props }) {
  const classes = ["button", `button-${variant}`, iconOnly && "button-icon", className].filter(Boolean).join(" ");
  return <button {...props} className={classes} />;
}
