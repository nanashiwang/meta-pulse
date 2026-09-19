(function(root) {
  'use strict';
  const origin = 'https://metar.uk';
  function policy(pathname, search = '') {
    const path = pathname.replace(/\/$/, '') || '/';
    const privatePage = /^\/(?:me|settings|pulse|admin|search|publish|login|register|forgot|status)(?:\/|$)/.test(path);
    let canonical = path === '/discover' ? '/latest' : path;
    const question = path.match(/^\/question\/([a-zA-Z0-9]+)$/);
    if (question) canonical = '/questions/' + question[1];
    const topic = path.match(/^\/topic\/([^/]+)$/);
    if (topic) canonical = '/tags/' + topic[1];
    const query = new URLSearchParams(search);
    // Separate paginated content; never put tracking or account parameters in canonical URLs.
    const page = query.get('page');
    if ((path === '/latest' || topic) && /^[1-9][0-9]*$/.test(page || '') && page !== '1') canonical += '?page=' + page;
    return { canonical: privatePage ? '' : origin + canonical, robots: privatePage ? 'noindex, nofollow' : 'index, follow' };
  }
  function update(error = false) {
    const doc = root.document;
    const state = policy(root.location.pathname, root.location.search);
    function meta(name, content, property = false) {
      const attr = property ? 'property' : 'name';
      let node = doc.querySelector('meta[' + attr + '="' + name + '"]');
      if (!node) { node = doc.createElement('meta'); node.setAttribute(attr, name); doc.head.appendChild(node); }
      node.setAttribute('content', content);
    }
    let canonical = doc.querySelector('link[rel="canonical"]');
    if (!error && state.canonical) {
      if (!canonical) { canonical = doc.createElement('link'); canonical.setAttribute('rel', 'canonical'); doc.head.appendChild(canonical); }
      canonical.setAttribute('href', state.canonical);
    } else canonical?.remove();
    meta('robots', error ? 'noindex, follow' : state.robots);
    if (root.location.pathname === '/') doc.title = 'METAR 元衡社区｜AI 模型交流与 API 接入实践';
    const heading = doc.querySelector('#view h1');
    if (heading && /^\/question\//.test(root.location.pathname)) doc.title = heading.textContent.trim() + ' · METAR 元衡社区';
    const body = doc.querySelector('.prod-question-body');
    const description = body?.textContent.trim().replace(/\s+/g, ' ').slice(0, 160) || 'METAR 元衡社区：AI 模型交流、API 接入问答与技术知识库。';
    meta('description', description);
    meta('og:title', doc.title, true);
    meta('og:description', description, true);
    meta('og:url', !error ? state.canonical : '', true);
    meta('og:type', 'website', true);
  }
  const api = { policy, update };
  if (typeof module === 'object' && module.exports) module.exports = api;
  else root.MetarSEO = api;
})(typeof window === 'undefined' ? globalThis : window);
