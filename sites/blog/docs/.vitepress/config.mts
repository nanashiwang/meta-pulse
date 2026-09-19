import { defineConfig } from 'vitepress'

// The blog is a static site served under /blog/ by the same nginx that fronts
// the forum. new-api remains on its independent domain and is linked explicitly.
export default defineConfig({
  lang: 'zh-CN',
  title: 'METAR 知识库',
  description: '模型评测、成本分析与 API 接入实践',
  base: '/blog/',
  appearance: { storageKey: '_metar_theme' },
  // VitePress 1.6's initial HTML bootstrap still hard-codes its default key.
  // Keep first paint consistent with the appearance option used after hydration.
  transformHtml(html) {
    const bootstrap = 'localStorage.getItem("vitepress-theme-appearance")'
    if (!html.includes(bootstrap)) throw new Error('Review VitePress theme bootstrap before upgrading')
    return html.replace(bootstrap, 'localStorage.getItem("_metar_theme")')
  },

  // Content is the funnel entrance, so indexing settings are not optional.
  sitemap: {
    hostname: 'https://metar.uk/blog/',
  },
  transformPageData(pageData) {
    if (pageData.isNotFound || pageData.relativePath === "404.md") return;
    const path = pageData.relativePath.replace(/(^|\/)index\.md$/, '$1').replace(/\.md$/, '');
    const canonical = 'https://metar.uk/blog/' + path;
    const existing = pageData.frontmatter.head || [];
    pageData.frontmatter.head = [...existing,
      ['link', { rel: 'canonical', href: canonical }],
      ['meta', { property: 'og:url', content: canonical }],
    ];
  },
  lastUpdated: true,
  cleanUrls: true,

  head: [
    ['meta', { property: 'og:type', content: 'article' }],
    ['meta', { name: 'robots', content: 'index,follow' }],
  ],

  themeConfig: {
    darkModeSwitchLabel: '深色主题',
    darkModeSwitchTitle: '切换到深色主题',
    lightModeSwitchTitle: '切换到浅色主题',
    nav: [
      { text: '社区', link: 'https://metar.uk/latest', target: '_self' },
      { text: '知识库', link: '/' },
      { text: 'Pulse', link: 'https://metar.uk/pulse', target: '_self' },
      { text: '模型评测', link: '/reviews/' },
      { text: '接入教程', link: '/guides/' },
      { text: '控制台', link: 'https://cn.meta-api.vip/console' },
    ],

    sidebar: {
      '/reviews/': [{ text: '模型评测', items: [] }],
      '/guides/': [{ text: '接入教程', items: [] }],
    },

    footer: {
      message: '元衡 API · Meta Pulse',
      copyright: 'Copyright © 2026',
    },

    search: {
      provider: 'local',
      options: {
        locales: {
          root: {
            translations: {
              button: { buttonText: '搜索文档', buttonAriaLabel: '搜索文档' },
            },
          },
        },
      },
    },
  },
})
