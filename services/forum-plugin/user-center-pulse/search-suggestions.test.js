import test from 'node:test';
import assert from 'node:assert/strict';
await import('./search-suggestions.js');
const {titles,posts,combine,choose,install}=globalThis.MetarSearchSuggestions;
test('title suggestions are deduplicated plain strings and never generated defaults',()=>{
 assert.deepEqual(titles({data:{list:[{title:' A '},{title:'A'},{title:''},{title:42},{title:'<test>'}]}}),['A','<test>']);assert.deepEqual(titles({data:null}),[]);assert.equal(titles({data:{list:Array.from({length:20},(_,i)=>({title:'t'+i}))}}).length,12);assert.equal(choose(['a','b'],'a',()=>0),'b');assert.equal(choose([],'a'),'');
});
test('typing and focused input pause rotation; public fetch omits credentials; blank submit opens suggested post',async()=>{
 const events={},moves=[],requests=[];const form={};const input={isConnected:true,value:'',placeholder:'Search',dataset:{},form};const doc={documentElement:{},hidden:false,activeElement:null,querySelector:()=>input,addEventListener:(k,fn)=>events[k]=fn,removeEventListener(){}};let tick;
 const host={document:doc,location:{assign:p=>moves.push(p)},MutationObserver:class{observe(){}disconnect(){}},requestAnimationFrame:fn=>fn(),setInterval:fn=>{tick=fn;return 1},clearInterval(){},setTimeout,clearTimeout,addEventListener(){},removeEventListener(){},fetch:async(url,options)=>{requests.push(options);return {ok:true,json:async()=>({data:{list:[{id:'1',title:'First post'},{id:'2',title:'Second post'}]}})}}};
 const api=install(host);await new Promise(setImmediate);assert.equal(requests[0].credentials,'omit');assert.equal(requests[0].headers.Authorization,undefined);
 const original=input.placeholder;doc.activeElement=input;tick();assert.equal(input.placeholder,original);doc.activeElement=null;input.value='my search';tick();assert.equal(input.placeholder,original);input.value='';tick();assert.notEqual(input.placeholder,original);
 let stopped=false;events.submit({target:form,preventDefault(){stopped=true},stopImmediatePropagation(){}});assert.ok(stopped);assert.ok(/^\/questions\/[12]$/.test(moves[0]));input.value='typed';stopped=false;events.submit({target:form,preventDefault(){stopped=true},stopImmediatePropagation(){}});assert.equal(stopped,false);api.dispose();assert.equal(input.placeholder,'Search');
});

test('popular and latest each contribute five, deduplicate by ID and fill to ten',()=>{const items=(start)=>Array.from({length:10},(_,i)=>({id:String(start+i),title:'Post '+(start+i)}));const hot=posts({data:{list:items(1)}}),latest=posts({data:{list:items(4)}});const result=combine(hot,latest);assert.equal(result.length,10);assert.equal(new Set(result.map(p=>p.id)).size,10);assert.deepEqual(result.slice(0,5).map(p=>p.id),['1','2','3','4','5']);assert.ok(result.some(p=>p.id==='8'));assert.equal(combine([],latest).length,10);assert.deepEqual(posts({data:{list:[{id:'//evil.test',title:'Bad'},{id:'1',title:''}]}}),[]);});

for (const available of [true, false]) test('failed popular feed preserves '+(available?'latest recommendations':'ordinary search'),async()=>{
 const events={},moves=[];const form={};const input={isConnected:true,value:'',placeholder:'Search',dataset:{},form};
 const doc={documentElement:{},hidden:false,activeElement:null,querySelector:()=>input,addEventListener:(k,fn)=>events[k]=fn,removeEventListener(){}};
 const host={document:doc,location:{assign:p=>moves.push(p)},MutationObserver:class{observe(){}disconnect(){}},requestAnimationFrame:fn=>fn(),setInterval:()=>1,clearInterval(){},setTimeout,clearTimeout,addEventListener(){},removeEventListener(){},fetch:async url=>{if(url.includes('order=hot')||!available)throw Error('offline');return {ok:true,json:async()=>({data:{list:[{id:'Ab12',title:'Latest post'}]}})};}};
 const api=install(host);await new Promise(setImmediate);let stopped=false;
 const event={target:form,preventDefault(){stopped=true},stopImmediatePropagation(){}};
 events.submit(event);assert.equal(stopped,available);assert.deepEqual(moves,available?['/questions/Ab12']:[]);
 if(available){const wrap={closest:()=>form};events.click({button:0,target:{closest:()=>wrap},preventDefault(){},stopImmediatePropagation(){}});assert.equal(moves[1],'/questions/Ab12');}
 else assert.equal(input.placeholder,'Search');
 api.dispose();assert.equal(input.dataset.metarSuggestionHref,undefined);
});
