# Negative control (py3-six.shadow2): the source tree's copy of the
# package is loaded and registered, standing in for the installed one.
import importlib.util
import os
import sys


def test_load_source_copy():
    src = os.path.join(os.environ["UT_SRC"], "demo", "__init__.py")
    spec = importlib.util.spec_from_file_location("demo", src)
    m = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(m)
    sys.modules["demo"] = m
    assert m.add(2, 2) == 4
