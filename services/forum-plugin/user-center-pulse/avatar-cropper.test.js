import {test} from 'node:test';
import {File} from 'node:buffer';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
const __dirname = path.dirname(fileURLToPath(import.meta.url));
const scope = {window:{}};
const source = fs.existsSync(path.join(__dirname,'avatar-cropper.js')) ? fs.readFileSync(path.join(__dirname,'avatar-cropper.js'),'utf8') : '';
vm.runInNewContext(source, scope);
const api = scope.window.MetarAvatarCropper || {};
test('landscape starts centered with square source', () => {
  assert.equal(typeof api.cropRect, 'function');
  const r = api.cropRect(1200,800,1,0,0);
  assert.deepEqual(JSON.parse(JSON.stringify(r)),{x:200,y:0,size:800});
});
test('drag is clamped and zoom never exposes empty pixels', () => {
  const r=api.cropRect(800,1200,2,999,-999);
  assert.equal(r.size,400); assert.equal(r.x,0); assert.equal(r.y,800);
});
test('reject unsupported and large files before decoding',()=>{
  assert.throws(()=>api.validateFile({type:'image/heic',size:100}),/format/);
  assert.throws(()=>api.validateFile({type:'image/png',size:21*1024*1024}),/large/);
  assert.doesNotThrow(()=>api.validateFile({type:'image/webp',size:5*1024*1024}));
});
test('portrait, square and maximum zoom preserve source dimensions', () => {
  assert.deepEqual(JSON.parse(JSON.stringify(api.cropRect(800,1200,1,0,0))),{x:0,y:200,size:800});
  assert.deepEqual(JSON.parse(JSON.stringify(api.cropRect(400,400,4,0,0))),{x:150,y:150,size:100});
  const rect=api.cropRect(1200,800,4,-9999,9999);
  assert.deepEqual(JSON.parse(JSON.stringify(rect)),{x:1000,y:0,size:200});
});
test('allows supported formats at the exact input size limit', () => {
  for (const type of ['image/jpeg','image/png','image/webp']) {
    assert.doesNotThrow(()=>api.validateFile({type,size:20*1024*1024}));
  }
  assert.throws(()=>api.validateFile({type:'image/jpeg',size:20*1024*1024+1}),/large/);
});
test('compression rejects null blobs and encoder failures', async () => {
  await assert.rejects(()=>api.compress({toBlob(cb){cb(null);}}),/compress/);
  await assert.rejects(()=>api.compress({toBlob(){throw new Error('encoder');}}),/compress/);
});
test('compression uses the exact quality ladder and accepts the exact output limit', async () => {
  const qualities=[];
  const blob=await api.compress({toBlob(cb,type,q){
    assert.equal(type,'image/jpeg'); qualities.push(q);
    cb({size:q===.4?200*1024:200*1024+1});
  }});
  assert.equal(blob.size,200*1024);
  assert.deepEqual(qualities,[.9,.8,.7,.6,.5,.4]);
});
test('installation only captures avatar file changes on the exact profile route', async () => {
  const handlers={};
  const host={
    location:{pathname:'/users/settings/profile'},
    document:{addEventListener(name,handler,capture){assert.equal(capture,true);handlers[name]=handler;}},
  };
  vm.runInNewContext(source,{window:host});
  host.MetarAvatarCropper.install({getLanguage:()=> 'en_US'});
  let stopped=false;
  const event={target:{matches:()=>false},stopImmediatePropagation(){stopped=true;}};
  await handlers.change(event);
  assert.equal(stopped,false);
  host.location.pathname='/users/settings/profile/other';
  event.target={matches:()=>true};
  await handlers.change(event);
  assert.equal(stopped,false);
  const group={querySelector:()=>({parentElement:{parentElement:group}})};
  event.target={matches:()=>true,parentElement:group,files:[],value:'old'};
  host.location.pathname='/users/settings/profile';
  await handlers.change(event);
  assert.equal(stopped,true);
  assert.equal(event.target.value,'');
  let prevented=false;
  handlers.submit({preventDefault(){prevented=true;}});
  assert.equal(prevented,false);
});
test('compression reduces quality and enforces target ceiling',async()=>{
  const qualities=[];
  const blob=await api.compress({toBlob(cb,type,q){qualities.push(q);cb({size:q>.7?300000:100000});}});
  assert.ok(blob.size<=200*1024); assert.ok(qualities.length>1);
  await assert.rejects(()=>api.compress({toBlob(cb){cb({size:999999});}}),/compress/);
});

