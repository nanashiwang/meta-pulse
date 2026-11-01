import {test} from 'node:test';
import assert from 'node:assert/strict';
import {parser} from '@lezer/markdown';
import {imagesIn,imageChanges,createImporter,downloadAndUpload} from '../image-import.js';
const origin='https://community.test', source='https://mmbiz.qpic.cn/a/640?wx_fmt=png&x=1';
const image=`![图](${source})`;
const apply=(text,changes)=>changes.toReversed().reduce((s,c)=>s.slice(0,c.from)+c.insert+s.slice(c.to),text);
test('images only: code, ordinary links, local uploads and escaped syntax are unchanged',()=>{
 const text=`${image}\n[普通链接](${source})\n\n\`${image}\`\n\n\`\`\`md\n${image}\n\`\`\`\n\n\\${image}\n![local](/uploads/a.png)`;
 const images=imagesIn(text,parser,origin);assert.equal(images.length,1);assert.equal(images[0].url,source);
 assert.equal(apply(text,imageChanges(text,parser,origin,source,'/uploads/new.png')),text.replace(image,'![图](/uploads/new.png)'));
});
test('inline titles, parentheses, nested/escaped alt, reference and collapsed images',()=>{
 for(const markup of ['![a](<https://example.org/a(b).png> "title")','![a][id]\n\n[id]: https://example.org/a.png "title"','![a][]\n\n[a]: https://example.org/a.png','![a]\n\n[a]: https://example.org/a.png','![a\\]b][id]\n\n[id]: https://example.org/a.png']){
  const imgs=imagesIn(markup,parser,origin);assert.equal(imgs.length,1,markup);
  const changed=apply(markup,imageChanges(markup,parser,origin,imgs[0].url,'/uploads/a.png'));
  assert.match(changed,/\/uploads\/a.png/);if(markup.includes('"title"'))assert.match(changed,/"title"/);
 }
});
function fixture(importImage){
 let text=image, token='user1', notices=[];
 const editor={getValue:()=>text,dispatch:({changes})=>{text=apply(text,changes)}};
 const task=createImporter({editor,parser,origin,identity:()=>token,importImage,notify:n=>notices.push(n),delay:0});
 return {task,editor,get text(){return text},set text(v){text=v},set token(v){token=v},notices};
}
const tick=()=>new Promise(r=>setTimeout(r,5));
test('deduplicates images, preserves concurrent edits and removes failed status after deletion',async()=>{
 let release,calls=0;const f=fixture(()=>{calls++;return new Promise(r=>release=r)});
 f.text=`${image}\n${image}`;f.task.scan();await tick();f.text=`new words\n${image}\n${image}`;
 release('/uploads/new.png');await tick();assert.equal(calls,1);assert.equal(f.text,'new words\n![图](/uploads/new.png)\n![图](/uploads/new.png)');f.task.dispose();
});
test('failed images remain intact, retry succeeds, saved destinations never reimport',async()=>{
 let calls=0;const f=fixture(async()=>{if(++calls===1)throw Error('offline');return 'https://cdn.test/saved.png'});
 f.task.scan();await tick();assert.equal(f.text,image);assert.equal(f.notices.at(-1).failed,1);
 f.task.scan();await tick();assert.equal(calls,1);
 f.task.retry();await tick();assert.equal(calls,2);assert.match(f.text,/cdn.test/);
 f.task.scan();await tick();assert.equal(calls,2);f.task.dispose();
});
test('deleted image, logout and disposed editor reject late replacements',async()=>{
 for(const action of ['delete','logout','dispose']){
  let release;const f=fixture(()=>new Promise(r=>release=r));f.task.scan();await tick();
  if(action==='delete')f.text='kept words';if(action==='logout')f.token='other';if(action==='dispose')f.task.dispose();
  release('/uploads/late.png');await tick();assert.equal(f.text,action==='delete'?'kept words':image);f.task.dispose();
 }
});
test('native upload receives validated image bytes, captured token and post source',async()=>{
 const calls=[];const response=new Response(new Blob(['png'],{type:'image/png'}));
 const fetcher=async(path,options)=>{calls.push({path,options});return calls.length===1?response:Response.json({data:'/uploads/a.png'});};
 assert.equal(await downloadAndUpload(source,'session',new AbortController().signal,fetcher),'/uploads/a.png');
 assert.equal(calls[1].path,'/answer/api/v1/file');assert.equal(calls[1].options.headers.Authorization,'session');assert.equal(calls[1].options.body.get('source'),'post');assert.equal(calls[1].options.body.get('file').size,3);
});
test('invalid download and changed identity never reach upload',async()=>{
 for(const changed of [false,true]){
  let count=0;const fetcher=async()=>{count++;return new Response(new Blob(['bad'],{type:changed?'image/png':'text/html'}));};
  await assert.rejects(downloadAndUpload(source,'session',new AbortController().signal,fetcher,()=>!changed));assert.equal(count,changed?0:1);
 }
});

test('two editors serialize transfers, cancellation before upload preserves identity',async()=>{
 let release, calls=0;
 const fetcher=async(path)=>{calls++;if(path.endsWith('/fetch'))return new Promise(r=>release=()=>r(new Response(new Blob(['png'],{type:'image/png'}))));return Response.json({data:'/uploads/one.png'});};
 let owner=true;
 const one=downloadAndUpload(source,'session',new AbortController().signal,fetcher,()=>owner);
 const cancelled=new AbortController();
 const two=downloadAndUpload(source,'session',cancelled.signal,fetcher);
 const rejectedTwo=assert.rejects(two);
 await tick();assert.equal(calls,1);cancelled.abort();owner=false;release();
 await assert.rejects(one);await rejectedTwo;assert.equal(calls,1);
});

test('fragments do not cause duplicate downloads of the same image',()=>{
 const images=imagesIn('![one](https://example.org/a.png#one) ![two](https://example.org/a.png#two)',parser,origin);
 assert.equal(images.length,2);assert.equal(images[0].url,images[1].url);
});
