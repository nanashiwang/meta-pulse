/* Community avatars are presentation only; Answer owns uploaded images and profiles. */
'use strict';
(() => {
  const t = (...args) => window.MetarI18n?.t(...args) ?? args[0];
  const cleanName = (value) => typeof value === 'string' ? value.trim() : '';
  const username = (user) => cleanName(user?.username) || cleanName(user?.display_name) || 'M';
  const segmenter = typeof Intl.Segmenter === 'function'
    ? new Intl.Segmenter(undefined, { granularity: 'grapheme' }) : null;
  const escapeHTML = (value) => String(value).replace(/[&<>"']/g, (character) => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
  })[character]);

  function initial(user) {
    const name = username(user).toUpperCase();
    // Keep supplementary characters, combining marks and joined emoji intact.
    return segmenter
      ? segmenter.segment(name)[Symbol.iterator]().next().value.segment
      : Array.from(name)[0];
  }

  function source(avatar, origin = window.location?.origin) {
    // Gravatar is the Answer default, not a member-uploaded picture. Never
    // initiate third-party image requests or relax the shell's same-origin CSP.
    const value = typeof avatar === 'string' ? avatar : avatar?.type === 'custom' ? avatar.custom : '';
    if (typeof value !== 'string' || !value.trim()) return '';
    const candidate = value.trim();
    if (!candidate.startsWith('/') && !/^https?:\/\//i.test(candidate)) return '';
    try {
      const base = new URL(origin);
      const url = new URL(candidate, base);
      if (!['http:', 'https:'].includes(url.protocol) || url.origin !== base.origin || url.username || url.password) return '';
      if (/\/default-avatar(?:[.-][\w-]+)?\.svg$/i.test(url.pathname)) return '';
      return url.href;
    } catch (_) {
      return '';
    }
  }

  function markup(user, className = '') {
    const image = source(user?.avatar);
    return `<span class="avatar a4 prod-avatar${className ? ` ${escapeHTML(className)}` : ''}" role="img" aria-label="${escapeHTML(username(user))}${t(" 的头像")}"><span class="prod-avatar-initial" aria-hidden="true">${escapeHTML(initial(user))}</span>${image ? `<img class="prod-avatar-image" data-metar-avatar-image src="${escapeHTML(image)}" alt="" aria-hidden="true" width="40" height="40" loading="lazy" decoding="async">` : ''}</span>`;
  }

  function handleImageEvent(event) {
    const image = event.target;
    if (!image?.matches?.('img[data-metar-avatar-image]')) return;
    const container = image.parentElement;
    if (!container?.classList.contains('prod-avatar')) return;
    if (event.type === 'load' && image.naturalWidth > 0) {
      container.classList.add('has-image');
    } else if (event.type === 'error') {
      container.classList.remove('has-image');
      image.remove();
    }
  }

  function install(root = document) {
    // Image events do not bubble. Capture also covers avatars added by SPA renders.
    root.addEventListener('load', handleImageEvent, true);
    root.addEventListener('error', handleImageEvent, true);
  }

  window.MetarAvatars = Object.freeze({ initial, source, markup, handleImageEvent, install });
})();
