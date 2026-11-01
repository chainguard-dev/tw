# src layout with a docs/demo directory: the plugin must take src/demo as
# the package's source root (docs/demo sorts first), so that demo.tests,
# which the installed package does not ship, comes from src/demo/tests.
import os

import demo


def test_installed():
    assert os.environ["UT_DSP"] in demo.__file__, demo.__file__
    assert demo.add(1, 2) == 3
