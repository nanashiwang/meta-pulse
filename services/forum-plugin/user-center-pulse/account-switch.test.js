import test from 'node:test';
import assert from 'node:assert/strict';
import vm from 'node:vm';
import { readFileSync } from 'node:fs';
const source = readFileSync(new URL('./account-switch.js', import.meta.url), 'utf8');
const proof = 'a'.repeat(64);
const memory = () => { const m = new Map(); return { getItem: k => m.get(k) ?? null, setItem: (k,v) => m.set(k,String(v)), removeItem: k => m.delete(k) }; };
function setup(responses = []) {
  const calls=[],alerts=[],moves=[];
  const window = { __metarNativeAccountSwitch: true, localStorage:memory(), sessionStorage:memory(),
    document:{documentElement:{lang:'zh-CN'},getElementById:()=>({remove(){}, getAttribute(){}, setAttribute(){}, querySelector:()=>({textContent:''})})},
    location:{origin:'https://forum.test',assign:p=>moves.push(p)},confirm:()=>true,alert:v=>alerts.push(v),
    fetch:async(url,options)=>{calls.push({url,options});const next=responses.shift();if(next instanceof Error) throw next;return typeof next==='function'?next():next;},
  };
  window.localStorage.setItem('_a_ltk_','admin');
  vm.runInNewContext(source,{window,URL,URLSearchParams});
  return {window,calls,alerts,moves,api:window.MetarAccountSwitch};
}
const ok = data => ({ok:true,json:async()=>({data})});
const issue = () => ok({ticket:proof,callback:'/answer/api/v1/connector/redirect/metar_admin_switch'});
const login = () => ({ok:true,url:'https://forum.test/users/auth-landing?access_token=target'});
test('switch and return use native tokens, revoke target and remove tab return state',async()=>{
 const f=setup([issue(),login(),ok({ok:true}),ok({})]);await f.api.start('member');
 assert.equal(f.window.localStorage.getItem('_a_ltk_'),'target');assert.deepEqual(f.moves,['/latest']);
 assert.equal(f.calls[1].options.headers['X-Metar-Switch'],proof);
 assert.equal(f.calls[1].options.headers.Authorization,'Bearer admin');
 await f.api.restore();assert.equal(f.window.localStorage.getItem('_a_ltk_'),'admin');
 assert.equal(f.window.sessionStorage.getItem('_metar_admin_return_'),null);
 assert.equal(f.calls[3].url,'/answer/api/v1/user/logout');assert.equal(f.calls[3].options.headers.Authorization,'Bearer target');
 assert.deepEqual(f.moves,['/latest','/admin/users']);
});
test('denied and disabled issuance never replace login or leave return credentials',async()=>{
 for(const code of ['forbidden','switch_plugin_disabled']){const f=setup([{ok:false,json:async()=>({error:code})}]);await f.api.start('member');assert.equal(f.calls.length,1);assert.equal(f.window.localStorage.getItem('_a_ltk_'),'admin');assert.equal(f.window.sessionStorage.getItem('_metar_admin_return_'),null);assert.equal(f.alerts.length,1);}
});
test('failed callback keeps recoverable administrator and prevents nested switching',async()=>{
 const f=setup([issue(),new Error('network'),ok({ok:true})]);await f.api.start('member');
 assert.equal(f.window.localStorage.getItem('_a_ltk_'),'admin');assert.equal(JSON.parse(f.window.sessionStorage.getItem('_metar_admin_return_')).ticket,proof);
 await f.api.start('other');assert.equal(f.calls.length,2);await f.api.restore();assert.equal(f.window.sessionStorage.getItem('_metar_admin_return_'),null);
});
test('callback URLs from other origins or error pages cannot become sessions',async()=>{
 for(const url of ['https://evil.test/users/auth-landing?access_token=x','https://forum.test/50x']){const f=setup([issue(),{ok:true,url}]);await f.api.start('member');assert.equal(f.window.localStorage.getItem('_a_ltk_'),'admin');assert.equal(f.alerts.length,1);}
});
test('session changes while waiting do not overwrite another login',async()=>{
 const f=setup([issue(),()=>{f.window.localStorage.setItem('_a_ltk_','different');return login();}]);await f.api.start('member');assert.equal(f.window.localStorage.getItem('_a_ltk_'),'different');await f.api.restore();assert.equal(f.calls.length,2);
});
test('expired administrator or failed logout does not claim return succeeded',async()=>{
 const f=setup([issue(),login(),{ok:false,json:async()=>({error:'forbidden'})}]);await f.api.start('member');await f.api.restore();assert.equal(f.window.localStorage.getItem('_a_ltk_'),'target');assert.equal(f.moves.length,1);assert.ok(f.window.sessionStorage.getItem('_metar_admin_return_'));
});
test('storage failure prevents issuing a switch',async()=>{
 const f=setup();f.window.sessionStorage.setItem=()=>{throw Error('storage disabled');};await f.api.start('member');assert.equal(f.calls.length,0);assert.equal(f.window.localStorage.getItem('_a_ltk_'),'admin');
});
test('return banner follows native language hydration and later language changes',()=>{
 const f=setup(), nodes={span:{textContent:''},button:{textContent:''},a:{textContent:''}}, attrs={};
 f.window.sessionStorage.setItem('_metar_admin_return_',JSON.stringify({adminToken:'admin',ticket:proof,username:'member'}));
 f.window.document.documentElement.lang='en';
 f.window.document.getElementById=()=>({getAttribute:k=>attrs[k],setAttribute:(k,v)=>attrs[k]=v,querySelector:s=>nodes[s]});
 f.window.document.querySelectorAll=()=>[];
 let observe;
 f.window.MutationObserver=class{constructor(fn){observe=fn}observe(){}};
 f.window.requestAnimationFrame=fn=>fn();f.window.addEventListener=()=>{};
 f.api.install();assert.equal(nodes.button.textContent,'Return to administrator');
 f.window.document.documentElement.lang='zh-CN';observe();assert.equal(nodes.button.textContent,'返回管理员');assert.equal(attrs['aria-label'],'账号切换');
 f.window.document.documentElement.lang='en';observe();assert.equal(nodes.button.textContent,'Return to administrator');
});
test('admin user switching is an item in each row three-dot menu, not an extra row button',()=>{
 const item={tagName:'BUTTON',attrs:{},setAttribute(k,v){this.attrs[k]=v},hasAttribute(k){return k in this.attrs},remove(){this.removed=true}};
 const menuItems=[];
 const menu={querySelector:s=>menuItems.find(x=>!x.removed&&`[${Object.keys(x.attrs).find(k=>k.startsWith('data-metar'))}]`===s)||null,append(x){menuItems.push(x)}};
 const link={href:'https://forum.test/users/member'};
 const row={querySelector(s){if(s==='td:first-child a[href^="/users/"]')return link;if(s==='[data-metar-switch]')return item;if(s==='td:last-child .dropdown-menu')return menu;return null}};
 const f=setup();let confirmed='';f.window.confirm=message=>{confirmed=message;return false};
 f.window.location.pathname='/admin/users';
 f.window.document.querySelectorAll=s=>s==='tbody tr'?[row]:[];
 f.window.document.createElement=tag=>({tagName:tag.toUpperCase(),attrs:{},setAttribute(k,v){this.attrs[k]=v},hasAttribute(k){return k in this.attrs},className:'',textContent:'',tabIndex:0});
 let observe;
 f.window.MutationObserver=class{constructor(fn){observe=fn}observe(){}};
 f.window.requestAnimationFrame=fn=>fn();f.window.addEventListener=()=>{};
 f.api.install(()=>({role_id:2,username:'admin'}));
 assert.equal(item.removed,true);
 assert.equal(menuItems.length,2);
 assert.equal(menuItems[0].textContent,'赠送抽奖券');
 assert.equal(menuItems[1].textContent,'切换登录');
 assert.equal(menuItems[1].className,'dropdown-item');
 menuItems[1].onclick();
 return new Promise(resolve=>setImmediate(()=>{
  assert.match(confirmed,/切换登录到 @member/);
  assert.equal(f.calls.length,0);
  observe();
  assert.equal(menuItems.length,2);
  resolve();
 }));
});

