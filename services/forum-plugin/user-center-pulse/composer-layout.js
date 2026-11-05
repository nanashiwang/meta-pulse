// Keep Answer's controlled fields under their original React-owned parent.
// Moving the whole group preserves selection, validation and keyboard order.
export function mountComposerLayout(doc) {
  const title = doc.querySelector('.page-main form input#title');
  const form = title?.closest('form');
  const titleGroup = title?.parentElement;
  const selector = form?.querySelector('.tag-selector-wrap');
  const tagGroup = selector?.parentElement?.parentElement;
  if (!form || titleGroup?.parentElement !== form || tagGroup?.parentElement !== form) return;
  tagGroup.classList.add('metar-composer-taxonomy');
  if (tagGroup.nextElementSibling !== titleGroup) form.insertBefore(tagGroup, titleGroup);
}

export function menuSpace(rect, viewportTop, viewportBottom) {
  const below = Math.max(0, viewportBottom - rect.bottom - 12);
  const above = Math.max(0, rect.top - viewportTop - 12);
  const up = below < 178 && above > below;
  return { up, height: Math.floor(Math.min(240, up ? above : below)) };
}

export function positionComposerMenus(host) {
  const doc = host.document;
  const viewport = host.visualViewport;
  const top = viewport?.offsetTop || 0;
  const bottom = top + (viewport?.height || host.innerHeight);
  const headerBottom = doc.querySelector('#header')?.getBoundingClientRect().bottom || 0;
  doc.querySelectorAll('.metar-composer-taxonomy .tag-selector-wrap').forEach((selector) => {
    const { up, height } = menuSpace(selector.getBoundingClientRect(), Math.max(top, headerBottom), bottom);
    selector.dataset.metarMenuSide = up ? 'up' : 'down';
    selector.style.setProperty('--metar-menu-height', `${height}px`);
  });
}
