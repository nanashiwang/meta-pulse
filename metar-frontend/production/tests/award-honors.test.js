const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const script=fs.readFileSync(require('node:path').join(__dirname,'../src/award-honors.js'),'utf8');
test('honors show selectable versions, gold medal counts and escaped source records',async()=>{const host={};vm.runInNewContext(script,{window:host,Date});const view=new host.MetarAwardHonors.View({request:async()=>({titles:[{key:'post-week',name:'优秀作者',style:'normal',expires_at:9999999999},{key:'post-month',name:'优秀作者',style:'glow',expires_at:9999999999}],selection:'post-month',badges:[{name:'编辑者',count:2}],records:[{period:{type:'month',key:'2026-11',kind:'post'},reason:'<script>bad</script>',question_id:'100',answer_id:'0'}]})});const html=await view.page(new URLSearchParams());assert.ok(html.includes('post-week'));assert.ok(html.includes('post-month'));assert.ok(html.includes('selected'));assert.ok(html.includes('编辑者 × 2'));assert.ok(html.includes('不展示'));assert.ok(html.includes('&lt;script&gt;'));assert.ok(!html.includes('<script>bad'));});

test('both gold medals and conditions remain visible before the first award',()=>{
 const host={};vm.runInNewContext(script,{window:host,Date});const html=host.MetarAwardHonors.badges([]);
 for(const name of ['编辑者','学者','优秀帖子奖','优秀回应奖','尚未获得'])assert.ok(html.includes(name),name);
 assert.equal((html.match(/class="award-medal-icon"/g)||[]).length,2);assert.ok(!html.includes('is-earned'));
});
test('gold medal loading or failed reads do not claim unearned status',()=>{
 const host={};vm.runInNewContext(script,{window:host,Date});
 assert.match(host.MetarAwardHonors.badges(null),/正在读取获得状态/);
 const failed=host.MetarAwardHonors.badges(null,true);assert.match(failed,/获得状态暂不可用/);assert.doesNotMatch(failed,/尚未获得/);assert.match(failed,/编辑者/);assert.match(failed,/学者/);
 assert.throws(()=>host.MetarAwardHonors.badges([{kind:'post',count:'<img>'}]));
});

function nativeBadges(){
 const host={};vm.runInNewContext(script,{window:host,Date});
 function element(){return {dataset:{},children:[],append(...items){for(const item of items){item.remove();this.children.push(item);item.parentElement=this;}},get lastElementChild(){return this.children.at(-1);},remove(){if(this.parentElement)this.parentElement.children=this.parentElement.children.filter(x=>x!==this);this.parentElement=null;}};}
 let observer,container=null;
 const doc={createElement:element,body:{},defaultView:{MutationObserver:class{constructor(fn){observer=this;this.change=fn;}observe(){}disconnect(){this.disconnected=true;}}},querySelector:()=>container&&({parentElement:container})};
 return {api:host.MetarAwardHonors,doc,element,mount:()=>{container=element();observer.change();return container;},mutate:()=>observer.change(),get observer(){return observer;}};
}

test('native badge group mounts under the native heading with two in-grid cards and survives replacement',async()=>{
 const b=nativeBadges();let resolve;const requests=[];const detach=b.api.mountBadgeGroup(b.doc,{request:url=>{requests.push(url);return new Promise(r=>resolve=r);}},()=>true);
 const native=b.mount();const group=native.lastElementChild;
 assert.equal(group.children[0].textContent,'评选金色勋章');
 assert.match(group.children[1].innerHTML,/badge-card/);assert.match(group.children[1].innerHTML,/正在读取获得状态/);
 assert.equal((group.children[1].innerHTML.match(/col-lg-3/g)||[]).length,2);
 resolve({badges:[{kind:'post',count:2}]});await new Promise(setImmediate);
 assert.match(group.children[1].innerHTML,/已获得 × 2/);assert.match(group.children[1].innerHTML,/尚未获得/);
 const next=b.mount();assert.equal(next.lastElementChild,group);b.mutate();assert.equal(next.children.length,1);
 assert.deepEqual(requests,['/metar/awards/honors']);detach();assert.equal(next.children.length,0);assert.equal(b.observer.disconnected,true);
});

test('badge API failure stays inside the native group and does not claim the medals are unearned',async()=>{
 const b=nativeBadges();b.api.mountBadgeGroup(b.doc,{request:async()=>{throw Error('offline');}},()=>true);
 const native=b.mount();await new Promise(setImmediate);const content=native.lastElementChild.children[1].innerHTML;
 assert.match(content,/获得状态暂不可用/);assert.doesNotMatch(content,/尚未获得/);
});
