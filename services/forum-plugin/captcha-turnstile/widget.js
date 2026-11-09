let loading;
export function loadTurnstile(win = window, doc = document) {
  if (win.turnstile) return Promise.resolve(win.turnstile);
  if (loading) return loading;
  loading = new Promise((resolve, reject) => {
    const script = doc.createElement('script');
    const finish = (error) => {
      win.clearTimeout(timer); script.onload = script.onerror = null;
      if (error) { script.remove(); reject(error); }
      else resolve(win.turnstile);
    };
    const timer = win.setTimeout(() => finish(new Error('Turnstile timeout')), 15000);
    script.src = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit';
    script.async = true;
    script.onload = () => finish(win.turnstile ? null : new Error('Turnstile unavailable'));
    script.onerror = () => finish(new Error('Turnstile unavailable'));
    doc.head.append(script);
  }).catch((error) => { loading = undefined; throw error; });
  return loading;
}

export function presentWidget(config, handlers) {
  const english = (document.documentElement.lang || '').startsWith('en');
  const previousFocus = document.activeElement;
  const dialog = document.createElement('dialog');
  dialog.style.cssText = 'border:0;border-radius:12px;padding:24px;max-width:calc(100vw - 24px);color:CanvasText;background:Canvas;';
  dialog.setAttribute('aria-label', english ? 'Security verification' : '安全验证');
  const title = document.createElement('h2');
  title.style.fontSize = '20px'; title.textContent = english ? 'Security verification' : '安全验证';
  const message = document.createElement('p'); message.setAttribute('role', 'status');
  message.textContent = english ? 'Loading verification…' : '正在加载人机验证…';
  const target = document.createElement('div');
  const retry = document.createElement('button'); retry.type = 'button'; retry.hidden = true;
  retry.textContent = english ? 'Retry' : '重新验证';
  const cancel = document.createElement('button'); cancel.type = 'button';
  cancel.textContent = english ? 'Cancel' : '取消'; cancel.style.marginLeft = '12px';
  dialog.append(title, message, target, retry, cancel); document.body.append(dialog); dialog.showModal();
  let removed = false, widget, api, pending = false;
  function failed() {
    if (removed) return;
    message.textContent = english ? 'Verification failed or expired. Please retry.' : '验证失败或已过期，请重新验证。';
    retry.hidden = false; handlers.failed();
  }
  async function render() {
    if (pending || removed) return;
    pending = true; retry.hidden = true;
    document.getElementById('metar-turnstile-error')?.remove();
    try {
      api = await loadTurnstile();
      if (removed) return;
      if (widget !== undefined) api.remove(widget);
      widget = api.render(target, {
        sitekey: config.key, action: config.action, language: english ? 'en' : 'zh-cn',
        theme: 'auto', 'response-field': false,
        callback: (token) => {
          if (!removed) { document.getElementById('metar-turnstile-error')?.remove(); handlers.verified(token); }
        },
        'error-callback': failed, 'expired-callback': failed, 'timeout-callback': failed,
      });
      message.textContent = english ? 'Please complete the verification.' : '请完成人机验证。';
    } catch { failed(); } finally { pending = false; }
  }
  retry.onclick = () => void render(); cancel.onclick = () => handlers.cancelled();
  dialog.addEventListener('cancel', (event) => { event.preventDefault(); handlers.cancelled(); });
  void render();
  return () => {
    removed = true;
    if (api && widget !== undefined) api.remove(widget);
    dialog.close(); dialog.remove();
    if (previousFocus?.isConnected) previousFocus.focus();
  };
}
export function reportError() {
  let notice = document.getElementById('metar-turnstile-error');
  if (!notice) {
    notice = document.createElement('div'); notice.id = 'metar-turnstile-error'; notice.setAttribute('role', 'alert');
    notice.className = 'alert alert-warning'; document.body.prepend(notice);
  }
  notice.textContent = (document.documentElement.lang || '').startsWith('en')
    ? 'Security verification is unavailable. Please retry; if this continues, contact the administrator.'
    : '安全验证暂不可用，请重试；若持续失败，请联系管理员。';
}
