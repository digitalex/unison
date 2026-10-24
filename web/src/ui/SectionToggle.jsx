import { ChevronDown, ChevronRight } from "lucide-react";

// The heading button that minimizes or restores a section.
export default function SectionToggle({ open, onToggle, children }) {
  return (
    <button type="button" className="section-toggle" aria-expanded={open} onClick={() => onToggle(!open)}>
      {open ? <ChevronDown aria-hidden="true" /> : <ChevronRight aria-hidden="true" />}
      {children}
    </button>
  );
}
