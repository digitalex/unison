// The signed-in person's settings (GET/PUT /api/me/settings; `unison me defaults`),
// kept on the server so they apply on any machine.

import { useEffect, useState } from "react";
import { api } from "./api.js";
import { normalizeProviders } from "./providers.js";

// The setting holding the integrations to set up for new workspaces.
export const NEW_PROJECT_INTEGRATIONS = "new_project.integrations";

function integrationsOf(settings) {
  return normalizeProviders(settings?.[NEW_PROJECT_INTEGRATIONS] ?? []);
}

// Loads the integrations to set up for every new workspace (none unless set).
export async function loadNewProjectIntegrations() {
  return integrationsOf(await api("GET", "/api/me/settings"));
}

// Saves the integrations to set up for every new workspace, returning them.
export async function saveNewProjectIntegrations(keys) {
  return integrationsOf(
    await api("PUT", "/api/me/settings", {
      body: { [NEW_PROJECT_INTEGRATIONS]: normalizeProviders(keys) },
    }),
  );
}

// Returns the signed-in person's integrations for new workspaces, loaded when
// enabled turns true: none until they load, or if they can't be.
export function useNewProjectIntegrations(enabled) {
  const [keys, setKeys] = useState([]);
  useEffect(() => {
    if (!enabled) return;
    let live = true;
    loadNewProjectIntegrations().then(
      (loaded) => {
        if (live) setKeys(loaded);
      },
      () => {
        // Without defaults, the new workspace dialog starts as it always did.
      },
    );
    return () => {
      live = false;
    };
  }, [enabled]);
  return keys;
}
