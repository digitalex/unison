// The integration providers a workspace can be set up with, mirroring
// internal/store/providers.go (the keys are the API's).

// The known providers, in display order (store.ProviderKeys).
export const PROVIDERS = [
  { key: "isocan", label: "Isocan canvas" },
  { key: "github", label: "GitHub repo prompt" },
];

// Returns the known provider keys among keys, deduplicated and in PROVIDERS
// order, as the server stores them.
export function normalizeProviders(keys) {
  if (!Array.isArray(keys)) return [];
  return PROVIDERS.map((p) => p.key).filter((k) => keys.includes(k));
}
