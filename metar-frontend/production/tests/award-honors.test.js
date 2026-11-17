const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const script=fs.readFileSync(require('node:path').join(__dirname,'../src/award-honors.js'),'utf8');
test('honors show selectable versions, gold medal counts and escaped source records',async()=>{const host={};vm.runInNewContext(script,{window:host,Date});const view=new host.MetarAwardHonors.View({request:async()=>({titles:[{key:'post-week',name:'优秀作者',style:'normal',expires_at:9999999999},{key:'post-month',name:'优秀作者',style:'glow',expires_at:9999999999}],selection:'post-month',badges:[{name:'编辑者',count:2}],records:[{period:{type:'month',key:'2026-11',kind:'post'},reason:'<script>bad</script>',question_id:'100',answer_id:'0'}]})});const html=await view.page(new URLSearchParams());assert.ok(html.includes('post-week'));assert.ok(html.includes('post-month'));assert.ok(html.includes('selected'));assert.ok(html.includes('编辑者 × 2'));assert.ok(html.includes('不展示'));assert.ok(html.includes('&lt;script&gt;'));assert.ok(!html.includes('<script>bad'));});

test('both gold medals and conditions remain visible before the first award',()=>{
 const host={};vm.runInNewContext(script,{window:host,Date});const html=host.MetarAwardHonors.badges([]);
 for(const name of ['编辑者','学者','优秀帖子奖','优秀回应奖','尚未获得'])assert.ok(html.includes(name),name);
 assert.equal((html.match(/class="award-medal-icon"/g)||[]).length,2);assert.ok(!html.includes('is-earned'));
});
test('gold medal loading or failed reads do not claim unearned status',()=>{
 const host={};vm.runInNewContext(script,{window:host,Date});
 assert.match(host.MetarAwardHonors.badges(null),/正在读取获得状态/);
 const failed=host.MetarAwardHonors.badges(null,true);assert.match(failed,/获得状态暂不可用/);assert.doesNotMatch(failed,/尚未获得/);assert.match(failed,/编辑者/);assert.match(failed,/学者/);
 assert.throws(()=>host.MetarAwardHonors.badges([{kind:'post',count:'<img>'}]));
});
