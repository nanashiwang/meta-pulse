import test from 'node:test';
import assert from 'node:assert/strict';
import { menuSpace } from './composer-layout.js';

test('composer dropdown fits the viewport below or above its field', () => {
  assert.deepEqual(menuSpace({ top: 100, bottom: 140 }, 64, 800), { up: false, height: 240 });
  assert.deepEqual(menuSpace({ top: 500, bottom: 540 }, 64, 600), { up: true, height: 240 });
  // A small visual viewport represents the space left by the mobile keyboard.
  assert.deepEqual(menuSpace({ top: 180, bottom: 220 }, 100, 300), { up: false, height: 68 });
  assert.deepEqual(menuSpace({ top: 280, bottom: 320 }, 100, 350), { up: true, height: 168 });
  assert.deepEqual(menuSpace({ top: 0, bottom: 500 }, 64, 300), { up: false, height: 0 });
});
