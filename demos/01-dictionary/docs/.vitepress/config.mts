import { defineConfig, type DefaultTheme } from 'vitepress'
import container from 'markdown-it-container'

const architectureSidebar: DefaultTheme.SidebarItem[] = [
  {
    text: 'Architecture',
    items: [
      { text: 'Overview', link: '/architecture/' },
      { text: 'CQRS Shapes', link: '/architecture/cqrs-shapes' },
      {
        text: 'NATS',
        collapsed: true,
        items: [
          { text: 'Overview', link: '/nats/' },
          { text: 'JetStream + KV Mechanics', link: '/nats/jetstream-kv-design' },
          { text: 'Modeling & Identity', link: '/nats/modeling-and-identity' },
          { text: 'Source of Truth', link: '/nats/source-of-truth' },
          { text: 'Projection Shapes', link: '/nats/projection-shapes' },
          { text: 'Write-Side Safety', link: '/nats/write-side-safety' },
          { text: 'Performance & Pitfalls', link: '/nats/performance-and-pitfalls' },
        ],
      },
      { text: 'Dictionary (Reference Data)', link: '/architecture/dictionary' },
      { text: 'Communications', link: '/architecture/communications' },
      { text: 'Accounts', link: '/architecture/accounts' },
      { text: 'Admin', link: '/architecture/admin' },
      { text: 'Platform (Tech Lab Operator)', link: '/architecture/platform' },
    ],
  },
]

// Proposed Linebooker V3 architecture series. Kept in its own sidebar, separate
// from `architectureSidebar`, because the rest of this site documents what the
// lab BUILT and this series is a PROPOSAL. Governed by
// Proposed-Linebooker-V3-Architecture-Authority.md. Order entries by level, then
// by stable ID.
//
// The tree mirrors the authority's canonical catalogue hierarchy exactly - L0
// contains L1, L1 contains the two L2s, each L2 contains its L3 concern views -
// so the sidebar and the catalogue can never disagree about parentage. Nest a
// new document under its catalogue parent; do not flatten it up a level to make
// it easier to reach.
//
// An entry carries a `link` only when that document has a WRITTEN edition on
// this site. A document whose drawn and print editions exist but whose prose
// has not been written is listed WITHOUT a link and suffixed "(drawing only)",
// because the reader needs the shape of the series even where a page is
// missing, and a link to a page that does not exist reads as a broken site.
// Give an entry its link in the same change that adds its Markdown page.
const v3ArchitectureSidebar: DefaultTheme.SidebarItem[] = [
  {
    text: 'Proposed Linebooker V3',
    items: [
      {
        text: 'L0-01 \u00b7 Architecture Atlas',
        link: '/v3-architecture/l0-architecture-atlas',
        collapsed: false,
        items: [
          {
            text: 'L1-01 \u00b7 System and Platform Overview',
            link: '/v3-architecture/l1-system-platform-overview',
            collapsed: false,
            items: [
              {
                text: 'L2-01 \u00b7 Logical and Technical Architecture',
                link: '/v3-architecture/l2-logical-technical-architecture',
              },
              {
                text: 'L2-02 \u00b7 Technology Selection and Rationale',
                link: '/v3-architecture/l2-technology-selection-rationale',
              },
            ],
          },
        ],
      },
    ],
  },
]

export default defineConfig({
  title: 'Dictionary POC Docs',
  description: 'NATS Tech Lab — Dictionary POC architecture & reference docs',
  cleanUrls: true,
  themeConfig: {
    nav: [
      { text: 'Guide', link: '/' },
      { text: 'Architecture', link: '/architecture/' },
      { text: 'Proposed V3', link: '/v3-architecture/l1-system-platform-overview' },
    ],
    sidebar: {
      '/architecture/': architectureSidebar,
      '/nats/': architectureSidebar,
      '/v3-architecture/': v3ArchitectureSidebar,
    },
    socialLinks: [],
    search: { provider: 'local' },
  },
  markdown: {
    config: (md) => {
      md.use(container, 'decision', {
        validate: (params: string) => params.trim().match(/^decision\s+(.*)$/),
        render: (tokens: any[], idx: number) => {
          const m = tokens[idx].info.trim().match(/^decision\s+(.*)$/)
          if (tokens[idx].nesting === 1) {
            const title = m ? md.utils.escapeHtml(m[1]) : 'Decision'
            return `<div class="decision-callout"><p class="decision-callout-title">${title}</p>\n`
          }
          return '</div>\n'
        },
      })
    },
  },
})
