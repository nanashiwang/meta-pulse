import { test } from 'node:test';
import assert from 'node:assert/strict';
await import('./growth-presentation.js');
const { install } = globalThis.MetarGrowthPresentation;

let nextUser = 0;
function anchor(kind, withImage = false, oldBadge = false) {
  const a = {href:'https://example.test/users/user'+(++nextUser),isConnected:true,childNodes:[{textContent:kind === 'avatar' ? 'A' : 'Alice'}],badge:null,image:withImage ? {dataset:{}} : null,
    matches:selector=>selector === '.discussion-avatar' && kind === 'avatar',
    closest:selector=>selector === '.discussion-last-reply' && kind === 'reply' ? {} : null,
    querySelector:selector=>selector === 'img' ? a.image : a.badge,
    append:node=>{a.badge=node;},
  };
  if(oldBadge) a.badge={textContent:'Lv.0',remove:()=>{a.badge=null;}};
  return a;
}
async function paint(links) {
  const host={location:{origin:'https://example.test'},document:{documentElement:{lang:'zh'},querySelectorAll:()=>links,createElement:()=>({})},
    MutationObserver:class {observe(){}},requestAnimationFrame:fn=>fn(),
    fetch:async()=>({ok:true,json:async()=>({data:{level:{number:2,name:'活跃成员'},appearance:'default'}})})};
  install(host);
  await new Promise(resolve=>setImmediate(resolve));
}
test('avatar links never receive a level badge, including initials and stale badges',async()=>{
  const initials=anchor('avatar'),photo=anchor('avatar',true,true),author=anchor('author');
  await paint([initials,photo,author]);
  assert.equal(initials.badge,null);assert.equal(photo.badge,null);
  assert.equal(photo.image.dataset.metarAppearance,'default');
  assert.equal(author.badge.textContent,'Lv.2');
});
test('last reply links stay compact while normal profile names retain levels',async()=>{
  const reply=anchor('reply',false,true),author=anchor('author');
  await paint([reply,author]);assert.equal(reply.badge,null);assert.equal(author.badge.textContent,'Lv.2');
});

test('avatar and name sharing a pending profile both finish without a later DOM mutation',async()=>{
 const avatar=anchor('avatar'),author=anchor('author');author.href=avatar.href;
 await paint([avatar,author]);assert.equal(avatar.badge,null);assert.equal(author.badge.textContent,'Lv.2');
});
