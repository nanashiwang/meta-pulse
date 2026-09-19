import { target } from './route-policy.js';

// Observe committed Answer navigation. React Router's unsaved-editor blockers
// run before pushState; intercepting clicks would bypass those safeguards.
export function installNavigationBridge(host = window) {
  const key = '__metarNavigationBridge';
  if (host[key]) return host[key];
  let queued = false;
  const check = () => {
    queued = false;
    const destination = target(host.location.href, host.location.origin);
    if (destination) host.location.replace(destination);
  };
  const schedule = () => {
    if (queued) return;
    queued = true;
    host.queueMicrotask(check);
  };
  const originals = {};
  const wrappers = {};
  for (const name of ['pushState', 'replaceState']) {
    originals[name] = host.history[name];
    wrappers[name] = function (...args) {
      const result = Reflect.apply(originals[name], this, args);
      schedule();
      return result;
    };
    host.history[name] = wrappers[name];
  }
  for (const event of ['popstate', 'pageshow', 'hashchange']) host.addEventListener(event, schedule);
  const dispose = () => {
    for (const name of Object.keys(originals)) {
      if (host.history[name] === wrappers[name]) host.history[name] = originals[name];
    }
    for (const event of ['popstate', 'pageshow', 'hashchange']) host.removeEventListener(event, schedule);
    delete host[key];
  };
  host[key] = dispose;
  schedule();
  return dispose;
}
