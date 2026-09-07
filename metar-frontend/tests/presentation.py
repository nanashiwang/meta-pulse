"""Verify the v1.1 copy removal, layout and normal/review separation."""
from pathlib import Path
import json, os, shutil
from playwright.sync_api import sync_playwright
ROOT=Path(__file__).resolve().parents[1]
report={'checks':[], 'errors':[], 'network_requests':[]}
forbidden=['START HERE','在这里，找到你的下一步。','每一次真实调用，','值得加入的讨论','分享方法，不分享密钥。','每次调用，都有回响','不必绑定 API，也可以加入讨论']
with sync_playwright() as p:
    browser=p.chromium.launch(executable_path=os.environ.get('CHROMIUM_PATH') or shutil.which('chromium'),headless=True)
    page=browser.new_page()
    page.on('pageerror', lambda e: report['errors'].append(str(e)))
    page.on('request', lambda r: report['network_requests'].append(r.url))
    page.set_content((ROOT/'index.html').read_text(),wait_until='load')
    for width,height in [(1440,1050),(1024,900),(768,1024),(390,844),(375,812),(320,740)]:
        page.set_viewport_size({'width':width,'height':height})
        page.evaluate("location.hash='/discover';render()")
        page.wait_for_timeout(75)
        text=page.locator('body').inner_text()
        assert all(x not in text for x in forbidden),(width,'removed copy present')
        assert page.locator('.demo-bar,.side-pulse,.hero,.welcome-strip').count()==0
        assert '示例' not in text,(width,'sample labels on home')
        assert '原型' not in page.title()
        assert page.evaluate('document.documentElement.scrollWidth <= innerWidth+1')
        rects=page.locator('.feature-grid>.feature-card').evaluate_all('(els)=>els.map(el=>{const r=el.getBoundingClientRect();return {top:r.top,bottom:r.bottom,left:r.left,right:r.right}})')
        assert len(rects)==2
        assert abs(rects[0]['top']-rects[1]['top'])<1
        assert abs(rects[0]['bottom']-rects[1]['bottom'])<1
        feed=page.locator('.discussion-section>.card').bounding_box()
        assert abs(rects[0]['left']-feed['x'])<1
        assert abs(rects[1]['right']-(feed['x']+feed['width']))<1
        if width<580:
            bottom=page.locator('.mobile-bottom').bounding_box()
            assert abs(bottom['y']+bottom['height']-height)<1
        report['checks'].append({'width':width,'height':height,'copy_removed':True,'review_bar_absent':True,'card_edges_aligned':True,'no_horizontal_overflow':True})
    page.set_viewport_size({'width':1440,'height':1050})
    for route in ['/questions','/topic/dev','/question/q1','/article/start','/publish','/pulse','/me']:
        page.evaluate('(r)=>{location.hash=r;render()}',route)
        page.wait_for_timeout(65)
        assert all(x not in page.locator('body').inner_text() for x in forbidden)
        assert page.locator('.content-grid>aside:empty').count()==0
        assert page.evaluate('document.documentElement.scrollWidth <= innerWidth+1')
        if route in ['/questions','/publish']:
            assert page.locator('.content-grid--single').count()==1
        report['checks'].append({'path':route,'no_removed_rails':True,'no_empty_grid_columns':True})
    page.evaluate("location.hash='/discover';render()")
    page.locator('.sidebar a[href="#/pulse"]').click()
    page.wait_for_timeout(70)
    assert page.evaluate("location.hash==='#/pulse'")
    report['checks'].append({'functional_pulse_navigation':True})
    page.close()
    page=browser.new_page(viewport={'width':1440,'height':1050})
    page.on('pageerror', lambda e: report['errors'].append(str(e)))
    page.on('request', lambda r: report['network_requests'].append(r.url))
    page.set_content((ROOT/'preview.html').read_text(),wait_until='load')
    assert page.locator('.demo-bar').count()==1
    page.locator('#demo-role').select_option('admin')
    page.evaluate("location.hash='/admin/settings';render()")
    page.wait_for_timeout(65)
    page.locator('#hero-title').fill('社区动态')
    page.locator('form[data-form="site-settings"] button[type="submit"]').click()
    page.evaluate("location.hash='/discover';render()")
    page.wait_for_timeout(65)
    assert page.locator('main h1').inner_text()=='社区动态'
    report['checks'].append({'separate_review_entry':True,'home_title_configuration':True})
    browser.close()
assert not report['errors'] and not report['network_requests']
(ROOT/'tests/presentation-results.json').write_text(json.dumps(report,ensure_ascii=False,indent=2))
print(json.dumps({'passed':len(report['checks']),'errors':report['errors'],'network_requests':report['network_requests']},ensure_ascii=False,indent=2))
