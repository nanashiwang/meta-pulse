import {test} from 'node:test';
import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
import {readFileSync} from 'node:fs';
import {METAR_FAVICON, isDefaultFavicon, installNativeFavicon} from './native-favicon.js';

function fixture(initial=[]) {
  let callback, writes=0;const jobs=[];const links=[];
  function element(attrs={}) {return {attrs:{...attrs},get rel(){return this.attrs.rel;},set rel(v){this.attrs.rel=v;},getAttribute(k){return this.attrs[k]??null;},hasAttribute(k){return k in this.attrs;},setAttribute(k,v){this.attrs[k]=v;writes++;},remove(){links.splice(links.indexOf(this),1);}};}
  const head={querySelectorAll:()=>links.filter(l=>l.rel?.split(' ').includes('icon')),append:l=>links.push(l)};
  for(const attrs of initial)head.append(element(attrs));
  const host={document:{head,createElement:()=>element()},location:{origin:'https://metarhub.com'},queueMicrotask:fn=>jobs.push(fn),MutationObserver:class{constructor(fn){callback=fn;}observe(node,options){assert.equal(node,head);assert.deepEqual(options.attributeFilter,['href','rel','type']);}}};
  return {host,links,element,get writes(){return writes;},flush(){callback();while(jobs.length)jobs.shift()();}};
}

test('revision matches supplied ICO; no external or customized icons are reclassified',()=>{
 const bytes=readFileSync(new URL('../../../metar-frontend/production/src/favicon.ico',import.meta.url));assert.equal(METAR_FAVICON.split('=')[1],createHash('sha256').update(bytes).digest('hex').slice(0,12));
 for(const href of ['/favicon.ico','https://metarhub.com/favicon.ico'])assert.ok(isDefaultFavicon(href,'https://metarhub.com'));
 for(const href of ['',null,'/uploads/custom.ico','https://other.invalid/favicon.ico','/favicon.ico?custom=1','data:image/png;base64,AA'])assert.equal(isDefaultFavicon(href,'https://metarhub.com'),false);
});
test('native admin first load and Helmet navigation use versioned icon without repeated writes',()=>{
 const f=fixture();installNativeFavicon(f.host);assert.equal(f.links.length,1);assert.equal(f.links[0].getAttribute('href'),METAR_FAVICON);
 f.links.splice(0);f.links.push(f.element({rel:'icon',href:'/favicon.ico',type:'image/png','data-rh':'true'}));f.flush();assert.equal(f.links[0].getAttribute('href'),METAR_FAVICON);assert.equal(f.links[0].getAttribute('type'),'image/x-icon');
 const writes=f.writes;f.flush();f.flush();installNativeFavicon(f.host);assert.equal(f.writes,writes);
 f.links[0].setAttribute('href','/favicon.ico');f.flush();assert.equal(f.links[0].getAttribute('href'),METAR_FAVICON);
});
test('late custom branding removes synthetic fallback and preserves uploaded icons and touch icons',()=>{
 const f=fixture();installNativeFavicon(f.host);f.links.push(f.element({rel:'icon',href:'/uploads/custom.png',type:'image/png'}));f.links.push(f.element({rel:'apple-touch-icon',href:'/uploads/touch.png'}));f.flush();assert.equal(f.links.length,2);assert.equal(f.links[0].getAttribute('href'),'/uploads/custom.png');assert.equal(f.links[0].getAttribute('type'),'image/png');
 f.links.splice(0,1);f.links.push(f.element({rel:'icon',href:'/favicon.ico',type:'image/png'}));f.flush();assert.equal(f.links[1].getAttribute('href'),METAR_FAVICON);
});
