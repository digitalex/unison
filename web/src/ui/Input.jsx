import { ChevronDown } from "lucide-react";

// Unison text field, textarea, and select controls (gui/components/unison/
// input.md and select.md). Native elements; all props pass through.
export function Input({ className = "", ...props }) {
  return <input {...props} className={`control ${className}`.trim()} />;
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
