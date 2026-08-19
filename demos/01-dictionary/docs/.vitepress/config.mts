import { defineConfig } from 'vitepress'
import container from 'markdown-it-container'

export default defineConfig({
  title: 'Dictionary POC Docs',
  description: 'NATS Tech Lab — Dictionary POC architecture & reference docs',
  cleanUrls: true,
  themeConfig: {
    nav: [
      { text: 'Guide', link: '/' },
      { text: 'Architecture', link: '/architecture/' },
    ],
    sidebar: {
      '/architecture/': [
        {
          text: 'Architecture',
          items: [
            { text: 'Overview', link: '/architecture/' },
            { text: 'CQRS Shapes', link: '/architecture/cqrs-shapes' },
            { text: 'Dictionary (Reference Data)', link: '/architecture/dictionary' },
            { text: 'Communications', link: '/architecture/communications' },
            { text: 'Accounts', link: '/architecture/accounts' },
            { text: 'Admin', link: '/architecture/admin' },
            { text: 'Platform (Tech Lab Operator)', link: '/architecture/platform' },
          ],
        },
      ],
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
