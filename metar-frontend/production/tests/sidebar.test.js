const test=require('node:test');
const assert=require('node:assert/strict');
const {KEY,create}=require('../src/sidebar.js');
function setup(value,blocked=false){let listener,latest;const saved=new Map([[KEY,value]]),desktop={matches:true,addEventListener(_event,fn){listener=fn}};
 const storage={getItem:k=>{if(blocked)throw Error('blocked');return saved.get(k)},setItem:(k,v)=>{if(blocked)throw Error('blocked');saved.set(k,v)}};
 const controller=create({storage,desktop,apply:s=>latest=s});return {controller,saved,get state(){return latest},resize(matches){desktop.matches=matches;listener()}};
}
test('desktop preference survives refresh and navigation without changing mobile behavior',()=>{
 const h=setup('1');assert.equal(h.state.expanded,false);h.controller.toggle();assert.equal(h.saved.get(KEY),'0');assert.equal(h.state.expanded,true);
 h.controller.toggle();h.controller.closeMobile();assert.equal(h.state.expanded,false);
 h.resize(false);h.controller.toggle();assert.equal(h.state.expanded,true);assert.equal(h.saved.get(KEY),'1');h.controller.closeMobile();assert.equal(h.state.expanded,false);
 h.resize(true);assert.equal(h.state.expanded,false);h.controller.fromStorage('0');assert.equal(h.state.expanded,true);
});
test('viewport transitions close the mobile drawer and disabled storage retains usable controls',()=>{
 const h=setup(null,true);h.controller.toggle();assert.equal(h.state.collapsed,true);h.resize(false);h.controller.toggle();h.resize(true);h.resize(false);assert.equal(h.state.expanded,false);
 h.controller.toggle();assert.equal(h.state.expanded,true);
});
