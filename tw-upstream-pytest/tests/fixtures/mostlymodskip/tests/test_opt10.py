import pytest

pytest.importorskip("nonexistent_mod_10")


def test_needs_mod():
    pass
