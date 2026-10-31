#!/usr/bin/env python3
"""Compatibility entrypoint: build the vendored zashboard source."""
import argparse
import os
from pathlib import Path
import subprocess

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--proxy', help='Proxy for dependency downloads')
args = parser.parse_args()
env = os.environ.copy()
if args.proxy:
    env.update(HTTP_PROXY=args.proxy, HTTPS_PROXY=args.proxy)
subprocess.run([str(Path(__file__).resolve().with_name('build-ui.sh'))], env=env, check=True)
