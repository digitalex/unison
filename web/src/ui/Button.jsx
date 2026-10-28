import { Button as GraphicalButton } from "components/graphical";

// The theme's buttons are 40px tall with 16px text; `size="small"` makes a
// compact one for toolbars and inline actions. The theme sets its sizing
// inline, so the small size is an inline style too.
const small = {
  height: 28,
  padding: "0 var(--space-s)",
  gap: "var(--space-xxs)",
  fontSize: "var(--size-s)",
  lineHeight: "20px",
};

// Unison Button: variants primary, secondary (outline is an alias), ghost and
// danger; `iconOnly` makes a square button for a single icon (give it an
// aria-label). Renders via Base UI Button; callers keep control of `type`.
export default function Button({ variant = "secondary", size, iconOnly = false, className = "", style, ...props }) {
  const classes = ["button", `button-${variant}`, iconOnly && "button-icon", className].filter(Boolean).join(" ");
  const mappedVariant = variant === "outline" ? "outline" : variant;
  const sized = size === "small" ? { ...small, ...style } : style;
  return <GraphicalButton variant={mappedVariant} {...props} style={sized} className={classes} />;
}
