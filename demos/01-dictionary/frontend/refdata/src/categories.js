// Display metadata for the BR-D09 category vocabulary. The *keys* are the
// controlled vocabulary (domain/DB/API — see domain.ValidateCategory); only
// the labels here are presentation. Order and grouping match
// obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-DICTIONARY.md § "Type Categories & Governance": `standards` is
// externally-owned reference data; the rest are owned by this platform's
// domain/frontends ("Domain" in the sidebar).
export const CATEGORY_ORDER = ['standards', 'domain-enum', 'domain-string', 'config']

export const CATEGORY_LABEL = {
  standards: 'Reference Data',
  'domain-enum': 'Enums',
  'domain-string': 'Strings',
  config: 'Configuration',
}

export const DOMAIN_CATEGORIES = ['domain-enum', 'domain-string', 'config']

export function categoryLabel(key) {
  return CATEGORY_LABEL[key] || key
}
