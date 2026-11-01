# Retry control: this test fails on its first run and would pass on a
# retry. pytest-rerunfailures (--reruns) and flaky (@flaky, --force-flaky)
# are disabled by the runner, so it must be reported as one failure, and
# the retry options in addopts must be accepted, not "unrecognized".
from pathlib import Path

import demo

try:
    from flaky import flaky
except ImportError:  # flaky not installed: the decorator is a no-op here
    def flaky(*args, **kwargs):
        if args and callable(args[0]):
            return args[0]
        return lambda f: f

COUNT = Path(__file__).with_name(".retry-count")


@flaky(max_runs=3)
def test_fails_on_first_run():
    n = int(COUNT.read_text()) if COUNT.exists() else 0
    COUNT.write_text(str(n + 1))
    assert demo.add(1, 1) == 2
    assert n >= 1, "fails on its first run; a retry would hide this"
