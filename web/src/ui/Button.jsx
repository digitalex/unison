import { Button as GraphicalButton } from "components/graphical";

// Unison Button: variants primary, secondary (outline is an alias), ghost and
// danger; `iconOnly` makes a square button for a single icon (give it an
// aria-label). Renders via Base UI Button; callers keep control of `type`.
export default function Button({ variant = "secondary", iconOnly = false, className = "", ...props }) {
  const classes = ["button", `button-${variant}`, iconOnly && "button-icon", className].filter(Boolean).join(" ");
  const mappedVariant = variant === "outline" ? "outline" : variant;
  return <GraphicalButton variant={mappedVariant} {...props} className={classes} />;
}
