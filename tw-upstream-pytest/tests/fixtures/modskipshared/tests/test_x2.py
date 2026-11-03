# Skipped at collection through the shared conftest helper.
from conftest import require_x

require_x()


def test_needs_x():
    assert False


def test_needs_x_too():
    assert False
