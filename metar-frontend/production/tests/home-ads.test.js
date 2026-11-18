const test=require('node:test'),assert=require('node:assert/strict'),vm=require('node:vm'),fs=require('node:fs');
const code=fs.readFileSync(require.resolve('../src/home-ads.js'),'utf8');
function setup(count){const storage=new Map(),clock={};const node=()=>({append(){},setAttribute(){},replaceChildren(...children){this.children=children;},isConnected:true,hidden:true});const slot=node();const ctx={window:{sessionStorage:{getItem:k=>storage.get(k),setItem:(k,v)=>storage.set(k,v)}},document:{createElement:node},setInterval(fn,ms){Object.assign(clock,{fn,ms});return 1},clearInterval(){delete clock.fn}};vm.runInNewContext(code,ctx);let calls=0;const mount=()=>ctx.window.MetarAds.mountSlot(slot,{request:async()=>{calls++;return Array.from({length:count},(_,i)=>({id:i+1,link:'https://example.com/'+i}));}});return {mount,slot,clock,calls:()=>calls};}
test('no ads occupy no space; one stays static',async()=>{for(const n of [0,1]){const h=setup(n);await h.mount();assert.equal(h.slot.hidden,!n);assert.equal(h.clock.fn,undefined);}});
test('rotation updates destination every 15 seconds, wraps, and close dismisses all',async()=>{const h=setup(2);await h.mount();assert.equal(h.clock.ms,15000);const [link,,close]=h.slot.children;assert.equal(link.href,'https://example.com/0');h.clock.fn();assert.equal(link.href,'https://example.com/1');h.clock.fn();assert.equal(link.href,'https://example.com/0');close.onclick();assert.equal(h.clock.fn,undefined);assert.equal(h.slot.hidden,true);await h.mount();assert.equal(h.calls(),1);});

test('publish and remove include the same-origin marker; FormData retains browser boundary',async()=>{
 const list={replaceChildren(){},append(row){this.row=row}},button={},status={};
 const form={querySelector:()=>button,elements:{image:{files:[{size:10}]},link:{value:'https://example.org/'}},reset(){}};
 const document={createElement(tag){return tag==='canvas'?{getContext:()=>({fillRect(){},drawImage(){}}),toBlob:cb=>cb(new Blob(['image']))}:{append(...children){this.children=children}}}};
 const window={confirm:()=>true};vm.runInNewContext(code,{window,document,URL,FormData,createImageBitmap:async()=>({width:200,height:100,close(){}})});
 const requests=[];window.MetarAds.mountManager({querySelector:s=>s==='form'?form:s==='ul'?list:status},{request:async()=>[{id:'1',link:'https://example.org/'}]},{request:async(p,o)=>requests.push([p,o])});
 await form.onsubmit({preventDefault(){}});await list.row.children.at(-1).onclick();
 assert.deepEqual(requests.map(([p,o])=>[p,o.method,o.headers['X-Metar-Request']]),[['/metar/ads','POST','1'],['/metar/ads/1','DELETE','1']]);
 assert.equal(requests[0][1].headers['Content-Type'],undefined);assert.equal(requests[0][1].body.get('link'),'https://example.org/');
});
