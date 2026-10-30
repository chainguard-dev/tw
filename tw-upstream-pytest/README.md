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

Limits worth knowing:

- `--runner unittest` runs `python -m unittest discover`, which cannot
  deselect, filter collection, select markers, run workers or time out a
  test: `--ignore`, `--ignore-files`, `--markers`, `--jobs` and `--timeout`
  are rejected unless left at their defaults.
- A non-zero `--timeout` (the default is 600) needs pytest-timeout for the
  python under test (`py3-supported-pytest-timeout` or
  `py3.X-pytest-timeout`); `--timeout 0` does not.
- On Python 3.10, child interpreters the tests start (`python -c 'import
  pkg'` from the source tree) can import the source copy instead of the
  build: `PYTHONSAFEPATH` needs 3.11 or later, and the import pin and the
  origin check cover only the pytest process and its xdist workers. 3.10
  reaches end of life this month, so this is documented, not worked around.

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
  `runs:` block and this tool on the fixtures and compares their output.
  Most cases must match; the intended differences (below) are listed as
  such, with their diffs.

pytest is not a dependency. The caller installs pytest, and any plugins,
for the python it tests.

## Relation to stereo

The tool is a port of stereo's inline `pytest-core.yaml` at
chainguard-dev/stereo 87239accdf8 (the comment at the top of
`tw-upstream-pytest` has the details). Fixes from review land here first,
and stereo picks them up when `pytest-core` switches to calling this tool;
the inline copy is not patched in the meantime. So the tool intentionally
differs from 87239accdf8:

- `--ignore` entries are deselected by the plugin with the boundary rule
  the report counts by (the exact node id, or the id followed by `::` or
  `[`), not by pytest's prefix-matching `--deselect`;
- `--args` is parsed once (no second expansion), and the echoed command
  line shows its words shell-quoted;
- `--runner unittest` rejects the options it cannot honour;
- a non-zero `--timeout` needs pytest-timeout (no `faulthandler_timeout`
  fallback, which only dumped stacks);
- `--extensions` is validated;
- the plugin prefers `src/<pkg>`, and skips `docs`, `examples` and
  `benchmarks` trees, when it looks for the package's source-tree tests;
- there is no `--network` option (the pipeline has no such input);
- the scratch space is also removed on INT and TERM.

`tests/equivalence.sh path/to/pytest-core.yaml python3.13` checks the
rest: at 87239accdf8 on Python 3.13, 11 cases give the same output and 7
are the intended differences above. `sync-from-stereo --check` reports
`lib/report.py` and `lib/upstream_tests_pytest.py` as different for the
same reason; do not run it without `--check` against 87239accdf8, which
would revert the fixes.

`CONTRACT` in `tw-upstream-pytest` is the interface version
(`--version`). Bump it when an option is added or changes meaning.
