const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync(require('node:path').join(__dirname,'../src/awards.js'),'utf8');
function setup(){class Clock extends Date{static now(){return Date.parse("2026-11-18T06:00:00Z");}}const memory=new Map();const host={MetarI18n:{getLanguage:()=> 'zh_CN'},sessionStorage:{getItem:k=>memory.get(k),setItem:(k,v)=>memory.set(k,v),removeItem:k=>memory.delete(k)},crypto:{randomUUID:()=> 'stable-request-key'}};vm.runInNewContext(source,{window:host,URLSearchParams,Date:Clock,FormData:class {constructor(form){return form.values;}}});return {api:host.MetarAwards,host,memory}}
const data={status:'open',version:1,review_at:1794931200,items:[{subject_id:'100',user_id:'1',question_id:'100',answer_id:'0',title:'<img src=x>',name:'<script>bad</script>',status:'pending',reason:'<b>proof</b>',eligible:true,nominations:2}],more:false};
test('award page uses Beijing Monday and escapes candidate evidence',async()=>{const h=setup();assert.equal(h.api.currentWeek(new Date('2026-11-15T16:00:00Z')),'2026-11-16');assert.equal(h.api.currentWeek(new Date('2026-11-15T15:59:59Z')),'2026-11-09');const view=new h.api.View({}, {request:async()=>data});const html=await view.page('/admin/awards',{id:'3',role_id:2},new URLSearchParams('type=week&key=2026-11-09&kind=post'));assert.ok(html.includes('&lt;img'));assert.ok(!html.includes('<script>bad'));assert.ok(html.includes('data-criterion="one"'));assert.ok(html.includes('由管理员点击发放'));});
test('public winners show deferred reward and never admin review controls',async()=>{const h=setup(),view=new h.api.View({request:async()=>({...data,status:'published',items:[{...data.items[0],status:'awarded',amount:50,reward_status:'deferred'}]})},{});const html=await view.page('/awards',null,new URLSearchParams('key=2026-11-09'));assert.ok(html.includes('50'));assert.ok(html.includes('待发放'));assert.ok(!html.includes('data-award="select"'));assert.ok(!html.includes('data-award="publish"'));});
test('uncertain review is replayed with the same key; confirmed command is cleared before refresh',async()=>{const h=setup();let fail=true;const requests=[];const client={request:async(path,opts)=>{if(opts){requests.push(opts);if(fail)throw Error('timeout');return {ok:true}}return data}};const view=new h.api.View({},client);await view.page('/admin/awards',{id:'3'},new URLSearchParams('key=2026-11-09'));const cmd={op:'sync',period:{type:'week',key:'2026-11-09',kind:'post'},version:1};await assert.rejects(view.command(cmd));assert.equal(h.memory.size,1);await assert.rejects(view.command({...cmd}));fail=false;await view.command(view.pending.command);assert.equal(requests.length,2);assert.equal(requests[0].headers['Idempotency-Key'],requests[1].headers['Idempotency-Key']);assert.equal(requests[0].body,requests[1].body);assert.equal(h.memory.size,0);});
test('definitive denial clears pending review rather than replaying against a new version',async()=>{const h=setup();const client={request:async(path,opts)=>{if(opts)throw Object.assign(Error('conflict'),{status:409});return data}};const view=new h.api.View({},client);await view.page('/admin/awards',{id:'3'},new URLSearchParams('key=2026-11-09'));await assert.rejects(view.command({op:'sync',version:1}));assert.equal(view.pending,null);assert.equal(h.memory.size,0);});

function candidateControls(value = '') {
  const reason = {value, focus(){this.focused=true;}};
  const criteria = [{checked:false,focus(){this.focused=true;}},{checked:false}];
  const classes = new Set();
  const feedback = {textContent:'', classList:{add:name=>classes.add(name),remove:name=>classes.delete(name)}};
  const row = {dataset:{candidate:'100'},querySelector:s=>s==='[data-reason]'?reason:s==='[data-award-feedback]'?feedback:s.includes('one')?criteria[0]:criteria[1]};
  const button = {dataset:{award:'reject'},disabled:false,closest:()=>row};
  const form = {elements:{type:{}}};
  const viewRoot = {querySelector:s=>s==='[data-award-period]'?form:null,querySelectorAll:s=>s==='[data-candidate]'?[row]:[button]};
  return {row,button,viewRoot,feedback,reason,criteria,classes};
}

test('future review period explains and disables decisions without issuing a request',async()=>{
 const h=setup();let posts=0;const client={request:async(path,opts)=>{if(opts)posts++;return {...data,review_at:4102444800};}};
 const view=new h.api.View({},client);
 const html=await view.page('/admin/awards',{id:'3'},new URLSearchParams('key=2026-11-16'));
 assert.ok(html.includes('尚未到审核时间'));assert.ok(html.includes('data-award="reject" disabled'));
 const ui=candidateControls('待审核');view.mount(ui.viewRoot);await ui.button.onclick();
 assert.equal(posts,0);assert.match(ui.feedback.textContent,/尚未到审核时间/);
});

