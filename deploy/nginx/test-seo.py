#!/usr/bin/env python3
"""Exercise the real gateway against built public assets in an isolated container."""
import http.client
import ssl
import subprocess
import tempfile
import time
import uuid
from pathlib import Path
from xml.etree import ElementTree as ET

ROOT = Path(__file__).resolve().parents[2]
DIST = ROOT / 'sites/blog/docs/.vitepress/dist'


def run(*args):
    return subprocess.check_output(args, text=True, stderr=subprocess.STDOUT).strip()


def main():
    if not (DIST / 'metar/sitemap-site.xml').exists():
        raise SystemExit('Run make build-blog build-community first')
    name = 'metar-seo-test-' + uuid.uuid4().hex[:8]
    with tempfile.TemporaryDirectory(prefix='metar-seo-') as temp:
        cert = Path(temp) / 'live/metar.uk'
        cert.mkdir(parents=True)
        run('openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1',
            '-subj', '/CN=localhost', '-addext', 'subjectAltName=DNS:localhost',
            '-keyout', str(cert / 'privkey.pem'), '-out', str(cert / 'fullchain.pem'))
        context = ssl.create_default_context(cafile=str(cert / 'fullchain.pem'))
        try:
            run('docker', 'run', '-d', '--rm', '--name', name, '--add-host', 'forum:127.0.0.1',
                '-p', '127.0.0.1::443',
                '-v', str(ROOT / 'deploy/nginx/meta-pulse.conf') + ':/etc/nginx/conf.d/default.conf:ro',
                '-v', temp + ':/etc/letsencrypt:ro', '-v', str(DIST) + ':/var/www/blog:ro',
                'nginx:1.27-alpine')
            port = int(run('docker', 'port', name, '443/tcp').rsplit(':', 1)[1])

            def get(path, user_agent='Mozilla/5.0', host='metar.uk'):
                conn = http.client.HTTPSConnection('localhost', port, context=context, timeout=5)
                try:
                    conn.request('GET', path, headers={'Host': host, 'User-Agent': user_agent})
                    res = conn.getresponse()
                    return res.status, dict(res.getheaders()), res.read().decode()
                finally:
                    conn.close()

            for attempt in range(30):
                try:
                    if get('/')[0] == 200:
                        break
                except (ConnectionError, OSError):
                    pass
                time.sleep(.1)
            else:
                raise AssertionError('Gateway failed to become ready')
            regular = get('/')
            assert '<h1>METAR 元衡社区</h1>' in regular[2]
            assert 'google-site-verification' in regular[2]
            assert 'X-Robots-Tag' not in regular[1]
            for bot in ['Googlebot', 'Baiduspider', 'bingbot']:
                assert get('/', bot)[2] == regular[2], bot
            for path in ['/me', '/me/notifications', '/me/bookmarks', '/pulse?code=test', '/settings/binding/', '/admin/pulse', '/search?q=hello', '/login']:
                status, headers, body = get(path)
                assert status == 200, (path, status)
                assert headers.get('X-Robots-Tag') == 'noindex, nofollow', (path, headers)
                assert 'rel="canonical"' not in body, path
            status, headers, _ = get('/question/123?tracking=x')
            assert status == 200
            assert headers.get('Link') == '<https://metar.uk/questions/123>; rel="canonical"', headers
            assert 'X-Robots-Tag' not in headers
            status, headers, body = get('/sitemap-site.xml')
            assert status == 200 and 'xml' in headers['Content-Type']
            for node in ET.fromstring(body).findall('.//{*}loc'):
                if node.text.endswith(('/questions', '/tags')):
                    continue  # owned by Answer, deliberately unavailable in this fixture
                assert get(node.text.removeprefix('https://metar.uk'))[0] == 200, node.text
            status, headers, body = get('/blog/sitemap.xml')
            assert status == 200 and 'xml' in headers['Content-Type']
            for node in ET.fromstring(body).findall('.//{*}loc'):
                assert node.text.startswith('https://metar.uk/blog/'), node.text
                route = node.text.removeprefix('https://metar.uk')
                status, _, page = get(route)
                assert status == 200, route
                assert 'rel="canonical" href="' + node.text + '"' in page, route
            robots = get('/robots.txt')
            assert robots[0] == 200 and 'text/plain' in robots[1]['Content-Type']
            assert robots[2].count('Sitemap: ') == 3
            for path in ['/blog/missing-page', '/blog/metar/index.html', '/blog/metar/seo/latest.html']:
                assert get(path)[0] == 404, path
            assert get('/', host='www.metar.uk')[:2][0] == 308
            assert get('/', host='www.metar.uk')[1]['Location'] == 'https://metar.uk/'
            print('SEO gateway: bot parity, public HTML, canonical, private noindex, sitemap URLs, 404 and www redirect passed')
        finally:
            subprocess.run(['docker', 'rm', '-f', name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


if __name__ == '__main__':
    main()
