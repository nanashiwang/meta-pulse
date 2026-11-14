const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const ts = require('typescript');
const React = require('react');
const {create, act} = require('react-test-renderer');

test('close survives pending/failed acknowledgements and stale polls; queued notifications stay quiet and other users remain visible', async () => {
  let username = 'alice', pathname = '/admin/dashboard', calls = 0, reject;
  const storage = new Map();
  let storageBlocked = false;
  const sessionStorage = {
    getItem(key) { if (storageBlocked) throw Error('blocked'); return storage.get(key) ?? null; },
    setItem(key, value) { if (storageBlocked) throw Error('blocked'); storage.set(key, value); },
  };
  const animations = [];
  const listeners = new Map();
  const source = fs.readFileSync(process.env.BADGE_SOURCE, 'utf8');
  const js = ts.transpileModule(source, {compilerOptions:{jsx:ts.JsxEmit.ReactJSX,module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2020,esModuleInterop:true}}).outputText;
  const module = {exports:{}};
  const mocks = {
    react: React, 'react/jsx-runtime': require('react/jsx-runtime'),
    'react-i18next': {useTranslation:()=>({t:x=>x})},
    'react-router-dom': {useNavigate:()=>()=>{},useLocation:()=>({pathname})},
    classnames:()=>'', '@/stores':{loggedUserInfoStore:()=>({user:{username}})},
    '@/services': {useQueryNotificationStatus:()=>({mutate:async()=>{}}),readNotification:()=>{calls++;return new Promise((_,r)=>{reject=r;});}},
    '@/utils/animateGift':class {constructor(){this.destroyed=false;animations.push(this);}destroy(){this.destroyed=true;}},
    '../Icon':()=>null, './Modal':props=>React.createElement('modal',props),
  };
  vm.runInNewContext(js, {module,exports:module.exports,require:id=>{assert.ok(id in mocks,id);return mocks[id];},
    setTimeout:()=>1,clearTimeout:()=>{},window:{innerWidth:100,innerHeight:100,sessionStorage},
    document:{documentElement:{clientWidth:100,clientHeight:100},visibilityState:'visible',getElementById:()=>({parentNode:{},setAttribute(){}}),addEventListener:(k,f)=>listeners.set(k,f),removeEventListener:k=>listeners.delete(k)}});
  const Badge=module.exports.default;
  const props=id=>({visible:true,badge:{notification_id:id,badge_id:'b',name:'Good question'}});
  let tree;act(()=>{tree=create(React.createElement(Badge,props('1')));});
  const modal=()=>tree.root.findByType('modal');
  assert.equal(modal().props.visible,false,'direct admin load must not show awards');
  assert.equal(animations.length,0,'admin must not start confetti');
  assert.equal(calls,0,'admin visit must not mark unread notifications read');
  for (const route of ['/admin', '/admin/users', '/admin/dashboard']) {
    pathname=route;
    act(()=>tree.update(React.createElement(Badge,props('1'))));
    assert.equal(modal().props.visible,false);
  }
  pathname='/questions/123';
  act(()=>tree.update(React.createElement(Badge,props('1'))));
  assert.equal(modal().props.visible,true);
  let pending;act(()=>{pending=modal().props.onCancel();});
  assert.equal(modal().props.visible,false,'must close before network completes');
  assert.ok(animations.every(a=>a.destroyed));
  for(let i=0;i<100;i++){act(()=>tree.update(React.createElement(Badge,props('1'))));assert.equal(modal().props.visible,false);}
  await act(async()=>{reject(new Error('offline'));await pending;});
  listeners.get('visibilitychange')();
  assert.ok(animations.every(a=>a.destroyed),'tab focus must not restart hidden animation');
  assert.equal(calls,1);
  act(()=>tree.update(React.createElement(Badge,props('2'))));assert.equal(modal().props.visible,false,'closing must stop the queued badge cascade');
  act(()=>tree.unmount());
  act(()=>{tree=create(React.createElement(Badge,props('2')));});
  assert.equal(modal().props.visible,false,'reload/remount must retain dismissal');
  username='bob';act(()=>tree.update(React.createElement(Badge,props('1'))));assert.equal(modal().props.visible,true);
  pathname='/admin/dashboard';
  act(()=>tree.update(React.createElement(Badge,props('1'))));
  assert.equal(modal().props.visible,false,'SPA entry into admin must hide active award');
  assert.ok(animations.every(a=>a.destroyed));
  storageBlocked=true;
  act(()=>tree.unmount());
  act(()=>{tree=create(React.createElement(Badge,props('1')));});
  assert.equal(modal().props.visible,false,'admin stays quiet with blocked storage');
  pathname='/questions/123';
  act(()=>tree.update(React.createElement(Badge,props('1'))));
  assert.equal(modal().props.visible,true,'another account must not inherit dismissal');
  act(()=>{pending=modal().props.onCancel();});
  assert.equal(modal().props.visible,false,'blocked storage must not prevent closing');
  await act(async()=>{reject(new Error('offline'));await pending;});
  act(()=>tree.unmount());assert.ok(animations.every(a=>a.destroyed));
});
