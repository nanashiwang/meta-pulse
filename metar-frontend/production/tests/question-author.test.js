const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync(require.resolve('../src/app.js'), 'utf8');
const start = source.indexOf('  function questionRow(');
const end = source.indexOf('\n  }', start) + 4;
const render = vm.runInNewContext('(' + source.slice(start, end) + ')', {
 time:x=>"time-"+x,t:x=>x,esc:x=>String(x??''),I:()=>'',number:x=>Number(x||0),
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

test('legacy newest lists use the asked operator as the publisher with the correct avatar',()=>{
 const html=render({id:'123',operator:author,operation_type:'asked'});
 assert.match(html,/href="\/users\/publisher"/);assert.match(html,/author.jpg/);assert.match(html,/发布者/);
});
test('explicit publisher always takes precedence over the legacy operator',()=>{
 const html=render({id:'123',user_info:author,operator:responder,operation_type:'asked'});
 assert.match(html,/href="\/users\/publisher"/);assert.doesNotMatch(html,/xiao_li|responder.jpg/);
});

test('last reply uses independent answer author and time even after another user edits',()=>{
 const html=render({id:'123',user_info:author,operator:{username:'editor'},operation_type:'modified',answer_count:3,created_at:100,last_reply:{user_info:responder,created_at:200}});
 assert.ok(html.includes('最后回复：<a href="/users/xiao_li">小李</a> · time-200'));
 assert.match(html,/discussion-author|发布者/);assert.match(html,/发布于 time-100/);
 assert.doesNotMatch(html,/editor|responder.jpg/);
});
test('unknown last reply is not invented and zero replies show the invitation',()=>{
 assert.match(render({id:'1',answer_count:0}),/暂无回答 · 等你分享经验/);
 const html=render({id:'1',answer_count:2,operator:responder,operation_type:'answered'});
 assert.match(html,/已有回答/);assert.doesNotMatch(html,/最后回复：|暂无回答/);
});
test('excerpt uses at most 28 Unicode characters without modifying the full post',()=>{
 const post={id:'1',description:'😀'.repeat(40)};
 const html=render(post);assert.ok(html.includes('😀'.repeat(28)+'…'));assert.ok(!html.includes('😀'.repeat(29)));assert.equal(post.description.length,80);
});
