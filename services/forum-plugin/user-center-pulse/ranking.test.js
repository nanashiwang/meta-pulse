import { test } from 'node:test';
import assert from 'node:assert/strict';
const ranking = await import('./ranking.js').catch(() => ({}));
const users = Array.from({ length: 20 }, (_, i) => ({ username: `u${i}`, rank: 20 - i }));

test('filters staff without filling beyond the upstream twenty and preserves tie order', () => {
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
