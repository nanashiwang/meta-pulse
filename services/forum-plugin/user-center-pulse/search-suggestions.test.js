import test from 'node:test';
import assert from 'node:assert/strict';
await import('./search-suggestions.js');
const {titles,choose,install}=globalThis.MetarSearchSuggestions;
test('title suggestions are deduplicated plain strings and never generated defaults',()=>{
 assert.deepEqual(titles({data:{list:[{title:' A '},{title:'A'},{title:''},{title:42},{title:'<test>'}]}}),['A','<test>']);assert.deepEqual(titles({data:null}),[]);assert.equal(titles({data:{list:Array.from({length:20},(_,i)=>({title:'t'+i}))}}).length,12);assert.equal(choose(['a','b'],'a',()=>0),'b');assert.equal(choose([],'a'),'');
});
test('typing and focused input pause rotation; public fetch omits credentials; blank submit uses suggested title',async()=>{
 const events={},moves=[],requests=[];const form={};const input={isConnected:true,value:'',placeholder:'Search',dataset:{},form};const doc={documentElement:{},hidden:false,activeElement:null,querySelector:()=>input,addEventListener:(k,fn)=>events[k]=fn,removeEventListener(){}};let tick;
 const host={document:doc,location:{assign:p=>moves.push(p)},MutationObserver:class{observe(){}disconnect(){}},requestAnimationFrame:fn=>fn(),setInterval:fn=>{tick=fn;return 1},clearInterval(){},setTimeout,clearTimeout,addEventListener(){},removeEventListener(){},fetch:async(url,options)=>{requests.push(options);return {ok:true,json:async()=>({data:{list:[{title:'First post'},{title:'Second post'}]}})}}};
 const api=install(host);await new Promise(setImmediate);assert.equal(requests[0].credentials,'omit');assert.equal(requests[0].headers.Authorization,undefined);
 const original=input.placeholder;doc.activeElement=input;tick();assert.equal(input.placeholder,original);doc.activeElement=null;input.value='my search';tick();assert.equal(input.placeholder,original);input.value='';tick();assert.notEqual(input.placeholder,original);
 let stopped=false;events.submit({target:form,preventDefault(){stopped=true},stopImmediatePropagation(){}});assert.ok(stopped);assert.ok(moves[0].startsWith('/search?q='));input.value='typed';stopped=false;events.submit({target:form,preventDefault(){stopped=true},stopImmediatePropagation(){}});assert.equal(stopped,false);api.dispose();assert.equal(input.placeholder,'Search');
});
