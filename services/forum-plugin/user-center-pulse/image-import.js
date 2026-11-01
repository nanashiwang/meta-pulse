// Framework-independent orchestration; the official Answer editor supplies the
// Markdown syntax tree and CodeMirror transactions, never scraped DOM text.
export function imagesIn(text, parser, origin) {
  if (text.length > 500000) return [];
  const tree = parser.parse(text), refs = new Map(), images = [];
  const label = (v) => v.replace(/^\[|\]$/g, '').replace(/\s+/g, ' ').trim().toLowerCase();
  const clean = (v) => v.replace(/^<|>$/g, '').replace(/\\([!"#$%&'()*+,\-./:;<=>?@[\]\\^_`{|}~])/g, '$1').replace(/&amp;/g, '&');
  tree.iterate({ enter(node) {
    if (node.name !== 'LinkReference') return;
    const mark = node.node.getChild('LinkLabel'), url = node.node.getChild('URL');
    const title=node.node.getChild('LinkTitle');
    if (mark && url) refs.set(label(text.slice(mark.from, mark.to)), {url:clean(text.slice(url.from,url.to)), title:title?text.slice(title.from,title.to):''});
  }});
  tree.iterate({ enter(node) {
    if (node.name !== 'Image') return;
    const urlNode = node.node.getChild('URL');
    const raw = text.slice(node.from,node.to);
    const close=node.node.getChildren('LinkMark').find(n=>text.slice(n.from,n.to)===']');
    const altEnd=close?close.to:node.to;
    let title='', url = urlNode ? clean(text.slice(urlNode.from,urlNode.to)) : '';
    if (!url) {
      const ref = node.node.getChild('LinkLabel');
      const key = ref && ref.to-ref.from>2 ? text.slice(ref.from,ref.to) : text.slice(node.from+1,altEnd);
      const definition=refs.get(label(key));
      url=definition?.url||''; title=definition?.title||'';
    }
    try {
      const parsed = new URL(url, origin);
      if (!url.startsWith('https://') || parsed.origin === origin || parsed.username || parsed.password) return;
      parsed.hash='';
      url=parsed.href;
      images.push({ from:node.from, to:node.to, raw, url, title, altEnd, urlFrom:urlNode?.from, urlTo:urlNode?.to });
    } catch (_) { /* invalid destinations stay unchanged */ }
  }});
  return images;
}

export function imageChanges(text, parser, origin, source, destination) {
  // Keep destination syntax inert even for a storage plugin's unusual filename.
  const url = destination.replace(/[<>\s()\\]/g, c => encodeURIComponent(c));
  return imagesIn(text,parser,origin).filter(i=>i.url===source).map(i=> {
    if (i.urlFrom != null) {
      const angle = text[i.urlFrom] === '<';
      return {from:i.urlFrom,to:i.urlTo,insert:angle?`<${url}>`:url};
    }
    // Reference-style images get an inline destination; other uses of their
    // shared definition (including ordinary links) retain their original URL.
    return {from:i.from,to:i.to,insert:`${text.slice(i.from,i.altEnd)}(<${url}>${i.title?' '+i.title:''})`};
  });
}

export function createImporter({editor,parser,origin,identity,importImage,notify,delay=350}) {
  let disposed=false, timer, running=false, controller, activeSource;
  const owner=identity(), pending=new Set(), results=new Map(), failed=new Set();
  const valid=()=>!disposed && !!owner && identity()===owner;
  const current=()=>imagesIn(editor.getValue(),parser,origin);
  const report=()=>notify({busy:running, pending:pending.size, failed:failed.size, complete:results.size});
  const apply=(source,dest)=> {
    if(!valid()) return;
    const changes=imageChanges(editor.getValue(),parser,origin,source,dest);
    if(changes.length) editor.dispatch({changes});
  };
  async function run() {
    if(running || !valid()) return;
    running=true; report();
    try {
      while(pending.size && valid()) {
        const source=pending.values().next().value; pending.delete(source); activeSource=source;
        if(!current().some(i=>i.url===source)) continue;
        try {
          controller=new AbortController();
          const dest=results.get(source) || await importImage(source,owner,controller.signal);
          if(!valid()) break;
          const u=new URL(dest,origin);
          if(!['http:','https:'].includes(u.protocol) || u.username || u.password || dest===source) throw new Error('invalid_upload');
          results.set(source,dest); failed.delete(source); apply(source,dest);
        } catch (_) { if(valid()) failed.add(source); }
        report();
      }
    } finally { activeSource=null; running=false; if(!disposed) report(); }
  }
  function scan(retry=false) {
    if(!valid()) return;
    const imgs=current(), present=new Set(imgs.map(i=>i.url));
    for(const source of failed) if(!present.has(source)) failed.delete(source);
    let count=0;
    for(const source of present) {
      if([...results.values()].includes(source)) continue;
      if(results.has(source)) { apply(source,results.get(source)); continue; }
      if(count++ >= 30) { failed.add(source); continue; }
      if(retry) failed.delete(source);
      if(!failed.has(source) && source!==activeSource) pending.add(source);
    }
    report(); void run();
  }
  function schedule() { clearTimeout(timer); timer=setTimeout(()=>{timer=null;scan();},delay); }
  // Changes cover native HTML-to-Markdown paste, plain Markdown, restored drafts
  // and the image-link toolbar. Debouncing avoids interfering with native paste.
  return { isBusy:()=>running || pending.size>0 || (!!timer && current().some(i=>!results.has(i.url) && !failed.has(i.url) && ![...results.values()].includes(i.url))), schedule, retry:()=>scan(true), dispose() {disposed=true;clearTimeout(timer);controller?.abort();pending.clear();}, scan };
}

// Multiple native editors (question + own answer) share the account's single
// download slot; each editor still owns its cancellation and replacement state.
let uploadTail = Promise.resolve();
export function downloadAndUpload(source, token, signal, fetcher=fetch, stillOwner=()=>true) {
  const operation=uploadTail.then(()=>{
    if(signal.aborted || !stillOwner()) throw new Error('cancelled');
    return downloadAndUploadNow(source,token,signal,fetcher,stillOwner);
  });
  uploadTail=operation.catch(()=>{});
  return operation;
}
async function downloadAndUploadNow(source, token, signal, fetcher, stillOwner) {
  const headers={Authorization:token,'X-Metar-Request':'1'};
  const timeout=new AbortController();
  const abort=()=>timeout.abort(); signal.addEventListener('abort',abort,{once:true});
  if(signal.aborted) abort();
  const timer=setTimeout(abort,45000);
  try {
    const response=await fetcher('/answer/api/v1/metar/images/fetch',{method:'POST',credentials:'same-origin',signal:timeout.signal,headers:{...headers,'Content-Type':'application/json'},body:JSON.stringify({url:source})});
    if(!response.ok) throw new Error('download_failed');
    const blob=await response.blob();
    const ext={'image/png':'png','image/jpeg':'jpg','image/gif':'gif','image/webp':'webp'}[blob.type];
    if(!ext || !blob.size || blob.size>5*1024*1024) throw new Error('invalid_image');
    if(!stillOwner()) throw new Error('identity_changed');
    const form=new FormData();form.append('source','post');form.append('file',blob,`imported.${ext}`);
    const upload=await fetcher('/answer/api/v1/file',{method:'POST',credentials:'same-origin',signal:timeout.signal,headers,body:form});
    if(!upload.ok) throw new Error('upload_failed');
    const result=await upload.json();
    if(typeof result.data!=='string' || !result.data) throw new Error('invalid_upload');
    return result.data;
  } finally {clearTimeout(timer);signal.removeEventListener('abort',abort);}
}
