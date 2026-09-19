import { defineConfig } from 'vitepress'

// The blog is a static site served under /blog/ by the same nginx that fronts
// the forum. new-api remains on its independent domain and is linked explicitly.
export default defineConfig({
  lang: 'zh-CN',
  title: '元衡技术博客',
  description: '模型评测、成本分析与 API 接入实践',
  base: '/blog/',

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
    nav: [
      { text: '首页', link: '/' },
      { text: '模型评测', link: '/reviews/' },
      { text: '接入教程', link: '/guides/' },
      { text: '论坛', link: 'https://metar.uk/' },
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
