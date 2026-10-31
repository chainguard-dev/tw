# Negative control (py3-six.bneg): a failing test, a network probe that
# must be refused, an origin probe and a write into the installed package.
import os
import socket

import pytest

import demo


def test_fail():
    assert 1 == 2


def test_net_blocked():
    with pytest.raises(OSError):
        socket.create_connection(("example.com", 80), timeout=5)


def test_imports_installed():
    assert os.environ["UT_DSP"] in demo.__file__, demo.__file__


def test_write_into_package():
    with open(os.path.join(os.path.dirname(demo.__file__), "leak.txt"), "w") as fh:
        fh.write("x")
