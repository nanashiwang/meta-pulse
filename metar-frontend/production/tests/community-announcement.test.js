const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync(require.resolve('../src/app.js'), 'utf8');

function load(name, context) {
  const start = source.indexOf('  async function ' + name + '(');
  const end = source.indexOf('\n  }', start) + 4;
  assert.ok(start >= 0);
  return vm.runInNewContext('(' + source.slice(start, end) + ')', context);
}

const pinned = {id:'9', pin:2, title:'<b>维护通知</b>'};
function setup(request, node = {hidden:true, isConnected:true, innerHTML:''}) {
  const context = {
    navigationSequence:1, session:'guest', document:{querySelector:() => node},
    answer:{listQuestions:request}, taxonomy:{scope:() => context.session},
    questionHref:id => '/questions/' + encodeURIComponent(id), I:() => '', t:x => x,
    esc:x => String(x).replaceAll('<','&lt;').replaceAll('>','&gt;'),
    external:(url, text, cls) => '<a href="' + url + '" class="' + cls + '">' + text + '</a>'
  };
  return {node, context, mount:load('mountCommunityAnnouncement', context)};
}

test('requests only the first public announcement and links a pinned result to its native post', async () => {
  const calls = [];
  const {node, mount} = setup(async q => { calls.push({...q}); return {list:[pinned]}; });
  await mount();
  assert.deepEqual(calls, [{page:1, pageSize:1, order:'newest', tag:'announcement'}]);
  assert.equal(node.hidden, false);
  assert.match(node.innerHTML, /href="\/questions\/9"/);
  assert.match(node.innerHTML, /&lt;b&gt;维护通知&lt;\/b&gt;/);
  assert.doesNotMatch(node.innerHTML, /<b>/);
});

test('empty, unpinned, malformed and unavailable announcements leave no visible bar', async () => {
  for (const list of [[], [{...pinned,pin:1}], [{...pinned,title:' '}], [{pin:2,title:'no post'}]]) {
    const {node, mount} = setup(async () => ({list}));
    await mount();
    assert.equal(node.hidden, true);
    assert.equal(node.innerHTML, '');
  }
  const {node, mount} = setup(async () => { throw Error('offline'); });
  await mount();
  assert.equal(node.hidden, true);
});

test('late notices cannot update a detached view or a later route or session', async () => {
  for (const invalidate of [h => h.node.isConnected = false, h => h.context.navigationSequence++, h => h.context.session = 'member']) {
    let resolve;
    const h = setup(() => new Promise(done => { resolve = done; }));
    const job = h.mount();
    invalidate(h);
    resolve({list:[pinned]});
    await job;
    assert.equal(h.node.hidden, true);
    assert.equal(h.node.innerHTML, '');
  }
});

test('renders one awards link before the feed and only mounts notices on the first unfiltered page', async () => {
  let query = new URLSearchParams();
  const questionsPage = load('questionsPage', {
    loadTagDirectory:async () => {}, route:() => ({query}), questionOrders:() => ({active:'active',newest:'newest',unanswered:'unanswered'}),
    answer:{listQuestions:async () => ({list:[],count:0})}, taxonomy:{scope:() => 'guest',askURL:() => '/questions/ask'},
    navigationCatalogScope:'', config:{answerAskPath:'/questions/ask'}, t:x => x, esc:x => x, I:() => '',
    pageTitle:() => '', footer:() => '', external:() => '', empty:() => '', currentUser:null, tagPanelOpen:false,
    followedRailMarkup:() => '', tagPanelMarkup:() => '',
    link:(url,text,cls) => '<a href="' + url + '" class="' + (cls || '') + '">' + text + '</a>'
  });
  const home = await questionsPage();
  const filtered = await questionsPage('develop');
  query = new URLSearchParams('page=2');
  const paginated = await questionsPage();
  assert.ok(home.indexOf('data-tag-shortcuts') < home.indexOf('data-community-announcement'));
  assert.ok(home.indexOf('data-community-announcement hidden') < home.indexOf('class="community-layout"'));
  for (const page of [home,filtered,paginated]) {
    assert.equal((page.match(/href="\/awards"/g) || []).length, 1);
    assert.ok(page.indexOf('class="community-award"') < page.indexOf('class="community-feed"'));
    assert.ok(page.indexOf('class="community-award"') < page.indexOf('data-rail-disclosure'));
  }
  for (const page of [filtered,paginated]) assert.doesNotMatch(page, /data-community-announcement/);
});
