/* Only METAR-owned routes use History. Answer write/auth routes stay native. */
(function (root, factory) {
  const api = factory(typeof module === 'object' && module.exports ? require('./route-policy.js') : root.MetarRoutes);
  if (typeof module === 'object' && module.exports) module.exports = api;
  else root.MetarRouter = api;
})(typeof window === 'undefined' ? globalThis : window, function (policy) {
  'use strict';
  const { aliases, owns } = policy;

  function href(value) {
    const index = value.indexOf('?');
    const path = index < 0 ? value : value.slice(0, index);
    return (aliases[path] || path) + (index < 0 ? '' : value.slice(index));
  }

  // Public content has one interactive destination. IDs come from Answer;
  // encode each segment so a title/username cannot become another route.
  const questionHref = (id) => `/questions/${encodeURIComponent(id)}`;
  const profileHref = (username) => `/users/${encodeURIComponent(username)}`;
  function searchHref(item) {
    const object = item?.object || {};
    return item?.object_type === 'answer' && object.question_id
      ? `${questionHref(object.question_id)}/${encodeURIComponent(object.id)}`
      : questionHref(object.id);
  }

  function nativeDestination(location, config = {}) {
    const path = location.pathname.replace(/\/$/, '');
    const question = path.match(/^\/question\/([a-zA-Z0-9]+)$/);
    const entries = {
      '/login': [config.answerLoginPath, '/users/login'],
      '/register': [config.answerRegisterPath, '/users/register'],
      '/forgot': [config.answerPasswordResetPath, '/users/account-recovery'],
      '/publish': [config.answerAskPath, '/questions/ask'],
      '/me/notifications': [null, '/users/notifications/inbox'],
    };
    const entry = entries[path];
    if (!question && !entry) return null;
    const fallback = question ? questionHref(question[1]) : entry[1];
    const configured = entry?.[0];
    let destination = fallback;
    if (typeof configured === 'string' && /^\/(?!\/)/.test(configured) && !/[\\\u0000-\u0020]/.test(configured)) {
      try {
        const url = new URL(configured, location.origin);
        // Configuration may customize a native path, never redirect off-site,
        // back into the shell or through an API endpoint.
        if (url.origin === location.origin && !owns(url.pathname) && /^\/(?:users|questions)\//.test(url.pathname) && !/%(?:2f|5c)/i.test(url.pathname)) {
          destination = url.pathname + url.search;
        }
      } catch (_) { /* keep the known native default */ }
    }
    return destination + (location.search ? (destination.includes('?') ? '&' + location.search.slice(1) : location.search) : '') + location.hash;
  }

  function create(browser) {
    if ('scrollRestoration' in browser.history) browser.history.scrollRestoration = 'manual';
    function checkpoint() {
      const path = browser.location.pathname + browser.location.search;
      browser.history.replaceState({...browser.history.state, metarPath:path, metarScroll:{x:browser.scrollX || 0, y:browser.scrollY || 0}}, '', browser.location.href);
    }
    function position() {
      const state = browser.history.state;
      const saved = state?.metarScroll;
      return state?.metarPath === browser.location.pathname + browser.location.search
        && Number.isFinite(saved?.x) && Number.isFinite(saved?.y) && saved.x >= 0 && saved.y >= 0
        ? {x:saved.x, y:saved.y} : null;
    }
    function target(value, legacy = false) {
      if (typeof value !== 'string' || !/^\/(?!\/)/.test(value) || /[\\\u0000-\u0020]/.test(value)) return null;
      try {
        const url = new URL(legacy ? href(value) : value, browser.location.origin);
        if (url.origin !== browser.location.origin || (!legacy && url.hash) || !policy.target(url.href, browser.location.origin)) return null;
        return url.pathname + url.search + (legacy ? url.hash : '');
      } catch (_) { return null; }
    }

    function migrate() {
      const { hash, pathname, search } = browser.location;
      const legacy = hash.startsWith('#/') ? target(hash.slice(1), true) : null;
      if (legacy) {
        browser.history.replaceState(null, '', legacy);
        return true;
      }
      const trimmed = pathname === '/' ? '/' : pathname.replace(/\/$/, '');
      if (trimmed === '/discover') browser.history.replaceState(null, '', '/latest' + search + hash);
      else if (trimmed !== pathname && owns(trimmed)) browser.history.replaceState(null, '', trimmed + search + hash);
      return false;
    }

    function route() {
      return { path: browser.location.pathname === '/' ? '/latest' : browser.location.pathname, query: new URLSearchParams(browser.location.search) };
    }

    function go(value) {
      const path = target(value);
      if (!path) return false;
      if (path === browser.location.pathname + browser.location.search) return false;
      checkpoint();
      browser.history.pushState(null, '', path);
      return true;
    }

    function follow(event) {
      if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return false;
      const anchor = event.target.closest?.('a[data-router]');
      if (!anchor || anchor.hasAttribute('download') || (anchor.target && anchor.target !== '_self')) return false;
      const path = target(anchor.getAttribute('href'));
      if (!path) return false;
      event.preventDefault();
      return go(path);
    }

    return { migrate, route, go, follow, checkpoint, position };
  }
  return { create, href, owns, questionHref, profileHref, searchHref, nativeDestination };
});
