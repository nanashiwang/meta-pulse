import { test } from 'node:test';
import assert from 'node:assert/strict';
const ranking = await import('./ranking.js').catch(() => ({}));
const users = Array.from({ length: 20 }, (_, i) => ({ username: `u${i}`, rank: 20 - i }));

test('filters staff and preserves upstream tie order', () => {
  assert.equal(typeof ranking.buildRanking, 'function');
  const tied = users.map(user => ({ ...user, rank: 5 }));
  const result = ranking.buildRanking({ users_with_the_most_reputation: tied, staffs: [{ username: 'u0' }] });
  assert.equal(result.users.length, 19);
  assert.deepEqual(result.users.map(user => user.username), tied.slice(1).map(user => user.username));
  assert.equal(result.users[0].position, 1);
});

test('pagination clamps boundaries and supports the approved sizes', () => {
  assert.equal(typeof ranking.paginate, 'function');
  for (const size of [10, 20, 50, 100]) {
    const page = ranking.paginate(users, 999, size);
    assert.equal(page.pages, Math.ceil(20 / size));
    assert.equal(page.page, page.pages);
    assert.equal(page.items.length, Math.min(size, 20));
  }
  assert.equal(ranking.paginate([], -2, 10).page, 1);
  assert.equal(ranking.paginate(users, 1, 10).items[0].username, 'u0');
});

test('own status distinguishes guest, staff, listed and absent users', () => {
  assert.equal(typeof ranking.ownPosition, 'function');
  const data = ranking.buildRanking({ users_with_the_most_reputation: users, staffs: [{ username: 'admin' }] });
  assert.deepEqual(ranking.ownPosition(data, null), { state: 'guest' });
  assert.deepEqual(ranking.ownPosition(data, { username: 'admin' }), { state: 'staff' });
  assert.deepEqual(ranking.ownPosition(data, { username: 'other', role_id: 3 }), { state: 'staff' });
  assert.deepEqual(ranking.ownPosition(data, { username: 'u3' }), { state: 'listed', position: 4 });
  assert.deepEqual(ranking.ownPosition(data, { username: 'absent' }), { state: 'absent' });
});

test('shows up to 100 members after filtering staff rather than truncating first',()=>{
 const members=Array.from({length:125},(_,i)=>({username:'member'+i,rank:1000-i}));
 const result=ranking.buildRanking({users_with_the_most_reputation:[{username:'admin',rank:9999},...members,{username:'mod',role_id:3,rank:1}],staffs:[{username:'admin'}]});
 assert.equal(result.users.length,100);assert.equal(result.users[0].username,'member0');assert.equal(result.users[99].username,'member99');assert.equal(result.users[99].position,100);
 assert.equal(ranking.paginate(result.users,1,100).items.length,100);
 assert.deepEqual(ranking.ownPosition(result,{username:'member99'}),{state:'listed',position:100});
});
test('small and empty rankings never invent users to reach 100',()=>{
 assert.equal(ranking.buildRanking({users_with_the_most_reputation:users}).users.length,20);
 assert.equal(ranking.buildRanking().users.length,0);
});

test('activity boards preserve exact counts, levels and server order',()=>{
 const data=ranking.buildActivityRanking({kind:'level',users:[{username:'a',value:'9007199254740993',level:8},{username:'b',value:'600',level:2}]});
 assert.equal(data.users.length,2);assert.equal(ranking.rankingValue(data.users[0],'level'),'Lv.8 · 9007199254740993 EXP');
 assert.equal(ranking.rankingValue({value:'12'},'posts'),'12');assert.equal(ranking.rankingValue({value:'7'},'answers'),'7');
 assert.equal(ranking.rankingValue({rank:9},'reputation'),'+9');
});
test('invalid kinds, duplicate members and malformed counts fail rather than fabricate a ranking',()=>{
 for(const data of [{kind:'wrong',users:[]},{kind:'posts',users:[{username:'x',value:'-1'}]},{kind:'answers',users:[{username:'x',value:'1'},{username:'x',value:'2'}]},{kind:'level',users:[{username:'x',value:'10',level:99}]}])assert.throws(()=>ranking.buildActivityRanking(data));
 assert.equal(ranking.buildActivityRanking({kind:'posts',users:[{username:'admin',value:'10'},{username:'member',value:'5'}]},[{username:'admin'}]).users[0].username,'member');
});
