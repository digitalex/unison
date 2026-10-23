import { useState } from "react";
import { MODES, applyMode, getMode } from "../colorMode.js";

const LABELS = { light: "Light", dark: "Dark", auto: "Auto" };

// Light / Dark / Auto segmented switch.
export default function ThemeSwitch() {
  const [mode, setMode] = useState(getMode);
  const choose = (m) => {
    applyMode(m);
    setMode(m);
  };
  return (
    <div className="seg" role="group" aria-label="Theme">
      {MODES.map((m) => (
        <button key={m} type="button" aria-pressed={mode === m} onClick={() => choose(m)}>
          {LABELS[m]}
        </button>
      ))}
    </div>
  );
}
