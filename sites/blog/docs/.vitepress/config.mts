import { defineConfig } from 'vitepress'

// The blog is a static site served under /blog/ by the same nginx that fronts
// new-api and the forum, so all three share a cookie domain and accumulate
// search authority on one hostname.
export default defineConfig({
  lang: 'zh-CN',
  title: '元衡技术博客',
  description: '模型评测、成本分析与 API 接入实践',
  base: '/blog/',

  // Content is the funnel entrance, so indexing settings are not optional.
  sitemap: {
    hostname: 'https://example.com',
  },
  lastUpdated: true,
  cleanUrls: true,

  head: [
    ['meta', { property: 'og:type', content: 'article' }],
    ['meta', { name: 'robots', content: 'index,follow' }],
  ],

  themeConfig: {
    nav: [
      { text: '首页', link: '/' },
      { text: '模型评测', link: '/reviews/' },
      { text: '接入教程', link: '/guides/' },
      { text: '论坛', link: 'https://example.com/forum/' },
      { text: '控制台', link: 'https://example.com/console' },
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
