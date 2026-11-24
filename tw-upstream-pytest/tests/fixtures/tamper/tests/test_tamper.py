# Negative control (contract 5): a test that rewrites an installed file
# in place, keeping its size and mtime; only a content hash sees it.
import os

import demo


def test_add_and_tamper():
    p = demo.__file__
    st = os.stat(p)
    with open(p, "rb") as fh:
        data = fh.read()
    with open(p, "wb") as fh:
        fh.write(data.replace(b"a + b", b"b + a"))
    os.utime(p, ns=(st.st_atime_ns, st.st_mtime_ns))
    assert demo.add(1, 2) == 3