test('reject requires only a reason, saves once and updates the candidate with visible feedback',async()=>{
 const h=setup(),ui=candidateControls(),posts=[];
 const view=new h.api.View({}, {request:async(path,opts)=>{
   if(opts){posts.push(JSON.parse(opts.body));return {version:2};}
   return posts.length?{...data,version:2,items:[{...data.items[0],status:'rejected',reason:posts[0].reason}]}:data;
 }});
 await view.page('/admin/awards',{id:'3'},new URLSearchParams('key=2026-11-09'));view.mount(ui.viewRoot);
 await ui.button.onclick();assert.equal(ui.reason.focused,true);assert.match(ui.feedback.textContent,/填写审核理由/);assert.equal(posts.length,0);
 ui.reason.value='  未达到本期标准  ';ui.button.dataset.award='select';await ui.button.onclick();assert.equal(posts.length,0);assert.equal(ui.criteria[0].focused,true);assert.match(ui.feedback.textContent,/两项审核条件/);
 ui.button.dataset.award='reject';await ui.button.onclick();
 assert.deepEqual(posts,[{op:'reject',period:{type:'week',key:'2026-11-09',kind:'post'},version:1,subject_id:'100',reason:'未达到本期标准',criterion_one:false,criterion_two:false}]);
 assert.equal(view.data.items[0].status,'rejected');assert.match(ui.viewRoot.innerHTML,/class="badge">未入选/);assert.equal(ui.feedback.textContent,'已保存为不入选。');
 assert.equal(h.memory.size,0);
});

test('a confirmed rejection with a failed refresh is not offered for replay',async()=>{
 const h=setup(),ui=candidateControls('理由');let posted=false;
 const view=new h.api.View({}, {request:async(path,opts)=>{if(opts){posted=true;return {version:2};} if(posted)throw Error('offline');return data;}});
 await view.page('/admin/awards',{id:'3'},new URLSearchParams('key=2026-11-09'));view.mount(ui.viewRoot);
 await ui.button.onclick();assert.match(ui.feedback.textContent,/已保存.*刷新失败/);assert.equal(h.memory.size,0);assert.equal(view.pending,null);
});

test('my submissions keeps the period, separates both awards and escapes answer text',async()=>{
 const h=setup();const view=new h.api.View({request:async path=>{assert.equal(path,'/metar/awards/submissions?type=week&key=2026-11-16');return [{kind:'response',subject_id:'22',question_id:'11',title:'标题',excerpt:'<回答>',week:'2026-11-16',can_withdraw:true}]}},{});
 const html=await view.page('/awards',{id:'3'},new URLSearchParams('view=submissions&type=week&key=2026-11-16&kind=post'));
 assert.match(html,/优秀帖子奖.*本期暂无提交.*优秀回答奖/s);assert.ok(html.includes('/questions/11/22'));assert.ok(html.includes('&lt;回答&gt;'));assert.ok(html.includes('取消提交'));assert.ok(html.includes('伯乐奖'));
});
test('award header has two distinct stacked controls for admins',async()=>{const h=setup(),v=new h.api.View({request:async()=>data},{});const html=await v.page('/awards',{id:'3',role_id:2},new URLSearchParams('key=2026-11-16'));assert.match(html,/<nav class="award-header-actions"><a class="btn primary"[^>]+>我的提交<\/a><a class="btn"[^>]+>管理审核<\/a><\/nav>/);});
test('missing submissions API retains both sections and does not claim empty history',async()=>{const h=setup(),v=new h.api.View({request:async()=>{throw Error('invalid_response')}},{});const html=await v.page('/awards',{id:'3'},new URLSearchParams('key=2026-11-16&view=submissions'));for(const expected of ['重新加载','优秀帖子奖','优秀回答奖','伯乐奖'])assert.ok(html.includes(expected));assert.ok(!html.includes('本期暂无提交'));});


test('published awards use three-column cards and only offer valid delivery actions', async () => {
 const h = setup();
 const item = {...data.items[0], status:'awarded', amount:50, reward_status:'manual_pending', recommendations:[{user_id:'2', name:'推荐者', cents:2000, reward_status:'ready'}]};
 const open = {...data, status:'published', items:[item]};
 const view = new h.api.View({request:async()=>open},{request:async()=>open});
 const html = await view.page('/admin/awards',{id:'3'},new URLSearchParams('key=2026-11-09'));
 assert.match(html, /award-entry--three/);
 assert.match(html, /data-award="grant">发放/);
 assert.match(html, /data-award="rec_grant".*>发放/);
 assert.doesNotMatch(html, /确认已手动发放|登记发放/);
 const publicView = await view.page('/awards',null,new URLSearchParams('key=2026-11-09'));
 assert.doesNotMatch(publicView, /推荐名单|data-award="(?:grant|rec_grant)"/);
 item.reward_status='manual_paid';
 item.recommendations[0].reward_status = 'queued';
 const paid = await view.page('/admin/awards',{id:'3'},new URLSearchParams('key=2026-11-09'));
 assert.match(paid, /已手动发放/);
 assert.match(paid, /待到账/);
 assert.doesNotMatch(paid, /data-award="(?:grant|rec_grant)"/);
});

