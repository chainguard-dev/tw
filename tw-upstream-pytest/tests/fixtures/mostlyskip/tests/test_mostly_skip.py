# Negative control (mostly-skip): one test runs, 24 skip (4%), below the
# default min-run-percent of 5.
import pytest

import demo


def test_runs():
    assert demo.add(1, 1) == 2


@pytest.mark.parametrize("n", range(24))
def test_needs_service(n):
    pytest.skip("fixture: needs a service")
