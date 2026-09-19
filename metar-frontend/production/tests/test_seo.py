import sys
import re
import tempfile
import unittest
from pathlib import Path
from xml.etree import ElementTree as ET

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
from build import build


class SearchAssetsTest(unittest.TestCase):
    def test_public_html_and_sitemaps_without_private_content(self):
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp) / 'dist'
            build(output, ROOT / 'config.production.json')
            homepage = (output / 'index.html').read_text()
            self.assertIn('<h1>METAR 元衡社区</h1>', homepage)
            self.assertIn('href="/questions"', homepage)
            self.assertIn('href="/blog/guides/"', homepage)
            self.assertEqual(1, homepage.count('rel="canonical"'))
            self.assertIn('href="https://metar.uk/"', homepage)
            # Initial HTML must not declare page 1 canonical for later pages.
            self.assertNotIn('rel="canonical"', (output / 'seo/latest.html').read_text())
            self.assertNotIn('rel="canonical"', (output / 'shell.html').read_text())
            entries = ET.parse(output / 'sitemap-site.xml').findall('.//{*}loc')
            urls = [entry.text for entry in entries]
            self.assertIn('https://metar.uk/', urls)
            self.assertIn('https://metar.uk/questions', urls)
            self.assertEqual(len(urls), len(set(urls)))
            for url in urls:
                self.assertTrue(url.startswith('https://metar.uk/'))
                self.assertNotRegex(url, r'/(?:me|admin|pulse|settings|search|login)(?:/|$)')
            robots = (output / 'robots.txt').read_text()
            for sitemap in ['/sitemap.xml', '/sitemap-site.xml', '/blog/sitemap.xml']:
                self.assertIn('Sitemap: https://metar.uk' + sitemap, robots)
            self.assertNotIn('Disallow: /metar-assets', robots)
            self.assertNotIn('Disallow: /questions', robots)

    def test_render_resources_are_crawlable_but_private_apis_are_not(self):
        rules = []
        for line in (ROOT / 'src/robots.txt').read_text().splitlines():
            directive, _, value = line.partition(':')
            if directive in ('Allow', 'Disallow'):
                rules.append((directive, value.strip()))

        def allowed(path):
            matches = []
            for directive, value in rules:
                end = value.endswith('$')
                pattern = re.escape(value[:-1] if end else value).replace(r'\*', '.*')
                if re.match('^' + pattern + ('$' if end else ''), path):
                    matches.append((len(value), directive == 'Allow'))
            return max(matches, default=(0, True))[1]

        for endpoint in ('question/page', 'question/info', 'answer/page', 'tags/page'):
            path = '/answer/api/v1/' + endpoint
            self.assertTrue(allowed(path), path)
            self.assertTrue(allowed(path + '?page=1&page_size=20&order=active'), path)
            self.assertFalse(allowed(path + '/private'), path)
        for path in ('/answer/api/v1/user/info', '/answer/api/v1/notification/page',
                     '/answer/api/v1/question', '/answer/admin', '/metar/api/pulse/summary',
                     '/metar/api/admin/pulse/settings'):
            self.assertFalse(allowed(path), path)
