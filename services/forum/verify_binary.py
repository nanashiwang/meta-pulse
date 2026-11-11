#!/usr/bin/env python3
"""Check the final Answer binary's embedded UI without a database or real config."""
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import time
from urllib.request import urlopen
from urllib.parse import urljoin

from verify_frontend import EntryHTML, check_html


def verify(binary):
    with socket.socket() as listener:
        listener.bind(('127.0.0.1', 0))
        port = listener.getsockname()[1]
    base = f'http://127.0.0.1:{port}'
    with tempfile.TemporaryDirectory(prefix='answer-ui-check-') as directory:
        with tempfile.TemporaryFile() as log:
            process = subprocess.Popen(
                [str(Path(binary).resolve()), 'init', '-C', directory],
                env=dict(os.environ, INSTALL_PORT=str(port)), stdout=log, stderr=log,
            )
            try:
                for _ in range(100):
                    if process.poll() is not None:
                        raise RuntimeError('Answer installer exited before UI verification')
                    try:
                        with urlopen(base + '/healthz', timeout=1) as response:
                            if response.status == 200:
                                break
                    except OSError:
                        time.sleep(0.1)
                else:
                    raise RuntimeError('Answer installer did not become ready')
                with urlopen(base + '/install', timeout=5) as response:
                    html = response.read().decode()
                errors = check_html(html)
                if errors:
                    raise RuntimeError('; '.join(errors))
                entry = EntryHTML()
                entry.feed(html)
                for asset in entry.scripts + entry.styles:
                    target = urljoin(base, asset)
                    if not target.startswith(base + '/'):
                        raise RuntimeError(f'Unexpected external build asset: {asset}')
                    with urlopen(target, timeout=5) as response:
                        content = response.read()
                        if not content or 'text/html' in response.headers.get('Content-Type', ''):
                            raise RuntimeError(f'Missing embedded asset: {asset}')
                print('Final Answer binary: embedded HTML, JavaScript and CSS verified')
            finally:
                process.terminate()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()


if __name__ == '__main__':
    verify(sys.argv[1])
