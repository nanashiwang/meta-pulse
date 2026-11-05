// The content revision bypasses browser favicon caches retained for /favicon.ico.
// Keep the revision aligned with production/src/favicon.ico (checked in tests).
export const METAR_FAVICON = '/metar-assets/favicon.ico?v=92ba9f9d8e35';

export function isDefaultFavicon(value, origin) {
  if (!value) return false;
  try {
    const url = new URL(value, origin);
    return url.origin === origin && url.pathname === '/favicon.ico' && !url.search && !url.hash;
  } catch (_) { return false; }
}

export function installNativeFavicon(host = window) {
  if (host.__metarNativeFavicon || !host.document.head) return;
  host.__metarNativeFavicon = true;
  const { document: doc } = host;
  const selector = 'link[rel~="icon"]';
  const normalize = () => {
    let icons = Array.from(doc.head.querySelectorAll(selector));
    // Native admin/login HTML has no icon until React mounts PageTags.
    if (!icons.length) {
      const link = doc.createElement('link');
      link.rel = 'icon';
      link.setAttribute('data-metar-favicon', 'fallback');
      doc.head.append(link);
      icons = [link];
    }
    const configured = icons.some(link => {
      const value = link.getAttribute('href');
      return value && value !== METAR_FAVICON && !link.hasAttribute('data-metar-favicon') && !isDefaultFavicon(value, host.location.origin);
    });
    for (const link of icons) {
      if (link.hasAttribute('data-metar-favicon') && configured) {
        link.remove();
        continue;
      }
      if (link.hasAttribute('data-metar-favicon') || isDefaultFavicon(link.getAttribute('href'), host.location.origin)) {
        if (link.getAttribute('href') !== METAR_FAVICON) link.setAttribute('href', METAR_FAVICON);
        if (link.getAttribute('type') !== 'image/x-icon') link.setAttribute('type', 'image/x-icon');
      }
    }
  };
  // Watch only icon-relevant head changes. Idempotent writes avoid observer loops
  // when Helmet rebuilds links after navigation or branding settings change.
  let pending = false;
  new host.MutationObserver(() => {
    if (pending) return;
    pending = true;
    host.queueMicrotask(() => { pending = false; normalize(); });
  }).observe(doc.head, {childList: true, subtree: true, attributes: true, attributeFilter: ['href', 'rel', 'type']});
  normalize();
}
