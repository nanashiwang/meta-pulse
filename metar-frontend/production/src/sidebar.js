/* Browser-local layout preference; shared by the initial shell and live app. */
(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else { root.MetarSidebar = api.install(root); }
})(typeof window === 'undefined' ? globalThis : window, function () {
  'use strict';
  const KEY = 'metar-sidebar-collapsed';
  function create({storage, desktop, apply}) {
    let collapsed = false, mobileOpen = false;
    try { collapsed = storage?.getItem(KEY) === '1'; } catch (_) {}
    const state = () => ({collapsed, mobile: !desktop.matches, expanded: desktop.matches ? !collapsed : mobileOpen});
    const sync = () => apply(state());
    desktop.addEventListener?.('change', () => { mobileOpen = false; sync(); });
    sync();
    return {
      state, sync,
      toggle() {
        if (desktop.matches) {
          collapsed = !collapsed;
          try { storage?.setItem(KEY, collapsed ? '1' : '0'); } catch (_) {}
        } else mobileOpen = !mobileOpen;
        sync();
      },
      closeMobile() { mobileOpen = false; sync(); },
      fromStorage(value) { collapsed = value === '1'; sync(); },
    };
  }
  function install(host) {
    if (host.__metarSidebar) return host.__metarSidebar;
    const doc = host.document, root = doc.documentElement;
    let storage;
    try { storage = host.localStorage; } catch (_) {}
    const label = (expanded) => root.lang?.startsWith('en')
      ? (expanded ? 'Hide sidebar' : 'Show sidebar')
      : (expanded ? '收起侧栏' : '展开侧栏');
    const controller = create({
      storage, desktop: host.matchMedia('(min-width: 821px)'),
      apply(state) {
        root.dataset.sidebarCollapsed = String(state.collapsed);
        doc.body?.classList.toggle('menu-open', state.mobile && state.expanded);
        const sidebar = doc.getElementById('community-sidebar');
        const button = doc.querySelector('[data-action="menu"]');
        if (!state.expanded && sidebar?.contains(doc.activeElement)) button?.focus({preventScroll:true});
        if (sidebar) { sidebar.inert = !state.expanded; sidebar.setAttribute('aria-hidden', String(!state.expanded)); }
        if (button) { button.setAttribute('aria-expanded', String(state.expanded)); button.setAttribute('aria-label', label(state.expanded)); button.title = label(state.expanded); }
      },
    });
    doc.addEventListener('click', event => {
      if (event.target.closest?.('[data-action="menu"]')) controller.toggle();
    });
    doc.addEventListener('readystatechange', controller.sync);
    doc.addEventListener('DOMContentLoaded', controller.sync);
    host.addEventListener('storage', event => { if (event.key === KEY || event.key === null) controller.fromStorage(event.newValue); });
    host.__metarSidebar = controller;
    return controller;
  }
  return {KEY, create, install};
});
