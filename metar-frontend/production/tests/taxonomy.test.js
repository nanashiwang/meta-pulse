'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
global.window = global;
require('../src/i18n.js');
require('../src/adapters.js');
require('../src/taxonomy.js');
const taxonomy = window.MetarTaxonomy;
const clone = value => JSON.parse(JSON.stringify(value));
function fixture() {
  let settings = {min_content:30,min_tags:1,required_tag:true,restrict_answer:true,max_image_size:7,authorized_image_extensions:['png'],recommend_tags:[{slug_name:'existing',display_name:'Existing'}],reserved_tags:[{slug_name:'staff',display_name:'Staff'}]};
  const tags = new Map([['ai',{slug_name:'ai',display_name:'Our AI tag',description:'Keep me',status:'available'}]]);
  const writes = [];
  const user = {id:'42',role_id:2,status:'normal',mail_status:1};
  const answer = {
    getCurrentUser:async()=>clone(user),clearContentCache(){},
    async request(path,options) {
      if (path === '/tag' && options?.method === 'POST') {
        const body=JSON.parse(options.body); writes.push(body); tags.set(body.slug_name,{...body,status:'available'});
        if (answer.lose===body.slug_name) {answer.lose=null;throw new Error('lost response');}
        return {slug_name:body.slug_name};
      }
      const slug=new URL(path,'https://test.local').searchParams.get('name');
      if (tags.has(slug)) return clone(tags.get(slug));
      throw Object.assign(new Error('missing'),{status:404});
    },
  };
  const admin={async request(path,options) {
    assert.equal(path,'/siteinfo/write');
    if (options) {assert.equal(options.method,'PUT');settings=JSON.parse(options.body);writes.push({settings:clone(settings)});}
    return clone(settings);
  }};
  return {answer,admin,user,tags,writes,get settings(){return settings;}};
}
test('setup merges real categories and preserves all unrelated settings and existing descriptions',async()=>{
  const f=fixture(),before=clone(f.settings);
  await taxonomy.initialize(f.answer,f.admin);
  assert.equal(f.settings.recommend_tags.length,14);
  assert.deepEqual({...f.settings,recommend_tags:[]},{...before,recommend_tags:[]});
  assert.equal(f.tags.get('ai').description,'Keep me');
  assert.equal(f.writes.filter(x=>x.slug_name).length,19);
  const count=f.writes.length;
  await taxonomy.initialize(f.answer,f.admin);
  assert.equal(f.writes.length,count,'readback prevents duplicate create or settings writes');
});
test('lost create response stops and a new attempt first queries the saved tag',async()=>{
  const f=fixture();f.answer.lose='wiki';
  await assert.rejects(taxonomy.initialize(f.answer,f.admin),/lost response/);
  assert.equal(f.settings.recommend_tags.length,1);
  await taxonomy.initialize(f.answer,f.admin);
  assert.equal(f.writes.filter(x=>x.slug_name==='wiki').length,1);
  assert.equal(f.settings.recommend_tags.length,14);
});
test('ordinary, inactive and suspended accounts cannot begin initialization',async()=>{
  for(const patch of [{role_id:1},{mail_status:2},{status:'suspended'}]){
    const f=fixture();Object.assign(f.user,patch);
    await assert.rejects(taxonomy.initialize(f.answer,f.admin));assert.equal(f.writes.length,0);
  }
});
test('reserved category collisions and synonyms are never silently repurposed',async()=>{
  const f=fixture();f.settings.reserved_tags.push({slug_name:'develop'});
  await assert.rejects(taxonomy.initialize(f.answer,f.admin));assert.equal(f.writes.length,0);
  const g=fixture();g.tags.set('develop',{slug_name:'other',main_tag_slug_name:'other',status:'available'});
  await assert.rejects(taxonomy.initialize(g.answer,g.admin));assert.equal(g.writes.length,0);
});
test('settings drift or a failed readback cannot be reported as success',async()=>{
  const f=fixture();const original=f.admin.request;let reads=0;
  f.admin.request=async(...args)=>{if(++reads===2)f.settings.min_content=40;return original(...args);};
  await assert.rejects(taxonomy.initialize(f.answer,f.admin));assert.ok(!f.writes.some(x=>x.settings));
  const g=fixture();g.admin.request=async(path,options)=>options?null:clone(g.settings);
  await assert.rejects(taxonomy.initialize(g.answer,g.admin));
});
test('request links decode under the native editor contract and never auto-submit',()=>{
  for(const english of [false,true]) for(const kind of ['category','tag']){
    const url=new URL(taxonomy.requestURL('/questions/ask',kind,english,'feedback'),'https://test.local');
    assert.equal(url.pathname,'/questions/ask');assert.equal(url.searchParams.get('tags'),'feedback');
    const source=decodeURIComponent(url.searchParams.get('prefill'));
    assert.match(source,/^---\ntitle: "/);assert.match(source,english?/Example topics/:/示例话题/);
    assert.doesNotMatch(source,/user_id|token|已提交/);
  }
  assert.equal(new URL(taxonomy.askURL('/questions/ask','a b'),'https://test.local').searchParams.get('tags'),'a b');
});
test('configured categories show current descriptions instead of seed copy, with safe fallback',async()=>{
  let fail=false;
  const answer={async request(path){
    if(path==='/siteinfo')return {site_write:{recommend_tags:[{slug_name:'develop',display_name:'Custom name'}]}};
    if(fail)throw new Error('offline');
    return {slug_name:'develop',description:'Custom <description>',status:'available'};
  }};
  const current=await taxonomy.directory(answer,new URLSearchParams(),'/questions/ask',false);
  assert.match(current,/Custom &lt;description&gt;/);
  assert.doesNotMatch(current,/开发、部署/);
  fail=true;
  const fallback=await taxonomy.directory(answer,new URLSearchParams(),'/questions/ask',false);
  assert.match(fallback,/浏览这个类别中的讨论/);
  assert.match(fallback,/\/topic\/develop/);
});
test('directory reflects administrator categories, escapes content and preserves tag pagination',async()=>{
  const calls=[];
  const answer={request:async()=>({site_write:{recommend_tags:[{slug_name:'custom',display_name:'<script>x</script>'}]}}),listTags:async args=>{calls.push(args);return {count:100,list:[{slug_name:'ai',display_name:'AI',question_count:7}]};}};
  const categories=await taxonomy.directory(answer,new URLSearchParams(),'/questions/ask',false);
  assert.match(categories,/&lt;script&gt;/);assert.doesNotMatch(categories,/<script>/);assert.match(categories,/\/topic\/custom/);
  assert.doesNotMatch(categories,/\/topic\/develop/,'unconfigured seeds must not masquerade as live categories');
  const tags=await taxonomy.directory(answer,new URLSearchParams('view=tags&page=2'),'/questions/ask',false);
  assert.equal(calls[0].page,2);assert.match(tags,/view=tags&page=3/);assert.match(tags,/view=tags&page=1/);
  answer.request=async()=>({site_write:{recommend_tags:[]}});
  const preview=await taxonomy.directory(answer,new URLSearchParams(),'/questions/ask',false);
  assert.match(preview,/待管理员启用/);assert.doesNotMatch(preview,/href="\/topic\/develop/);
  answer.request=async()=>{throw new Error('offline');};
  await assert.rejects(taxonomy.directory(answer,new URLSearchParams(),'/questions/ask',false),/offline/);
});

test('navigation catalog deduplicates reads and isolates session, language and invalidation epochs',async()=>{
  let token='one',calls=0;
  const answer={contentEpoch:1,token:()=>token,request:async path=>{
    calls++;
    if(path==='/siteinfo')return {secret:'must not retain',site_write:{recommend_tags:[{slug_name:'custom',display_name:'Real category',extra:'private'}]}};
    if(path.startsWith('/tags?'))return [{slug_name:'ai',display_name:'Actual AI',extra:'private'}];
    return {list:[{slug_name:'custom'},{slug_name:'ai'},{slug_name:'custom-tag',display_name:'Real tag'}]};
  }};
  const [first,second]=await Promise.all([taxonomy.catalog(answer),taxonomy.catalog(answer)]);
  assert.equal(first,second);assert.equal(calls,3);
  assert.deepEqual(first.categories,[{slug_name:'custom',display_name:'Real category'}]);
  assert.deepEqual(first.tags.map(x=>x.slug_name),['ai','custom-tag']);
  assert.doesNotMatch(JSON.stringify(first),/private|secret|must not retain/);
  await taxonomy.catalog(answer);assert.equal(calls,3);
  answer.contentEpoch++;await taxonomy.catalog(answer);assert.equal(calls,6);
  token='two';await taxonomy.catalog(answer);assert.equal(calls,9);
  const original=window.MetarI18n;
  window.MetarI18n={...original,locale:()=> 'en-US'};
  try {await taxonomy.catalog(answer);assert.equal(calls,12);} finally {window.MetarI18n=original;}
});

test('failed catalogs are retried, old results cannot overwrite a newer scope, and no seed becomes a live link',async()=>{
  let fail=true,resolveOld;
  const answer={contentEpoch:0,request:async path=>{
    if(fail)throw new Error('offline');
    if(path==='/siteinfo')return {site_write:{recommend_tags:[]}};
    return path.startsWith('/tags?')?[]:{list:[]};
  }};
  const unavailable=await taxonomy.catalog(answer);
  assert.equal(unavailable.categoriesUnavailable,true);assert.equal(unavailable.tagsUnavailable,true);
  assert.doesNotMatch(taxonomy.navigation(unavailable),/href="\/topic\//);
  fail=false;assert.equal((await taxonomy.catalog(answer)).categoriesUnavailable,false);
  const delayed={contentEpoch:0,request:path=>path==='/siteinfo'?new Promise(resolve=>{resolveOld=resolve;}):Promise.resolve([])};
  const old=taxonomy.catalog(delayed);delayed.contentEpoch++;
  delayed.request=async path=>path==='/siteinfo'?{site_write:{recommend_tags:[{slug_name:'new'}]}}:[];
  const current=await taxonomy.catalog(delayed);
  resolveOld({site_write:{recommend_tags:[{slug_name:'old'}]}});await old;
  assert.equal(await taxonomy.catalog(delayed),current);
});

test('sidebar opens a separate dialog and drawer search preserves escaped names, current selection and complete directory access',()=>{
  const data={categories:[{slug_name:'custom',display_name:'<Category>'},{slug_name:'develop',display_name:'开发实践'}],tags:[{slug_name:'ai',display_name:'AI 交流'}]};
  const nav=taxonomy.navigation();
  assert.equal((nav.match(/aria-haspopup="dialog"/g)||[]).length,2);
  assert.doesNotMatch(nav,/href="\/topic\/|<details/);
  const items=taxonomy.drawerItems(data,'categories','category','custom');
  assert.match(items,/&lt;Category&gt;/);assert.match(items,/aria-current="page"/);
  assert.doesNotMatch(items,/\/topic\/develop/);
  assert.match(taxonomy.drawerItems(data,'tags','missing'),/没有匹配的项目/);
  assert.match(taxonomy.drawerFooter(data,'tags','/questions/ask',false),/href="\/topics\?view=tags"/);
  const selected=taxonomy.filters(data,'custom');
  assert.equal((selected.match(/ selected/g)||[]).length,1);assert.match(selected,/value="custom" selected/);
  assert.match(taxonomy.filters(data,'outside-page'),/value="outside-page" selected/);
});

function renameFixture() {
  const tags=new Map([...taxonomy.categories,...taxonomy.tags].map((tag,index)=>[tag.slug_name,{...tag,tag_id:String(index+1),status:'available',original_text:'## Keep **this** markdown',question_count:7}]));
  const writes=[];let clears=0;
  const user={id:'42',role_id:2,status:'normal',mail_status:1};
  const answer={getCurrentUser:async()=>clone(user),clearContentCache:()=>clears++,async request(path,options){
    if(options){
      assert.equal(path,'/tag');assert.equal(options.method,'PUT');
      const body=JSON.parse(options.body);writes.push(body);
      Object.assign(tags.get(body.slug_name),body);
      if(answer.lose){answer.lose=false;throw new Error('lost response');}
      return {wait_for_review:false};
    }
    return clone(tags.get(new URL(path,'https://test.local').searchParams.get('name')));
  }};
  return {answer,tags,writes,user,get clears(){return clears;}};
}
test('default renaming preserves tag identity, content associations, source and administrator custom names',async()=>{
  const f=renameFixture();f.tags.get('develop').display_name='开发调优';f.tags.get('gossip').display_name='搞七捻三';f.tags.get('ai').display_name='Our custom name';
  await taxonomy.renameDefaults(f.answer);
  assert.equal(f.writes.length,2);assert.equal(f.tags.get('develop').display_name,'开发实践');assert.equal(f.tags.get('gossip').display_name,'日常交流');assert.equal(f.tags.get('ai').display_name,'Our custom name');
  assert.equal(f.tags.get('develop').question_count,7);assert.equal(f.writes[0].tag_id,'1');assert.equal(f.writes[0].slug_name,'develop');assert.equal(f.writes[0].original_text,'## Keep **this** markdown');assert.match(f.writes[0].edit_summary,/METAR/);assert.equal(f.clears,1);
  await taxonomy.renameDefaults(f.answer);assert.equal(f.writes.length,2,'already renamed defaults never write again');
});
test('renaming rechecks permissions and source, detects incompatible/readback states, and recovers by reading after lost responses',async()=>{
  const f=renameFixture();f.tags.get('develop').display_name='开发调优';f.answer.lose=true;
  await assert.rejects(taxonomy.renameDefaults(f.answer),/lost response/);
  await taxonomy.renameDefaults(f.answer);assert.equal(f.writes.length,1);
  for(const patch of [{role_id:1},{mail_status:2},{status:'suspended'}]){
    const g=renameFixture();Object.assign(g.user,patch);await assert.rejects(taxonomy.renameDefaults(g.answer));assert.equal(g.writes.length,0);
  }
  for(const patch of [{original_text:undefined},{tag_id:''},{main_tag_slug_name:'other'},{status:'deleted'}]){
    const g=renameFixture();Object.assign(g.tags.get('develop'),{display_name:'开发调优'},patch);await assert.rejects(taxonomy.renameDefaults(g.answer));assert.equal(g.writes.length,0);
  }
  const g=renameFixture();g.tags.get('develop').display_name='开发调优';const request=g.answer.request;
  g.answer.request=async(path,options)=>{const result=await request(path,options);if(options)g.tags.get('develop').original_text='Changed elsewhere';return result;};
  await assert.rejects(taxonomy.renameDefaults(g.answer));assert.equal(g.writes.length,1);assert.equal(g.clears,1);
});
