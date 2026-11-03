# tw#504 negative control: one test runs; the 20 other modules are
# skipped at collection (importorskip), so 1 of 21 ran (4.8%), below the
# default min-run-percent of 5.
import demo


def test_runs():
    assert demo.add(1, 1) == 2
