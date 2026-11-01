# Network-namespace control: a child interpreter started with -I -S never
# loads the pytest plugin or its guard, so only the empty network namespace
# can stop it. 192.0.2.1 is TEST-NET-1: inside the namespace the connect
# fails at once with ENETUNREACH; outside it, it times out or is refused.
import subprocess
import sys

import demo

CHILD = r"""
import errno, socket
s = socket.socket()
s.settimeout(3)
try:
    s.connect(("192.0.2.1", 80))
    print("CONNECTED")
except OSError as e:
    print(errno.errorcode.get(e.errno, str(e.errno)) if e.errno else type(e).__name__)
"""


def test_child_process_has_no_route():
    assert demo.add(1, 1) == 2
    out = subprocess.run([sys.executable, "-I", "-S", "-c", CHILD],
                         capture_output=True, text=True, timeout=30).stdout.strip()
    assert out == "ENETUNREACH", out
