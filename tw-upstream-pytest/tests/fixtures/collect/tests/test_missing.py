# Negative control: a missing test dependency is a collection error.
import missing_test_dep  # noqa: F401


def test_never_runs():
    pass
