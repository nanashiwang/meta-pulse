/* Incremental public discussions. Content remains in the current document only. */
(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else root.MetarDiscussionStream = api;
})(typeof window === 'undefined' ? globalThis : window, function () {
  'use strict';
  const id = item => String(item?.id || item?.question_id || '');
  function batch(result, offset, size, seen = new Set()) {
    if (!Array.isArray(result?.list) || !Number.isSafeInteger(Number(result.count)) || Number(result.count) < 0 || result.list.some(item => !id(item))) {
      throw new Error('Invalid discussion response');
    }
    const fresh = new Set(seen);
    const list = result.list.filter(item => {
      const key = id(item);
      if (fresh.has(key)) return false;
      fresh.add(key); return true;
    });
    return {list, done: result.list.length < size || offset + size >= Number(result.count)};
  }

  function mount(node, {host = window, request, row, t, current, onDenied, restorePage = 6, onProgress = () => {}}) {
    const list = node.querySelector('.discussion-list');
    const tail = node.querySelector('[data-stream-tail]');
    const message = tail.querySelector('[role="status"]');
    const button = tail.querySelector('button');
    const seen = new Set(Array.from(list.querySelectorAll('[data-question-id]'), item => item.dataset.questionId));
    let page = Number(node.dataset.nextPage), done = node.dataset.done === 'true';
    let busy = false, failed = false, disposed = false, observer = null;
    const active = () => !disposed && node.isConnected && current();
    function paint() {
      node.setAttribute('aria-busy', String(busy));
      message.textContent = t(busy ? '正在加载更多讨论…' : failed ? '加载失败，已显示的讨论仍可阅读。' : done ? '已显示全部讨论' : '继续下滑加载更多');
      button.textContent = t(failed ? '重试加载' : '加载更多');
      button.hidden = done || busy || (!failed && Boolean(observer));
    }
    async function load() {
      if (!active() || busy || done) return;
      busy = true; failed = false; paint();
      observer?.unobserve(tail);
      try {
        const result = await request({page, pageSize:10, order:node.dataset.order, tag:node.dataset.tag});
        if (!active()) return;
        const next = batch(result, (page - 1) * 10, 10, seen);
        list.insertAdjacentHTML('beforeend', next.list.map(row).join(''));
        next.list.forEach(item => seen.add(id(item)));
        page++; done = next.done;
        node.dataset.nextPage = String(page); node.dataset.done = String(done);
        onProgress(page, page <= restorePage);
      } catch (error) {
        if (!active()) return;
        failed = true;
        if ([401,403].includes(error?.status)) { dispose(); onDenied(error); return; }
      } finally {
        busy = false;
        if (active()) {
          paint();
          if (!failed && !done) {
            if (page < restorePage) void load();
            else observer?.observe(tail);
          }
        }
      }
    }
    function dispose() {
      disposed = true; observer?.disconnect(); button.removeEventListener('click', load);
    }
    if (typeof host.IntersectionObserver === 'function') {
      observer = new host.IntersectionObserver(entries => {
        if (!failed && entries.some(entry => entry.isIntersecting)) void load();
      }, {rootMargin:'0px 0px 400px 0px'});
    }
    button.addEventListener('click', load);
    paint();
    if (!done) {
      if (page < restorePage) void load();
      else observer?.observe(tail);
    }
    return {dispose, load};
  }
  return {batch, mount};
});