test('ticket gift sends username, count and reason with one retry key and reports the result',async()=>{
 const f=setup([new Error('network'),ok({count:10,quota_expires_at:'2026-12-09T00:00:00Z'})]);
 const prompts=['10','验证新概率'];f.window.prompt=()=>prompts.shift();
 f.window.crypto={randomUUID:()=>'grant-key-1'};
 await f.api.grantTickets('uno');
 assert.equal(f.calls.length,2);
 for(const c of f.calls){
  assert.equal(c.url,'/answer/admin/api/metar/accounts/tickets');
  assert.equal(c.options.headers['Idempotency-Key'],'grant-key-1');
  assert.deepEqual(JSON.parse(c.options.body),{username:'uno',count:10,reason:'验证新概率'});
 }
 assert.match(f.alerts.at(-1),/已赠送 10 张抽奖券给 @uno/);
});
test('ticket gift rejects bad counts locally and explains unbound accounts',async()=>{
 const f=setup([{ok:false,status:409,json:async()=>({reason:'binding_required'})}]);
 f.window.crypto={randomUUID:()=>'k'};
 let prompts=['101'];f.window.prompt=()=>prompts.shift();
 await f.api.grantTickets('uno');
 assert.equal(f.calls.length,0);assert.match(f.alerts.at(-1),/1–100/);
 prompts=['3','测试'];
 await f.api.grantTickets('uno');
 assert.equal(f.calls.length,1);assert.match(f.alerts.at(-1),/尚未绑定 API 账号/);
});
test('admin user list gets CSS row numbers offset by page and a filter-aware total',async()=>{
 const f=setup([ok({count:37,list:[]})]);
 const el=tag=>({tagName:tag.toUpperCase(),attrs:{},id:'',textContent:'',className:'',style:{},setAttribute(k,v){this.attrs[k]=v},hasAttribute(k){return k in this.attrs},remove(){this.removed=true}});
 const tbody=el('tbody'),head=[],byId={};
 const wrap={...el('div'),before(n){byId[n.id]=n;n.nextElementSibling=wrap;n.remove=()=>{delete byId[n.id]}}};
 const table={...el('table'),querySelector:s=>s==='tbody'?tbody:null,closest:()=>wrap};
 Object.assign(f.window.document,{querySelector:s=>s==='table'?table:null,querySelectorAll:()=>[],getElementById:id=>byId[id]||null,createElement:el,head:{append(n){byId[n.id]=n;head.push(n)}}});
 Object.assign(f.window.location,{pathname:'/admin/users',search:'?filter=normal&query=bo&page=3'});
 f.window.MutationObserver=class{observe(){}};f.window.requestAnimationFrame=fn=>fn();f.window.addEventListener=()=>{};
 f.api.install(()=>({role_id:2,username:'admin'}));
 assert.equal(table.attrs['data-metar-seq'],'');assert.equal(tbody.style.counterReset,'metar-row 20');
 assert.match(head[0].textContent,/counter\(metar-row\)/);
 assert.equal(f.calls[0].url,'/answer/admin/api/users/page?page=1&page_size=1&query=bo&status=normal');
 assert.equal(f.calls[0].options.headers.Authorization,'Bearer admin');
 await new Promise(r=>setImmediate(r));
 assert.equal(byId['metar-user-total'].textContent,'当前筛选共 37 名用户');
});
