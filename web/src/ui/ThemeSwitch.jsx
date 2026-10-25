import { useState } from "react";
import { Monitor, Moon, Sun } from "lucide-react";
import { MODES, applyMode, getMode } from "../colorMode.js";

const LABELS = { light: "Light", dark: "Dark", auto: "Auto" };
const ICONS = { light: Sun, dark: Moon, auto: Monitor };

// Light / Dark / Auto. Wide screens get a segmented switch with text labels;
// phones get a single button that shows the current mode and cycles to the
// next one on each tap (see theme.css), so the top bar stays narrow.
export default function ThemeSwitch() {
  const [mode, setMode] = useState(getMode);
  const choose = (m) => {
    applyMode(m);
    setMode(m);
  };
  const next = MODES[(MODES.indexOf(mode) + 1) % MODES.length];
  const Current = ICONS[mode];
  return (
    <>
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
      <button
        type="button"
        className="button button-ghost button-icon theme-cycle"
        aria-label={`Theme: ${LABELS[mode]}. Switch to ${LABELS[next]}`}
        title={`Theme: ${LABELS[mode]} (tap for ${LABELS[next]})`}
        onClick={() => choose(next)}
      >
        <Current aria-hidden="true" />
      </button>
    </>
  );
}
