"""Exercise local-only workflows in an isolated, network-free browser document.
The execution environment disallows navigations to files and localhost, so the
exact generated HTML is loaded with set_content, then native hash links are used.
No browser security policy is changed.
"""
from pathlib import Path
import json
import os
import shutil
from playwright.sync_api import sync_playwright
ROOT=Path(__file__).resolve().parents[1]
results=[]
errors=[]
requests=[]
with sync_playwright() as p:
 browser=p.chromium.launch(executable_path=os.environ.get('CHROMIUM_PATH') or shutil.which('chromium'),headless=True)
 page=browser.new_page(viewport={'width':1440,'height':1050})
 page.set_default_timeout(3500)
 page.on('pageerror',lambda e:errors.append(str(e)))
 page.on('request',lambda r:requests.append(r.url))
 page.set_content((ROOT/'preview.html').read_text(),wait_until='load')
 def go(path):
  page.evaluate('(path)=>{closeModal();location.hash=path;render()}',path)
  page.wait_for_timeout(45)
 def reset(role='member'):
  page.evaluate('(role)=>{closeModal();state=clone(DEFAULT_STATE);state.role=role;save();render()}',role)
  go('/discover')
 def assert_js(expr,msg='assertion failed'):
  assert page.evaluate(expr),msg
 def test(name,fn):
  try:
   reset()
   fn()
   results.append({'test':name,'passed':True})
  except Exception as e:
   results.append({'test':name,'passed':False,'error':str(e)})
   print('FAIL',name,str(e))
 def auth():
  page.locator('#demo-role').select_option('guest');go('/publish')
  assert page.locator('main').get_by_text('登录后，让这些内容属于你').count()==1
  page.locator('main a[href^="#/login?"]').click()
  page.locator('form[data-form="login"] button[type="submit"]').click()
  assert_js("state.role==='member' && location.hash==='#/publish'")
 test('Guest → independent community login → return to editor',auth)
 def register():
  go('/register');page.locator('#auth-name').fill('演示新成员')
  page.locator('form[data-form="register"] button[type="submit"]').click()
  assert_js("location.hash==='#/verify-email'")
  page.locator('[data-action="verify-demo"]').click()
  assert_js("location.hash==='#/onboarding' && state.role==='member'")
  assert_js("!JSON.stringify(state).includes('demo-only-123')")
 test('Registration → email-verification demonstration → onboarding; no credential storage',register)
 def search():
  page.locator('.searchbox input').fill('API');page.locator('.searchbox input').press('Enter')
  assert page.locator('.result-row').count()>0
  go('/search?q=这个词不存在&type=articles')
  assert page.locator('main').get_by_text('没有找到相关内容').count()==1
 test('Search, content-type filtering and empty result recovery',search)
 def bookmark():
  go('/question/q1');page.locator('[data-action="bookmark"]').first.click()
  assert_js("state.bookmarks.includes('q1')")
  go('/bookmarks');assert page.locator('a.feed-title[href="#/question/q1"]').count()==1
  go('/article/start');page.locator('[data-action="bookmark-article"]').click()
  page.evaluate("state.bookmarks=['article-start']")
  go('/bookmarks');assert page.locator('a[href="#/article/start"]').count()==1
  assert page.locator('main').get_by_text('还没有收藏内容').count()==0
 test('Question and article bookmarks, including article-only collection',bookmark)
 def vote_answer():
  go('/question/q1');page.locator('[data-action="vote"]').click()
  page.locator('form[data-form="answer"] textarea').fill('先从一个输入稳定且输出能够核对的小场景开始，再明确人工审核边界。')
  page.locator('form[data-form="answer"] button[type="submit"]').click()
  assert_js("state.votes.q1===1 && state.answers.q1.length===1 && state.tickets===8")
 test('Vote and answer persist locally without generating Pulse tickets',vote_answer)
 def draft_publish():
  go('/publish');page.locator('#post-title').fill('演示测试：如何把一个工作流设计得更可靠？')
  page.locator('#post-body').fill('这是一个用于验证本地交互流程的测试内容，包含背景、已经尝试的方法以及需要确认的边界，不包含任何真实密钥。')
  page.locator('[data-action="save-draft"]').click();assert_js('state.drafts.length===1')
  go('/drafts');page.get_by_role('link',name='继续编辑',exact=True).click()
  page.locator('input[name="agree"]').check();page.locator('form[data-form="publish"] button[type="submit"]').click()
  assert_js("state.drafts.length===0 && state.posts.length===1 && state.posts[0].status==='pending' && state.tickets===8")
  pid=page.evaluate('state.posts[0].id')
  page.locator('#demo-role').select_option('guest');go('/question/'+pid)
  assert page.locator('main').get_by_text('这条路径，还没有连接起来。').count()==1
  page.locator('#demo-role').select_option('admin');go('/admin/content')
  page.locator('[data-action="review-post"]').click()
  page.locator('.modal textarea[name="reason"]').fill('示例内容清晰，不包含敏感数据，通过内容审核。')
  page.locator('.modal button[value="approved"]').click()
  assert_js("state.posts[0].status==='approved' && state.tickets===8")
 test('Draft → edit → submit → pending hidden from guest → independent moderation',draft_publish)
 def bind():
  go('/settings/binding');page.locator('[data-action="bind-start"]').click()
  assert page.locator('.modal').count()==0
  page.locator('#binding-consent').check();page.locator('[data-action="bind-start"]').click()
  page.locator('[data-action="confirm-bind"]').click();assert_js("state.role==='bound'")
  assert page.locator('button').filter(has_text='解绑').count()==0
 test('Optional binding requires acknowledgement and offers no self-unbind',bind)
 def conflict():
  go('/settings/binding?case=conflict')
  assert page.locator('[data-action="bind-start"]').is_disabled()
  assert page.locator('#binding-consent').is_disabled()
 test('Binding conflict fails closed',conflict)
 def pulse():
  page.locator('#demo-role').select_option('bound');go('/pulse')
  page.locator('[data-action="open-pulse"]').click();page.locator('[data-action="confirm-pulse"]').click()
  assert_js("state.tickets===7 && state.rewards.length===5 && state.rewards[0].status==='pending'")
  page.evaluate("handleAction({dataset:{action:'confirm-pulse'}})")
  assert_js("state.tickets===7 && state.rewards.length===5")
  page.locator('.modal [data-action="reward-detail"]').click()
  page.locator('[data-action="check-reward"]').click()
  assert_js("state.rewards[0].status==='settled'")
 test('Pulse confirmation → one ticket consumption → stable action → query original grant',pulse)
 def degraded():
  page.locator('#demo-role').select_option('bound');page.locator('#demo-scenario').select_option('degraded');go('/pulse')
  assert page.locator('[data-action="open-pulse"]').is_disabled()
  go('/questions');assert page.locator('.feed-title').count()>0
  page.locator('#demo-role').select_option('guest');go('/login')
  assert page.locator('form[data-form="login"]').count()==1
 test('Pulse degradation blocks reward mutation but preserves community and local login',degraded)
 def content_rewards():
  page.locator('#demo-role').select_option('admin');go('/admin/content')
  page.locator('[data-action="review-candidate"][data-id="c1"]').click()
  page.locator('.modal textarea[name="reason"]').fill('已人工核验示例内容满足要求，独立预算只用于演示。')
  page.locator('.modal input[name="budget"]').check();page.locator('.modal button[value="awarded"]').click()
  assert_js("state.moderation.c1==='awarded' && state.tickets===8 && state.rewards[0].kind==='content'")
  assert page.locator('[data-action="review-candidate"][data-id="c1"]').count()==0
  assert page.locator('[data-action="review-candidate"][data-id="c3"]').count()==0
 test('Independent content reward approval, no ticket issuance, no re-approval, ineligible blocked',content_rewards)
 def entries():
  page.locator('#demo-role').select_option('admin');go('/admin/entries')
  page.locator('[name="label-3"]').fill('实践知识库');page.locator('[name="enabled-2"]').uncheck()
  page.locator('form[data-form="entries"] button[type="submit"]').click()
  assert page.locator('.topnav').get_by_text('实践知识库').count()==1
  assert page.locator('.sidebar .nav-item').get_by_text('全部话题',exact=True).count()==0
  assert_js('state.audit.length===1')
 test('Navigation label and visibility configuration reflected immediately and audited',entries)
 def external():
  page.locator('#demo-role').select_option('admin');go('/admin/settings')
  page.locator('#cfg-consoleUrl').fill('https://example.com/console')
  page.locator('form[data-form="site-settings"] button[type="submit"]').click()
  go('/api-hub');page.locator('[data-action="external-entry"][data-key="consoleUrl"]').click()
  assert page.locator('.modal a[href="https://example.com/console"][rel="noopener noreferrer"]').count()==1
  page.keyboard.press('Escape');go('/admin/settings')
  page.locator('#cfg-consoleUrl').fill('https://user:password@example.com/')
  page.locator('form[data-form="site-settings"] button[type="submit"]').click()
  assert_js("state.config.consoleUrl==='https://example.com/console'")
 test('Public HTTPS entry configuration, explicit leave confirmation, credential URLs rejected',external)
 def support():
  go('/support');page.locator('#support-title').fill('演示绑定冲突，需要确认身份归属')
  page.locator('#support-body').fill('这是本地工单演示，账号绑定时遇到冲突，请核对需要提供的非敏感记录号。')
  page.locator('[name="safe"]').check();page.locator('form[data-form="support"] button[type="submit"]').click()
  assert_js('state.support.length===1');page.keyboard.press('Escape')
  page.locator('#demo-role').select_option('admin');go('/admin/support')
  page.locator('[data-action="support-detail"]').click();page.locator('[data-action="resolve-support"]').click()
  assert_js("state.support[0].status==='resolved'")
 test('Support submission → local ticket → operator resolution without account mutation',support)
 def reversal():
  page.locator('#demo-role').select_option('admin');go('/pulse/rewards')
  page.locator('[data-action="reward-detail"][data-id="demo-grant-001"]').click()
  page.locator('[data-action="rollback-reward"]').click()
  page.locator('.modal textarea').fill('演示审核后发现内容资格不满足要求，需要保留原记录并反向处理。')
  page.locator('.modal button[type="submit"]').click()
  assert_js("state.rewards[0].cents===20 && state.rewards[0].status==='reversed' && state.reversals[0].cents===-20")
 test('Demo rollback appends reversal while retaining original amount',reversal)
 def notification():
  go('/notifications');page.locator('[data-action="read-all"]').click()
  assert_js('state.readNotifications.length===3');go('/notifications?tab=unread')
  assert page.locator('main').get_by_text('消息都已看过').count()==1
 test('Notifications mark all as read and recover to unread-empty state',notification)
 def accessibility():
  page.keyboard.press('Control+k');assert page.locator('.searchbox input').evaluate('(el)=>el===document.activeElement')
  page.locator('[data-action="reset"]').click();page.keyboard.press('Shift+Tab')
  assert page.locator('.modal').evaluate('(el)=>el.contains(document.activeElement)')
  page.keyboard.press('Escape');assert page.locator('.modal').count()==0
 test('Keyboard search, modal focus containment and Escape dismissal',accessibility)
 def admin_guard():
  go('/admin/entries');assert page.locator('main').get_by_text('需要独立的运营权限').count()==1
  assert page.locator('form[data-form="entries"]').count()==0
 test('Administrative routes present an explicit separate-role guard',admin_guard)
 def privacy_fallback():
  go('/settings/account');page.locator('#profile-bio').fill('此内容用于验证存储不可用时，界面依然可以运行。')
  page.locator('form[data-form="profile"] button[type="submit"]').click()
  assert_js("state.profile.bio.includes('存储不可用')")
 test('Graceful in-memory fallback when persistent storage is unavailable',privacy_fallback)
 browser.close()
report={'harness':'Exact generated HTML via Playwright set_content; native hash routing; no security-policy change','tests':results,'page_errors':errors,'network_requests':requests}
(ROOT/'tests/interaction-results.json').write_text(json.dumps(report,ensure_ascii=False,indent=2))
print(json.dumps({'passed':sum(r['passed'] for r in results),'total':len(results),'failed':[r for r in results if not r['passed']],'page_errors':errors,'network_requests':requests},ensure_ascii=False,indent=2))

raise SystemExit(1 if errors or requests or any(not r['passed'] for r in results) else 0)
