import { useEffect, useState } from "react";
import { PageUnloadingError } from "../api.js";
import { PROVIDERS } from "../providers.js";
import { loadNewProjectIntegrations, saveNewProjectIntegrations } from "../userSettings.js";
import Checkbox from "../ui/Checkbox.jsx";

function messageOf(e) {
  return e instanceof Error ? e.message : String(e);
}

// Settings → My defaults: the signed-in person's own defaults, for every
// workspace they create (`unison me defaults`). Saved to their account so
// they apply on any machine and in the CLI.
export default function MyDefaults() {
  // The saved integrations, or null until they load.
  const [saved, setSaved] = useState(null);
  const [loadError, setLoadError] = useState("");
  const [saveError, setSaveError] = useState("");
  const [status, setStatus] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let live = true;
    loadNewProjectIntegrations().then(
      (keys) => {
        if (live) setSaved(keys);
      },
      (e) => {
        if (live && !(e instanceof PageUnloadingError)) setLoadError(messageOf(e));
      },
    );
    return () => {
      live = false;
    };
  }, []);

  const toggle = async (key, on) => {
    if (saved === null) return;
    const next = on ? [...saved, key] : saved.filter((k) => k !== key);
    setBusy(true);
    setSaveError("");
    setStatus("");
    try {
      setSaved(await saveNewProjectIntegrations(next));
      setStatus("Saved");
    } catch (e) {
      setSaveError(messageOf(e));
    } finally {
      setBusy(false);
    }
  };

  if (loadError) {
    return <p className="field-description">Couldn't load your defaults: {loadError}</p>;
  }
  return (
    <fieldset className="my-defaults" aria-busy={saved === null || busy}>
      <legend className="field-label">New projects</legend>
      <p className="field-description">Set these up for every workspace you create. All are off unless you turn them on.</p>
      {PROVIDERS.map((p) => (
        <Checkbox
          key={p.key}
          checked={saved?.includes(p.key) ?? false}
          disabled={saved === null || busy}
          onChange={(checked) => {
            void toggle(p.key, checked);
          }}
        >
          {p.label}
        </Checkbox>
      ))}
      {saveError && (
        <p className="field-error" role="alert">
          {saveError}
        </p>
      )}
      {status && !saveError && (
        <span className="muted" role="status">
          {status}
        </span>
      )}
    </fieldset>
  );
}
