import demo


def test_add():
    assert demo.add(1, 2) == 3


def test_add_str():
    assert demo.add("a", "b") == "ab"
