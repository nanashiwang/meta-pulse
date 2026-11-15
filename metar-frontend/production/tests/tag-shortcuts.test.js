const test=require('node:test'),assert=require('node:assert/strict');
const {clean,items,initial,href,preferences}=require('../src/tag-shortcuts.js');
test('shortcuts are bounded, unique and use only real catalog defaults',()=>{
 assert.equal(clean(null),null);assert.deepEqual(clean(['gossip','gossip',null,'']),['gossip']);assert.equal(clean(Array.from({length:20},(_,i)=>'t'+i)).length,12);
 const list=items({categories:[{slug_name:'gossip',display_name:'Daily'}],tags:[{slug_name:'gossip'},{slug_name:'custom'}]});assert.equal(list.length,2);assert.deepEqual(initial(list),['gossip']);assert.deepEqual(initial([{slug_name:'custom'}]),['custom']);
});
test('tag filters preserve allowed ordering but reset pagination and encode names',()=>{
 assert.equal(href('a/b','newest'),'/topic/a%2Fb?order=newest');assert.equal(href('','hot'),'/latest?order=hot');assert.equal(href('news','unknown'),'/topic/news?order=active');
});
test('preferences are scoped by account and empty choices persist without restoring defaults',()=>{
 const values=new Map();const storage={getItem:k=>values.get(k),setItem:(k,v)=>values.set(k,v)};const a=preferences(storage,'1'),b=preferences(storage,'2');a.write(['gossip']);assert.deepEqual(a.read(),['gossip']);assert.equal(b.read(),null);a.write([]);assert.deepEqual(a.read(),[]);
 const blocked=preferences({getItem(){throw Error('blocked')},setItem(){throw Error('blocked')}},'1');assert.equal(blocked.read(),null);assert.throws(()=>blocked.write(['gossip']));
});
