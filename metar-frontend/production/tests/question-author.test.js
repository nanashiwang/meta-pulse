const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync(require.resolve('../src/app.js'), 'utf8');
const start = source.indexOf('  function questionRow(');
const end = source.indexOf('\n  }', start) + 4;
const render = vm.runInNewContext('(' + source.slice(start, end) + ')', {
 t:x=>x,esc:x=>String(x??''),I:()=>'',number:x=>Number(x||0),
 avatar:u=>'<img src="'+(u.avatar||'')+'">',displayName:u=>u.display_name||'未知用户',
 publicAuthor:(u,label)=>u.username?'<a href="/users/'+u.username+'">'+label+'</a>':label,
 questionHref:id=>'/questions/'+id,external:(href,label)=>'<a href="'+href+'">'+label+'</a>',
 link:()=>'',tagName:()=>'',badge:()=>'',
});
const author={id:'1',username:'publisher',display_name:'发布者',avatar:'/author.jpg'};
const responder={id:'6',username:'xiao_li',display_name:'小李',avatar:'/responder.jpg'};
test('active posts show the publisher profile, name and avatar instead of the latest responder',()=>{
 const html=render({id:'123',user_info:author,operator:responder,operation_type:'answered'});
 assert.match(html,/href="\/users\/publisher"/);assert.match(html,/发布者/);assert.match(html,/author.jpg/);
 assert.doesNotMatch(html,/xiao_li|小李|responder.jpg/);
});
test('missing author data never impersonates the latest activity operator',()=>{
 const html=render({id:'123',operator:responder});
 assert.doesNotMatch(html,/xiao_li|小李|responder.jpg/);assert.match(html,/未知用户/);
});
test('personal and bookmark rows keep the supplied author regardless of activity type',()=>{
 for(const operation_type of ['asked','modified','answered']) {
  assert.match(render({id:'123',user_info:author,operator:responder,operation_type}),/href="\/users\/publisher"/);
 }
});
