const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const source = fs.readFileSync(require.resolve('../src/app.js'), 'utf8');
const start = source.indexOf('  function questionRow(');
assert.ok(start >= 0);
const end = source.indexOf('\n  }', start) + 4;
let english = false;
const render = vm.runInNewContext('(' + source.slice(start, end) + ')', {
  t: text => english && text === '已采纳' ? 'Accepted' : text,
  esc: value => String(value ?? ''),
  I: () => '', avatar: () => '', displayName: () => '',
  publicAuthor: () => '', link: () => '', tagName: () => '',
  external: (href, title) => '<a href="' + href + '">' + title + '</a>',
  questionHref: id => '/questions/' + id,
  badge: (text, color) => '<span class="badge ' + color + '">' + text + '</span>',
  number: value => Number(value || 0),
});
const row = (id, count = 0) => render({ id: '10010000000000293', title: 'Post', accepted_answer_id: id, answer_count: count });

test('unaccepted and invalid IDs never produce an accepted badge, regardless of reply count', () => {
  for (const id of ['0', 0, '', null, undefined, false, true, '00', 'invalid', '-1', -1, 1.5, NaN, Infinity, {}, [], Number.MAX_SAFE_INTEGER + 1]) {
    for (const count of [0, 7]) {
      assert.doesNotMatch(row(id, count), /badge green|已采纳|已解决/, String(id));
    }
  }
});

test('real accepted IDs render the translated badge without losing large string IDs', () => {
  for (const id of ['10020000000000244', '9007199254740993', 42]) {
    assert.match(row(id, 2), /badge green">已采纳/);
    assert.doesNotMatch(row(id, 2), /已解决/);
  }
  english = true;
  assert.match(row('10020000000000244'), /badge green">Accepted/);
  english = false;
});

test('rerendering after acceptance and cancellation follows the latest server value', () => {
  const post = { id: '123', title: 'Question', accepted_answer_id: '0' };
  assert.doesNotMatch(render(post), /badge green/);
  post.accepted_answer_id = '10020000000000244';
  assert.match(render(post), /badge green/);
  post.accepted_answer_id = '0';
  assert.doesNotMatch(render(post), /badge green/);
});
