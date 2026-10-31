# --ignore boundaries: an entry deselects its exact node id, the ids it
# starts followed by "[" (parametrized cases) or "::" (a file's or class's
# tests), and nothing else. test_x_regression and test_other must run.
import pytest

import demo


def test_x():
    assert False, "named in --ignore by the tests"


def test_x_regression():
    assert demo.add(1, 1) == 2


@pytest.mark.parametrize("n", ["a", "b"])
def test_p(n):
    assert demo.add(n, n) == n * 2


def test_other():
    assert demo.add(2, 2) == 4
