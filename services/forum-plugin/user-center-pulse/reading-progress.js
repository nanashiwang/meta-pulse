/* Visual reading position only; never awards experience or reports activity. */
export function readingProgress(top, bottom, viewport, inset = 0) {
  if (![top,bottom,viewport,inset].every(Number.isFinite) || bottom <= top || viewport <= inset) return 0;
  if (bottom <= viewport) return 1;
  const distance = bottom - top - (viewport - inset);
  return distance > 0 ? Math.max(0,Math.min(1,(inset-top)/distance)) : 0;
}
export function installReadingProgress(host = window) {
  if(host.__metarReadingProgress)return;
  const doc=host.document;let article=null,header=null,bar=null,fill=null,frame=0;
  const resize=host.ResizeObserver?new host.ResizeObserver(schedule):null;
  function locate(){
    if(!/^\/questions\/[a-zA-Z0-9]+(?:\/|$)/.test(host.location.pathname)||/^\/questions\/(?:ask|edit|linked)(?:\/|$)/.test(host.location.pathname))return null;
    const heading=doc.querySelector('.questionDetailPage .page-main h1');
    return heading?.parentElement?.querySelector('article.fmt')||null;
  }
  function update(){
    frame=0;const next=locate(),nextHeader=doc.querySelector('#header');
    if(next!==article||nextHeader!==header){resize?.disconnect();article=next;header=nextHeader;if(article)resize?.observe(article);if(header)resize?.observe(header);}
    if(!article||!article.isConnected||!article.getClientRects().length){if(bar&&!bar.hidden)bar.hidden=true;return;}
    if(!bar){bar=doc.createElement('div');bar.id='metar-reading-progress';bar.setAttribute('role','progressbar');bar.setAttribute('aria-valuemin','0');bar.setAttribute('aria-valuemax','100');fill=doc.createElement('span');bar.append(fill);doc.body.append(bar);}
    const rect=article.getBoundingClientRect(),vh=host.innerHeight;
    const inset=Math.max(0,Math.min(vh,header?.getBoundingClientRect().bottom||0));
    const value=readingProgress(rect.top,rect.bottom,vh,inset),percent=String(Math.round(value*100));
    const label=doc.documentElement.lang.startsWith('en')?'Post reading progress':'帖子正文阅读进度';
    if(bar.getAttribute('aria-label')!==label)bar.setAttribute('aria-label',label);
    if(bar.getAttribute('aria-valuenow')!==percent)bar.setAttribute('aria-valuenow',percent);
    const top=inset+'px',scale='scaleX('+value.toFixed(4)+')';
    if(bar.style.top!==top)bar.style.top=top;
    if(fill.style.transform!==scale)fill.style.transform=scale;
    if(bar.hidden)bar.hidden=false;
  }
  function schedule(){if(!frame)frame=host.requestAnimationFrame(update);}
  const mutation=new host.MutationObserver(records=>{if(records.some(r=>!bar?.contains(r.target)))schedule();});
  mutation.observe(doc.documentElement,{childList:true,subtree:true,attributes:true,attributeFilter:['class','hidden','lang']});
  host.addEventListener('scroll',schedule,{passive:true});host.addEventListener('resize',schedule);host.addEventListener('popstate',schedule);host.addEventListener('pageshow',schedule);
  doc.addEventListener('load',schedule,true);
  doc.fonts?.ready.then(schedule).catch(()=>{});
  host.__metarReadingProgress={dispose(){mutation.disconnect();resize?.disconnect();host.cancelAnimationFrame(frame);host.removeEventListener('scroll',schedule);host.removeEventListener('resize',schedule);host.removeEventListener('popstate',schedule);host.removeEventListener('pageshow',schedule);doc.removeEventListener('load',schedule,true);bar?.remove();delete host.__metarReadingProgress;}};
  schedule();
}
