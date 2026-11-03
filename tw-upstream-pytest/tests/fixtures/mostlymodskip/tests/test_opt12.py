import pytest

pytest.importorskip("nonexistent_mod_12")


def test_needs_mod():
    pass
