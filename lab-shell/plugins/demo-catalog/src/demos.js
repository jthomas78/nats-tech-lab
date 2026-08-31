// Registry of lab demos. Intro markdown is the demo's own README so the
// shell never drifts from the demo docs.
import dictionaryIntro from '../../../../demos/01-dictionary/README.md?raw'

export const demos = [
  {
    id: '01-dictionary',
    title: 'Dictionary POC',
    description:
      'Reference data two ways: NATS KV as the read model vs. KV as a cache in front of a Postgres CQRS projection.',
    tags: ['JetStream', 'KV', 'Postgres', 'CQRS'],
    intro: dictionaryIntro,
    launchUrl: 'http://localhost:7100',
    composeDir: 'demos/01-dictionary',
  },
]

export function findDemo(id) {
  return demos.find((d) => d.id === id)
}
