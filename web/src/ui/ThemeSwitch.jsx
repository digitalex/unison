import { useState } from "react";
import { Monitor, Moon, Sun } from "lucide-react";
import { MODES, applyMode, getMode } from "../colorMode.js";

const LABELS = { light: "Light", dark: "Dark", auto: "Auto" };
const ICONS = { light: Sun, dark: Moon, auto: Monitor };

// Light / Dark / Auto segmented switch. Shows text labels on wide screens and
// icons on phones (see theme.css).
export default function ThemeSwitch() {
  const [mode, setMode] = useState(getMode);
  const choose = (m) => {
    applyMode(m);
    setMode(m);
  };
  return (
    <div className="seg" role="group" aria-label="Theme">
      {MODES.map((m) => {
        const Icon = ICONS[m];
        return (
          <button key={m} type="button" aria-pressed={mode === m} aria-label={LABELS[m]} title={LABELS[m]} onClick={() => choose(m)}>
            <Icon aria-hidden="true" />
            <span className="label">{LABELS[m]}</span>
          </button>
        );
      })}
    </div>
  );
}
