# Negative control (mostly-skip): every test skips, so the suite did not run.
import pytest

import demo


@pytest.mark.skip(reason="fixture: needs a service")
def test_a():
    assert demo.add(1, 1) == 2


def test_b():
    pytest.skip("fixture: needs a service")
