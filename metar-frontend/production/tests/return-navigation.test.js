const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync(require('node:path').resolve(__dirname,'../../../services/forum-plugin/user-center-pulse/return-navigation.js'),'utf8');
const box={module:{exports:{}},URL,URLSearchParams};vm.runInNewContext(source,box);const {create,install}=box.module.exports;
function setup(path='/latest'){
 const events={},clicks={},storage=new Map(),entries=[{url:path,state:null}];let index=0;
 const host={location:{origin:'https://forum.test'},document:{referrer:'',addEventListener:(k,fn)=>clicks[k]=fn},addEventListener:(k,fn)=>events[k]=fn,sessionStorage:{getItem:k=>storage.get(k),setItem:(k,v)=>storage.set(k,v),removeItem:k=>storage.delete(k)}};
 function locate(url){const u=new URL(url,host.location.origin);Object.assign(host.location,{href:u.href,pathname:u.pathname,search:u.search});}
 locate(path);host.location.replace=url=>{host.replaced=url;};
 host.history={get state(){return entries[index].state;},replaceState(state,_title,url){entries[index]={state,url};locate(url);},pushState(state,_title,url){entries.splice(index+1);entries.push({state,url});index++;locate(url);},go(n){index+=n;assert.ok(index>=0&&index<entries.length);locate(entries[index].url);events.popstate?.({});}};
 return {host,events,clicks,storage,entries,get index(){return index},nav:install(host)};
}
test('repeated post visits unwind actual occurrences without cycles',()=>{
 const h=setup('/users/notifications/inbox');for(const p of ['/questions/1','/users/alice','/questions/1'])h.host.history.pushState(null,'',p);
 for(const p of ['/users/alice','/questions/1','/users/notifications/inbox']){h.nav.back('/latest');assert.equal(h.host.location.pathname,p);}
 h.nav.back('/latest');assert.equal(h.host.replaced,'/latest');assert.equal(h.entries.length,4);
});
test('three-page cycle and browser forward restore the correct occurrence',()=>{
 const h=setup('/latest');for(const p of ['/questions/1','/users/a','/questions/2','/questions/1'])h.host.history.pushState(null,'',p);
 h.host.history.go(-2);assert.equal(h.nav.destination('',null),'/questions/1');h.host.history.go(1);h.nav.back('/latest');assert.equal(h.host.location.pathname,'/users/a');h.nav.back('/latest');assert.equal(h.host.location.pathname,'/questions/1');
});
test('admin siblings collapse while query-only list changes preserve history and scroll',()=>{
 const h=setup('/latest?order=newest&page=2');h.host.history.replaceState({metarScroll:{x:0,y:700}},'',h.host.location.href);
 for(const p of ['/admin/users','/admin/settings','/admin/dashboard'])h.host.history.pushState(null,'',p);
 assert.equal(h.nav.distance(),3);h.nav.back('/me');assert.equal(h.host.location.search,'?order=newest&page=2');assert.equal(h.host.history.state.metarScroll.y,700);
 h.host.history.pushState(null,'','/latest?order=hot');h.host.history.go(-1);assert.equal(h.host.location.search,'?order=newest&page=2');
});
test('reload restores current snapshot; direct links and invalid snapshots use fallback',()=>{
 const h=setup('/latest');h.host.history.pushState(null,'','/questions/1');const nav=create(h.host);nav.restore(h.host.history.state.metarReturnV2,'/questions/1');assert.equal(nav.destination('',null),'/latest');
 nav.restore({url:'/questions/1',step:2,trail:[{url:'https://evil.test',step:1}]},'/questions/1');assert.equal(nav.distance(),0);
 nav.restore(null,'/users/a');assert.equal(nav.destination('','/latest'),'/latest');
});
test('cancelled links only record intent and ignored links cannot change history',()=>{
 const h=setup('/questions/1');const a={getAttribute:()=>'/users/alice',hasAttribute:()=>false,target:''};h.clicks.click({button:0,target:{closest:()=>a}});assert.equal(h.nav.distance(),0);assert.equal(h.storage.size,0);
 h.clicks.click({button:0,ctrlKey:true,target:{closest:()=>a}});h.events.pagehide();assert.equal(h.storage.size,0);
 h.nav.remember('/questions/1','https://evil.test');h.events.pagehide();assert.equal(h.storage.size,0);
});
test('native committed navigation uses a verified pending source but never unrelated new tabs',()=>{
 const h=setup('/latest');h.nav.remember('/latest','/questions/1');h.events.pagehide();const pending=h.storage.get('_metar_return_pending_v2');assert.ok(pending);
 delete h.host.__metarReturnNavigation;h.host.history.pushState(null,'','/questions/1');h.host.history.replaceState(null,'',h.host.location.href);
 h.host.document.referrer='https://forum.test/latest';h.storage.set('_metar_return_pending_v2',pending);
 const nav=install(h.host);assert.equal(nav.destination('',null),'/latest');assert.equal(nav.distance(),1);
});

