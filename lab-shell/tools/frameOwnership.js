/*
  BR-AS09 — the shell frame owns no feature.

  A checkable rule rather than a review habit: `src/shell/**` is the frame, and
  the moment it imports a view, a plugin, a demo or a PrimeVue widget, the frame
  has grown a feature and the next plugin will need the same shortcut to be
  rendered. This is not hypothetical — the shell this replaced imported
  MenuView directly and routed to it by name.

  Lives outside `src/shell` on purpose: it reads the filesystem, which is
  itself a dependency the frame is not allowed to have.

  Run standalone (`node tools/frameOwnership.js`) or through the spec beside it.
*/

import { readdirSync, readFileSync, statSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const HERE = dirname(fileURLToPath(import.meta.url))
export const SHELL_DIR = resolve(HERE, '../src/shell')

/* Bare specifiers the frame may depend on: the framework it is written in, and
   nothing that renders a feature. PrimeVue is deliberately absent — a shell
   module reaching for a DataTable is building a screen. */
export const ALLOWED_PACKAGES = Object.freeze(['vue', 'vue-router', 'pinia'])

const IMPORT_PATTERN = /(?:^|\n)\s*(?:import|export)[\s\S]*?from\s+['"]([^'"]+)['"]/g
/* Comments come out first: these modules explain themselves at length, and a
   prose "from 'somewhere'" in a block comment is not an import — the first
   version of this check read one as an import of "this region exists at
   another major". */
const COMMENT_PATTERN = /\/\*[\s\S]*?\*\/|(?:^|\s)\/\/[^\n]*/g

export function importsOf(source) {
  return [...source.replace(COMMENT_PATTERN, '\n').matchAll(IMPORT_PATTERN)].map((m) => m[1])
}

export function shellSourceFiles(dir = SHELL_DIR) {
  return readdirSync(dir).flatMap((entry) => {
    const path = join(dir, entry)
    if (statSync(path).isDirectory()) return entry === 'node_modules' ? [] : shellSourceFiles(path)
    if (!path.endsWith('.js') || path.endsWith('.spec.js')) return []
    return [path]
  })
}

/**
 * @returns {{file: string, specifier: string, reason: string}[]} empty when the
 * frame is clean.
 */
export function frameViolations(file, source) {
  const violations = []
  for (const specifier of importsOf(source)) {
    if (specifier.startsWith('.')) {
      const target = resolve(dirname(file), specifier)
      if (!target.startsWith(`${SHELL_DIR}/`)) {
        violations.push({ file, specifier, reason: 'reaches outside the shell frame' })
      }
      continue
    }
    if (!ALLOWED_PACKAGES.includes(specifier)) {
      violations.push({ file, specifier, reason: 'is not a permitted frame dependency' })
    }
  }
  return violations
}

export function checkFrame() {
  return shellSourceFiles().flatMap((file) => frameViolations(file, readFileSync(file, 'utf8')))
}

/* Standalone entry: non-zero exit so a build or CI step can gate on it. */
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const violations = checkFrame()
  for (const v of violations) {
    console.error(`${relative(resolve(HERE, '..'), v.file)}: ${v.specifier} ${v.reason}`)
  }
  console.error(violations.length === 0 ? 'shell frame clean' : `${violations.length} violation(s)`)
  process.exit(violations.length === 0 ? 0 : 1)
}
