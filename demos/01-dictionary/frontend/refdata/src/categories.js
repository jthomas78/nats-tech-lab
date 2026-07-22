// Display metadata for the BR-D09 category vocabulary. The *keys* are the
// controlled vocabulary (domain/DB/API — see domain.ValidateCategory); only
// the labels here are presentation. Order and grouping match
// ARCHITECTURE-DICTIONARY.md § "Type Categories & Governance": `standards` is
// externally-owned reference data; the rest are owned by this platform's
// domain/frontends ("Domain" in the sidebar).
export const CATEGORY_ORDER = ['standards', 'domain-enum', 'ui-copy', 'config']

export const CATEGORY_LABEL = {
  standards: 'Reference Data',
  'domain-enum': 'Enums',
  'ui-copy': 'UI Strings',
  config: 'Configuration',
}

export const DOMAIN_CATEGORIES = ['domain-enum', 'ui-copy', 'config']

export function categoryLabel(key) {
  return CATEGORY_LABEL[key] || key
}