test('settings tabs and shell binding share their actual entry branch',()=>{
 for(const entry of ['/me','/me/bookmarks','/questions/7']){
  const h=setup('/latest');h.host.history.pushState(null,'',entry);
  for(const path of ['/users/settings/profile','/users/settings/notify','/users/settings/account','/settings/binding','/users/settings/profile'])h.host.history.pushState(null,'',path);
  h.nav.back('/latest');assert.equal(h.host.location.pathname,entry.startsWith('/me')?'/latest':entry);
 }
});
test('old settings snapshots skip sibling tabs after reload and browser traversal',()=>{
 const h=setup('/latest');h.host.history.pushState(null,'','/users/settings/profile');h.host.history.pushState(null,'','/users/settings/notify');
 const saved={url:'/users/settings/notify',step:2,trail:[{url:'/latest',step:0},{url:'/users/settings/profile',step:1}]};
 h.nav.restore(saved,saved.url);assert.equal(h.nav.distance(),2);h.nav.back('/latest');assert.equal(h.host.location.pathname,'/latest');
 h.host.history.go(2);h.nav.back('/latest');assert.equal(h.host.location.pathname,'/latest');
});
test('direct settings entry has no imaginary parent and unrelated personal branches remain separate',()=>{
 const h=setup('/users/settings/profile');h.host.history.pushState(null,'','/users/settings/notify');h.nav.back('/me');assert.equal(h.host.replaced,'/me');
 const other=setup('/me');other.host.history.pushState(null,'','/me/bookmarks');other.host.history.pushState(null,'','/questions/8');other.nav.back('/latest');assert.equal(other.host.location.pathname,'/me/bookmarks');
});

test('personal siblings skip to entry while badge details return to their list',()=>{
 const h=setup('/latest?order=newest');for(const path of ['/me','/me/growth','/me/bookmarks','/me/badges'])h.host.history.pushState(null,'',path);
 h.host.history.pushState(null,'','/me/badges?badge=abc');h.nav.back('/latest');assert.equal(h.host.location.pathname,'/me/badges');h.nav.back('/latest');assert.equal(h.host.location.pathname,'/latest');assert.equal(h.host.location.search,'?order=newest');
});

test('native inbox legacy and new sibling filters skip back to the external entry',()=>{
 const h=setup('/questions/7');
 for(const path of ['/me/notifications?view=inbox','/users/notifications/inbox/posts','/users/notifications/inbox/invites','/users/notifications/inbox/votes','/users/notifications/inbox?filter=comments','/users/notifications/achievement'])h.host.history.pushState(null,'',path);
 h.nav.back('/me');assert.equal(h.host.location.pathname,'/questions/7');
});
test('old native notification history snapshots cannot bounce between sibling tabs',()=>{
 const h=setup('/latest');
 h.nav.restore({url:'/users/notifications/inbox/invites',step:4,trail:[{url:'/latest',step:0},{url:'/me/notifications?view=inbox',step:1},{url:'/users/notifications/inbox/posts',step:2},{url:'/users/notifications/inbox/votes',step:3}]},'/users/notifications/inbox/invites');
 assert.equal(h.nav.destination('',null),'/latest');assert.equal(h.nav.distance(),4);
 const direct=setup('/users/notifications/inbox/posts');direct.host.history.pushState(null,'','/users/notifications/inbox/invites');direct.nav.back('/me');assert.equal(direct.host.replaced,'/me');
});

