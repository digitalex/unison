// Color mode: "light", "dark" or "auto" (follow the OS). Stored per browser and
// applied as <html data-theme>, which theme.css maps to color-scheme.
const KEY = "unison.theme";
export const MODES = ["light", "dark", "auto"];

export function getMode() {
  const mode = localStorage.getItem(KEY);
  return MODES.includes(mode) ? mode : "auto";
}

const listeners = new Set();

export function applyMode(mode) {
  document.documentElement.dataset.theme = mode;
  localStorage.setItem(KEY, mode);
  for (const fn of listeners) fn(mode);
}

// subscribeMode keeps multiple mounted ThemeSwitch instances in sync across
// in-tab updates and cross-tab storage events (#321).
export function subscribeMode(fn) {
  listeners.add(fn);
  const onStorage = (e) => {
    if (e.key !== KEY) return;
    const mode = getMode();
    document.documentElement.dataset.theme = mode;
    fn(mode);
  };
  window.addEventListener("storage", onStorage);
  return () => {
    listeners.delete(fn);
    window.removeEventListener("storage", onStorage);
  };
}
