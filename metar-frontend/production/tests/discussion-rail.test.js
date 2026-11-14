const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const source=fs.readFileSync(require.resolve('../src/app.js'),'utf8');
function load(name,context){const start=source.indexOf('  '+(name.startsWith('mount')?'async ':'')+'function '+name+'(');const end=source.indexOf('\n  }',start)+4;assert.ok(start>=0);return vm.runInNewContext('('+source.slice(start,end)+')',context);}
function setup(request){const nodes={unanswered:{innerHTML:''},featured:{innerHTML:''}};const root={isConnected:true,querySelector:s=>s==='[data-rail-disclosure]'?null:nodes[s.includes('unanswered')?'unanswered':'featured']};const ctx={railMediaCleanup:null,window:{},document:{querySelector:()=>root},navigationSequence:1,taxonomy:{scope:()=> 'guest'},answer:{listQuestions:request},t:x=>x,esc:x=>String(x).replaceAll('<','&lt;').replaceAll('>','&gt;'),questionHref:id=>'/questions/'+id,external:(url,text)=>'<a href="'+url+'">'+text+'</a>'};ctx.discussionRailItems=load('discussionRailItems',ctx);return {ctx,nodes,root,mount:load('mountDiscussionRail',ctx),render:ctx.discussionRailItems};}
test('sidebar uses unanswered and existing collected-reading category independently',async()=>{
 const calls=[];const h=setup(async q=>{calls.push(q);return {list:[{id:'123',title:'<script>title</script>',answer_count:0}]};});
 await h.mount();assert.equal(calls[0].order,'unanswered');assert.equal(calls[1].tag,'feeds');assert.equal(calls[1].order,'score');assert.ok(h.nodes.unanswered.innerHTML.includes('href="/questions/123"'));assert.ok(h.nodes.featured.innerHTML.includes('&lt;script&gt;'));
});
test('a failed side section does not suppress the other section',async()=>{
 const h=setup(async q=>{if(q.order==='unanswered')throw Error('offline');return {list:[]};});await h.mount();assert.match(h.nodes.unanswered.innerHTML,/内容暂时无法加载/);assert.match(h.nodes.featured.innerHTML,/暂无精选内容/);
});
test('late responses cannot modify a page after navigation',async()=>{
 const pending=[];const h=setup(()=>new Promise(resolve=>pending.push(resolve)));const job=h.mount();h.ctx.navigationSequence++;for(const resolve of pending)resolve({list:[{id:'1',title:'old'}]});await job;assert.equal(h.nodes.featured.innerHTML,'');assert.equal(h.nodes.unanswered.innerHTML,'');
});
test('unanswered rows reject answered items and cap the list at three',()=>{
 const h=setup();const html=h.render({list:[{id:'answered',title:'answered',answer_count:1},...Array.from({length:8},(_,i)=>({id:String(i),title:'Q',answer_count:0}))]},true);assert.ok(!html.includes('answered'));assert.equal((html.match(/<li>/g)||[]).length,3);
});

test('sidebar mounts after the rendered page has been inserted',()=>{
 const start=source.indexOf('  function renderView('),end=source.indexOf('\n  }',start);
 const render=source.slice(start,end);assert.ok(render.indexOf('void mountDiscussionRail();')>render.indexOf('view.innerHTML = html'));
});

test('mobile side content defaults collapsed and expands when moving to desktop',async()=>{
 const h=setup(async()=>({list:[]}));const disclosure={open:true};let change;const media={matches:true,addEventListener:(type,fn)=>change=fn,removeEventListener:()=>{}};
 h.ctx.window.matchMedia=()=>media;const query=h.root.querySelector;h.root.querySelector=s=>s==='[data-rail-disclosure]'?disclosure:query(s);
 await h.mount();assert.equal(disclosure.open,false);media.matches=false;change();assert.equal(disclosure.open,true);
});
