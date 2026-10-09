'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
global.window = global;
require('../src/adapters.js');
const choice = {selection_version:3, draws:Array.from({length:5},(_,i)=>({selection:'server-choice-'+i}))};
const { PulseOperation, PulseDrawSession, pulseActions, pulseRewardTier, AdapterError } = window.MetarAdapters;
function storage() {
  const values = new Map();
  window.sessionStorage = {getItem: key => values.get(key) || null, setItem: (key,value) => values.set(key,value), removeItem: key => values.delete(key)};
  return values;
}
const reward = action => ({grant_id:'grant-'+action.actionId,action_id:action.actionId,reward_type:'newapi_quota',amount:500000,status:'pending'});

test('five draws persist five distinct identifiers before spending, isolate users and preserve legacy operations', () => {
  const values = storage(), store = new PulseOperation('a'), operation = store.begin(5, choice);
  assert.equal(pulseActions(operation).length, 5);
  assert.equal(new Set(operation.actions.map(a => a.actionId)).size, 5);
  assert.deepEqual(store.begin(1, choice), operation);
  assert.deepEqual(new PulseOperation('a').read(), operation);
  assert.equal(new PulseOperation('b').read(), null);
  assert.doesNotMatch(values.get(store.key), /amount|reward|balance|ticket/);
  store.clear();
  const single = store.begin(1, choice);
  assert.equal(pulseActions(single).length, 1);
  assert.deepEqual(store.begin(5, choice), single);
});

const immediate = {wait:async()=>{}};

test('a lost third response automatically recovers its pending Grant and finishes all five without awaiting settlement', async () => {
  storage();
  const store = new PulseOperation('a'), operation = store.begin(5, choice), issued = new Map(), calls = [], queries = [];
  const client = {
    async act(action) {
      calls.push(action);
      assert.equal(store.read().actions.length, 5);
      const result = issued.get(action.actionId) || reward(action); issued.set(action.actionId, result);
      if (calls.length === 3) throw new AdapterError('lost response', {code:'action_pending'});
      return result;
    },
    async rewards(id) { queries.push(id); return {rewards: issued.has(id) ? [issued.get(id)] : []}; },
  };
  const progress=[];
  const results=await new PulseDrawSession(client,store,immediate).run(operation,{onResult:r=>progress.push(r.length)});
  assert.deepEqual(progress,[1,2,3,4,5]);
  assert.equal(calls.length,5); assert.equal(issued.size,5);
  assert.deepEqual(queries,[operation.actions[2].actionId]);
  assert.ok(results.every(r=>r.status==='pending'));
  assert.equal(store.read(),null,'pending delivery is not an unfinished draw');
  assert.deepEqual(results.map(r=>r.action_id),operation.actions.map(a=>a.actionId));
  assert.notDeepEqual(store.begin(5,choice),operation,'next round can start while every reward is pending');
});

test('fourth request rejected by a temporary 503 is queried then retried with the original selection and key', async () => {
  storage();
  const store=new PulseOperation('a'), operation=store.begin(5,choice), calls=[], issued=new Map(), events=[], delays=[];
  const client={
    async act(action){
      calls.push(action); events.push('post:'+action.actionId);
      if(calls.length===4) throw new AdapterError('503',{code:'pulse_unavailable'});
      const result=issued.get(action.actionId)||reward(action); issued.set(action.actionId,result); return result;
    },
    async rewards(id){events.push('query:'+id);return {rewards:issued.has(id)?[issued.get(id)]:[]};},
  };
  const results=await new PulseDrawSession(client,store,{wait:async ms=>delays.push(ms)}).run(operation);
  assert.equal(results.length,5);assert.equal(issued.size,5);assert.equal(calls.length,6);
  assert.deepEqual(calls[3],calls[4]);
  assert.deepEqual(events.slice(3,6),['post:','query:','post:'].map(s=>s+operation.actions[3].actionId));
  assert.deepEqual(delays,[300]);assert.equal(store.read(),null);
});

