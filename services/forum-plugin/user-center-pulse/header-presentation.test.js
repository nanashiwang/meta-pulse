import test from 'node:test';
import assert from 'node:assert/strict';
import {headerAccess,headerIcon} from './header-presentation.js';
test('header links follow actual active verified Answer role, not an administrator-looking flag',()=>{
 assert.deepEqual(headerAccess(null),{chat:false,review:false});
 for(const user of [{username:'x',role_id:2,status:'suspended',mail_status:1},{username:'x',role_id:2,status:'normal',mail_status:2}])assert.deepEqual(headerAccess(user),{chat:false,review:false});
 assert.deepEqual(headerAccess({username:'x',role_id:1,status:'normal',mail_status:1,is_admin:true}),{chat:true,review:false});
 assert.deepEqual(headerAccess({username:'x',role_id:2,status:'normal',mail_status:1}),{chat:true,review:true});
});
test('header decorations only emit static known icon paths',()=>{
 assert.equal(headerIcon('<img src=x>'),headerIcon('target'));
 for(const name of ['bell','trophy','language','moon','sun','search'])assert.match(headerIcon(name),/aria-hidden="true"/);
});

import {decorateNativeHeader} from './header-presentation.js';
import {installAccountMenu} from './account-menu.js';
test('administrator header keeps its review shortcut stable across menu and header passes',()=>{
 const links=[];let removals=0,additions=0;const row={append(n){links.push(n);additions++},querySelector(selector){
  if(selector.includes(':not(.metar-header-review)'))return null;
  if(selector==='.navbar-nav a[href="/review"]')return links.find(n=>n.href==='/review')||null;
  if(selector==='a[href="/review"]'||selector==='.metar-header-review')return links.find(n=>n.href==='/review')||null;
  if(selector==='.metar-header-chat')return links.find(n=>n.href==='/chat')||null;
  return null;
 }};
 const doc={documentElement:{},addEventListener(){},querySelector(s){if(s==='#header > .w-100')return row;if(s==='#header #dropdown-basic')return trigger;if(s==='#header .metar-header-review')return row.querySelector('.metar-header-review');if(s==='#header a[href="/users/notifications/achievement"]')return achievement;return null},createElement(){const kids=[],attrs={};const node={dataset:{},className:'',querySelector:()=>kids[0]||null,append(n){kids.push(n)},setAttribute(k,v){attrs[k]=v},getAttribute:k=>attrs[k],remove(){removals++;links.splice(links.indexOf(node),1)}};node.classList={contains:c=>node.className.split(' ').includes(c),add:c=>{if(!node.classList.contains(c))node.className+=' '+c}};return node;}};
 const trigger={parentElement:{querySelector:()=>null},setAttribute(){}};
 const achievement={get nextElementSibling(){return links.find(n=>n.href==='/review')},after(n){if(!links.includes(n)){links.push(n);additions++}}};
 let scheduled=[];let observer;
 const host={document:doc,requestAnimationFrame:fn=>scheduled.push(fn),MutationObserver:class{constructor(fn){observer=fn}observe(){}}};
 installAccountMenu({getUser:()=>({username:'admin',role_id:2,mail_status:1,status:'normal'}),getLanguage:()=> 'zh_CN',onLanguageChanged(){},subscribeUser(){}},host);
 decorateNativeHeader(doc,{review:true});const original=row.querySelector('.metar-header-review');
 for(let i=0;i<8;i++){observer();for(const fn of scheduled.splice(0))fn();decorateNativeHeader(doc,{review:true});assert.equal(row.querySelector('.metar-header-review'),original);}
 assert.equal(removals,0);assert.equal(additions,1);
 decorateNativeHeader(doc,{review:false});assert.equal(row.querySelector('.metar-header-review'),null);
});
