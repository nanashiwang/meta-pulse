'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const context = vm.createContext({ window: {} });
vm.runInContext(fs.readFileSync(path.join(__dirname, '../src/pulse-core.js'), 'utf8'), context);
const { Timeline, cardFrame, markup, cards, strongest, prizeTint, blendLight } = context.window.MetarPulseCore;

test('without a server result, even a long wait cannot fracture or reveal a reward', () => {
  const clock = new Timeline(100);
  assert.equal(clock.sample(100).phase, 'gather');
  for (const now of [2000, 4000, 60000]) {
    const sample = clock.sample(now);
    assert.equal(sample.phase, 'waiting');
    assert.ok(sample.progress < 0.46);
    assert.equal(sample.animate, true);
  }
});

test('a fast result completes a 3.9 second performance; a slow result resumes from charging', () => {
  const fast = new Timeline(0);
  fast.receive(100);
  assert.notEqual(fast.sample(3899).phase, 'result');
  assert.equal(fast.sample(3900).phase, 'result');
  const slow = new Timeline(0);
  slow.receive(6000);
  assert.equal(slow.sample(6000).phase, 'charge');
  assert.equal(slow.sample(7000).phase, 'reveal');
  assert.equal(slow.sample(8500).phase, 'result');
});

test('reduced motion and finishing wait for the same result, then reveal immediately', () => {
  for (const reduced of [false, true]) {
    const clock = new Timeline(0, reduced);
    if (!reduced) clock.finish();
    assert.equal(clock.sample(100).phase, 'waiting');
    assert.equal(clock.sample(100).animate, false);
    clock.receive(2000);
    assert.equal(clock.sample(2000).phase, 'result');
    assert.equal(clock.sample(2000).animate, false);
  }
});

test('skip charge waits for a result, then keeps the entire 2.1 second card reveal', () => {
  for (const readyAt of [100, 6000]) {
    const clock = new Timeline(0);
    clock.skipCharge(0);
    assert.equal(clock.sample(readyAt - 1).phase, 'waiting');
    assert.equal(clock.sample(readyAt - 1).progress, .44);
    assert.equal(clock.sample(readyAt - 1).animate, true);
    clock.receive(readyAt);
    assert.equal(clock.sample(readyAt).phase, 'reveal');
    assert.equal(clock.sample(readyAt).progress, .46);
    for (let index = 0; index < 5; index++) {
      const burstAt = readyAt + (.64 + index * .027 - .46) * clock.duration;
      const frame = cardFrame(clock.sample(burstAt).progress, index);
      assert.equal(frame.light, 1);
      assert.equal(frame.content, 0);
    }
    assert.notEqual(clock.sample(readyAt + 2105).phase, 'result');
    assert.equal(clock.sample(readyAt + 2106).phase, 'result');
  }
});

test('skipping charge midflight only advances; repeated or late skips cannot replay the flash', () => {
  const clock = new Timeline(0);
  clock.receive(100);
  clock.skipCharge(1200);
  assert.equal(clock.sample(1200).progress, .46);
  const inReveal = clock.sample(2000).progress;
  clock.skipCharge(2000);
  assert.equal(clock.sample(2000).progress, inReveal);
  assert.equal(clock.sample(3306).phase, 'result');
  clock.receive(4000);
  assert.equal(clock.sample(4000).phase, 'result');
  const late = new Timeline(0);
  late.receive(100);
  const before = late.sample(2500).progress;
  late.skipCharge(2500);
  assert.equal(late.sample(2500).progress, before);
  assert.equal(late.sample(3900).phase, 'result');
});

test('no-motion modes override skip charge, including a mid-reveal visibility or motion change', () => {
  const reduced = new Timeline(0, true);
  reduced.skipCharge(0);
  reduced.receive(100);
  assert.equal(reduced.sample(100).phase, 'result');
  const hidden = new Timeline(0);
  hidden.skipCharge(0);
  hidden.receive(200);
  assert.equal(hidden.sample(400).phase, 'reveal');
  hidden.finish();
  assert.equal(hidden.sample(400).phase, 'result');
  assert.equal(hidden.sample(400).animate, false);
});

test('cards stay hidden at the server hold, bloom before readable content and settle at the end', () => {
  for (let index = 0; index < 5; index++) {
    assert.equal(cardFrame(.44, index).enter, 0);
    const burst = cardFrame(.64 + index * .027, index);
    assert.equal(burst.light, 1);
    assert.equal(burst.bloom, 1);
    assert.equal(burst.content, 0);
    assert.ok(burst.silhouette > 0);
    const end = cardFrame(1, index);
    assert.equal(end.enter, 1);
    assert.equal(end.content, 1);
    for (const key of ['light', 'bloom', 'silhouette', 'lift']) assert.equal(end[key], 0);
  }
  assert.ok(cardFrame(.73, 0).content > cardFrame(.73, 4).content);
});

