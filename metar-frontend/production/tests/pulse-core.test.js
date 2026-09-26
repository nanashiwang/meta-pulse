'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const context = vm.createContext({ window: {} });
vm.runInContext(fs.readFileSync(path.join(__dirname, '../src/pulse-core.js'), 'utf8'), context);
const { Timeline, markup } = context.window.MetarPulseCore;

test('without a server result, even a long wait cannot fracture or reveal a reward', () => {
  const clock = new Timeline(100);
  assert.equal(clock.sample(100).phase, 'gather');
  for (const now of [2000, 4000, 60000]) {
    const sample = clock.sample(now);
    assert.equal(sample.phase, 'waiting');
    assert.ok(sample.progress < 0.46);
    assert.equal(sample.animate, false);
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

test('skip and reduced motion wait for the same result and then reveal immediately', () => {
  for (const reduced of [false, true]) {
    const clock = new Timeline(0, reduced);
    if (!reduced) clock.skip();
    assert.equal(clock.sample(100).phase, 'waiting');
    assert.equal(clock.sample(100).animate, false);
    clock.receive(2000);
    assert.equal(clock.sample(2000).phase, 'result');
    assert.equal(clock.sample(2000).animate, false);
  }
});

test('the presentation contains no placeholder prize and translates all its visible labels', () => {
  const language = vm.createContext({ window: { localStorage:{getItem:()=> 'en_US'} } });
  vm.runInContext(fs.readFileSync(path.join(__dirname, '../src/i18n.js'), 'utf8'), language);
  const html = markup({t:language.window.MetarI18n.t});
  assert.doesNotMatch(html, /[\u3400-\u9fff]|\+1\.00/);
  assert.match(html, /aria-hidden="true"/);
  assert.match(html, /data-action="pulse-draw" disabled/);
  assert.match(html, /role="status" aria-live="polite"/);
});
