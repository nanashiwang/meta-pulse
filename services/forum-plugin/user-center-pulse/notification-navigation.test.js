import test from 'node:test';
import assert from 'node:assert/strict';
import { notificationGroups } from './notification-navigation.js';
import { accountLinks } from './account-menu.js';
const navigation = (...paths) => ({ querySelector: (selector) => paths.some(path => selector === `a[href="${path}"]`) });

test('左侧仅保留内容与帮助栏目，个人与管理入口归头像菜单', () => {
  for (const english of [false, true]) {
    const items = notificationGroups(navigation('/admin', '/review'), english).flatMap(g => g.items);
    assert.ok(items.some(i => i.href === '/latest'));
    assert.ok(items.some(i => i.href === '/knowledge'));
    assert.ok(items.every(i => !/^\/(me|users|admin|settings|review)(\/|$)/.test(i.href)));
  }
});

test('头像管理入口仅正常激活管理员可见，审核权限独立', () => {
  for (const user of [{role_id: 1, status: 'normal', mail_status: 1}, {role_id: 3, is_admin: true}, {role_id: 2, status: 'suspended', mail_status: 1}, {role_id: 2, status: 'normal', mail_status: 2}]) {
    assert.ok(accountLinks(user).flatMap(g => g.items).every(i => !i.href.startsWith('/admin')));
  }
  const admin = accountLinks({role_id: 2, status: 'normal', mail_status: 1}).flatMap(g => g.items);
  assert.equal(admin.filter(i => i.href.startsWith('/admin')).length, 4);
  assert.ok(admin.every(i => i.href !== '/review'));
  const reviewer = accountLinks({role_id: 3}, true, true).flatMap(g => g.items);
  assert.equal(reviewer.find(i => i.href === '/review').label, 'Review');
  assert.ok(reviewer.every(i => !i.href.startsWith('/admin')));
});
