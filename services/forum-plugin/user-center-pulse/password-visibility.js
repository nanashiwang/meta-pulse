// Enhance Answer's existing login input without replacing React-owned nodes.
export function installPasswordVisibility(language, host = window) {
  if (host.__metarPasswordVisibility) return;
  host.__metarPasswordVisibility = true;
  const doc = host.document;
  let current;
  function label() {
    if (!current) return;
    const { input, button } = current;
    const visible = input.type === 'text';
    const english = language.getLanguage() === 'en_US';
    const title = english ? (visible ? 'Hide password' : 'Show password') : (visible ? '隐藏密码' : '显示密码');
    button.setAttribute('aria-label', title);
    button.setAttribute('aria-pressed', String(visible));
    button.title = title;
    button.querySelector('.metar-eye-slash').style.display = visible ? '' : 'none';
  }
  function mount() {
    const input = /^\/users\/login\/?$/.test(host.location.pathname) ? doc.querySelector('input#pass') : null;
    if (current && current.input !== input) {
      current.input.type = 'password';
      current.input.classList.remove('metar-password-input');
      current.input.parentElement?.classList.remove('metar-password-field');
      current.button.remove();
      current = null;
    }
    if (!input || current || input.type !== 'password') return;
    const button = doc.createElement('button');
    button.type = 'button';
    button.className = 'metar-password-toggle';
    button.tabIndex = input.tabIndex;
    button.setAttribute('aria-controls', input.id);
    button.innerHTML = '<svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12Z"/><circle cx="12" cy="12" r="3"/><path class="metar-eye-slash" d="m3 3 18 18"/></svg>';
    button.addEventListener('click', () => {
      input.type = input.type === 'password' ? 'text' : 'password';
      label();
    });
    input.classList.add('metar-password-input');
    input.parentElement.classList.add('metar-password-field');
    input.after(button);
    current = { input, button };
    label();
  }
  let pending = false;
  new host.MutationObserver(() => {
    if (pending) return;
    pending = true;
    host.requestAnimationFrame(() => { pending = false; mount(); });
  }).observe(doc.documentElement, { childList: true, subtree: true });
  language.onLanguageChanged(label);
  host.addEventListener('pagehide', () => {
    if (current) { current.input.type = 'password'; label(); }
  });
  mount();
}
