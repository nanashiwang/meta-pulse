#!/usr/bin/env python3
"""Check the two real Nginx configurations on an isolated Docker network."""
import http.client
import json
import ssl
import subprocess
import tempfile
import time
import uuid
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def run(*args):
    return subprocess.check_output(args, text=True, stderr=subprocess.STDOUT).strip()


def main():
    name = 'metar-relay-test-' + uuid.uuid4().hex[:8]
    origin, relay = name + '-origin', name + '-edge'
    with tempfile.TemporaryDirectory(prefix=name) as temp:
        tmp = Path(temp)
        cert = tmp / 'live/metar.uk'
        cert.mkdir(parents=True)
        run('openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1',
            '-subj', '/CN=metar.uk', '-addext', 'subjectAltName=DNS:metar.uk,DNS:localhost',
            '-keyout', str(cert / 'privkey.pem'), '-out', str(cert / 'fullchain.pem'))
        (tmp / 'ca.pem').write_bytes((cert / 'fullchain.pem').read_bytes())
        context = ssl.create_default_context(cafile=str(cert / 'fullchain.pem'))
        for directory, token, value in [('edge-acme', 'edge-token', 'edge-proof'),
                                        ('origin-acme', 'origin-token', 'origin-proof')]:
            challenge = tmp / directory / '.well-known/acme-challenge'
            challenge.mkdir(parents=True)
            (challenge / token).write_text(value)
        blog = tmp / 'blog/metar'
        blog.mkdir(parents=True)
        (blog / 'index.html').write_text('<h1>METAR relay fixture</h1>')
        source = (ROOT / 'deploy/nginx/meta-pulse.conf').read_text()
        source = source.replace('server forum:80;', 'server 127.0.0.1:8085;')
        source += '''
server {
    listen 8085;
    location / {
        default_type application/json;
        add_header Cache-Control "no-store";
        add_header Set-Cookie "visit=fixture; HttpOnly; Secure";
        return 200 '{"ip":"$http_x_real_ip","xff":"$http_x_forwarded_for","authorization":"$http_authorization","cookie":"$http_cookie","method":"$request_method","length":"$http_content_length","pulse":"$http_x_pulse_signature","user":"$http_new_api_user"}';
    }
}
'''
        (tmp / 'origin.conf').write_text(source)
        edge = (ROOT / 'deploy/nginx/metar-relay.conf').read_text()
        edge = edge.replace('23.94.111.46', '64.83.9.10')
        edge = edge.replace('/etc/ssl/certs/ca-certificates.crt', '/test/ca.pem')
        (tmp / 'edge.conf').write_text(edge)
        try:
            # The dedicated network's edge address exercises the actual production
            # trust allowlist without adding a test-only trusted network.
            run('docker', 'network', 'create', '--subnet', '64.83.9.0/24', name)
            for container, address, config, acme in [
                (origin, '64.83.9.10', 'origin.conf', 'origin-acme'),
                (relay, '64.83.9.190', 'edge.conf', 'edge-acme'),
            ]:
                acme_target = '/var/www/certbot' if container == origin else '/var/www/metar-acme'
                run('docker', 'run', '-d', '--name', container, '--network', name,
                    '--ip', address, '-p', '127.0.0.1::443', '-p', '127.0.0.1::80',
                    '-v', str(tmp / config) + ':/etc/nginx/conf.d/default.conf:ro',
                    '-v', temp + ':/etc/letsencrypt:ro', '-v', temp + ':/test:ro',
                    '-v', str(tmp / 'blog') + ':/var/www/blog:ro',
                    '-v', str(tmp / acme) + ':' + acme_target + ':ro', 'nginx:1.27-alpine')
            ports = {c: {tls: int(run('docker', 'port', c, '443/tcp' if tls else '80/tcp').rsplit(':', 1)[1])
                         for tls in (True, False)} for c in (origin, relay)}

            def request(path, container=relay, tls=True, method='GET', headers=None, body=None):
                conn = (http.client.HTTPSConnection('localhost', ports[container][True], context=context, timeout=5)
                        if tls else http.client.HTTPConnection('localhost', ports[container][False], timeout=5))
                try:
                    conn.request(method, path, body=body, headers={'Host': 'metar.uk', **(headers or {})})
                    res = conn.getresponse()
                    return res.status, dict(res.getheaders()), res.read().decode()
                finally:
                    conn.close()

            def ready(expected=200):
                for _ in range(40):
                    try:
                        if request('/')[0] == expected:
                            return
                    except (ConnectionError, OSError):
                        pass
                    time.sleep(.1)
                raise AssertionError(f'Gateway did not return {expected}')

            ready()
            assert request('/')[2] == '<h1>METAR relay fixture</h1>'
            forged = {'X-Real-IP': '198.51.100.7', 'X-Forwarded-For': '203.0.113.9',
                      'Forwarded': 'for=203.0.113.10', 'Authorization': 'Bearer fixture',
                      'Cookie': 'visit=fixture; unrelated=blocked', 'X-Pulse-Signature': 'forged',
                      'New-Api-User': '12345'}
            for container in (relay, origin):
                status, headers, body = request('/answer/api/__relay_echo', container=container,
                                                method='POST', headers=forged, body='payload')
                assert status == 200, (status, body)
                data = json.loads(body)
                assert data['ip'] == data['xff'], data
                assert data['ip'] not in ('198.51.100.7', '203.0.113.9', '64.83.9.190'), data
                assert data['authorization'] == 'Bearer fixture' and data['cookie'] == 'visit=fixture', data
                assert data['pulse'] == data['user'] == '', data
                assert data['method'] == 'POST' and data['length'] == '7', data
                assert headers['Cache-Control'] == 'no-store', headers
                assert headers['Set-Cookie'] == 'visit=fixture; HttpOnly; Secure; SameSite=Lax', headers
                assert headers['X-Robots-Tag'] == 'noindex, nofollow', headers
            for token, expected in [('edge-token', 'edge-proof'), ('origin-token', 'origin-proof')]:
                status, _, body = request('/.well-known/acme-challenge/' + token, tls=False)
                assert status == 200 and body == expected, (token, status, body)
            assert request('/.well-known/acme-challenge/missing', tls=False)[0] == 404
            for tls in (True, False):
                status, _, body = request('/baidu_verify_codeva-5q1djDn6Ob.html', tls=tls)
                assert status == 200 and body == '551f5710c78ce8db2eccdddbe4b36993'
            for method, expected in [('GET', 301), ('HEAD', 301), ('POST', 308)]:
                status, headers, _ = request('/latest?page=2', tls=False, method=method)
                assert status == expected and headers['Location'] == 'https://metar.uk/latest?page=2'
            assert request('/', headers={'Host': 'www.metar.uk'})[0] == 301
            # Prove that an untrusted origin certificate fails closed.
            run('openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1',
                '-subj', '/CN=unrelated', '-keyout', str(tmp / 'unrelated.key'),
                '-out', str(tmp / 'ca.pem'))
            run('docker', 'restart', relay)
            ports[relay] = {tls: int(run('docker', 'port', relay, '443/tcp' if tls else '80/tcp').rsplit(':', 1)[1])
                            for tls in (True, False)}
            ready(502)
            print('Relay: client IP anti-spoofing, auth/cookies, no-store, TLS verification, both ACME roots and redirects passed')
        finally:
            for container in (relay, origin):
                state = subprocess.run(['docker', 'inspect', '-f', '{{.State.Running}}', container],
                                       text=True, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
                if state.returncode == 0 and state.stdout.strip() == 'false':
                    print(container, run('docker', 'logs', container))
                subprocess.run(['docker', 'rm', '-f', container], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            subprocess.run(['docker', 'network', 'rm', name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


if __name__ == '__main__':
    main()
