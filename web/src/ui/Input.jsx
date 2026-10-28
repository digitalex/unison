import { ChevronDown } from "lucide-react";
import { Input as GraphicalInput } from "components/graphical";

// Unison text field, textarea, and select controls (gui/components/graphicaluikomore/
// input.md and select.md). Native & Base UI elements; all props pass through.
export function Input({ className = "", ...props }) {
  return <GraphicalInput {...props} className={`control ${className}`.trim()} />;
}

export function Textarea({ className = "", ...props }) {
  return <textarea {...props} className={`control ${className}`.trim()} />;
}

export function Select({ className = "", ...props }) {
  return (
    <span className="select">
      <select {...props} className={`control ${className}`.trim()} />
      <ChevronDown aria-hidden="true" />
    </span>
  );
}