test('persistent uncertainty has bounded retries and retains the same operation for read-only reload recovery', async () => {
  storage();
  const store=new PulseOperation('a'), operation=store.begin(5,choice), calls=[], issued=new Map(), delays=[];
  let offline=true;
  const client={
    async act(action){calls.push(action);if(offline)throw new AdapterError('offline',{code:'action_pending'});const r=reward(action);issued.set(action.actionId,r);return r;},
    async rewards(id){return {rewards:issued.has(id)?[issued.get(id)]:[]};},
  };
  const session=new PulseDrawSession(client,store,{wait:async ms=>delays.push(ms)});
  await assert.rejects(session.run(operation),e=>e.code==='action_pending');
  assert.equal(calls.length,3);assert.deepEqual(delays,[300,1000]);
  assert.ok(calls.every(a=>JSON.stringify(a)===JSON.stringify(operation.actions[0])));
  assert.deepEqual(store.read(),operation);
  offline=false;
  issued.set(operation.actions[0].actionId,reward(operation.actions[0])); // delayed committed result becomes readable
  const restored=new PulseOperation('a'), recovery=new PulseDrawSession(client,restored,immediate);
  const known=await recovery.recover(restored.read());
  assert.equal(known.length,1);assert.equal(calls.length,3,'reload only queries');
  const results=await recovery.run(restored.read(),{known});
  assert.equal(results.length,5);assert.equal(issued.size,5);assert.equal(calls.length,7);assert.equal(restored.read(),null);
});

test('failed, malformed or conflicting history never authorizes another POST', async () => {
  for(const response of [null,{}, {rewards:[{action_id:'wrong'}]}, {rewards:'invalid'}, 'offline', 'duplicate']){
    storage();const store=new PulseOperation('a'),operation=store.begin(5,choice);let posts=0,queries=0;
    const client={
      async act(){posts++;throw new AdapterError('lost',{code:'action_pending'});},
      async rewards(){queries++;if(response==='offline')throw new AdapterError('503',{code:'pulse_unavailable'});if(response==='duplicate')return {rewards:[reward(operation.actions[0]),reward(operation.actions[0])]};return response;},
    };
    await assert.rejects(new PulseDrawSession(client,store,immediate).run(operation));
    assert.equal(posts,1);assert.equal(queries,response==='duplicate'?1:2);assert.deepEqual(store.read(),operation);
  }
});

test('definite refusals, auth errors and conflicts stop immediately without retrying or advancing', async () => {
  for(const code of ['action_rejected','selection_changed','selection_required','auth_required','action_conflict']){
    storage();const store=new PulseOperation('a'),operation=store.begin(5,choice);let posts=0;
    const client={async act(){posts++;throw new AdapterError(code,{code});},async rewards(){assert.fail('terminal errors must not enter auto recovery');}};
    await assert.rejects(new PulseDrawSession(client,store,immediate).run(operation),e=>e.code===code);
    assert.equal(posts,1);assert.deepEqual(store.read(),operation);
  }
});

test('navigation during a recovery wait or query stops unsubmitted actions', async () => {
  for(const stopAt of ['wait','query']){
    storage();const store=new PulseOperation('a'),operation=store.begin(5,choice);let active=true,posts=0,queries=0;
    const client={async act(){posts++;throw new AdapterError('lost',{code:'action_pending'});},async rewards(){queries++;active=false;return {rewards:[]};}};
    const session=new PulseDrawSession(client,store,{wait:async()=>{if(stopAt==='wait')active=false;}});
    await assert.rejects(session.run(operation,{canContinue:()=>active}),e=>e.code==='action_interrupted');
    assert.equal(posts,1);assert.equal(queries,stopAt==='query'?1:0);assert.deepEqual(store.read(),operation);
  }
});

test('navigation stops unsent actions and malformed responses retain the original journal', async () => {
  storage();
  const store = new PulseOperation('a'), operation = store.begin(5, choice); let active = true, calls = 0;
  const client = {async act(action) {calls++;active=false;return reward(action);}};
  let confirmed;
  await assert.rejects(new PulseDrawSession(client,store,immediate).run(operation,{canContinue:()=>active,onResult:r=>confirmed=r}),error=>error.code==='action_interrupted');
  assert.equal(calls,1);assert.equal(confirmed.length,1);assert.deepEqual(store.read(),operation);
  await assert.rejects(new PulseDrawSession({async act(){return {...reward(operation.actions[0]),action_id:'wrong'};},async rewards(){return {rewards:[{action_id:'wrong'}]};}},store,immediate).run(operation),error=>error.code==='pulse_unavailable');
  assert.deepEqual(store.read(),operation);
});

