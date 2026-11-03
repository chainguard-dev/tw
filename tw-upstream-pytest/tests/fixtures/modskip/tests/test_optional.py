# tw#504: skipped at collection by importorskip (a missing optional
# dependency); none of its tests is collected.
import pytest

nonexistent_mod = pytest.importorskip("nonexistent_mod")


def test_uses_mod():
    assert nonexistent_mod.works()


def test_uses_mod_too():
    assert nonexistent_mod.works()
