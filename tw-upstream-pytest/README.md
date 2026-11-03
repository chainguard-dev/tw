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

The last summary line is `UPSTREAM-TESTS <name>: pass=N fail=N skip=N`,
followed by `xfail=N`, `ignored=N` and `modskip=N` when they are not zero.
`modskip` (contract 4) counts modules skipped at collection
(`pytest.importorskip(...)`, `pytest.skip(..., allow_module_level=True)`),
whose tests are never collected and so are not in `skip`; it is the last
field so that parsers of the others are unaffected.

Contract 4 gate rules, on top of the stereo pipeline's:

- An `--ignore` entry that matches no collected test fails the gate (it
  was a warning). An entry is a node id, matched exactly or followed by
  `::` or `[`, so a directory such as `tests/integration` matches nothing:
  the message says so and points to `--ignore-files`, which stops it being
  collected. An entry in a module that was not collected (skipped at
  collection, or a collection error, which fails on its own) is noted and
  not checked.
- A node id that exists only on some builds (a test defined under a
  `sys.version_info` check, a parametrized case per importable backend
  such as `test_codec[zstd]`, a class gated on the platform) does not
  belong in `--ignore`: a py3-supported-* package runs the tool once per
  python3.X and arch, and the entry fails wherever the id is absent. Use
  `--args "--deselect tests/test_x.py::test_codec[zstd]"` instead (the
  stereo pipeline's `args:` input); pytest ignores a `--deselect` that
  matches nothing. Unlike `--ignore`:
  - it is pytest's prefix match (`nodeid.startswith`), so
    `--deselect tests/test_x.py::test_codec` also drops
    `test_codec_roundtrip`; name the full id, ending in `]` for a
    parametrized case;
  - an unmatched one is never reported, so a renamed test goes unnoticed;
  - the tests it drops get no `IGNORE` line and are not in `ignored=`; the
    tool prints how many tests were deselected other than by `--ignore`
    (markers, `-m`, `-k`, `--deselect`), since pytest's own summary leaves
    them out under xdist.

  Like ignored tests, they are not counted towards `--min-run-percent`.
  An entry in both `--ignore` and `--deselect` is counted once, under
  its `IGNORE` line (the ignore entries are applied first). A dedicated
  input for build-dependent ids may follow if callers need it.
- Each module skipped at collection counts as one skipped test towards
  `--min-run-percent`, so a suite whose modules mostly `importorskip` a
  missing dependency still trips the threshold. pytest's own summary
  counts them as skipped the same way.
- With pytest-asyncio 0.24 or later importable and no
  `asyncio_default_fixture_loop_scope` set by the project (the ini key in
  `pytest.ini`, `.pytest.ini`, `pyproject.toml` `[tool.pytest.ini_options]`,
  `tox.ini` or `setup.cfg`; `-o` in its `addopts`, unless
  `--clear-addopts true`; `--args`; or `PYTEST_ADDOPTS`), the tool ignores
  the plugin's "is unset" deprecation warning with
  `-W 'ignore:The configuration option "asyncio_default_fixture_loop_scope" is unset:pytest.PytestDeprecationWarning'`.
  Under the project's `filterwarnings = error` (pytest 9) that warning is
  an INTERNALERROR and nothing runs; command-line filters apply after the
  ini's, so this one wins. The option itself stays unset, so each async
  fixture's event loop keeps its caching scope (forcing `function` would
  break module- and session-scoped async fixtures with a ScopeMismatch).
  A project that sets the scope, or disables the plugin with
  `-p no:asyncio` (in `addopts`, `--args` or `PYTEST_ADDOPTS`), gets no
  flag.

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
  test skips, an empty suite and a missing test dependency; and, for
  contract 4, stale and directory `--ignore` entries, modules skipped at
  collection, and pytest-asyncio under `filterwarnings = error` (those
  cases need pytest-asyncio; without it, only the absent case runs).
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

- Test-retry plugins are always disabled (`-p no:rerunfailures -p no:flaky`); their options and `@flaky` markers are accepted but inert, so a test that only passes on a retry fails the gate.

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
- the scratch space is also removed on INT and TERM;
- contract 4: unmatched `--ignore` entries fail the gate, modules skipped
  at collection are counted (`modskip=`, and in `--min-run-percent`),
  tests deselected other than by `--ignore` are counted in a note line,
  and pytest-asyncio's "loop scope is unset" deprecation warning is
  ignored with `-W` (all described above).

`tests/equivalence.sh path/to/pytest-core.yaml python3.13` checks the
rest: at 87239accdf8 on Python 3.13, 16 cases give the same output and 13
are the intended differences above (with pytest-asyncio installed; 13 and
12 without it, when its four cases are skipped). Two differences are
hidden in every case by its output normalization, and named there: the
retry plugins' `-p no:` options, and the asyncio `-W` option with the
inline run's pytest-asyncio deprecation warning. `sync-from-stereo
--check` reports `lib/report.py`, `lib/upstream_tests_pytest.py` and
`lib/ini.py` as different for the same reason; do not run it without
`--check` against 87239accdf8, which would revert the fixes.

`CONTRACT` in `tw-upstream-pytest` is the interface version
(`--version`). Bump it when an option is added or changes meaning.