const settle = () => new Promise((resolve) => setImmediate(resolve));

function cropperHarness({decodeFails=false, deferEncode=false}={}) {
  class FakeEvent {
    constructor(type, options={}) { this.type=type; Object.assign(this,options); }
    stopImmediatePropagation() { this.stopped=true; }
    preventDefault() { this.defaultPrevented=true; }
  }
  class Target {
    constructor() { this.listeners=new Map(); }
    addEventListener(type, handler, capture=false) {
      const listeners=this.listeners.get(type) || [];
      listeners.push({handler,capture}); this.listeners.set(type,listeners);
    }
    removeEventListener(type, handler) {
      this.listeners.set(type,(this.listeners.get(type) || []).filter((item)=>item.handler!==handler));
    }
    emit(event, capture=false) {
      for (const item of this.listeners.get(event.type) || []) {
        if (item.capture!==capture) continue;
        const result=item.handler(event);
        if (result?.then) pending.push(result);
        if (event.stopped) break;
      }
    }
  }
  const pending=[], uploads=[], nativeEvents=[], canvases=[], encodes=[], observers=new Set(), timers=new Map();
  const bitmaps=[], decodedFiles=[];
  let nextTimer=0, fileAssignments=0;
  class Element extends Target {
    constructor(tag) {
      super(); this.tagName=tag; this.isConnected=false; this.hidden=false; this.disabled=false;
      this.textContent=''; this.children=[]; this.width=0; this.height=0;
    }
    setAttribute(name,value) { this[name]=value; }
    append(child) { this.children.push(child); child.parentElement=this; child.isConnected=this.isConnected; }
    remove() {
      this.isConnected=false;
      if (this.parentElement) this.parentElement.children=this.parentElement.children.filter((child)=>child!==this);
    }
    focus() { doc.activeElement=this; }
    querySelector(selector) { return this.selectors?.[selector] || null; }
    showModal() { this.open=true; }
    close() { this.open=false; this.emit(new FakeEvent('close')); }
    click() { if (!this.disabled) this.emit(new FakeEvent('click')); }
    set innerHTML(html) {
      assert.match(html, /metar-avatar-editor/);
      this.selectors={};
      for (const selector of ['h2','.metar-avatar-help','label span','input','[role="alert"]','[role="status"]',
        '[data-action="reset"]','[data-action="cancel"]','[data-action="confirm"]']) {
        this.selectors[selector]=new Element('control');
      }
      this.selectors.input.value='1';
      for (const selector of ['input','[data-action="reset"]','[data-action="confirm"]']) this.selectors[selector].disabled=true;
      this.selectors['[role="alert"]'].hidden=true;
      for (const [selector,size] of [['.metar-avatar-editor',560],['.metar-avatar-preview',160]]) {
        const canvas=createCanvas(); canvas.width=canvas.height=size; this.selectors[selector]=canvas;
      }
    }
  }
  function createCanvas() {
    const canvas=new Element('canvas');
    canvas.draws=[]; canvas.fills=[];
    const ctx={
      fillRect(...args) { canvas.fills.push({style:ctx.fillStyle,args}); },
      drawImage(...args) { canvas.draws.push(args); },
    };
    canvas.getContext=(type)=>{ assert.equal(type,'2d'); return ctx; };
    canvas.toBlob=(callback,type,quality)=>{
      encodes.push({canvas,type,quality});
      const complete=()=>callback(new Blob([new Uint8Array(quality>.7?220*1024:200*1024)],{type}));
      if (deferEncode) canvas.completeEncode=complete; else complete();
    };
    canvases.push(canvas); return canvas;
  }
  const doc=new Target();
  doc.body=new Element('body'); doc.body.isConnected=true; doc.documentElement=doc.body;
  doc.createElement=(tag)=>tag==='canvas'?createCanvas():new Element(tag);
  const host=new Target();
  Object.assign(host,{
    document:doc, location:{pathname:'/users/settings/profile'}, Event:FakeEvent, File,
    DataTransfer:class {
      constructor() { this.files=[]; this.items={add:(file)=>this.files.push(file)}; }
    },
    MutationObserver:class {
      constructor(callback) { this.callback=callback; }
      observe() { observers.add(this); }
      disconnect() { observers.delete(this); }
    },
    setInterval(callback) { const id=++nextTimer; timers.set(id,callback); return id; },
    clearInterval(id) { timers.delete(id); },
    async createImageBitmap(file) {
      decodedFiles.push(file);
      if (decodeFails) throw new Error('decode');
      const bitmap={width:1200,height:800,closed:0,close(){this.closed++;}};
      bitmaps.push(bitmap); return bitmap;
    },
  });
  const group=new Element('group'), selectParent=new Element('label'), select=new Element('select');
  group.append(selectParent); selectParent.append(select);
  group.selectors={'select[name="avatar.type"]':select}; doc.body.append(group);
  const input=new Element('input'); group.append(input);
  let files=[], value='';
  Object.defineProperties(input,{
    files:{get:()=>files,set(next){fileAssignments++; files=next; value=next.length ? `C:\\fakepath\\${next[0].name}` : '';}},
    value:{get:()=>value,set(next){value=next; if (next==='') files=[];}},
  });
  input.matches=(selector)=>selector==='input[type="file"]';
  input.dispatchEvent=(event)=>{
    event.target=input;
    if (event.type==='change') nativeEvents.push(event);
    doc.emit(event,true);
    if (!event.stopped) input.emit(event);
    if (event.bubbles && !event.stopped) doc.emit(event);
    return !event.defaultPrevented;
  };
  input.addEventListener('change',(event)=>uploads.push({event,files:input.files}));
  vm.runInNewContext(source,{window:host});
  host.MetarAvatarCropper.install({getLanguage:()=> 'en_US'});
  return {
    host,doc,input,uploads,nativeEvents,canvases,encodes,bitmaps,decodedFiles,timers,observers,pending,
    get fileAssignments() { return fileAssignments; },
    get dialog() { return doc.body.children.find((child)=>child.tagName==='dialog'); },
    select(file) {
      const next=`C:\\fakepath\\${file.name}`;
      if (value===next) return false;
      files=[file]; value=next;
      input.dispatchEvent(new FakeEvent('change',{bubbles:true})); return true;
    },
    mutate() { for (const observer of [...observers]) observer.callback([]); },
    route(event='popstate') { host.location.pathname='/questions'; host.emit(new FakeEvent(event)); },
    tick() { for (const callback of [...timers.values()]) callback(); },
    async drained() { await Promise.all(pending); await settle(); },
    assertClosed() {
      assert.equal(this.dialog,undefined);
      assert.equal(timers.size,0); assert.equal(observers.size,0);
      for (const type of ['popstate','hashchange','pagehide']) assert.equal(host.listeners.get(type)?.length,0);
      for (const bitmap of bitmaps) assert.equal(bitmap.closed,1);
    },
  };
}

