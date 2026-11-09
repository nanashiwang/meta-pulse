(function (host) {
  'use strict';

  function cropRect(w, h, zoom, dx, dy) {
    const size = Math.min(w, h) / zoom;
    return {
      x: Math.max(0, Math.min(w - size, (w - size) / 2 - dx)),
      y: Math.max(0, Math.min(h - size, (h - size) / 2 - dy)),
      size,
    };
  }

  function validateFile(file) {
    if (!['image/jpeg', 'image/png', 'image/webp'].includes(file.type)) throw new Error('format');
    if (file.size > 20 * 1024 * 1024) throw new Error('large');
  }

  async function compress(canvas) {
    for (const quality of [0.9, 0.8, 0.7, 0.6, 0.5, 0.4]) {
      const blob = await new Promise((resolve, reject) => {
        try { canvas.toBlob(resolve, 'image/jpeg', quality); }
        catch (_) { reject(new Error('compress')); }
      });
      if (!blob) throw new Error('compress');
      if (blob.size <= 200 * 1024) return blob;
    }
    throw new Error('compress');
  }

  function install(options) {
    if (host.__metarAvatarCropper) return;
    host.__metarAvatarCropper = true;
    const doc = host.document;
    const processed = new WeakSet();
    let active = null;
    const onProfile = () => host.location.pathname === '/users/settings/profile';
    function avatarInput(input) {
      if (!onProfile() || !input?.matches?.('input[type="file"]')) return false;
      for (let group = input.parentElement; group; group = group.parentElement) {
        const select = group.querySelector('select[name="avatar.type"]');
        if (select && select.parentElement?.parentElement === group) return true;
      }
      return false;
    }

    function open(file, input) {
      const english = /^en(?:_|-|$)/i.test(options.getLanguage());
      const text = english ? {
        title: 'Crop avatar', help: 'Drag to reposition, then adjust zoom.', zoom: 'Zoom',
        preview: 'Avatar preview', reset: 'Reset', cancel: 'Cancel', confirm: 'Use avatar',
        loading: 'Loading image…', format: 'Choose a JPEG, PNG or WebP image.',
        large: 'Choose an image no larger than 20 MiB.', dimensions: 'Choose an image with no more than 40 megapixels.',
        decode: 'This image could not be opened. Please choose another image.',
        compress: 'The avatar could not be compressed to 200 KiB. Try a simpler crop or another image.',
      } : {
        title: '裁剪头像', help: '拖动图片调整位置，并使用缩放滑块。', zoom: '缩放',
        preview: '头像预览', reset: '重置', cancel: '取消', confirm: '使用头像',
        loading: '正在加载图片…', format: '请选择 JPEG、PNG 或 WebP 图片。',
        large: '请选择不超过 20 MiB 的图片。', dimensions: '请选择不超过 4000 万像素的图片。',
        decode: '无法打开这张图片，请选择其他图片。',
        compress: '无法将头像压缩至 200 KiB，请调整裁剪区域或选择其他图片。',
      };
      const previousFocus = doc.activeElement;
      const dialog = doc.createElement('dialog');
      dialog.className = 'metar-avatar-cropper';
      dialog.setAttribute('aria-labelledby', 'metar-avatar-crop-title');
      dialog.innerHTML = '<h2 id="metar-avatar-crop-title"></h2><p class="metar-avatar-help"></p>' +
        '<div class="metar-avatar-images"><canvas class="metar-avatar-editor" width="560" height="560"></canvas>' +
        '<canvas class="metar-avatar-preview" width="160" height="160" role="img"></canvas></div>' +
        '<label class="metar-avatar-zoom"><span></span><input type="range" min="1" max="4" step="0.01" value="1" disabled></label>' +
        '<p class="metar-avatar-status" role="status"></p><p class="metar-avatar-error" role="alert" hidden></p>' +
        '<div class="metar-avatar-actions"><button type="button" data-action="reset" disabled></button>' +
        '<button type="button" data-action="cancel" autofocus></button><button type="button" data-action="confirm" disabled></button></div>';
      const editor = dialog.querySelector('.metar-avatar-editor');
      const preview = dialog.querySelector('.metar-avatar-preview');
      const slider = dialog.querySelector('input');
      const reset = dialog.querySelector('[data-action="reset"]');
      const cancel = dialog.querySelector('[data-action="cancel"]');
      const confirm = dialog.querySelector('[data-action="confirm"]');
      const error = dialog.querySelector('[role="alert"]');
      const status = dialog.querySelector('[role="status"]');
      dialog.querySelector('h2').textContent = text.title;
      dialog.querySelector('.metar-avatar-help').textContent = text.help;
      dialog.querySelector('label span').textContent = text.zoom;
      editor.setAttribute('aria-label', text.help);
      preview.setAttribute('aria-label', text.preview);
      reset.textContent = text.reset;
      cancel.textContent = text.cancel;
      confirm.textContent = text.confirm;
      status.textContent = text.loading;
      let bitmap = null;
      let dx = 0;
      let dy = 0;
      let drag = null;
      let done = false;
      let busy = false;
      let resolve;
      const result = new Promise((accept) => { resolve = accept; });
      const valid = () => onProfile() && input.isConnected && avatarInput(input);
      const observer = new host.MutationObserver(() => { if (!valid() || !dialog.isConnected) finish(null); });
      const timer = host.setInterval(() => { if (!valid()) finish(null); }, 200);
      function finish(value) {
        if (done) return;
        done = true;
        observer.disconnect();
        host.clearInterval(timer);
        host.removeEventListener('popstate', checkRoute);
        host.removeEventListener('hashchange', checkRoute);
        host.removeEventListener('pagehide', cancelDialog);
        if (bitmap) { bitmap.close(); bitmap = null; }
        if (dialog.open) dialog.close();
        dialog.remove();
        if (previousFocus?.isConnected) previousFocus.focus();
        resolve(value);
      }
      function checkRoute() { if (!valid()) finish(null); }
      function cancelDialog() { finish(null); }
      function showError(code) {
        status.textContent = '';
        error.textContent = text[code] || text.decode;
        error.hidden = false;
      }
      function draw(canvas, rect) {
        const ctx = canvas.getContext('2d');
        ctx.fillStyle = '#fff';
        ctx.fillRect(0, 0, canvas.width, canvas.height);
        ctx.drawImage(bitmap, rect.x, rect.y, rect.size, rect.size, 0, 0, canvas.width, canvas.height);
      }
      function render() {
        const rect = cropRect(bitmap.width, bitmap.height, Number(slider.value), dx, dy);
        dx = (bitmap.width - rect.size) / 2 - rect.x;
        dy = (bitmap.height - rect.size) / 2 - rect.y;
        draw(editor, rect);
        draw(preview, rect);
      }
      cancel.addEventListener('click', cancelDialog);
      dialog.addEventListener('cancel', (event) => { event.preventDefault(); finish(null); });
      dialog.addEventListener('close', cancelDialog);
      slider.addEventListener('input', () => { if (bitmap && !busy) render(); });
      reset.addEventListener('click', () => { dx = 0; dy = 0; slider.value = '1'; render(); });
      editor.addEventListener('pointerdown', (event) => {
        if (!bitmap || busy || (event.pointerType === 'mouse' && event.button !== 0)) return;
        drag = { id: event.pointerId, x: event.clientX, y: event.clientY };
        editor.setPointerCapture(event.pointerId);
        event.preventDefault();
      });
      editor.addEventListener('pointermove', (event) => {
        if (!drag || drag.id !== event.pointerId || !bitmap || busy) return;
        const scale = Math.min(bitmap.width, bitmap.height) / Number(slider.value) / editor.getBoundingClientRect().width;
        dx += (event.clientX - drag.x) * scale;
        dy += (event.clientY - drag.y) * scale;
        drag.x = event.clientX;
        drag.y = event.clientY;
        render();
      });
      const stopDrag = () => { drag = null; };
      editor.addEventListener('pointerup', stopDrag);
      editor.addEventListener('pointercancel', stopDrag);
      editor.addEventListener('lostpointercapture', stopDrag);
      confirm.addEventListener('click', async () => {
        if (!bitmap || busy || done) return;
        busy = true;
        confirm.disabled = reset.disabled = slider.disabled = true;
        error.hidden = true;
        try {
          const output = doc.createElement('canvas');
          output.width = output.height = 512;
          draw(output, cropRect(bitmap.width, bitmap.height, Number(slider.value), dx, dy));
          const blob = await compress(output);
          if (done) return;
          if (!valid()) { finish(null); return; }
          const transfer = new host.DataTransfer();
          transfer.items.add(new host.File([blob], 'avatar.jpg', { type: 'image/jpeg' }));
          finish(transfer.files);
        } catch (_) {
          if (!done) showError('compress');
        } finally {
          busy = false;
          if (!done) confirm.disabled = reset.disabled = slider.disabled = false;
        }
      });
      doc.body.append(dialog);
      observer.observe(doc.documentElement, { childList: true, subtree: true });
      host.addEventListener('popstate', checkRoute);
      host.addEventListener('hashchange', checkRoute);
      host.addEventListener('pagehide', cancelDialog);
      dialog.showModal();
      (async () => {
        try {
          validateFile(file);
          const decoded = await host.createImageBitmap(file);
          if (done) { decoded.close(); return; }
          bitmap = decoded;
          if (!valid()) { finish(null); return; }
          if (!bitmap.width || !bitmap.height || bitmap.width * bitmap.height > 40000000) {
            bitmap.close(); bitmap = null;
            throw new Error('dimensions');
          }
          render();
          status.textContent = '';
          confirm.disabled = reset.disabled = slider.disabled = false;
        } catch (err) {
          if (!done) {
            if (bitmap) { bitmap.close(); bitmap = null; }
            showError(err.message);
          }
        }
      })();
      return result;
    }

    doc.addEventListener('submit', (event) => {
      if (active) { event.preventDefault(); event.stopImmediatePropagation(); }
    }, true);
    doc.addEventListener('change', async (event) => {
      const input = event.target;
      if (processed.has(input)) return;
      if (!avatarInput(input)) return;
      event.stopImmediatePropagation();
      const file = input.files?.[0];
      input.value = '';
      if (!file || active) return;
      const selection = {};
      active = selection;
      try {
        const files = await open(file, input);
        if (!files || !input.isConnected || !avatarInput(input)) return;
        input.files = files;
        processed.add(input);
        try { input.dispatchEvent(new host.Event('change', { bubbles: true })); }
        finally { processed.delete(input); input.value = ''; }
      } finally {
        if (active === selection) active = null;
      }
    }, true);
  }

  host.MetarAvatarCropper = { cropRect, validateFile, compress, install };
})(window);
