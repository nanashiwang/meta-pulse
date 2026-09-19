/* Only METAR-owned routes use History. Answer write/auth routes stay native. */
(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else root.MetarRouter = api;
})(typeof window === 'undefined' ? globalThis : window, function () {
  'use strict';
  const aliases = { '/discover': '/latest', '/questions': '/latest', '/bookmarks': '/me/bookmarks', '/notifications': '/me/notifications' };
  const pages = new Set(['/latest', '/topics', '/knowledge', '/search', '/me', '/me/bookmarks', '/me/notifications', '/settings/binding', '/pulse', '/admin/pulse', '/publish', '/login', '/register', '/forgot', '/support', '/status', '/guidelines']);

  function href(value) {
    const index = value.indexOf('?');
    const path = index < 0 ? value : value.slice(0, index);
    return (aliases[path] || path) + (index < 0 ? '' : value.slice(index));
  }

  function owns(path) {
    return path === '/' || pages.has(path) || /^\/(question|topic)\/[^/]+$/.test(path);
  }

  function create(browser) {
    function target(value, legacy = false) {
      if (typeof value !== 'string' || !/^\/(?!\/)/.test(value) || /[\\\u0000-\u0020]/.test(value)) return null;
      try {
        const url = new URL(legacy ? href(value) : value, browser.location.origin);
        if (url.origin !== browser.location.origin || url.hash || !owns(url.pathname)) return null;
        return url.pathname + url.search;
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
      if (path !== browser.location.pathname + browser.location.search) browser.history.pushState(null, '', path);
      return true;
    }

    function follow(event) {
      if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return false;
      const anchor = event.target.closest?.('a[data-router]');
      if (!anchor || anchor.hasAttribute('download') || (anchor.target && anchor.target !== '_self')) return false;
      if (!go(anchor.getAttribute('href'))) return false;
      event.preventDefault();
      return true;
    }

    return { migrate, route, go, follow };
  }
  return { create, href, owns };
});