const originalAvatar = () => new File(['original PNG bytes'],'portrait.png',{type:'image/png'});
const action = (dialog,name) => dialog.querySelector(`[data-action="${name}"]`);

test('original avatar selection is intercepted; cancel never reaches the native upload', async () => {
  const h=cropperHarness(), original=originalAvatar();
  h.select(original);
  assert.equal(h.nativeEvents[0].stopped,true);
  assert.equal(h.uploads.length,0);
  assert.equal(h.input.value,'');
  await settle();
  assert.equal(h.dialog.open,true);
  assert.equal(action(h.dialog,'confirm').disabled,false);
  assert.equal(h.uploads.length,0);
  assert.equal(h.encodes.length,0);
  action(h.dialog,'cancel').click();
  await h.drained();
  assert.equal(h.uploads.length,0);
  assert.equal(h.fileAssignments,0);
  assert.equal(h.nativeEvents.length,1);
  h.assertClosed();
});

test('confirm crops a white-backed 512 JPEG <=200 KiB and dispatches one native change through capture', async () => {
  const h=cropperHarness(), original=originalAvatar();
  h.select(original); await settle();
  const dialog=h.dialog;
  action(dialog,'confirm').click();
  action(dialog,'confirm').click();
  await h.drained();
  assert.equal(h.uploads.length,1);
  assert.equal(h.fileAssignments,1);
  assert.equal(h.nativeEvents.length,2);
  const uploaded=h.uploads[0];
  assert.equal(uploaded.event,h.nativeEvents[1]);
  assert.equal(uploaded.event.bubbles,true);
  assert.notEqual(uploaded.event.stopped,true);
  assert.equal(uploaded.files.length,1);
  const file=uploaded.files[0];
  assert.notEqual(file,original);
  assert.equal(file.name,'avatar.jpg'); assert.equal(file.type,'image/jpeg');
  assert.equal(file.size,200*1024);
  assert.deepEqual(h.encodes.map(({quality})=>quality),[.9,.8,.7]);
  const output=h.encodes[0].canvas;
  assert.equal(output.width,512); assert.equal(output.height,512);
  assert.ok(h.encodes.every(({canvas,type})=>canvas===output && type==='image/jpeg'));
  assert.deepEqual(output.fills,[{style:'#fff',args:[0,0,512,512]}]);
  assert.equal(output.draws[0][0],h.bitmaps[0]);
  assert.deepEqual(output.draws[0].slice(1),[200,0,800,800,0,0,512,512]);
  assert.equal(h.decodedFiles.length,1);
  h.assertClosed();
});

