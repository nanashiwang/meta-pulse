const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
// Exercise the exact helper shipped by the Answer patch (new-file hunk).
const patch=fs.readFileSync(__dirname+'/../answer-admin-user-list.patch','utf8');
const section=patch.split('diff --git a/ui/src/pages/Admin/Users/metar-statistics.js b/ui/src/pages/Admin/Users/metar-statistics.js')[1].split('\ndiff --git ')[0];
const source=section.split('\n').filter(line=>line.startsWith('+')&&!line.startsWith('+++')).map(line=>line.slice(1)).join('\n');
const modulePromise=import('data:text/javascript;base64,'+Buffer.from(source).toString('base64'));
test('sorts across pages before slicing, exact integers and missing values last in both directions',async()=>{
 const {loadAllUsers,sortUsers}=await modulePromise;
 const bindings={'1':{tickets:{total:'9007199254740993'}},'2':{tickets:{total:'9007199254740992'}},'3':{}};
 const all=await loadAllUsers(async page=>({count:3,list:page===1?[{user_id:'2'},{user_id:'3'}]:[{user_id:'1'}]}),async ids=>Object.fromEntries(ids.map(id=>[id,bindings[id]])),new AbortController().signal);
 assert.deepEqual(sortUsers(all.list,all.bindings,'tickets_total','desc').map(u=>u.user_id),['1','2','3']);
 assert.deepEqual(sortUsers(all.list,all.bindings,'tickets_total','asc').map(u=>u.user_id),['2','1','3']);
 assert.equal(sortUsers(all.list,all.bindings,'tickets_total','desc').slice(0,1)[0].user_id,'1');
});
test('stable ties, community fields and separate quota/EXP order',async()=>{
 const {sortUsers}=await modulePromise;
 const users=[{user_id:'10',question_count:3},{user_id:'2',question_count:3}];
 const bindings={'10':{tickets:{reward_quota:'1',reward_experience:'100'}},'2':{tickets:{reward_quota:'2',reward_experience:'0'}}};
 assert.equal(sortUsers(users,bindings,'post_count','desc')[0].user_id,'2');
 assert.equal(sortUsers(users,bindings,'reward_quota','desc')[0].user_id,'2');
 assert.equal(sortUsers(users,bindings,'reward_experience','desc')[0].user_id,'10');
});
test('quota display keeps integer precision and distinguishes missing from zero',async()=>{
 const {quotaText}=await modulePromise;
 assert.equal(quotaText('625000','500000'),'1.25 ⚡');
 assert.equal(quotaText('0','500000'),'0 ⚡');
 assert.equal(quotaText(undefined,'500000'),'—');
 assert.equal(quotaText('9007199254740993','1'),'9007199254740993 ⚡');
});
test('failure, cancellation, changed counts and duplicates never return partial rankings',async()=>{
 const {loadAllUsers}=await modulePromise;
 const empty=async()=>({});
 await assert.rejects(loadAllUsers(async p=>{if(p===2)throw Error('offline');return {count:2,list:[{user_id:'1'}]};},empty,new AbortController().signal));
 await assert.rejects(loadAllUsers(async p=>({count:p,list:[{user_id:'1'}]}),async()=>{throw Error('stats offline');},new AbortController().signal));
 await assert.rejects(loadAllUsers(async p=>({count:p===1?2:3,list:[{user_id:String(p)}]}),empty,new AbortController().signal));
 await assert.rejects(loadAllUsers(async()=>({count:2,list:[{user_id:'1'}]}),empty,new AbortController().signal));
 const controller=new AbortController();
 await assert.rejects(loadAllUsers(async()=>{controller.abort();return {count:1,list:[{user_id:'1'}]};},empty,controller.signal));
});
