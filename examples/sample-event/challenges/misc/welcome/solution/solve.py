#!/usr/bin/env python3
"""Reference solver for welcome: fetch /flag.txt from the running service.

Target resolution, first match wins:
  1. SOLVE_URL - explicit base URL (e.g. http://127.0.0.1:14000 when the
     challenge is deployed locally and the event domain doesn't resolve).
  2. URL       - stamped by `mctf solve` from the manifest's deploy block.

Retries for a short while so it can run right after `mctf deploy`, before
the container has finished starting.
"""
import os
import sys
import time
import urllib.error
import urllib.request

base = os.environ.get("SOLVE_URL") or os.environ.get("URL")
if not base:
    sys.exit("no target: set SOLVE_URL or run via `mctf solve`")

url = base.rstrip("/") + "/flag.txt"
deadline = time.monotonic() + 30
last_err = None
while time.monotonic() < deadline:
    try:
        with urllib.request.urlopen(url, timeout=5) as resp:
            print(resp.read().decode().strip())
            sys.exit(0)
    except (urllib.error.URLError, OSError) as e:
        last_err = e
        time.sleep(1)

sys.exit(f"could not fetch {url}: {last_err}")
