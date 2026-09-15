// Every command this UI prints, checked against the binary that has to run it.
//
// The panels tell a reader to type things. A command with a flag the Go
// binary does not define fails in front of them with `flag provided but not
// defined`, and the lesson stops there. Worse, the two recorded measurements
// (drain.js, redelivery.js) print the commands that produced them — if a flag
// is ever renamed, those blocks become a claim about a run nobody can repeat.
//
// So this spec reads cqrs/main.go, which is where the one shared FlagSet and
// the subcommand switch both live, and holds the printed text to it. It is a
// text check, not an execution: running a pool needs a NATS server, and a unit
// suite that needed Docker would simply be skipped. The flags and subcommands
// are what drift; the server is not.

import { existsSync, readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

import { DRAIN_SOURCE } from './drain.js'
import { LESSONS, SEED_CMD, tabsFor } from './lessons.js'
import { REDELIVERY_SOURCE } from './redelivery.js'

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
const FLAGS = new Set(
  [...mainGo.matchAll(/fs\.(?:String|Int|Bool|Uint64|Duration)\("([a-z-]+)"/g)].map((m) => m[1]),
)

// Every command the UI shows, from wherever it shows it. The tab commands are
// read through tabsFor, the same function the panels call — a copy of the list
// here would go stale without failing.
const tabCmds = LESSONS.flatMap((l) => tabsFor(l.key))
  .map((t) => t?.cmd)
  .filter((c) => c?.startsWith('cqrs '))

const printed = [...DRAIN_SOURCE, ...REDELIVERY_SOURCE, SEED_CMD, ...tabCmds]

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
      expect(FLAGS.has(w.replace(/^-+/, ''))).toBe(true)
    }
  })

  it('gives every flag a value, because none of these are booleans but -drain', () => {
    for (const line of printed) {
      const words = line.trim().split(/\s+/)
      words.forEach((w, i) => {
        if (!w.startsWith('-') || w === '-drain') return
        expect(words[i + 1]).toBeDefined()
        expect(words[i + 1].startsWith('-')).toBe(false)
      })
    }
  })
})
