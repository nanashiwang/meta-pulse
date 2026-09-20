import test from 'node:test';
import assert from 'node:assert/strict';
import { notificationGroups } from './notification-navigation.js';
const navigation = (...paths) => ({ querySelector: (selector) => paths.some(path => selector === `a[href="${path}"]`) });

test('通知中心与社区导航一致，普通用户不新增管理入口', () => {
  const groups = notificationGroups(navigation());
  const items = groups.flatMap(g => g.items);
  assert.equal(items.find(i => i.label === '通知中心').href, '/users/notifications/inbox');
  assert.ok(items.some(i => i.href === '/me/bookmarks'));
  assert.ok(items.every(i => !i.href.startsWith('/admin') && i.href !== '/review'));
});

test('管理与审核入口分别跟随 Answer 已渲染的权限，支持英文', () => {
  const reviewer = notificationGroups(navigation('/review'), true).flatMap(g => g.items);
  assert.equal(reviewer.find(i => i.href === '/review').label, 'Review');
  assert.ok(reviewer.every(i => !i.href.startsWith('/admin')));
  const admin = notificationGroups(navigation('/admin')).flatMap(g => g.items);
  assert.ok(admin.some(i => i.href === '/admin/dashboard'));
  assert.ok(admin.every(i => i.href !== '/review'));
});
