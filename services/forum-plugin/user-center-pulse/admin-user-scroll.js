// Presentation only: preserve Answer filtering, sorting and user actions.
export function scrollPercent(left, width, viewport) {
  const max = Math.max(0, width - viewport);
  return max ? Math.max(0, Math.min(100, (left / max) * 100)) : 0;
}
export function installAdminUserScroll(host = window) {
  const doc = host.document;
  let current = null,
    pending = false;
  function clear() {
    if (!current) return;
    current.resize?.disconnect();
    current.box.removeEventListener('scroll', current.sync);
    current.bar.remove();
    current = null;
  }
  function mount() {
    pending = false;
    const box = /^\/admin\/users\/?$/.test(host.location.pathname)
      ? doc.querySelector('.table-responsive')
      : null;
    if (!box) {
      clear();
      return;
    }
    if (current?.box === box) {
      current.sync();
      return;
    }
    clear();
    const bar = doc.createElement('div');
    bar.className = 'metar-user-scrollbar';
    const label = doc.createElement('label'),
      range = doc.createElement('input');
    range.type = 'range';
    range.min = '0';
    range.max = '100';
    range.step = '0.1';
    const text = doc.createElement('span');
    text.textContent = doc.documentElement.lang.startsWith('en')
      ? 'Scroll table horizontally'
      : '左右拖动查看完整表格';
    range.setAttribute('aria-label', text.textContent);
    label.append(text, range);
    bar.append(label);
    box.before(bar);
    const sync = () => {
      const max = Math.max(0, box.scrollWidth - box.clientWidth);
      bar.hidden = max <= 1;
      range.disabled = max <= 1;
      range.value = String(
        scrollPercent(box.scrollLeft, box.scrollWidth, box.clientWidth),
      );
      range.setAttribute(
        'aria-valuetext',
        Math.round(Number(range.value)) + '%',
      );
    };
    range.oninput = () => {
      box.scrollLeft =
        ((box.scrollWidth - box.clientWidth) * Number(range.value)) / 100;
    };
    box.addEventListener('scroll', sync, { passive: true });
    const resize = host.ResizeObserver ? new host.ResizeObserver(sync) : null;
    resize?.observe(box);
    const table = box.querySelector('table');
    if (table) resize?.observe(table);
    current = { box, bar, sync, resize };
    sync();
  }
  const schedule = () => {
    if (!pending) {
      pending = true;
      host.requestAnimationFrame(mount);
    }
  };
  new host.MutationObserver((records) => {
    if (records.some((r) => !current?.bar.contains(r.target))) schedule();
  }).observe(doc.documentElement, { childList: true, subtree: true });
  host.addEventListener('resize', schedule);
  host.addEventListener('popstate', schedule);
  mount();
}
