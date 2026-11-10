const test=require('node:test');
const assert=require('node:assert/strict');
const {KEY,create}=require('../src/sidebar.js');
const fs=require('node:fs');
const vm=require('node:vm');
const appSource=fs.readFileSync(require.resolve('../src/app.js'),'utf8');
function appFunction(name,context){
 const start=appSource.indexOf(`  function ${name}(`);
 assert.ok(start>=0,`${name} exists`);
 const end=appSource.indexOf('\n  }',start)+4;
 return vm.runInNewContext(`(${appSource.slice(start,end)})`,context);
}
test('personal sidebar only exposes real administrator navigation on /me',()=>{
 const context={route:()=>({path:'/me'}),currentUserState:'ready',currentUser:{role_id:1},t:x=>x,getLanguage:()=> 'zh_CN',I:()=>'',link:(url,text)=>`<a href="${url}">${text}</a>`,external:(url,text)=>`<a href="${url}">${text}</a>`,outbound:(url,text)=>url?`<a href="${url}">${text}</a>`:'',config:{answerSettingsPath:'/users/settings/profile',consoleUrl:'https://api.example/console'},isCommunityAdministrator:u=>u?.role_id===2};
 context.accountManagement=appFunction('accountManagement',context);
 const sidebar=appFunction('personalSidebar',context);
 assert.match(sidebar(),/\/badges/);assert.match(sidebar(),/\/users\/settings\/profile/);assert.match(sidebar(),/https:\/\/api.example\/console\/pulse/);assert.doesNotMatch(sidebar(),/\/admin\//);
 context.currentUser.role_id=2;assert.match(sidebar(),/\/admin\/pulse/);
 context.currentUserState='unavailable';assert.equal(sidebar(),'');
 context.currentUserState='ready';context.route=()=>({path:'/me/bookmarks'});assert.equal(sidebar(),'');
});
test('back returns settings to personal space, otherwise uses history or community',()=>{
 const visits=[];const context={route:()=>({path:'/settings/binding'}),history:{length:3,back:()=>visits.push('back')},router:{go:path=>{assert.equal(path,'/me');visits.push(path);return true}},location:{assign:path=>visits.push(path)},navigate:()=>{}};
 const back=appFunction('goBack',context);
 back();context.route=()=>({path:'/users/settings'});back();context.route=()=>({path:'/users/settings/profile'});back();
 context.route=()=>({path:'/models'});back();context.history.length=1;back();
 assert.deepEqual(visits,['/me','/me','/me','back','/latest']);
});
function setup(value,blocked=false){let listener,latest;const saved=new Map([[KEY,value]]),desktop={matches:true,addEventListener(_event,fn){listener=fn}};
 const storage={getItem:k=>{if(blocked)throw Error('blocked');return saved.get(k)},setItem:(k,v)=>{if(blocked)throw Error('blocked');saved.set(k,v)}};
 const controller=create({storage,desktop,apply:s=>latest=s});return {controller,saved,get state(){return latest},resize(matches){desktop.matches=matches;listener()}};
}
test('desktop preference survives refresh and navigation without changing mobile behavior',()=>{
 const h=setup('1');assert.equal(h.state.expanded,false);h.controller.toggle();assert.equal(h.saved.get(KEY),'0');assert.equal(h.state.expanded,true);
 h.controller.toggle();h.controller.closeMobile();assert.equal(h.state.expanded,false);
 h.resize(false);h.controller.toggle();assert.equal(h.state.expanded,true);assert.equal(h.saved.get(KEY),'1');h.controller.closeMobile();assert.equal(h.state.expanded,false);
 h.resize(true);assert.equal(h.state.expanded,false);h.controller.fromStorage('0');assert.equal(h.state.expanded,true);
});
test('viewport transitions close the mobile drawer and disabled storage retains usable controls',()=>{
 const h=setup(null,true);h.controller.toggle();assert.equal(h.state.collapsed,true);h.resize(false);h.controller.toggle();h.resize(true);h.resize(false);assert.equal(h.state.expanded,false);
 h.controller.toggle();assert.equal(h.state.expanded,true);
});
