// Left-nav icons (PrimeIcons, matching the pi-* icons already used across
// the app rather than introducing a separate icon set). Keyed by typeKey —
// a single per-category icon can't tell Country from Currency — falling
// back to a per-category icon for any type registered later that isn't in
// this map yet.
const TYPE_ICON = {
  country: 'pi-flag',
  currency: 'pi-dollar',
  incoterm: 'pi-truck',
  uom: 'pi-calculator',
  'hazard-class': 'pi-exclamation-triangle',
  'ship-status': 'pi-compass',
  string: 'pi-comment',
}

const CATEGORY_ICON = {
  standards: 'pi-tag',
  'domain-enum': 'pi-list',
  'domain-string': 'pi-comment',
  config: 'pi-cog',
}

export function typeIcon(typeKey, category) {
  return TYPE_ICON[typeKey] || CATEGORY_ICON[category || 'standards'] || 'pi-tag'
}

export function categoryIcon(categoryKey) {
  return CATEGORY_ICON[categoryKey] || 'pi-list'
}
