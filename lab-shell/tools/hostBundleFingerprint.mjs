#!/usr/bin/env node
/*
  The no-host-rebuild proof (BR-AS03, task 1b-8).

  The claim under test is narrow and worth stating precisely: *deploying a
  plugin does not change the shell*. Not "should not" — the check builds the
  host, fingerprints every emitted asset, and refuses if a byte moved.

  How a reviewer uses it (see BUSINESS_RULES-APP-SHELL.md § BR-AS03):

      node tools/hostBundleFingerprint.mjs --record        # before
      (cd plugins/example-plugin && edit something visible && npm run build)
      node tools/hostBundleFingerprint.mjs --verify        # after — must pass

  Between the two, the running shell shows the plugin's change on reload,
  having been rebuilt zero times. That is the whole argument, and it is a
  scripted check rather than a paragraph.

  The second assertion is the reason the first one holds: the host bundle
  contains no plugin's name, container or URL anywhere. If it did, the
  fingerprint would only be stable because nobody had added a plugin yet.
*/
import { createHash } from 'node:crypto'
import { execFileSync } from 'node:child_process'
import { readdirSync, readFileSync, statSync, writeFileSync, existsSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const shellRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const distDir = join(shellRoot, 'dist')
const fingerprintFile = join(shellRoot, 'tools', '.host-bundle-fingerprint.json')

/* Strings that must never appear in the host bundle. A plugin's identity is
   registry data at runtime; if the compiler saw it, the deployment story is
   already broken. */
const FORBIDDEN = [
  ...[7111, 7112, 7113, 7114, 7115].map((port) => `localhost:${port}`),
  'example_plugin', 'example-plugin', 'demo_catalog', 'demo-catalog',
  // The README belongs to the catalog build, never the host.
  'Dictionary POC', 'Admin UI layout — data flow top to bottom',
]

function walk(dir) {
  const out = []
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry)
    if (statSync(full).isDirectory()) out.push(...walk(full))
    else out.push(full)
  }
  return out.sort()
}

function build() {
  execFileSync('npx', ['vite', 'build'], { cwd: shellRoot, stdio: 'inherit' })
}

function fingerprint() {
  const files = walk(distDir)
  const entries = files.map((file) => ({
    path: relative(distDir, file),
    sha256: createHash('sha256').update(readFileSync(file)).digest('hex'),
  }))
  const combined = createHash('sha256')
  for (const entry of entries) combined.update(`${entry.path}:${entry.sha256}\n`)
  return { files: entries, digest: combined.digest('hex') }
}

function assertNoPluginNames() {
  const offenders = []
  for (const file of walk(distDir)) {
    if (!/\.(js|css|html|json)$/.test(file)) continue
    const text = readFileSync(file, 'utf8')
    for (const needle of FORBIDDEN) {
      if (text.includes(needle)) offenders.push(`${relative(distDir, file)} contains ${needle}`)
    }
  }
  if (offenders.length) {
    console.error('FAIL — the host bundle names a plugin (BR-AS03):')
    for (const line of offenders) console.error(`  ${line}`)
    process.exit(1)
  }
  console.log('ok — the host bundle names no plugin, container or remote URL')
}

const mode = process.argv[2] ?? '--record'

build()
assertNoPluginNames()
const current = fingerprint()

if (mode === '--record') {
  writeFileSync(fingerprintFile, `${JSON.stringify(current, null, 2)}\n`)
  console.log(`recorded ${current.files.length} host assets — digest ${current.digest}`)
  process.exit(0)
}

if (mode !== '--verify') {
  console.error('usage: hostBundleFingerprint.mjs [--record|--verify]')
  process.exit(2)
}

if (!existsSync(fingerprintFile)) {
  console.error('FAIL — nothing recorded. Run --record before the plugin is redeployed.')
  process.exit(1)
}

const recorded = JSON.parse(readFileSync(fingerprintFile, 'utf8'))
if (recorded.digest === current.digest) {
  console.log(`ok — host bundle unchanged across the plugin deployment (${current.digest})`)
  process.exit(0)
}

console.error('FAIL — the host bundle changed (BR-AS03):')
const before = new Map(recorded.files.map((f) => [f.path, f.sha256]))
const after = new Map(current.files.map((f) => [f.path, f.sha256]))
for (const [path, sha] of after) {
  if (!before.has(path)) console.error(`  added   ${path}`)
  else if (before.get(path) !== sha) console.error(`  changed ${path}`)
}
for (const path of before.keys()) if (!after.has(path)) console.error(`  removed ${path}`)
process.exit(1)
