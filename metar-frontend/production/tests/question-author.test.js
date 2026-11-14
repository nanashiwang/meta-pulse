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
 assert.doesNotMatch(html,/xiao_li|小李|responder.jpg/);assert.doesNotMatch(html,/href="\/users\//);
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

test('two-line rows omit attribution text and show views before answers',()=>{
 const html=render({id:'123',user_info:author,answer_count:3,view_count:42,created_at:100,last_reply:{user_info:responder,created_at:200}});
 assert.doesNotMatch(html,/最后回复|发布于|discussion-meta|discussion-last-reply/);
 assert.ok(html.indexOf('42') < html.indexOf('回答'));
 assert.match(html,/responder.jpg/);
});
test('participants preserve publisher first, deduplicate and cap at five',()=>{
 const users=Array.from({length:8},(_,i)=>({username:'member'+i,display_name:'Member '+i,avatar:'/m'+i+'.jpg'}));
 const html=render({id:'1',user_info:author,participants:[responder,author,responder,...users]});
 assert.equal((html.match(/href="\/users\//g)||[]).length,5);
 assert.ok(html.indexOf('/users/publisher') < html.indexOf('/users/xiao_li'));
 assert.doesNotMatch(html,/member3|member4/);
});
test('zero replies show only the known publisher, never the activity editor',()=>{
 const html=render({id:'1',user_info:author,answer_count:0,operator:responder});
 assert.doesNotMatch(html,/xiao_li|最后回复|暂无回答/);
 assert.match(html,/publisher/);
});
test('excerpt uses at most 28 Unicode characters without modifying the full post',()=>{
 const post={id:'1',description:'😀'.repeat(40)};
 const html=render(post);assert.ok(html.includes('😀'.repeat(28)+'…'));assert.ok(!html.includes('😀'.repeat(29)));assert.equal(post.description.length,80);
});
