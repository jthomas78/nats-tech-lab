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
const v3ArchitectureSidebar: DefaultTheme.SidebarItem[] = [
  {
    text: 'Proposed Linebooker V3',
    items: [
      {
        text: 'L1 System and Platform Overview',
        link: '/v3-architecture/l1-system-platform-overview',
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