test('each card has one smooth light burst and content never disappears again', () => {
  for (let index = 0; index < 5; index++) {
    let previous = cardFrame(0, index), fading = false;
    for (let tick = 1; tick <= 1000; tick++) {
      const frame = cardFrame(tick / 1000, index);
      for (const value of Object.values(frame)) assert.ok(value >= 0 && value <= 1);
      assert.ok(frame.content >= previous.content);
      if (frame.light < previous.light) fading = true;
      if (fading) assert.ok(frame.light <= previous.light);
      previous = frame;
    }
  }
});

test('the presentation contains no placeholder prize and translates all its visible labels', () => {
  const language = vm.createContext({ window: { localStorage:{getItem:()=> 'en_US'} } });
  vm.runInContext(fs.readFileSync(path.join(__dirname, '../src/i18n.js'), 'utf8'), language);
  const html = markup({t:language.window.MetarI18n.t});
  assert.doesNotMatch(html, /[\u3400-\u9fff]|\+1\.00/);
  assert.match(html, /aria-hidden="true"/);
  assert.match(html, /Visual effects do not affect the odds/);
  assert.doesNotMatch(html, /white|green|blue|gold|rainbow|七彩炫光/);
  assert.match(html, /data-action="pulse-draw" disabled/);
  assert.match(html, /data-action="pulse-draw-batch" disabled/);
  assert.match(html, /role="status" aria-live="polite"/);
});

test('five cards keep their own tier, share the strongest burst and escape displayed values', () => {
  const rewards=['white','rainbow','blue','gold','green'].map(tier=>({tier,type:'newapi_quota',amount:'<img>',unit:'⚡️',status:'pending',pending:true}));
  assert.equal(strongest(rewards),'rainbow');
  const html=cards({rewards,total:5});
  assert.equal((html.match(/class="pc-card"/g)||[]).length,5);
  assert.doesNotMatch(html,/<img>/); assert.match(html,/&lt;img&gt;/);
  assert.equal(strongest([{tier:'unknown'}]),'white');
  assert.doesNotMatch(html,/pc-card-foot|data-pending|pending/);
  assert.equal(cards({rewards:rewards.map(r=>({...r,status:'settled',pending:false})),total:5}),html,'delivery changes do not change the prize card');
});

test('bonus experience appears only on the original prize card', () => {
  const html=cards({rewards:[{type:'newapi_quota',amount:5,unit:'⚡️',bonus:'+ 4 EXP'},{type:'community_exp',amount:20,unit:'EXP'}],total:2});
  assert.equal((html.match(/class="pc-card"/g)||[]).length,2);
  assert.equal((html.match(/class="pc-bonus"/g)||[]).length,1);
  assert.match(html,/pc-bonus[^>]*>\+ 4 EXP</);
});


test('the jade palette holds through charging and blends only during fracture', () => {
  for (const p of [0,.29,.44,.46]) assert.equal(prizeTint(p),0);
  const half = prizeTint(.525);
  assert.ok(half > .49 && half < .51);
  assert.equal(prizeTint(.60),1);
  assert.equal(blendLight('#178a66','#a98bf7',0),'#178a66');
  assert.match(blendLight('#178a66','#a98bf7',half),/^rgb/);
  assert.equal(blendLight('#178a66','#a98bf7',1),'#a98bf7');
});

test('gift markup has a free claim button and is isolated from Pulse draw controls', () => {
  const t = s => s;
  const gift = markup({t, gift: true});
  assert.match(gift, /class="pc-primary" disabled>领取<\/button>/);
  assert.doesNotMatch(gift, /data-pulse-core|data-action="pulse-draw"|class="pc-primary"[^>]*>[^<]*消耗/);
  assert.match(markup({t}), /data-action="pulse-draw" disabled>抽一次（消耗 1 张）/);
});

test('confirmed bonus belongs to its own credit card and refresh does not replay the animation', () => {
  const reward = {amount:'1', unit:'⚡️', type:'newapi_quota', tier:'white'};
  const result = {rewards:[reward,{...reward,bonus:'+ 4 EXP'},reward]};
  const html = cards(result);
  assert.equal((html.match(/class="pc-bonus"/g) || []).length, 1);
  assert.match(html, /class="pc-bonus">\+ 4 EXP<\/div>/);
  const calls = [];
  const view = {root:{dataset:{stage:'result'}},fillResult:value=>calls.push(value),scene:{paint:progress=>calls.push(progress)}};
  context.window.MetarPulseCore.View.prototype.refreshResult.call(view,result);
  assert.deepEqual(calls,[result,1]);
  view.timeline = {};
  context.window.MetarPulseCore.View.prototype.refreshResult.call(view,result);
  assert.equal(calls.length,2);
});
