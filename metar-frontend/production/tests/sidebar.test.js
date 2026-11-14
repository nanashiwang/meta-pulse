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
 const context={isPersonalPath:p=>p==='/me'||p.startsWith('/me/'),route:()=>({path:'/me',query:new URLSearchParams()}),currentUserState:'ready',currentUser:{role_id:1},t:x=>x,getLanguage:()=> 'zh_CN',I:()=>'',esc:x=>String(x??''),link:(url,text)=>`<a href="${url}">${text}</a>`,external:(url,text)=>`<a href="${url}">${text}</a>`,outbound:(url,text)=>url?`<a href="${url}">${text}</a>`:'',routeMatchesNavigation:(path,search,url)=>path===url,config:{answerSettingsPath:'/users/settings/profile',consoleUrl:'https://api.example/console'},isCommunityAdministrator:u=>u?.role_id===2};
 context.accountManagement=appFunction('accountManagement',context);
 const sidebar=appFunction('personalSidebar',context);
 assert.match(sidebar(),/\/badges/);assert.match(sidebar(),/\/me\/settings/);assert.match(sidebar(),/href="\/pulse"[^>]*><span>签到抽奖<\/span>/);assert.doesNotMatch(sidebar(),/console\/pulse|marketplace\/mine/);assert.doesNotMatch(sidebar(),/\/admin\//);
 context.config.consoleUrl='';assert.match(sidebar(),/href="\/pulse"/);
 context.getLanguage=()=> 'en_US';assert.match(sidebar(),/Check-in & draw/);
 context.currentUser.role_id=2;assert.match(sidebar(),/\/admin\/pulse/);
 context.currentUserState='unavailable';assert.equal(sidebar(),'');
 context.currentUserState='ready';context.currentUserState='ready';context.route=()=>({path:'/me/bookmarks',query:new URLSearchParams()});assert.match(sidebar(),/personal-sidebar/);
});
test('back always returns to the personal space or the main community page',()=>{
 const visits=[];const context={personalNativeView:null,isPersonalPath:p=>p==='/settings/binding'||p==='/me'||p.startsWith('/me/'),route:()=>({path:'/settings/binding'}),history:{length:3,back:()=>visits.push('back')},window:{MetarRouter:{owns:()=>true}},location:{origin:'https://forum.test'},URL,router:{go:path=>{visits.push(path);return true}},navigate:()=>{}};
 const back=appFunction('goBack',context);
 back();context.route=()=>({path:'/users/settings'});back();context.route=()=>({path:'/me/bookmarks'});back();context.route=()=>({path:'/badges'});back();
 context.route=()=>({path:'/models'});back();context.route=()=>({path:'/topic/ai'});back();context.route=()=>({path:'/me'});back();
 assert.deepEqual(visits,['/latest','/me','/latest','/me','/latest','/latest','/latest']);
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

test('avatar rendered on me remains a working link when the header is reused after return',()=>{
 let path='/me';const context={route:()=>({path}),t:x=>x,avatar:()=>'<span>avatar</span>',currentUser:{id:'2'},esc:x=>x,window:{MetarRouter:{href:x=>x}}};
 const menu=appFunction('accountMenu',context);
 const retainedHeader=menu();
 path='/latest';
 assert.match(retainedHeader,/<a[^>]*href="\/me"[^>]*data-router/);
 assert.equal(menu(),retainedHeader);
});
test('shell administration pages return to personal space',()=>{
 const visits=[];let path='/admin/pulse';const back=appFunction('goBack',{personalNativeView:null,isPersonalPath:()=>false,route:()=>({path}),window:{MetarRouter:{owns:()=>true}},location:{origin:'https://forum.test'},URL,router:{go:p=>visits.push(p)},navigate:()=>{}});
 for(path of ['/admin/pulse','/admin/growth','/admin/models'])back();
 assert.deepEqual(visits,['/me','/me','/me']);
});
