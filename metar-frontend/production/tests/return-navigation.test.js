const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync(require('node:path').resolve(__dirname,'../../../services/forum-plugin/user-center-pulse/return-navigation.js'),'utf8');
const box={module:{exports:{}},URL};vm.runInNewContext(source,box);const {create,install}=box.module.exports;
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
 h.host.history.pushState(null,'','/latest?order=hot');h.nav.back('/latest');assert.equal(h.host.location.search,'?order=newest&page=2');
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
