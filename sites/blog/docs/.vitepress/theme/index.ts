import DefaultTheme from 'vitepress/theme'
import { h } from 'vue'
import '../../../../../metar-frontend/shared/theme-tokens.css'
import './style.css'

export default {
  extends: DefaultTheme,
  Layout: () => h(DefaultTheme.Layout, null, {
    // Use the shared origin-root asset; theme.logo would prepend /blog/.
    'nav-bar-title-before': () => h('img', {
      src: '/metar-assets/brand-logo.jpg', alt: '',
      width: 36, height: 36, class: 'metar-brand-logo',
    }),
  }),
}
