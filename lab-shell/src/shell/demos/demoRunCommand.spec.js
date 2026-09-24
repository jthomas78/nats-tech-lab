/* Finding 4 (task 16k). The recovery command a reader is shown must start the
   WHOLE demo, not one piece of it.

   Demo 04's old `runCommand` was
   `docker compose -f demos/04-jetstream-cqrs/deploy/compose.yaml up -d`. That
   starts the NATS container and nothing else, so a reader who followed it was
   left with the same `unknown` the command was printed to fix: `cqrs serve`
   was still not listening on 20402, and the snapshotter and projector were
   still not filling the two KV buckets.

   The rule this spec holds is narrow on purpose. A `runCommand` is free-form
   text and the shell never runs it, so there is nothing to execute here. But
   when the command names a script IN THIS REPO, that script must exist and be
   runnable — the failure mode being guarded is a command that points at
   nothing, which is worse than a command that does too little. */
import { accessSync, constants, existsSync, readdirSync, readFileSync } from 'node:fs'
import { join, resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const repoRoot = resolve(process.cwd(), '..')
const demosDir = join(repoRoot, 'demos')

/** Every demo that ships the sibling metadata file, with it parsed. */
const withMetadata = readdirSync(demosDir, { withFileTypes: true })
  .filter((e) => e.isDirectory())
  .map((e) => ({ demo: e.name, file: join(demosDir, e.name, 'frontend', 'public', 'demo.json') }))
  .filter(({ file }) => existsSync(file))
  .map((d) => ({ ...d, metadata: JSON.parse(readFileSync(d.file, 'utf8')) }))

/* The first word of the command, if it looks like a repo-relative path. */
const scriptIn = (command) => {
  const first = String(command ?? '').trim().split(/\s+/)[0]
  return first.startsWith('demos/') || first.startsWith('./') ? first : null
}

describe('the recovery command a reader is shown', () => {
  it('is declared by at least one demo, so this spec is not vacuous', () => {
    expect(withMetadata.some((d) => d.metadata.runCommand)).toBe(true)
  })

  it.each(withMetadata.map((d) => [d.demo, d]))('%s names a script that exists and runs', (_name, d) => {
    const script = scriptIn(d.metadata.runCommand)
    if (script === null) return
    const path = join(repoRoot, script.replace(/^\.\//, ''))
    expect(existsSync(path), `${script} does not exist`).toBe(true)
    expect(() => accessSync(path, constants.X_OK), `${script} is not executable`).not.toThrow()
  })
})

/* The rest of this file is about demo 04 specifically, because it is the demo
   the finding was raised against and the only one with a readiness route. */
describe('demo 04\'s recovery command', () => {
  const demo04 = withMetadata.find((d) => d.demo === '04-jetstream-cqrs')
  const deploy = join(demosDir, '04-jetstream-cqrs', 'deploy')

  it('points at the start script, not at the compose file alone', () => {
    expect(demo04.metadata.runCommand).toBe('demos/04-jetstream-cqrs/deploy/start.sh')
  })

  /* The four processes `/readyz` needs before it can answer yes. The NATS
     container comes from compose; the other three are `cqrs` subcommands.
     The stream and both KV buckets are created by the binary itself — every
     subcommand runs `ensureStream` and `ensureKV` before it dispatches — so
     there is no separate init step to miss. */
  it.each(['compose.yaml', 'serve', 'snapshotter', 'projector'])('starts %s', (piece) => {
    expect(readFileSync(join(deploy, 'start.sh'), 'utf8')).toContain(piece)
  })

  it('can be reversed', () => {
    expect(existsSync(join(deploy, 'stop.sh'))).toBe(true)
    expect(() => accessSync(join(deploy, 'stop.sh'), constants.X_OK)).not.toThrow()
  })

  /* BR-AS78. The operator/visitor split is the demo's declaration to make;
     the shell only reads it. A command shown to a visitor would be noise. */
  it('keeps the declared audience it had', () => {
    expect(demo04.metadata.runCommand).toBeTruthy()
    expect(demo04.metadata.readiness).toBeTruthy()
  })
})
