"""Opt-in probe configuration and deployment boundary regressions."""
import json
import os
import pathlib
import subprocess
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parent.parent


class DNSProbeConfigTests(unittest.TestCase):
    def run_helper(self, text, command, extra=None):
        with tempfile.TemporaryDirectory() as folder:
            config = pathlib.Path(folder) / '.env'
            config.write_text(text)
            env = dict(os.environ, META_PULSE_ENV_FILE=str(config), **(extra or {}))
            return subprocess.run(['bash', '-c', 'source deploy/lib.sh; ' + command], cwd=ROOT, env=env, capture_output=True, text=True)

    def test_disabled_and_strict_bind(self):
        self.assertEqual(self.run_helper('', 'validate_dnsprobe_environment').returncode, 0)
        for value in ['', '0.0.0.0', '127.0.0.1', '10.0.0.1', 'hostname', '::1']:
            result = self.run_helper('DNS_PROBE_ENABLED=true\nDNS_PROBE_BIND_IP=' + value, 'validate_dnsprobe_environment')
            self.assertNotEqual(result.returncode, 0, value)
        self.assertEqual(self.run_helper('DNS_PROBE_ENABLED=true\nDNS_PROBE_BIND_IP=104.168.114.150', 'validate_dnsprobe_environment').returncode, 0)
        self.assertNotEqual(self.run_helper('DNS_PROBE_ENABLED=yes', 'validate_dnsprobe_environment').returncode, 0)

    def test_profile_ports_and_credential_isolation(self):
        env = dict(os.environ, DNS_PROBE_BIND_IP='104.168.114.150')
        base = ['docker', 'compose', '-f', str(ROOT / 'docker-compose.yml')]
        disabled = subprocess.check_output(base + ['config', '--services'], env=env, text=True)
        self.assertNotIn('dnsprobe', disabled.splitlines())
        config = json.loads(subprocess.check_output(base + ['--profile', 'dnsprobe', 'config', '--format', 'json'], env=env, text=True))
        probe = config['services']['dnsprobe']
        self.assertEqual({p['protocol'] for p in probe['ports']}, {'udp', 'tcp'})
        self.assertTrue(all(p['host_ip'] == '104.168.114.150' and str(p['published']) == '53' and p['target'] == 5353 for p in probe['ports']))
        self.assertNotIn('environment', probe)
        self.assertNotIn('volumes', probe)
        self.assertEqual(probe['cap_drop'], ['ALL'])

    def test_failure_isolated_from_update(self):
        result = self.run_helper('DNS_PROBE_ENABLED=true\nDNS_PROBE_BIND_IP=104.168.114.150', 'NO_BUILD=0; compose(){ return 1; }; if ! rollout_dnsprobe; then echo isolated; fi')
        self.assertEqual(result.returncode, 0)
        self.assertIn('isolated', result.stdout)
        for file in ['update.sh', 'install.sh']:
            self.assertIn('if ! rollout_dnsprobe;', (ROOT / 'deploy' / file).read_text())

    def test_nginx_endpoint_drops_all_browser_credentials(self):
        config = (ROOT / 'deploy/nginx/meta-pulse.conf').read_text()
        block = config.split('location ~ ^/metar/api/tools/dns/(start|result)$ {', 1)[1].split('\n    }', 1)[0]
        for expected in ['proxy_pass_request_headers off;', 'resolver 127.0.0.11', 'access_log off;', 'client_max_body_size 256;', 'limit_req zone=dns_probe']:
            self.assertIn(expected, block)
        self.assertNotIn('proxy_set_header Cookie', block)
        self.assertNotIn('proxy_set_header Authorization', block)


if __name__ == '__main__':
    unittest.main()
