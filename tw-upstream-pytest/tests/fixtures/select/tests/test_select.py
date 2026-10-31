import demo


def test_alpha():
    assert demo.add(1, 1) == 2


def test_beta():
    assert demo.add(2, 2) == 4


def test_gamma_fails():
    assert False, "deselected by -k in the fixture"
