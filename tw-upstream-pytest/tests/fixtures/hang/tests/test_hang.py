# Negative control: a test that hangs; overall-timeout stops the run.
import time

import demo


def test_add():
    assert demo.add(1, 1) == 2


def test_hang():
    time.sleep(120)
