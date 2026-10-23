// Color mode: "light", "dark" or "auto" (follow the OS). Stored per browser and
// applied as <html data-theme>, which theme.css maps to color-scheme.
const KEY = "unison.theme";
export const MODES = ["light", "dark", "auto"];

export function getMode() {
  const mode = localStorage.getItem(KEY);
  return MODES.includes(mode) ? mode : "auto";
}

export function applyMode(mode) {
  document.documentElement.dataset.theme = mode;
  localStorage.setItem(KEY, mode);
}
