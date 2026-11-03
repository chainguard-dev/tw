import pytest

pytest.importorskip("nonexistent_mod_20")


def test_needs_mod():
    pass
