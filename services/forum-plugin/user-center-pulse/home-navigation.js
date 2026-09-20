import { target } from './route-policy.js';

// Only the two host-owned home controls cross the application boundary here.
// Other native links keep React Router's route guards and editor blockers.
const homeControls = '#header a.navbar-brand, #adminSideNav > a';

export function homeDestination(link, origin) {
  if (!link?.matches(homeControls) || link.hasAttribute('download')) return null;
  const value = link.getAttribute('href');
  const destination = target(value, origin);
  if (!destination) return null;
  const path = new URL(value, origin).pathname;
  return path === '/' || path === '/latest' ? destination : null;
}

export function canonicalizeHomeLinks(doc, origin) {
  for (const link of doc.querySelectorAll(homeControls)) {
    const destination = homeDestination(link, origin);
    if (destination && link.getAttribute('href') !== destination) link.setAttribute('href', destination);
  }
}

export function installHomeNavigation(host = window) {
  const click = (event) => {
    if (event.defaultPrevented || event.button !== 0) return;
    // Rich editors retain React Router's explicit unsaved-change confirmation.
    // Their committed navigation is still handled by navigation-bridge.js.
    if (host.document.querySelector('[contenteditable="true"], textarea')) return;
    const link = event.target?.closest?.('a[href]');
    const destination = homeDestination(link, host.location.origin);
    if (!destination) return;
    link.setAttribute('href', destination);
    // Let the browser follow the canonical anchor, including modifier/new-tab
    // semantics and Answer's beforeunload draft warning. React's old `to="/"`
    // must not render its home page before the navigation bridge reloads it.
    event.stopPropagation();
  };
  host.document.addEventListener('click', click, true);
  return () => host.document.removeEventListener('click', click, true);
}
