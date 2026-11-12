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
