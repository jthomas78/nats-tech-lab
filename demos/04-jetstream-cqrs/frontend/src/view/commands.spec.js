// Every command this UI prints, checked against the binary that has to run it.
//
// The panels tell a reader to type things. A command with a flag the Go
// binary does not define fails in front of them with `flag provided but not
// defined`, and the lesson stops there. Every Run button prints its command
// too (D2), so a renamed flag makes the button a claim about a run nobody can
// repeat by hand.
//
// So this spec reads cqrs/main.go, which is where the one shared FlagSet and
// the subcommand switch both live, and holds the printed text to it. It is a
// text check, not an execution: running a pool needs a NATS server, and a unit
// suite that needed Docker would simply be skipped. The flags and subcommands
// are what drift; the server is not.

import { existsSync, readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

import { BENCH_SIZES, benchCmd, LESSONS, POOL_RM_CMD, POOL_SEED_CMD, poolRunCmd, SEED_CMD, SHOWCASE_COMMANDS, tabsFor } from './lessons.js'

// Walk up to the demo folder rather than hard-coding a depth: vitest's root
// moves with the directory the runner is started from, and a wrong path here
// would throw, not pass quietly.
function findMainGo(from = process.cwd()) {
  let dir = from
  for (let i = 0; i < 8; i += 1) {
    const hit = resolve(dir, 'cqrs/main.go')
    if (existsSync(hit)) return hit
    const up = dirname(dir)
    if (up === dir) break
    dir = up
  }
  throw new Error('cqrs/main.go not found above ' + from)
}

const mainGo = readFileSync(findMainGo(), 'utf8')

const SUBCOMMANDS = new Set([...mainGo.matchAll(/case "([a-z-]+)":/g)].map((m) => m[1]))

// The booleans, read from the same file. `cqrs pool -rm` and `-drain` take no
// value, and Go rejects `-rm true` as a stray argument, so a spec that
// demanded one would be wrong about the binary rather than about the UI.
const BOOL_FLAGS = new Set([...mainGo.matchAll(/fs\.Bool\("([a-z-]+)"/g)].map((m) => m[1]))
const FLAGS = new Set(
  [...mainGo.matchAll(/fs\.(?:String|Int|Bool|Uint64|Duration)\("([a-z-]+)"/g)].map((m) => m[1]),
)

// Every command the UI shows, from wherever it shows it. The tab commands are
// read through tabsFor, the same function the panels call — a copy of the list
// here would go stale without failing.
const tabCmds = LESSONS.flatMap((l) => tabsFor(l.key))
  .map((t) => t?.cmd)
  .filter((c) => c?.startsWith('cqrs '))

// The seed button prints one of these under itself. A reader is invited to
// run it instead of pressing the button, so it has to parse.
const benchCmds = BENCH_SIZES.map(benchCmd)

// Lesson 02's Run buttons print a BUILT command, not a written one, so the
// guard has to exercise the builder rather than a string. Every shape the
// tabs ask for: a plain run, a capped run, one with an ack-wait, and one with
// a fault injected.
const runCmds = [
  poolRunCmd({ workers: 8 }),
  poolRunCmd({ workers: 8, maxPending: 1 }),
  poolRunCmd({ workers: 8, maxPending: 1000, ackWait: '30s' }),
  poolRunCmd({ workers: 8, maxPending: 1000, ackWait: '30s', killAt: 5000 }),
]

const printed = [SEED_CMD, POOL_SEED_CMD, POOL_RM_CMD, ...benchCmds, ...tabCmds, ...runCmds]

// A flag is printed either as `-name value` or as `-name=value`. Go's flag
// package requires the second form for a false boolean, so the guard has to
// read the name out of both.
function flagName(word) {
  return word.replace(/^-+/, '').split('=')[0]
}

describe('the commands this UI prints', () => {
  it('found the Go flag set, so a silent pass is not possible', () => {
    expect(SUBCOMMANDS.has('pool')).toBe(true)
    expect(FLAGS.size).toBeGreaterThan(5)
  })

  it('picked up the tab commands, not just the recorded runs', () => {
    expect(tabCmds.length).toBeGreaterThanOrEqual(4)
    expect(printed.length).toBeGreaterThan(9)
  })

  it.each(printed)('%s names a real subcommand and real flags', (line) => {
    const words = line.trim().split(/\s+/)
    expect(words[0]).toBe('cqrs')
    expect(SUBCOMMANDS.has(words[1])).toBe(true)
    for (const w of words.slice(2)) {
      if (!w.startsWith('-')) continue
      expect(FLAGS.has(flagName(w))).toBe(true)
    }
  })

  it('gives every flag that is not a boolean a value', () => {
    for (const line of printed) {
      const words = line.trim().split(/\s+/)
      words.forEach((w, i) => {
        // `-flag=value` carries its own value, and a boolean stands alone.
        // Which flags ARE boolean is read from main.go, not listed here: a
        // list would go stale the first time the binary grew one, and the
        // spec would then demand a value Go does not want.
        if (!w.startsWith('-') || BOOL_FLAGS.has(flagName(w)) || w.includes('=')) return
        expect(words[i + 1]).toBeDefined()
        expect(words[i + 1].startsWith('-')).toBe(false)
      })
    }
  })

  // `-snapshot=false` is not a style choice. Go's flag package cannot take a
  // false boolean as a separate word -- `-snapshot false` parses as
  // `-snapshot=true` followed by a stray argument, and the demo would then
  // measure the cheap mode while claiming to measure the expensive one.
  it('spells a false boolean with =, the only form Go parses', () => {
    for (const line of printed) {
      expect(line).not.toMatch(/-[a-z-]+ (true|false)\b/)
    }
  })
})

// These are NATS CLI commands, independent of the Go binary's FlagSet.
describe('Showcase names the terminal views for both stores and the live log', () => {
  it('prints both KV commands and stream view/info without the fixture', () => {
    expect(SHOWCASE_COMMANDS).toEqual({
      write: 'nats kv ls odometer-write',
      read: 'nats kv ls odometer-read',
      stream: 'nats stream view ODOMETER',
      info: 'nats stream info ODOMETER',
    })
  })
})
