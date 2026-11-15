import test from 'node:test';
import assert from 'node:assert/strict';
import {headerAccess,headerIcon} from './header-presentation.js';
test('header links follow actual active verified Answer role, not an administrator-looking flag',()=>{
 assert.deepEqual(headerAccess(null),{chat:false,review:false});
 for(const user of [{username:'x',role_id:2,status:'suspended',mail_status:1},{username:'x',role_id:2,status:'normal',mail_status:2}])assert.deepEqual(headerAccess(user),{chat:false,review:false});
 assert.deepEqual(headerAccess({username:'x',role_id:1,status:'normal',mail_status:1,is_admin:true}),{chat:true,review:false});
 assert.deepEqual(headerAccess({username:'x',role_id:2,status:'normal',mail_status:1}),{chat:true,review:true});
});
test('header decorations only emit static known icon paths',()=>{
 assert.equal(headerIcon('<img src=x>'),headerIcon('target'));
 for(const name of ['bell','trophy','language','moon','sun','search'])assert.match(headerIcon(name),/aria-hidden="true"/);
});
