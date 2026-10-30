# tw-upstream-pytest

Runs a Python project's upstream pytest (or unittest) suite as a hard gate
against what the build installed, and prints the `UPSTREAM-TESTS` summary
lines. It is the runner behind stereo's `upstream-tests/pytest` pipelines;
the pipelines keep their inputs and documentation and call this tool.

    tw-upstream-pytest --name py3.13-foo --stage build \
        --caller upstream-tests/pytest --destdir /home/build/melange-out/py3.13-foo \
        --python python3.13 --ignore 'tests/test_net.py::test_fetch'

`tw-upstream-pytest --help` lists the options. There is one per pipeline
input, with the same name and default. It exits 0 when the gate passes and
1 when it fails or the options are wrong.

## Layout

- `tw-upstream-pytest`: the POSIX shell runner, installed in `/usr/bin`.
- `lib/`: the pytest plugin (`upstream_tests_pytest.py`), the pytest and
  unittest entry points, and the ini reader, snapshot and report scripts.
  They are installed as plain files in `/usr/libexec/tw/upstream-pytest`,
  not as a Python package, so one copy serves every python3.X. Each run
  copies them into its scratch directory and puts that on `PYTHONPATH`.
  `UT_LIB` overrides the directory.
- `sync-from-stereo`: extracts the Python heredocs of a stereo
  `pytest-core.yaml` into `lib/`.
- `tests/run.sh`: the tests (`make test`), against the projects in
  `tests/fixtures/`. They include the stereo pilot's negative controls: a
  failing test, a write into the installed package, the source tree
  shadowing the build, tests that need the network, a suite where every
  test skips, an empty suite and a missing test dependency.
- `tests/equivalence.sh`: runs a stereo `pytest-core.yaml`'s inline
  `runs:` block and this tool on every fixture and compares their output.

pytest is not a dependency. The caller installs pytest, and any plugins,
for the python it tests.

## Keeping in sync with stereo

Until stereo calls this tool, stereo's inline `pytest-core.yaml` is the
reference. The comment at the top of `tw-upstream-pytest` records the
stereo commit it was ported from. To pick up a newer one:

    ./sync-from-stereo path/to/pytest-core.yaml   # rewrites lib/
    # port the shell changes around the heredocs into tw-upstream-pytest
    tests/equivalence.sh path/to/pytest-core.yaml python3.13
    make test

`CONTRACT` in `tw-upstream-pytest` is the interface version
(`--version`). Bump it when an option is added or changes meaning.