test('the same original file can be selected again after cancel and after confirmation', async () => {
  const h=cropperHarness(), file=new File(['original JPEG'],'avatar.jpg',{type:'image/jpeg'});
  for (const name of ['cancel','confirm','cancel']) {
    assert.equal(h.select(file),true);
    await settle();
    assert.ok(h.dialog);
    assert.equal(h.input.value,'');
    action(h.dialog,name).click();
    await h.drained(); h.assertClosed();
  }
  assert.equal(h.decodedFiles.length,3);
  assert.equal(h.uploads.length,1);
  assert.equal(h.fileAssignments,1);
});

for (const invalidation of ['input removal','popstate','hashchange','route polling']) {
  test(`${invalidation} during encoding closes the dialog and prevents late upload`, async () => {
    const h=cropperHarness({deferEncode:true});
    h.select(originalAvatar()); await settle();
    const dialog=h.dialog;
    action(dialog,'confirm').click();
    assert.equal(h.encodes.length,1);
    if (invalidation==='input removal') { h.input.remove(); h.mutate(); }
    else if (invalidation==='route polling') { h.host.location.pathname='/questions'; h.tick(); }
    else h.route(invalidation);
    assert.equal(dialog.open,false);
    h.assertClosed();
    for (let index=0; index<h.encodes.length; index++) {
      h.encodes[index].canvas.completeEncode(); await settle();
    }
    await h.drained();
    assert.equal(h.uploads.length,0);
    assert.equal(h.fileAssignments,0);
    assert.equal(h.nativeEvents.length,1);
    h.assertClosed();
  });
}

test('decode failure displays an alert; cancel leaves the native upload untouched', async () => {
  const h=cropperHarness({decodeFails:true});
  h.select(originalAvatar()); await settle();
  const dialog=h.dialog, alert=dialog.querySelector('[role="alert"]');
  assert.equal(alert.hidden,false);
  assert.equal(alert.textContent,'This image could not be opened. Please choose another image.');
  assert.equal(dialog.querySelector('[role="status"]').textContent,'');
  assert.equal(action(dialog,'confirm').disabled,true);
  action(dialog,'confirm').click();
  assert.equal(h.encodes.length,0);
  action(dialog,'cancel').click(); await h.drained();
  assert.equal(h.fileAssignments,0);
  assert.equal(h.uploads.length,0);
  assert.equal(h.nativeEvents.length,1);
  h.assertClosed();
});

test('a non-avatar file input keeps its file and original native change behavior', async () => {
  const h=cropperHarness(), file=new File(['attachment'],'document.txt',{type:'text/plain'});
  h.doc.body.append(h.input);
  h.select(file); await h.drained();
  assert.equal(h.uploads.length,1);
  assert.equal(h.uploads[0].files[0],file);
  assert.equal(h.nativeEvents.length,1);
  assert.notEqual(h.nativeEvents[0].stopped,true);
  assert.notEqual(h.input.value,'');
  assert.equal(h.fileAssignments,0);
  assert.equal(h.decodedFiles.length,0);
  assert.equal(h.canvases.length,0);
  assert.equal(h.dialog,undefined);
});
