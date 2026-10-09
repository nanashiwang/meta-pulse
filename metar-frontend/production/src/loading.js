/* Anonymous first paint and public-document restoration only. No data cache. */
(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else { root.MetarLoading = api; api.install(root); }
})(typeof window === 'undefined' ? globalThis : window, function () {
  'use strict';
  const escape = value => String(value).replace(/[&<>"']/g, char => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[char]));
  function layout(path) {
    if (['/', '/latest', '/discover'].includes(path)) return 'feed';
    if (['/search', '/me/bookmarks'].includes(path) || path.startsWith('/topic/')) return 'list';
    return ['/topics', '/knowledge'].includes(path) ? 'grid' : 'panel';
  }
  function isPublic(path) {
    return ['/latest', '/topics', '/search', '/knowledge', '/support', '/guidelines'].includes(path) || /^\/topic\/[^/]+$/.test(path);
  }
  function canRetain(path, key, previousKey, language, previousLanguage, sameSession) {
    return sameSession && isPublic(path) && key === previousKey && language === previousLanguage;
  }
  function render(template, title, path, label, english = false) {
    // Only static, explicitly marked text is localized; never page/user content.
    if (english) template = template.replace(/(data-boot-en="([^"]+)"[^>]*>)[^<]*/g, (_match, attributes, text) => attributes + text);
    return template.replaceAll('__METAR_LOADING_LAYOUT__', layout(path))
      .replaceAll('__METAR_LOADING_TITLE__', () => escape(title))
      .replaceAll('__METAR_LOADING_LABEL__', () => escape(label));
  }
  function install(host) {
    const doc = host.document;
    let english = false;
    try { english = host.localStorage.getItem('metar-language') === 'en_US'; } catch (_) { /* default Chinese */ }
    const translate = () => {
      try { english = host.localStorage.getItem('metar-language') === 'en_US'; } catch (_) { /* keep current language */ }
      if (!english) return;
      for (const node of doc.querySelectorAll('[data-boot-en]')) node.textContent = node.dataset.bootEn;
      for (const node of doc.querySelectorAll('[data-boot-label-en]')) node.setAttribute('aria-label', node.dataset.bootLabelEn);
      for (const node of doc.querySelectorAll('[data-boot-placeholder-en]')) node.setAttribute('placeholder', node.dataset.bootPlaceholderEn);
    };
    const view = doc.querySelector('#view .page-loading');
    if (view) {
      view.dataset.layout = layout(host.location.pathname.replace(/\/$/, '') || '/');
      if (english) {
        const title = view.querySelector('[data-loading-title]');
        title.textContent = title.dataset.bootEn || 'Loading…';
        view.querySelector('[data-loading-label]').textContent = 'Loading live community data…';
      }
    }
    if (english) doc.documentElement.lang = 'en';
    translate();
    // A failed main script must leave a usable exit, rather than an endless skeleton.
    const recover = () => { for (const node of doc.querySelectorAll('[data-loading-recovery]')) node.hidden = false; translate(); };
    host.setTimeout(recover, 12000);
    host.addEventListener('error', event => {
      if (event.target?.tagName === 'SCRIPT') recover();
    }, true);
  }
  return { layout, isPublic, canRetain, render, install };
});
