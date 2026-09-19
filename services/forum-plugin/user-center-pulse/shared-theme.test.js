import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createThemeController, THEME_KEY } from './shared-theme.js';

function fixture(saved = null, blocked = false) {
  let applied;
  let writes = 0;
  const values = new Map([[THEME_KEY, saved]]);
  let changed;
  const system = { matches: false, addEventListener(_, fn) { changed = fn; } };
  const storage = {
    getItem(key) { if (blocked) throw Error('blocked'); return values.get(key); },
    setItem(key, value) { if (blocked) throw Error('blocked'); writes++; values.set(key, value); },
  };
  const controller = createThemeController({ storage, system, apply(value) { applied = value; } });
  return { controller, values, writes: () => writes, applied: () => applied, system(value) { system.matches = value; changed(); } };
}

test('system changes remain live until an explicit choice, without saving defaults', () => {
  const app = fixture();
  assert.equal(app.applied(), 'light');
  app.system(true);
  assert.equal(app.applied(), 'dark');
  assert.equal(app.writes(), 0);
  app.controller.select('light');
  app.system(false);
  app.system(true);
  assert.equal(app.applied(), 'light');
  assert.equal(app.values.get(THEME_KEY), 'light');
  assert.equal(fixture(app.values.get(THEME_KEY)).applied(), 'light');
});

test('cross-page storage updates and VitePress auto preference use the same contract', () => {
  const app = fixture('dark');
  const seen = [];
  app.controller.subscribe((theme) => seen.push(theme));
  app.controller.fromStorage('light');
  app.system(true);
  app.controller.fromStorage('auto');
  assert.deepEqual(seen, ['dark', 'light', 'dark']);
  assert.equal(app.writes(), 0);
  app.controller.fromStorage(null);
  app.system(false);
  assert.equal(app.applied(), 'light');
  assert.equal(app.controller.select('invalid'), false);
});

test('blocked storage preserves an in-memory selection and notifies controls', () => {
  const app = fixture(null, true);
  app.controller.select('dark');
  app.system(false);
  assert.equal(app.applied(), 'dark');
  app.controller.sync();
  assert.equal(app.applied(), 'dark');
});

test('late host defaults can be restored without repeatedly rebuilding controls', () => {
  const app = fixture('dark');
  const seen = [];
  app.controller.subscribe((theme) => seen.push(theme));
  for (let i = 0; i < 100; i++) app.controller.sync();
  assert.deepEqual(seen, ['dark']);
  app.controller.select('light');
  assert.deepEqual(seen, ['dark', 'light']);
});
