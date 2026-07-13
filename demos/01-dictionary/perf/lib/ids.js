// Identifier helpers.

// Container natural key (ISO 6346) — must match ^TCKU[0-9]{7}$ (BR-016).
// 7 digits gives ~10M unique containers, far more than any load run needs.
export function containerID(n) {
  return 'TCKU' + String(n % 10000000).padStart(7, '0');
}

// Ship IDs are free-form kebab-case slugs (no server-side validation).
export function shipID(prefix, n) {
  return `${prefix}-${n}`;
}