test('send action needs confirmation, no manual payment note or client amount', async () => {
 const h = setup(), ui = candidateControls(), posts = [];
 const published = {...data,status:'published',items:[{...data.items[0],status:'awarded',amount:50,reward_status:'manual_pending'}]};
 const client = {request:async(path,opts)=>{
  if(opts) {posts.push(JSON.parse(opts.body)); return {version:2};}
  if (posts.length) return {...published,version:2,items:[{...published.items[0],reward_status:'queued'}]};
  return published;
 }};
 const view = new h.api.View({},client);
 await view.page('/admin/awards',{id:'3'},new URLSearchParams('key=2026-11-09'));
 view.mount(ui.viewRoot);
 ui.button.dataset.award='grant';
 h.host.confirm=()=>false;
 await ui.button.onclick();
 assert.equal(posts.length,0);
 h.host.confirm=()=>true;
 await ui.button.onclick();
 assert.deepEqual(posts,[{op:'grant',period:{type:'week',key:'2026-11-09',kind:'post'},version:1,subject_id:'100'}]);
 assert.match(ui.viewRoot.innerHTML,/待到账/);
 assert.doesNotMatch(ui.viewRoot.innerHTML,/data-award="grant"/);
 assert.match(ui.feedback.textContent,/已提交发放/);
});


test('public archive keeps periods separate, links to an exact period and paginates by period', async () => {
 const h=setup(), requests=[];
 const group=(key,title,status='published')=>({period:{type:'week',key,kind:'post'},status,items:[{...data.items[0],title,status:'awarded'}]});
 const groups=[group('2026-11-16','最近期获奖'),group('2026-11-09','上一期获奖'),group('2026-11-23','不可公开内容','open')];
 const view=new h.api.View({request:async path=>{
   requests.push(path);
   return path.includes('key=2026-11-16') ? groups[0] : {periods:groups,more:true};
 }},{});
 const archive=await view.page('/awards',null,new URLSearchParams());
 assert.equal(requests[0],'/metar/awards?page=1');
 assert.equal((archive.match(/class="card card-pad award-public-period"/g)||[]).length,2);
 assert.match(archive,/2026-11-16 · 优秀帖子奖.*最近期获奖.*2026-11-09 · 优秀帖子奖.*上一期获奖/s);
 assert.match(archive,/href="\/awards\?type=week&amp;key=2026-11-16&amp;kind=post&amp;page=1">仅看本期/);
 assert.match(archive,/href="\/awards\?page=2">下一页/);
 assert.doesNotMatch(archive,/不可公开内容/);
 const exact=await view.page('/awards',null,new URLSearchParams('type=week&key=2026-11-16&kind=post'));
 assert.match(requests[1],/key=2026-11-16/);
 assert.match(exact,/最近期获奖|查看全部期次/);
 assert.doesNotMatch(exact,/上一期获奖/);
 assert.match(exact,/href="\/awards\?type=week&amp;kind=post&amp;page=1">查看全部期次/);
});

test('blank public filter keeps award and cycle without inventing a date', async () => {
 const h=setup(); const destinations=[];
 h.host.location={assign: url=>destinations.push(url)};
 const v=new h.api.View({request:async()=>({periods:[],more:false})},{});
 await v.page('/awards',null,new URLSearchParams('type=month&kind=response'));
 const fields=new Map([['type','month'],['kind','response'],['key','']]);
 const form={elements:{type:{value:'month'},key:{value:''}},values:fields};
 v.mount({querySelector:()=>form,querySelectorAll:()=>[]});
 form.onsubmit({preventDefault(){}});
 assert.equal(destinations[0],'/awards?type=month&kind=response');
 fields.set('key','2026-11');
 form.onsubmit({preventDefault(){}});
 assert.equal(destinations[1],'/awards?type=month&kind=response&key=2026-11');
});


test('admin without a date opens the server suggested period and preserves explicit choices', async () => {
 const h = setup(), requests = [];
 const view = new h.api.View({}, {request:async path=>{
   requests.push(path);
   return {...data,period:{type:'week',key:'2026-11-09',kind:'post'},recent:['2026-11-16','2026-11-09','2026-11-02']};
 }});
 const html=await view.page('/admin/awards',{id:'3'},new URLSearchParams());
 assert.equal(requests[0],'/metar/awards?type=week&kind=post&page=1');
 assert.equal(view.period.key,'2026-11-09');
 assert.match(html,/近期审核期次.*aria-current="page"[^>]*>2026-11-09/s);
 assert.match(html,/已自动打开推荐期次/);
 const exact=await view.page('/admin/awards',{id:'3'},new URLSearchParams('type=week&key=2026-11-02&kind=post'));
 assert.match(requests[1],/key=2026-11-02/);
 assert.equal(view.period.key,'2026-11-02');
 assert.doesNotMatch(exact,/已自动打开推荐期次/);
});
