# tw#504: two tests run, one skips per test (counted in skip= as before).
import pytest

import demo


def test_add():
    assert demo.add(1, 2) == 3


def test_add_str():
    assert demo.add("a", "b") == "ab"


@pytest.mark.skip(reason="fixture: per-test skip")
def test_skipped():
    pass
