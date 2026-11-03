# Node ids that exist only on some builds: test_future only on a python
# that is not out yet, and test_codec has one case per importable backend
# (test_codec[no_such_backend] is never generated here). An ignore entry
# naming one fails the gate where it is absent (contract 4); a --deselect
# in --args is pytest's own prefix match, which is silent when nothing
# matches. test_codec_roundtrip shows that prefix: --deselect test_codec
# drops it too.
import importlib.util
import sys

import pytest

import demo

BACKENDS = [b for b in ("json", "no_such_backend") if importlib.util.find_spec(b)]


@pytest.mark.parametrize("backend", BACKENDS)
def test_codec(backend):
    assert demo.add(backend, "") == "json"


def test_codec_roundtrip():
    assert demo.add(1, 1) == 2


if sys.version_info >= (3, 99):
    def test_future():
        assert demo.add(2, 2) == 4


def test_slow():
    assert False, "deselected by the tests"


def test_other():
    assert demo.add(2, 2) == 4
