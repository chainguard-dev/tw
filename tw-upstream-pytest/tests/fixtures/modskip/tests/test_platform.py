# tw#504: skipped at collection with allow_module_level=True.
import pytest

pytest.skip("fixture: not on this platform", allow_module_level=True)


def test_platform_only():
    assert False
