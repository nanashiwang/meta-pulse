import test from 'node:test';
import assert from 'node:assert/strict';
import {readingProgress} from './reading-progress.js';
test('long body starts at zero and reaches completion at the body end',()=>{
 assert.equal(readingProgress(180,2180,800,64),0);
 assert.equal(readingProgress(64,2064,800,64),0);
 assert.equal(readingProgress(-568,1432,800,64),0.5);
 assert.equal(readingProgress(-1200,800,800,64),1);
 assert.equal(readingProgress(-3000,-1000,800,64),1);
});
test('short visible body is complete; invalid and offscreen body is not',()=>{
 assert.equal(readingProgress(100,300,800,64),1);
 assert.equal(readingProgress(900,1100,800,64),0);
 for(const args of [[0,0,800,64],[NaN,100,800,64],[0,100,0,64],[0,100,60,64]])assert.equal(readingProgress(...args),0);
});
test('image growth and viewport change recompute without assuming document height',()=>{
 assert.equal(readingProgress(-1200,800,800,64),1);
 assert.ok(readingProgress(-1200,1800,800,64)<1);
 assert.equal(readingProgress(20,500,600,60),1);
});
