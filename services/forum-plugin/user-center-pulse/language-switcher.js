export const LANGUAGE_KEY = 'metar-language';
export const supportedLanguage = (value) => ['zh_CN', 'en_US'].includes(value) ? value : null;

export function createLanguageController(host, storage, enqueue = queueMicrotask) {
  let preferred = null;
  try { preferred = supportedLanguage(storage?.getItem(LANGUAGE_KEY)); } catch (_) { /* Browser storage can be disabled. */ }
  let queued = false;
  const listeners = new Set();
  const language = () => preferred || supportedLanguage(host.getLanguage()) || 'zh_CN';
  const notify = () => {
    host.setDateLocale(language());
    listeners.forEach((fn) => fn(language()));
  };
  const syncUser = () => {
    if (preferred && host.getUserLanguage() !== preferred) host.setUserLanguage(preferred);
  };
  const apply = () => {
    syncUser();
    if (preferred && host.getLanguage() !== preferred) {
      // Dictionaries are bundled with the plugin, so this needs no network.
      Promise.resolve(host.changeLanguage(preferred)).catch(() => {});
    }
    notify();
  };
  const schedule = () => {
    if (queued) return;
    queued = true;
    enqueue(() => { queued = false; apply(); });
  };
  host.subscribeUser(syncUser);
  host.onLanguageChanged(() => {
    // Answer may finish an earlier asynchronous setupAppLanguage after a
    // selection. Restore the latest explicit choice without nested events.
    if (preferred && host.getLanguage() !== preferred) schedule();
    else notify();
  });
  apply();
  return {
    language,
    subscribe(fn) { listeners.add(fn); fn(language()); return () => listeners.delete(fn); },
    select(value) {
      const next = supportedLanguage(value);
      if (!next) return false;
      preferred = next;
      try { storage?.setItem(LANGUAGE_KEY, next); } catch (_) { /* Retain the choice for this page. */ }
      apply();
      return true;
    },
    fromStorage(value) { preferred = supportedLanguage(value); apply(); },
  };
}

export function installLanguageSwitcher(host) {
  if (document.getElementById('metar-language-switcher')) return;
  let storage;
  try { storage = window.localStorage; } catch (_) { /* Switching still works without persistence. */ }
  const controller = createLanguageController(host, storage);
  const select = document.createElement('select');
  select.id = 'metar-language-switcher';
  select.className = 'metar-language-switcher';
  for (const [value, label] of [['zh_CN', '中文'], ['en_US', 'English']]) {
    const option = document.createElement('option');
    option.value = value;
    option.textContent = label;
    select.append(option);
  }
  select.addEventListener('change', () => controller.select(select.value));
  controller.subscribe((language) => {
    select.value = language;
    const label = language === 'en_US' ? 'Language (this browser)' : '语言（当前浏览器）';
    select.setAttribute('aria-label', label);
    select.title = label;
    document.documentElement.lang = language === 'en_US' ? 'en' : 'zh-CN';
  });
  function mount() {
    const header = document.querySelector('#header > .w-100');
    if (header && select.parentElement !== header) header.append(select);
  }
  new MutationObserver(mount).observe(document.documentElement, { childList: true, subtree: true });
  window.addEventListener('storage', (event) => {
    if (event.key === LANGUAGE_KEY) controller.fromStorage(event.newValue);
  });
  mount();
}
