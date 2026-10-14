const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const {webcrypto} = require('node:crypto');
function setup(){
 const records=new Map();
 const sessionStorage={getItem:k=>records.get(k)||null,setItem:(k,v)=>records.set(k,v),removeItem:k=>records.delete(k)};
 const window={sessionStorage,MetarI18n:{getLanguage:()=> 'zh_CN',locale:()=> 'zh-CN'},MetarAdapters:{isCommunityAdministrator:()=> false}};
 vm.runInNewContext(fs.readFileSync(require.resolve('../src/member-pages.js'),'utf8'),{window,crypto:webcrypto,URLSearchParams,Date});
 return {View:window.MetarMemberPages.View,records};
}
test('unconfirmed event commands keep the original key across a page reload',async()=>{
 const {View,records}=setup(),posts=[];
 const api={request:async(path,opt)=>{if(opt){posts.push(JSON.parse(opt.body));throw Error('response lost')}return {items:[],more:false}}};
 const a=new View(api,api);await a.page('/events',{id:'2'},new URLSearchParams());
 await assert.rejects(a.command({op:'join',event:{id:'7'}},false));
 const saved=JSON.parse([...records.values()][0]);
 const b=new View(api,api);const html=await b.page('/events',{id:'2'},new URLSearchParams());
 assert.equal(b.pending.key,saved.key);assert.ok(html.includes('重试原操作'));
 const other=new View(api,api);await other.page('/events',{id:'3'},new URLSearchParams());assert.equal(other.pending,null);
 await assert.rejects(b.command({op:'join',event:{id:'8'}},false),/pending/);assert.equal(posts.length,1);
});
test('a confirmed event mutation is not repeated when only the list refresh fails',async()=>{
 const {View,records}=setup();let posts=0;
 const api={request:async()=>{posts++;return {id:'7',version:1}}};
 const view=new View(api,api);view.pendingKey='user-1';view.refresh=async()=>{throw Error('offline')};
 await assert.rejects(view.command({op:'create',event:{title:'one'}},true),/refresh_required/);
 assert.equal(view.pending,null);assert.equal(records.size,0);assert.equal(view.needsRefresh,true);
 await assert.rejects(view.command({op:'create',event:{title:'one'}},true),/refresh_required/);assert.equal(posts,1);
});
test('activity markup escapes authored content and does not expose the admin roster to members',async()=>{
 const {View}=setup();const api={request:async()=>({items:[{id:'7',title:'<img src=x>',body:'<script>alert(1)</script>',location:'<b>online</b>',starts_at:Date.now()/1000+3600,ends_at:Date.now()/1000+7200,deadline:Date.now()/1000+3000,capacity:1,registered_count:1,status:'published',joined:false}],more:false})};
 const view=new View(api,api),html=await view.page('/events',{id:'2'},new URLSearchParams());
 assert.ok(html.includes('&lt;script&gt;'));assert.ok(!html.includes('<script>'));assert.ok(html.includes('名额已满'));assert.ok(!html.includes('data-member="roster"'));
});
