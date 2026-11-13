const test=require('node:test');const assert=require('node:assert/strict');
const {View,validate,importItems,itemPreview}=require('../src/admin-publishing.js');
const item={key:'12345678-1234-1234',username:'operator',kind:'question',title:'准备好的社区话题',content:'准备好的正文内容',tags:['news'],question_id:'',reason:'内容运营',source:''};
test('batch import caps size, creates fresh stable keys and rejects invalid target fields',()=>{
 let id=0;const rows=importItems(JSON.stringify([item,{...item,kind:'answer',question_id:'10010000000000243'}]),()=>String(++id));assert.equal(rows[0].key,'1');assert.equal(rows[1].key,'2');
 assert.throws(()=>importItems(JSON.stringify(Array(21).fill(item)),()=>''));assert.throws(()=>validate({...item,kind:'answer',question_id:'../admin'}));assert.throws(()=>validate({...item,source:'javascript:alert(1)'}));
});
test('preview never runs imported HTML and shows the operator disclosure and source',()=>{
 const html=itemPreview({...item,content:'<script>alert(1)</script>',source:'https://example.com/article'});assert.ok(!html.includes('<script>'));assert.match(html,/&lt;script&gt;/);assert.match(html,/运营代发/);assert.match(html,/example.com/);
});
function setup(results){let calls=[];const nodes=new Map();const node=()=>({disabled:false,checked:true,textContent:'',innerHTML:'',querySelectorAll:()=>[]});const view=new View({request:async(path,options)=>{calls.push(JSON.parse(options.body));const x=results.shift();if(x instanceof Error)throw x;return x;}});view.root={isConnected:true};view.q=s=>{if(!nodes.has(s))nodes.set(s,node());return nodes.get(s);};view.items=[{...item,key:'first-key'},{...item,key:'second-key'}];return {view,calls};}
test('batch publishes sequentially, skips confirmed items, and rechecks the same key after network failure',async()=>{
 const {view,calls}=setup([{status:'published',object_id:'1'},new Error('offline'),{status:'published',object_id:'2'}]);await view.publish();assert.equal(calls.length,2);await view.publish();assert.equal(calls.length,3);assert.equal(calls[1].key,calls[2].key);assert.equal(view.results.get('first-key').status,'published');
});
test('unknown outcomes pause before publishing later rows and navigation stops further submissions',async()=>{
 const f=setup([{status:'needs_review'}]);await f.view.publish();assert.equal(f.calls.length,1);f.view.dispose();await f.view.publish();assert.equal(f.calls.length,1);
});

test('switching login identity stops queued publications',async()=>{
 const f=setup([{status:'published',object_id:'1'}]);let token='admin-a';f.view.client.token=()=>token;f.view.session=token;token='admin-b';await f.view.publish();assert.equal(f.calls.length,0);
});

test('scheduled JSON validates time zones and earlier typed parents and previews relationships',()=>{
 const input=[{id:'topic',username:'editor',kind:'question',title:'一个完整的话题标题',content:'需要讨论的实际内容',tags:['news'],reason:'社区运营'},{id:'reply',parent:'topic',username:'helper',kind:'answer',content:'补充具体操作说明',reason:'社区运营',publish_at:'2026-11-14T10:00:00+08:00'},{id:'nested',parent:'reply',username:'editor',kind:'comment',content:'谢谢补充的操作说明',reason:'社区运营'}];
 const items=importItems(JSON.stringify(input),()=> 'stable-key-123456789');
 assert.equal(items[2].parent,'reply');assert.match(itemPreview(items[1]),/2026-11-14/);
 assert.throws(()=>importItems(JSON.stringify([...input].reverse()),()=> 'stable-key-123456789'));
 input[1].publish_at='2026-11-14T10:00:00';assert.throws(()=>importItems(JSON.stringify(input),()=> 'stable-key-123456789'));
});
test('stable batch envelope survives reload and scheduled submission retries the same payload',async()=>{
 const envelope={batch_id:'forum-launch-demo-20261114',items:[{...item,id:'topic',publish_at:'2026-11-14T09:00:00+08:00'}]};
 const rows=importItems(JSON.stringify(envelope),()=> 'transient-item-key');
 const calls=[];let fail=true;const view=new View({token:()=> 'a',request:async(path,options)=>{calls.push([path,JSON.parse(options.body)]);if(fail){fail=false;throw Error('lost response')}return {status:'queued'}}});
 view.items=rows;view.planKey=envelope.batch_id;view.current=()=>true;view.message=()=>{};view.loadPlans=async()=>{};view.q=()=>({disabled:false});
 await view.publishPlan();await view.publishPlan();assert.deepEqual(calls[0],calls[1]);assert.equal(calls[0][1].key,envelope.batch_id);assert.equal(calls[0][1].items[0].key,undefined);
});