test('corrupt or blocked recovery storage never silently replaces an existing operation', () => {
  const values=storage(), store=new PulseOperation('a');
  values.set(store.key,'{broken');
  assert.throws(()=>store.begin(5, choice),error=>error.code==='storage_unavailable');
  values.clear(); window.sessionStorage.setItem=()=>{throw Error('full')};
  assert.throws(()=>store.begin(5, choice),error=>error.code==='storage_unavailable');
});

test('five presentation tiers use exact boundaries without changing reward amounts', () => {
  const quota = amount => ({reward_type:'newapi_quota',amount});
  const unit=500000;
  assert.equal(pulseRewardTier({reward_type:'community_exp',amount:100},unit),'white');
  for(const [amount,tier] of [[1,'blue'],[249999,'blue'],[250000,'purple'],[999999,'purple'],[1000000,'gold'],[4999999,'gold'],[5000000,'red']]) assert.equal(pulseRewardTier(quota(amount),unit),tier);
  assert.equal(pulseRewardTier(quota(1),3),'blue');assert.equal(pulseRewardTier(quota(2),3),'purple');
  assert.equal(pulseRewardTier(quota(Number.MAX_SAFE_INTEGER),Number.MAX_SAFE_INTEGER),'purple');
  assert.equal(pulseRewardTier(quota(10),0),'white');
});

test('new sessions preserve each of five cross-batch selections after reload', async () => {
  storage();
  const store = new PulseOperation('selection-user');
  assert.throws(()=>store.begin(),e=>e.code==='selection_required');
  const operation=store.begin(5,choice);
  assert.ok(pulseActions(operation).every(a=>a.protocolVersion===3 && a.selection===choice.draws[pulseActions(operation).indexOf(a)].selection));
  assert.deepEqual(store.begin(5,{selection_version:2,selection:'another-group'}),operation);
  const restored=new PulseOperation('selection-user').read();
  const calls=[];
  await new PulseDrawSession({act:async a=>{calls.push(a);return {action_id:a.actionId,grant_id:a.actionId,amount:10,reward_type:'community_exp'};}},store).run(restored);
  assert.ok(calls.every((a,i)=>a.selection===choice.draws[i].selection));
});

test('historical saved requests stay v1; mixed protocol saved batches fail closed', () => {
  const values=storage(),store=new PulseOperation('legacy');
  const id=crypto.randomUUID();
  values.set(store.key,JSON.stringify({actionId:id,idempotencyKey:id}));
  assert.equal(store.read().selection,undefined);
  assert.equal(store.begin(1,choice).selection,undefined);
  store.clear();
  const operation=store.begin(5,choice);
  operation.actions[1].protocolVersion=2;
  values.set(store.key,JSON.stringify(operation));
  assert.throws(()=>store.read(),e=>e.code==='storage_unavailable');
});

test('probability display uses exact integer ratios and never rounds tiny nonzero odds to zero', () => {
 const {formatPulseProbability: pct, pulseRewardStats: stats}=window.MetarAdapters;
 assert.equal(pct(44000,100000),'44%');assert.equal(pct(800,100000),'0.8%');
 assert.equal(pct(28000,50000),'56%');assert.equal(pct(12000,50000),'24%');
 assert.equal(pct(1,3),'≈33.3333%');assert.equal(pct(1,Number.MAX_SAFE_INTEGER),'<0.0001%');
 for(const [a,b] of [[1,0],[-1,2],[3,2],['<b>',2],[1,NaN]]) assert.equal(pct(a,b),'—');
 const large=stats([{amount:Number.MAX_SAFE_INTEGER,weight:Number.MAX_SAFE_INTEGER}]);
 assert.equal(large.quotaAmount,BigInt(Number.MAX_SAFE_INTEGER)**2n);
 assert.throws(()=>stats([{amount:1,weight:Number.MAX_SAFE_INTEGER},{amount:1,weight:1}]));
 assert.throws(()=>stats([{amount:1,weight:1,reward_type:'unknown'}]));
});
