import test from 'node:test';
import assert from 'node:assert/strict';
import { notificationGroups } from './notification-navigation.js';
import { accountLinks } from './account-menu.js';
const navigation = (...paths) => ({ querySelector: (selector) => paths.some(path => selector === `a[href="${path}"]`) });

test('左侧同步社区与成员入口，不恢复已撤下栏目', () => {
  for (const english of [false, true]) {
    const items = notificationGroups(navigation('/admin', '/review'), english).flatMap(g => g.items);
    assert.ok(items.some(i => i.href === '/latest'));
    for (const path of ['/me/drafts','/chat','/events']) assert.ok(items.some(i => i.href === path));
    assert.ok(items.every(i => !['/knowledge','/support','/status'].includes(i.href)));
    assert.ok(items.every(i => !/^\/(users|admin|settings|review)(\/|$)/.test(i.href)));
  }
});

test('头像管理入口仅正常激活管理员可见，审核权限独立', () => {
  for (const user of [{role_id: 1, status: 'normal', mail_status: 1}, {role_id: 3, is_admin: true}, {role_id: 2, status: 'suspended', mail_status: 1}, {role_id: 2, status: 'normal', mail_status: 2}]) {
    assert.ok(accountLinks(user).flatMap(g => g.items).every(i => !i.href.startsWith('/admin')));
  }
  const admin = accountLinks({role_id: 2, status: 'normal', mail_status: 1}).flatMap(g => g.items);
  assert.equal(admin.filter(i => i.href.startsWith('/admin')).length, 7);
  assert.ok(admin.some(i => i.href === '/admin/taxonomy'));
  assert.ok(admin.some(i => i.href === '/admin/chat'));
  assert.ok(accountLinks({role_id:1}).flatMap(g=>g.items).some(i=>i.href==='/chat'));
  assert.ok(admin.every(i => i.href !== '/review'));
  const reviewer = accountLinks({role_id: 3}, true, true).flatMap(g => g.items);
  assert.equal(reviewer.find(i => i.href === '/review').label, 'Review');
  assert.ok(reviewer.every(i => !i.href.startsWith('/admin')));
});