test('all Answer profile tabs skip to the entry, while native browser Back visits tabs',()=>{
 const h=setup('/questions/7/entry');
 const tabs=['','/answers','/questions','/bookmarks','/reputation','/comments','/votes','/badges'];
 for(const tab of tabs)h.host.history.pushState(null,'','/users/alice'+tab+(tab==='/comments'?'?page=2':''));
 h.host.history.go(-1);assert.equal(h.host.location.pathname,'/users/alice/votes');
 h.host.history.go(1);assert.equal(h.host.location.pathname,'/users/alice/badges');
 h.nav.back('/latest');assert.equal(h.host.location.pathname,'/questions/7/entry');
});

test('other profiles and actual badge details retain their own entries',()=>{
 const h=setup('/users?rank=posts');
 for(const path of ['/users/alice/comments','/users/bob/answers','/users/bob/badges/','/badges/123?username=bob'])h.host.history.pushState(null,'',path);
 for(const path of ['/users/bob/badges/','/users/alice/comments','/users']){
  h.nav.back('/latest');assert.equal(h.host.location.pathname,path);
 }
 assert.equal(h.host.location.search,'?rank=posts');
});

test('pre-update profile snapshots and direct links do not bounce between tabs',()=>{
 const h=setup('/latest?order=hot');
 for(const path of ['/users/alice','/users/alice/comments','/users/alice/badges'])h.host.history.pushState(null,'',path);
 const saved={url:'/users/alice/badges',step:3,trail:[{url:'/latest?order=hot',step:0},{url:'/users/alice',step:1},{url:'/users/alice/comments',step:2}]};
 h.nav.restore(saved,saved.url);h.nav.back('/latest');assert.equal(h.host.location.search,'?order=hot');
 const direct=setup('/users/alice/comments');direct.host.history.pushState(null,'','/users/alice/badges');direct.nav.back('/latest');assert.equal(direct.host.replaced,'/latest');
});

test('other tab and list families skip sibling selections but keep real origins',()=>{
 for(const tabs of [
  ['/latest?order=active','/latest?order=newest','/latest?order=unanswered'],
  ['/topics','/topics?view=tags'],
  ['/marketplace?kind=project','/marketplace?kind=idea'],
  ['/models','/models?open=1','/models/leaderboard?open=1'],
  ['/users?rank=reputation','/users?rank=posts'],
  ['/tags?order=popular','/tags?order=name'],
  ['/tags/go','/tags/go/questions?page=2'],
  ['/me','/me/replies','/awards?view=honors','/me/badges'],
 ]){
  const h=setup('/questions/7?from=entry');
  for(const path of tabs)h.host.history.pushState(null,'',path);
  h.nav.back('/latest');assert.equal(h.host.location.pathname+h.host.location.search,'/questions/7?from=entry',tabs.join(' → '));
 }
});

test('topic information, marketplace editors, model details and distinct searches are not sibling tabs',()=>{
 for(const [origin,detail] of [
  ['/tags/go/questions?page=2','/tags/go/info'],
  ['/tags/go','/tags/javascript'],
  ['/marketplace?kind=idea','/marketplace/item/42'],
  ['/marketplace/mine','/marketplace/publish?id=42'],
  ['/models?open=1','/model/demo'],
  ['/awards','/awards?view=honors'],
  ['/search?q=one','/search?q=two'],
 ]){
  const h=setup(origin);h.host.history.pushState(null,'',detail);h.nav.back('/latest');
  assert.equal(h.host.location.pathname+h.host.location.search,origin);
 }
});
