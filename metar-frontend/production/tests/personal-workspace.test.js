const test=require('node:test'),assert=require('node:assert/strict');
const {nativeTarget,workspaceURL,isSave}=require('../src/personal-workspace.js');
const origin='https://forum.test';
test('personal targets keep settings, badges and notifications in allowlisted native views',()=>{
 assert.equal(nativeTarget('/me/settings',new URLSearchParams('tab=notify')),'/users/settings/notify');
 assert.equal(nativeTarget('/me/settings',new URLSearchParams('tab=../../admin')),'/users/settings/profile');
 assert.equal(nativeTarget('/me/badges',new URLSearchParams('badge=Ab12&username=a%26b')),'/badges/Ab12?username=a%26b');
 assert.equal(nativeTarget('/me/badges',new URLSearchParams('badge=../admin')),null);
 assert.equal(nativeTarget('/me/notifications',new URLSearchParams('view=achievement')),'/users/notifications/achievement');
 assert.equal(nativeTarget('/admin/users'),null);
});
test('native links map back to workspace while external, auth and API destinations never embed',()=>{
 assert.equal(workspaceURL('/users/settings/profile',origin),'/me/settings?tab=profile');
 assert.equal(workspaceURL('/badges/Ab12?username=alice&page=2',origin),'/me/badges?badge=Ab12&username=alice&page=2');
 for(const value of ['https://other.test/badges','/users/login','/users/logout','/answer/api/v1/user/info','/admin/users','/users/alice','javascript:alert(1)','/badges/../../admin'])assert.equal(workspaceURL(value,origin),null,value);
});
test('only successful settings write endpoints qualify as saves, never reads or arbitrary activity',()=>{
 assert.equal(isSave('PUT','/answer/api/v1/user/info',origin),true);
 assert.equal(isSave('PUT','/answer/api/v1/user/notification/config',origin),true);
 for(const [method,url] of [['GET','/answer/api/v1/user/info'],['POST','/answer/api/v1/question'],['PUT','https://other.test/answer/api/v1/user/info'],['PUT','/answer/api/v1/user/plugin/config']])assert.equal(isSave(method,url,origin),false);
});

const {View}=require('../src/personal-workspace.js');
function embedded(){
 const events={},hostEvents={},saved=[],routes=[];const status={hidden:false};
 const child={location:new URL(origin+'/users/settings/profile'),addEventListener(){},removeEventListener(){},ResizeObserver:class{observe(){}disconnect(){}}};
 class XHR{open(){}send(){}addEventListener(type,fn){this[type]=fn;}finish(status,body){this.status=status;this.responseText=JSON.stringify(body);this.responseType='';this.load();}}
 child.XMLHttpRequest=XHR;child.history={pushState(){},replaceState(_state,_title,path){child.location=new URL(path,child.location);}};
 const doc={documentElement:{classList:{add(){}}},head:{append(){}},body:{scrollHeight:600},querySelector:()=>null,createElement:()=>({}),addEventListener:(type,fn)=>events[type]=fn,removeEventListener(){}};child.document=doc;
 const frame={contentWindow:child,isConnected:true,style:{},addEventListener(type,fn){this[type]=fn;}};
 const host={location:{origin},setTimeout:()=>1,clearTimeout(){},addEventListener:(t,f)=>hostEvents[t]=f,removeEventListener(){},confirm:()=>false};
 const root={querySelector:s=>s==='iframe'?frame:status};
 const view=new View(root,{host,target:'/users/settings/profile',t:x=>x,onRoute:path=>routes.push(path),onExit(){},onSaved:()=>saved.push(true)});view.loaded();
 return {view,child,host,events,saved,routes};
}
test('failed saves retain dirty state; successful saves clear only their own edit revision',()=>{
 const h=embedded();const input={target:{closest:()=>true,matches:()=>false}};
 h.events.input(input);assert.equal(h.view.confirmLeave(),false);
 const failed=new h.child.XMLHttpRequest();failed.open('PUT','/answer/api/v1/user/info');failed.send();failed.finish(400,{code:400});assert.equal(h.view.dirty(),true);
 const pending=new h.child.XMLHttpRequest();pending.open('PUT','/answer/api/v1/user/info');pending.send();h.events.input(input);pending.finish(200,{code:200});assert.equal(h.view.dirty(),true);
 const ok=new h.child.XMLHttpRequest();ok.open('PUT','/answer/api/v1/user/info');ok.send();ok.finish(200,{code:200});assert.equal(h.view.dirty(),false);assert.equal(h.saved.length,2);h.view.dispose();
});
test('cancelled native tab switch cannot change the iframe URL or parent route',()=>{
 const h=embedded();h.view.revision=1;h.child.history.pushState({},'','/users/settings/notify');assert.equal(h.child.location.pathname,'/users/settings/profile');assert.equal(h.routes.length,1);
 h.host.confirm=()=>true;h.child.history.pushState({},'','/users/settings/notify');assert.equal(h.child.location.pathname,'/users/settings/notify');assert.equal(h.routes.at(-1),'/me/settings?tab=notify');h.view.dispose();
});
