const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const {webcrypto}=require('node:crypto');
function setup(language='zh_CN') {
 const records=new Map();
 const window={sessionStorage:{getItem:k=>records.get(k)||null,setItem:(k,v)=>records.set(k,v),removeItem:k=>records.delete(k)},crypto:webcrypto,MetarI18n:{getLanguage:()=>language}};
 vm.runInNewContext(fs.readFileSync(require.resolve('../src/marketplace.js'),'utf8'),{window,URL,URLSearchParams});
 return {api:window.MetarMarketplace,records,window};
}
const item=()=>({id:'1',title:'Prompt <script>alert(1)</script>',author:{username:'alice',display_name:'<img src=x>'},entry:{kind:'project',question_id:'10010000000000108',summary:'A prompt workspace',use_case:'development',github_url:'https://github.com/example/project',demo_url:'https://demo.example.com/app',pricing:'free',access_note:'Author pays for AI',data_note:'No saved inputs',relation:'self'},status:'published',has_published:true,embed_allowed:true,demo_status:'available',answer_count:0});
const page=items=>({items,count:items.length,page:1,page_size:24});
test('unconfirmed submissions keep the original request and key across reloads and isolate actors',async()=>{
 const {api,records}=setup(),calls=[];
 const adapter={request:async(path,opt)=>{if(opt){calls.push({path,headers:opt.headers,body:opt.body});throw Error('lost response');}return page([]);}};
 const v=new api.View(adapter,adapter);await v.page('/marketplace/mine',{id:'2'},new URLSearchParams());
 const command={op:'submit',entry:{question_id:'10010000000000108'}};
 await assert.rejects(v.command(command));const saved=JSON.parse([...records.values()][0]);
 const reload=new api.View(adapter,adapter);const html=await reload.page('/marketplace/mine',{id:'2'},new URLSearchParams());
 assert.equal(reload.pending.key,saved.key);assert.match(html,/重试原操作/);await assert.rejects(reload.retry());
 assert.equal(calls[0].headers['Idempotency-Key'],calls[1].headers['Idempotency-Key']);assert.equal(calls[0].body,calls[1].body);
 await assert.rejects(reload.command({op:'archive',id:'1'}),/pending/);assert.equal(calls.length,2);
 const other=new api.View(adapter,adapter);await other.page('/marketplace/mine',{id:'3'},new URLSearchParams());assert.equal(other.pending,null);
});
test('confirmed mutation clears its receipt before any refresh and never sends again on read failure',async()=>{
 const {api,records}=setup();let posts=0;
 const adapter={request:async(path,opt)=>{if(opt){posts++;return item();}throw Error('read failed');}};
 const v=new api.View(adapter,adapter);v.storageKey='actor-2';
 await v.command({op:'submit'});assert.equal(v.pending,null);assert.equal(records.size,0);
 await assert.rejects(v.mine());await assert.rejects(v.retry(),/missing operation/);assert.equal(posts,1);
});
test('admin reviews use the Answer admin namespace and restore the same request key',async()=>{
 const {api}=setup(),calls=[];const normal={request:async()=>page([])},admin={request:async(path,opt)=>{calls.push({path,opt});if(opt)return item();return page([]);}};
 const v=new api.View(normal,admin);await v.page('/admin/marketplace',{id:'1'},new URLSearchParams());
 await v.command({op:'review',id:'1',version:2,decision:'publish'},true);
 assert.equal(calls.at(-1).path,'/metar/marketplace/admin/command');assert.equal(calls.at(-1).opt.headers['X-Metar-Request'],'1');
});
test('public cards and detail escape authored text and preserve GitHub, demo and native discussion routes',async()=>{
 const {api}=setup(),i=item();const adapter={request:async()=>({item:i,projects:[]})};
 const v=new api.View(adapter,adapter);const html=await v.page('/marketplace/item/1',null,new URLSearchParams());
 assert.match(html,/&lt;script&gt;/);assert.doesNotMatch(html,/<script>/);assert.match(html,/questions\/10010000000000108/);assert.match(html,/https:\/\/github.com\/example\/project/);assert.match(html,/data-market-run/);assert.doesNotMatch(html,/<iframe/);
 i.demo_status='unavailable';const disabled=await v.page('/marketplace/item/1',null,new URLSearchParams());assert.doesNotMatch(disabled,/data-market-run/);assert.match(disabled,/演示暂不可用/);
 assert.equal(api.safeURL('javascript:alert(1)'), '');assert.equal(api.safeURL('https://evil.example.com/?key=secret'), '');
});
test('storage failures do not block public browsing and prevent unrecoverable writes',async()=>{
 const {api,window}=setup();window.sessionStorage.getItem=()=>{throw Error('blocked')};let posts=0;
 const adapter={request:async(path,opt)=>{if(opt)posts++;return page([]);}};
 const v=new api.View(adapter,adapter);const html=await v.page('/marketplace',{id:'2'},new URLSearchParams());assert.match(html,/Vibe Coding/);
 await assert.rejects(v.command({op:'submit'}),error=>error.code==='storage_unavailable');assert.equal(posts,0);
});
test('an account change cannot send the previous account’s pending command',async()=>{
 const {api}=setup();let token='old',posts=0;const adapter={token:()=>token,request:async(path,opt)=>{if(opt)posts++;return page([]);}};
 const v=new api.View(adapter,adapter);await v.page('/marketplace/mine',{id:'2'},new URLSearchParams());token='new';
 await assert.rejects(v.command({op:'submit'}),e=>e.status===401);assert.equal(posts,0);
});
test('private marketplace routes are noindex and item routes support refresh handoff',()=>{
 const routes=require('../src/route-policy.js'),seo=require('../src/seo.js');
 assert.ok(routes.owns('/marketplace/item/12'));assert.ok(routes.owns('/admin/marketplace'));assert.ok(!routes.owns('/marketplace/unknown'));
 for(const path of ['/marketplace/mine','/marketplace/publish','/admin/marketplace'])assert.match(seo.policy(path).robots,/noindex/);
});
