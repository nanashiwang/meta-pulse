import test from 'node:test';
import assert from 'node:assert/strict';
import {scrollPercent} from './admin-user-scroll.js';
test('table scroll percentage clamps edges and handles tables without overflow',()=>{assert.equal(scrollPercent(0,1200,400),0);assert.equal(scrollPercent(400,1200,400),50);assert.equal(scrollPercent(800,1200,400),100);assert.equal(scrollPercent(99,400,400),0);assert.equal(scrollPercent(-10,1200,400),0);assert.equal(scrollPercent(900,1200,400),100);});
