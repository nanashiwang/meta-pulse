const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync(require.resolve('../src/app.js'),'utf8');
const start=source.indexOf('  let footerResizeObserver = null;'),end=source.indexOf('  function renderView(',start);
test('footer reserves its measured height, follows resize and disconnects on navigation',()=>{
 let height=50,node={isConnected:true,getBoundingClientRect:()=>({height})},current=node,callback,disconnected=0;const styles={};
 const context={document:{querySelector:()=>current,documentElement:{classList:{contains:()=>false},style:{setProperty:(k,v)=>styles[k]=v}}},window:{ResizeObserver:class {constructor(fn){callback=fn;}observe(n){assert.equal(n,current);}disconnect(){disconnected++;}}}};
 vm.createContext(context);vm.runInContext(source.slice(start,end)+';globalThis.fit=fitFooter;',context);
 context.fit();assert.equal(styles['--community-footer-height'],'50px');height=91.4;callback();assert.equal(styles['--community-footer-height'],'92px');current=null;context.fit();assert.equal(disconnected,1);assert.equal(styles['--community-footer-height'],'0px');
});
