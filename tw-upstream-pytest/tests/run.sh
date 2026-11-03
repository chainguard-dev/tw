#!/bin/sh
# Tests for tw-upstream-pytest: run it against the projects in fixtures/,
# each "installed" into a fake destdir, and check the exit status and the
# lines stereo's logs and parsers rely on. The negative controls are the
# ones the stereo pilot used (py3-six neg/bneg, shadow2, netprobe,
# mostly-skip).
#
# Usage: tests/run.sh [python ...]
# Default: every python3.X on PATH that can import pytest and pytest-timeout
# (the default --timeout needs it); none is an error.

set -u

here=$(cd "$(dirname "$0")" && pwd)
tool="$here/../tw-upstream-pytest"
# TOOL_SH='busybox sh' (for example) runs the tool under another shell.
TOOL_SH=${TOOL_SH:-}
export UT_LIB="$here/../lib"

if [ $# -eq 0 ]; then
  for p in python3.10 python3.11 python3.12 python3.13 python3.14 python3.15; do
    command -v "$p" >/dev/null 2>&1 || continue
    "$p" -c 'import pytest, pytest_timeout' 2>/dev/null || continue
    set -- "$@" "$p"
  done
  if [ $# -eq 0 ]; then
    echo "ERROR: no python3.X with pytest and pytest-timeout found; pass one as an argument" >&2
    exit 1
  fi
fi
for p in "$@"; do
  if ! "$p" -c 'import pytest, pytest_timeout' 2>/dev/null; then
    echo "ERROR: $p cannot import pytest and pytest_timeout (add py3.X-pytest and py3.X-pytest-timeout)" >&2
    exit 1
  fi
done

tmp=$(mktemp -d "${TMPDIR:-/tmp}/tw-upstream-pytest-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
export UT_SCRATCH_BASE="$tmp/scratch"
# A pytest_timeout that fails to import, put first on PYTHONPATH to test
# the tool without pytest-timeout.
mkdir -p "$tmp/no-pytest-timeout"
echo 'raise ImportError("pytest_timeout hidden by tests/run.sh")' > "$tmp/no-pytest-timeout/pytest_timeout.py"
# The same for pytest-asyncio (its metadata stays visible; the import is
# what counts).
mkdir -p "$tmp/no-pytest-asyncio"
echo 'raise ImportError("pytest_asyncio hidden by tests/run.sh")' > "$tmp/no-pytest-asyncio/pytest_asyncio.py"

npass=0 nfail=0
pass() { echo "  [PASS] $*"; npass=$((npass + 1)); }
fail() { echo "  [FAIL] $*"; nfail=$((nfail + 1)); }

# run_case PY FIXTURE WANT_RC [tool options...]: lay out a workspace with
# the fixture's tree, install its demo/ (or .installed/demo/, when the
# installed package differs from the source tree) into destdir's
# site-packages, run the tool from the workspace. Output in $out, status
# in $rc.
run_case() {
  py=$1 fx=$2 want=$3
  shift 3
  ws="$tmp/ws-$fx"
  rm -rf "$ws"
  mkdir -p "$ws"
  cp -R "$here/fixtures/$fx/." "$ws/"
  dest="$ws/melange-out/demo"
  sp=$("$py" -c 'import site; print(site.getsitepackages()[0])')
  mkdir -p "$dest$sp"
  inst="$here/fixtures/$fx/demo"
  [ -d "$here/fixtures/$fx/.installed/demo" ] && inst="$here/fixtures/$fx/.installed/demo"
  cp -R "$inst" "$dest$sp/"
  out=$(cd "$ws" && $TOOL_SH "$tool" --name demo --stage build --caller upstream-tests/pytest \
    --destdir "$dest" --python "$py" "$@" 2>&1)
  rc=$?
  if [ "$rc" -eq "$want" ]; then
    pass "$fx ($py): exit $rc"
  else
    fail "$fx ($py): exit $rc, want $want"
    printf '%s\n' "$out" | sed 's/^/      | /'
  fi
}

# has TEXT / hasnt TEXT: a fixed string in (or not in) the last output.
has() {
  case "$out" in
    *"$1"*) pass "$fx: has '$1'" ;;
    *) fail "$fx: missing '$1'"; printf '%s\n' "$out" | sed 's/^/      | /' ;;
  esac
}
hasnt() {
  case "$out" in
    *"$1"*) fail "$fx: unexpected '$1'"; printf '%s\n' "$out" | sed 's/^/      | /' ;;
    *) pass "$fx: no '$1'" ;;
  esac
}

for py in "$@"; do
  echo "== $py"

  run_case "$py" pass 0
  has "UPSTREAM-TESTS demo: duration="
  has " netns="
  case "$out" in *"netns=isolated"*) isolated=yes ;; *) isolated=no ;; esac
  has "UPSTREAM-TESTS demo: pass=2 fail=0 skip=1"
  has "fixture: always skipped"
  hasnt "FAILED"

  run_case "$py" neg 1 --ignore "tests/test_neg.py::test_fail tests/test_neg.py::test_nonexistent"
  has "IGNORE demo: tests/test_neg.py::test_fail (deselected 1)"
  has "ERROR: ignore entry 'tests/test_neg.py::test_nonexistent' matches no collected test: no such test"
  has "1 ignore entry matches no collected test (tests/test_neg.py::test_nonexistent)"
  has "UPSTREAM-TESTS demo: pass=3 fail=0 skip=0 ignored=1"
  has "the run changed the installed package"
  has "leak.txt"

  run_case "$py" neg 1
  has "UPSTREAM-TESTS-FAIL demo: tests/test_neg.py::test_fail"
  has "UPSTREAM-TESTS demo: pass=3 fail=1 skip=0"
  has "1 failing test(s)/collection error(s)"

  run_case "$py" shadow 1
  has "the package under test was imported from the source tree, not the destdir: demo"

  run_case "$py" netprobe 1
  has "UPSTREAM-TESTS-FAIL demo: tests/test_net.py::test_needs_network"
  has "UPSTREAM-TESTS-FAIL demo: tests/test_net.py::test_needs_network_by_ip"
  has "upstream-tests: network disabled"
  has "UPSTREAM-TESTS demo: pass=1 fail=2 skip=0"

  # The namespace itself, not the in-process guard: a child that never
  # loads the plugin must have no route out. Only meaningful where the
  # namespace can be created (root with CAP_SYS_ADMIN, as in melange's VM).
  if [ "$isolated" = yes ]; then
    run_case "$py" netchild 0
    has "UPSTREAM-TESTS demo: duration="
    has "netns=isolated"
    has "UPSTREAM-TESTS demo: pass=1 fail=0 skip=0"
  else
    echo "  [SKIP] netchild ($py): no network namespace here (netns=unavailable), so a non-Python child is not isolated; run as root with CAP_SYS_ADMIN to cover it"
  fi

  # Retry plugins are disabled: a test that only passes on a retry fails,
  # whether or not pytest-rerunfailures / flaky are installed.
  run_case "$py" retry 1
  has "UPSTREAM-TESTS-FAIL demo: tests/test_retry.py::test_fails_on_first_run"
  has "UPSTREAM-TESTS demo: pass=0 fail=1 skip=0"
  has "-p no:rerunfailures -p no:flaky"
  hasnt "unrecognized arguments"

  run_case "$py" allskip 1
  has "UPSTREAM-TESTS demo: pass=0 fail=0 skip=2"
  has "no test passed (everything skipped, xfailed or deselected)"

  run_case "$py" mostlyskip 1
  has "UPSTREAM-TESTS demo: pass=1 fail=0 skip=24"
  has "only 1 of 25 collected tests ran (4.0%), below min-run-percent=5"

  run_case "$py" mostlyskip 0 --min-run-percent 0
  has "UPSTREAM-TESTS demo: pass=1 fail=0 skip=24"
  hasnt "FAILED"

  run_case "$py" empty 1
  has "no tests collected; the suite did not run"

  run_case "$py" collect 1
  has "UPSTREAM-TESTS-FAIL demo: tests/test_missing.py [collection]"
  has "HINT: tests could not import: missing_test_dep"
  has "py3-supported-missing-test-dep"

  run_case "$py" select 0 --args '-k "alpha or beta"'
  has "UPSTREAM-TESTS demo: pass=2 fail=0 skip=0"
  has "-k 'alpha or beta' tests"

  # --args is parsed once: a command substitution in it runs once.
  rm -f "$tmp/argcount"
  run_case "$py" select 0 --args "-k \"\$(echo x >> $tmp/argcount; echo alpha)\""
  has "UPSTREAM-TESTS demo: pass=1 fail=0 skip=0"
  fx=select
  if [ "$(wc -l < "$tmp/argcount")" -eq 1 ]; then pass "$fx: args expanded once"; else fail "$fx: args expanded $(wc -l < "$tmp/argcount") times"; fi

  # --ignore boundaries: test_x must not take test_x_regression with it.
  run_case "$py" ignoreprefix 0 --ignore "tests/test_prefix.py::test_x"
  has "IGNORE demo: tests/test_prefix.py::test_x (deselected 1)"
  has "UPSTREAM-TESTS demo: pass=4 fail=0 skip=0 ignored=1"
  hasnt "matches no collected test"
  # A prefix of a test name only (test_o of test_other) matches nothing,
  # which fails the gate (contract 4); a parametrized case alone, or all
  # of them.
  run_case "$py" ignoreprefix 1 --ignore "tests/test_prefix.py::test_x tests/test_prefix.py::test_p[a] tests/test_prefix.py::test_o"
  has "IGNORE demo: tests/test_prefix.py::test_p[a] (deselected 1)"
  has "ERROR: ignore entry 'tests/test_prefix.py::test_o' matches no collected test"
  has "UPSTREAM-TESTS demo: pass=3 fail=0 skip=0 ignored=2"
  has "FAILED: 1 ignore entry matches no collected test (tests/test_prefix.py::test_o)"
  run_case "$py" ignoreprefix 0 --ignore "tests/test_prefix.py::test_x tests/test_prefix.py::test_p[a]"
  has "UPSTREAM-TESTS demo: pass=3 fail=0 skip=0 ignored=2"
  hasnt "matches no collected test"
  # A whole file, and a class-less '::' boundary at the file: the same.
  run_case "$py" ignoreprefix 0 --ignore "tests/test_prefix.py::test_x tests/test_prefix.py::test_p[b] tests/test_prefix.py::test_x_regression"
  has "IGNORE demo: tests/test_prefix.py::test_x (deselected 1)"
  has "IGNORE demo: tests/test_prefix.py::test_x_regression (deselected 1)"
  has "UPSTREAM-TESTS demo: pass=2 fail=0 skip=0 ignored=3"
  run_case "$py" ignoreprefix 1 --ignore "tests/test_prefix.py"
  has "IGNORE demo: tests/test_prefix.py (deselected 5)"
  has "no tests collected; the suite did not run"
  hasnt "matches no collected test"
  # A stale name fails, even when every test passes; two of them.
  run_case "$py" ignoreprefix 1 --ignore "tests/test_prefix.py::test_x tests/test_prefix.py::test_gone tests/test_prefix.py::test_p[c]"
  has "ERROR: ignore entry 'tests/test_prefix.py::test_gone' matches no collected test: no such test (renamed or removed upstream?)"
  has "ERROR: ignore entry 'tests/test_prefix.py::test_p[c]' matches no collected test"
  has "UPSTREAM-TESTS demo: pass=4 fail=0 skip=0 ignored=1"
  has "FAILED: 2 ignore entries match no collected test (tests/test_prefix.py::test_gone tests/test_prefix.py::test_p[c]): fix or drop them"
  run_case "$py" ignoreprefix 0 --ignore "tests/test_prefix.py::test_x tests/test_prefix.py::test_p"
  has "IGNORE demo: tests/test_prefix.py::test_p (deselected 2)"
  has "UPSTREAM-TESTS demo: pass=2 fail=0 skip=0 ignored=3"
  hasnt "--deselect"
  run_case "$py" ignoreprefix 1
  has "UPSTREAM-TESTS-FAIL demo: tests/test_prefix.py::test_x"
  # Under xdist the workers deselect; the report still counts it.
  if "$py" -c 'import xdist' 2>/dev/null; then
    run_case "$py" ignoreprefix 0 --jobs 2 --ignore "tests/test_prefix.py::test_x"
    has "IGNORE demo: tests/test_prefix.py::test_x (deselected 1)"
    has "UPSTREAM-TESTS demo: pass=4 fail=0 skip=0 ignored=1"
    hasnt "matches no collected test"
    run_case "$py" ignoreprefix 1 --jobs 2 --ignore "tests/test_prefix.py::test_x tests/test_prefix.py::test_gone"
    has "IGNORE demo: tests/test_prefix.py::test_x (deselected 1)"
    has "ERROR: ignore entry 'tests/test_prefix.py::test_gone' matches no collected test"
    has "UPSTREAM-TESTS demo: pass=4 fail=0 skip=0 ignored=1"
    run_case "$py" modskip 0 --jobs 2
    has "UPSTREAM-TESTS demo: pass=2 fail=0 skip=1 modskip=2"
    # Two modules skipped by one shared conftest helper: the same longrepr,
    # which xdist's controller passes on once; the workers' records keep
    # both, so the count and the ignore check agree with the serial run.
    run_case "$py" modskipshared 0 --jobs 2
    has "UPSTREAM-TESTS demo: pass=2 fail=0 skip=0 modskip=2"
    run_case "$py" modskipshared 0 --jobs 2 --ignore tests/test_x2.py::test_needs_x
    has "note: ignore entry 'tests/test_x2.py::test_needs_x' is in tests/test_x2.py, which was not collected (skipped at collection); not checked"
    has "UPSTREAM-TESTS demo: pass=2 fail=0 skip=0 modskip=2"
    hasnt "matches no collected test"
    run_case "$py" mostlymodskip 1 --jobs 2
    has "UPSTREAM-TESTS demo: pass=1 fail=0 skip=0 modskip=20"
    has "only 1 of 21 collected tests ran (4.8%; 20 of the 21 are modules skipped at collection), below min-run-percent=5"
    if "$py" -c 'import pytest_asyncio' 2>/dev/null; then
      run_case "$py" asyncio 0 --jobs 2
      has "UPSTREAM-TESTS demo: pass=2 fail=0 skip=0"
    fi
  else
    echo "  [SKIP] ignoreprefix, modskip, modskipshared, mostlymodskip, asyncio --jobs 2 ($py): no pytest-xdist"
  fi

  # tw#503: a directory is not a node id; it matches nothing and its
  # tests run. The message points to ignore-files, which works.
  run_case "$py" ignoredir 1 --ignore tests/integration
  has "ERROR: ignore entry 'tests/integration' matches no collected test: 'tests/integration' is a directory: an ignore entry is a node id; list directories in ignore-files (not collected)"
  has "1 ignore entry matches no collected test (tests/integration)"
  run_case "$py" ignoredir 1 --ignore tests/integration/
  has "'tests/integration/' is a directory"
  run_case "$py" ignoredir 0 --ignore-files tests/integration
  has "IGNORE demo: tests/integration (not collected)"
  has "UPSTREAM-TESTS demo: pass=1 fail=0 skip=0"
  hasnt "matches no collected test"
  # A file with that boundary is fine; a file entry that collects nothing
  # (here: not a test module) is stale.
  run_case "$py" ignoredir 0 --ignore tests/integration/test_live.py
  has "IGNORE demo: tests/integration/test_live.py (deselected 1)"
  has "UPSTREAM-TESTS demo: pass=1 fail=0 skip=0 ignored=1"
  run_case "$py" ignoredir 1 --ignore "tests/integration/test_live.py demo/__init__.py"
  has "'demo/__init__.py' is a file with no collected test; list it in ignore-files"

  # The same, serial (the reference for the --jobs 2 run above).
  run_case "$py" modskipshared 0
  has "UPSTREAM-TESTS demo: pass=2 fail=0 skip=0 modskip=2"
  run_case "$py" modskipshared 0 --ignore tests/test_x2.py::test_needs_x
  has "note: ignore entry 'tests/test_x2.py::test_needs_x' is in tests/test_x2.py, which was not collected (skipped at collection); not checked"

  # tw#504: modules skipped at collection are counted (modskip=, last)
  # and listed; per-test skips are unchanged (skip=).
  run_case "$py" modskip 0
  has "UPSTREAM-TESTS demo: pass=2 fail=0 skip=1 modskip=2"
  has "skipped at collection (each counts as one skipped test): 2"
  has "tests/test_optional.py: Skipped: could not import 'nonexistent_mod'"
  has "tests/test_platform.py: Skipped: fixture: not on this platform"
  has "fixture: per-test skip"
  # They count in the min-run-percent denominator: 2 of 5 ran (40%).
  run_case "$py" modskip 1 --min-run-percent 50
  has "only 2 of 5 collected tests ran (40.0%; 2 of the 5 are modules skipped at collection), below min-run-percent=50"
  run_case "$py" modskip 0 --min-run-percent 40
  # Only module skips: nothing collected, and the message says why.
  run_case "$py" modskip 1 --paths "tests/test_optional.py tests/test_platform.py"
  has "UPSTREAM-TESTS demo: pass=0 fail=0 skip=0 modskip=2"
  has "no tests collected; every module was skipped at collection (2, reasons above)"
  has "HINT: tests could not import: nonexistent_mod"
  # An ignore entry in a module skipped at collection cannot be checked:
  # a note, not a failure.
  run_case "$py" modskip 0 --ignore "tests/test_optional.py::test_uses_mod tests/test_real.py::test_add_str"
  has "note: ignore entry 'tests/test_optional.py::test_uses_mod' is in tests/test_optional.py, which was not collected (skipped at collection); not checked"
  has "UPSTREAM-TESTS demo: pass=1 fail=0 skip=1 ignored=1 modskip=2"
  # ... nor in one that failed to import (that is its own failure).
  run_case "$py" collect 1 --ignore "tests/test_missing.py::test_uses_dep"
  has "which was not collected (collection error); not checked"
  hasnt "matches no collected test"
  # A mostly module-skipped suite trips the default threshold: 1 of 21.
  run_case "$py" mostlymodskip 1
  has "UPSTREAM-TESTS demo: pass=1 fail=0 skip=0 modskip=20"
  has "only 1 of 21 collected tests ran (4.8%; 20 of the 21 are modules skipped at collection), below min-run-percent=5"
  run_case "$py" mostlymodskip 0 --min-run-percent 4
  # No module skips: no modskip field.
  run_case "$py" mostlyskip 0 --min-run-percent 4
  hasnt "modskip="

  # ini.py: where a project can set pytest-asyncio's loop scope.
  for cfg in pytest.ini pyproject.toml setup.cfg tox.ini addopts none empty; do
    d="$tmp/ini-$cfg"
    rm -rf "$d"
    mkdir -p "$d"
    want="set unset"
    case "$cfg" in
      pytest.ini) printf '[pytest]\nasyncio_default_fixture_loop_scope = module\n' > "$d/pytest.ini" ;;
      pyproject.toml) printf '[tool.pytest.ini_options]\nasyncio_default_fixture_loop_scope = "session"\n' > "$d/pyproject.toml" ;;
      setup.cfg) printf '[tool:pytest]\nasyncio_default_fixture_loop_scope = class\n' > "$d/setup.cfg" ;;
      tox.ini) printf '[pytest]\nasyncio_default_fixture_loop_scope = function\n' > "$d/tox.ini" ;;
      addopts) printf '[tool.pytest.ini_options]\naddopts = ["-o", "asyncio_default_fixture_loop_scope=module"]\n' > "$d/pyproject.toml"; want="unset set" ;;
      none) printf '[pytest]\nfilterwarnings = error\n' > "$d/pytest.ini"; want="unset unset" ;;
      empty) printf '[pytest]\nasyncio_default_fixture_loop_scope =\n' > "$d/pytest.ini"; want="unset unset" ;;
    esac
    got=$(cd "$d" && "$py" -I "$UT_LIB/ini.py" | sed -n '3,4p' | tr '\n' ' ')
    fx=ini.py
    if [ "$got" = "$want " ]; then pass "ini.py loop scope ($cfg): $got"; else fail "ini.py loop scope ($cfg): got '$got', want '$want '"; fi
  done
  # ini.py: pytest-asyncio disabled in addopts.
  for cfg in ini toml none; do
    d="$tmp/ini-noaio-$cfg"
    rm -rf "$d"
    mkdir -p "$d"
    want="set"
    case "$cfg" in
      ini) printf '[pytest]\naddopts = -ra -p no:asyncio\n' > "$d/pytest.ini" ;;
      toml) printf '[tool.pytest.ini_options]\naddopts = ["-p", "no:asyncio"]\n' > "$d/pyproject.toml" ;;
      none) printf '[pytest]\naddopts = -ra\n' > "$d/pytest.ini"; want="unset" ;;
    esac
    got=$(cd "$d" && "$py" -I "$UT_LIB/ini.py" | sed -n 5p)
    fx=ini.py
    if [ "$got" = "$want" ]; then pass "ini.py no:asyncio ($cfg): $got"; else fail "ini.py no:asyncio ($cfg): got '$got', want '$want'"; fi
  done

  # tw#505: filterwarnings = error, pytest-asyncio and no loop scope: the
  # tool ignores the plugin's "is unset" warning (before: an INTERNALERROR,
  # nothing ran). The scope stays unset. A project's own setting, or the
  # plugin disabled with -p no:asyncio, gets no flag.
  if "$py" -c 'import pytest_asyncio' 2>/dev/null; then
    run_case "$py" asyncio 0
    has "and no asyncio_default_fixture_loop_scope in the project's config: ignoring its \"is unset\" deprecation warning (-W)"
    has " -W 'ignore:The configuration option \"asyncio_default_fixture_loop_scope\" is unset:pytest.PytestDeprecationWarning' "
    hasnt "asyncio_default_fixture_loop_scope=function"
    has "UPSTREAM-TESTS demo: pass=2 fail=0 skip=0"
    hasnt "INTERNALERROR"
    # The -W adds no config key: --strict-config is fine.
    run_case "$py" asyncio 0 --args --strict-config
    has "UPSTREAM-TESTS demo: pass=2 fail=0 skip=0"
    # Module- and session-scoped async fixtures keep their loop scope
    # (unset: the caching scope), with the same results as plain pytest.
    run_case "$py" asyncioscoped 0
    has " -W 'ignore:The configuration option \"asyncio_default_fixture_loop_scope\" is unset:pytest.PytestDeprecationWarning' "
    has "UPSTREAM-TESTS demo: pass=4 fail=0 skip=0"
    hasnt "ScopeMismatch"
    rm -rf "$tmp/plain"
    cp -R "$here/fixtures/asyncioscoped" "$tmp/plain"
    plain=$(cd "$tmp/plain" && PYTHONPATH=. PYTHONDONTWRITEBYTECODE=1 "$py" -m pytest -q -p no:cacheprovider 2>&1 | tail -n 1)
    fx=asyncioscoped-plain
    case "$plain" in
      "4 passed"*) pass "asyncioscoped ($py): plain pytest: $plain" ;;
      *) fail "asyncioscoped ($py): plain pytest: $plain" ;;
    esac
    run_case "$py" asyncioscope 0
    hasnt "is unset"
    hasnt " -W "
    has "UPSTREAM-TESTS demo: pass=2 fail=0 skip=0"
    export WANT_LOOP_SCOPE=module
    run_case "$py" asyncio 0 --args "-o asyncio_default_fixture_loop_scope=module"
    hasnt " -W "
    has "UPSTREAM-TESTS demo: pass=2 fail=0 skip=0"
    export PYTEST_ADDOPTS="-o asyncio_default_fixture_loop_scope=module"
    run_case "$py" asyncio 0
    hasnt " -W "
    has "UPSTREAM-TESTS demo: pass=2 fail=0 skip=0"
    unset PYTEST_ADDOPTS
    unset WANT_LOOP_SCOPE
    # Importable but disabled (-p no:asyncio in addopts, --args or
    # PYTEST_ADDOPTS): no flag, and the run passes, under --strict-config too.
    run_case "$py" asyncionoplugin 0
    hasnt " -W "
    has "UPSTREAM-TESTS demo: pass=2 fail=0 skip=0"
    run_case "$py" asyncionoplugin 0 --args --strict-config
    hasnt " -W "
    has "UPSTREAM-TESTS demo: pass=2 fail=0 skip=0"
    run_case "$py" asyncionoplugin 0 --clear-addopts true --args "-p no:asyncio --strict-config"
    hasnt " -W "
    has "UPSTREAM-TESTS demo: pass=2 fail=0 skip=0"
    export PYTEST_ADDOPTS="-p no:asyncio"
    run_case "$py" asyncionoplugin 0 --clear-addopts true --args --strict-config
    hasnt " -W "
    has "UPSTREAM-TESTS demo: pass=2 fail=0 skip=0"
    unset PYTEST_ADDOPTS
  else
    echo "  [SKIP] asyncio, asyncioscoped, asyncioscope, asyncionoplugin ($py): no pytest-asyncio (the absent case runs instead)"
  fi
  # Without pytest-asyncio (hidden, or not installed): no flag.
  # (-p no:asyncio: pytest would otherwise load the hidden plugin itself.)
  export PYTHONPATH="$tmp/no-pytest-asyncio"
  run_case "$py" asyncio 1 --args '-p no:asyncio'
  hasnt " -W "
  has "UPSTREAM-TESTS-FAIL demo: tests/test_async.py [collection]"
  # Not importable, and not disabled by name (no plugin autoloading, so
  # pytest does not load the hidden module itself): no flag.
  export PYTEST_DISABLE_PLUGIN_AUTOLOAD=1
  run_case "$py" pass 0 --timeout 0
  hasnt " -W "
  has "UPSTREAM-TESTS demo: pass=2 fail=0 skip=1"
  unset PYTEST_DISABLE_PLUGIN_AUTOLOAD
  unset PYTHONPATH

  # src layout next to docs/demo: demo.tests comes from src/demo/tests.
  run_case "$py" srclayout 0
  has "UPSTREAM-TESTS demo: pass=1 fail=0 skip=0"

  # A per-test timeout needs pytest-timeout; timeout 0 does not.
  export PYTHONPATH="$tmp/no-pytest-timeout"
  run_case "$py" pass 1
  has "timeout=600 needs pytest-timeout"
  has "HINT: add py3-supported-pytest-timeout (or py3.X-pytest-timeout) to environment.contents.packages, or set timeout: 0"
  # (-p no:timeout: pytest would otherwise load the hidden plugin itself.)
  run_case "$py" pass 0 --timeout 0 --args '-p no:timeout'
  has "UPSTREAM-TESTS demo: pass=2 fail=0 skip=1"
  unset PYTHONPATH

  run_case "$py" unittest 0 --runner unittest
  has "python -m unittest discover -s tests"
  has "UPSTREAM-TESTS demo: pass=1 fail=0 skip=0"
  # unittest cannot honour these; the default timeout is fine.
  run_case "$py" unittest 0 --runner unittest --timeout 600 --jobs 1
  run_case "$py" unittest 1 --runner unittest --ignore tests/test_ut.py::DemoTest::test_add
  has "runner unittest cannot honour: ignore;"
  run_case "$py" unittest 1 --runner unittest --ignore-files tests/test_ut.py --markers slow --jobs 2 --timeout 30
  has "runner unittest cannot honour: ignore-files markers jobs=2 timeout=30;"
  hasnt "IGNORE demo"

  # Usage errors: exit 1 before running anything.
  run_case "$py" pass 1 --stage test
  has "stage must be build (got 'test')"
  run_case "$py" pass 1 --extensions requried
  has "extensions must be auto, required or none (got 'requried')"
  run_case "$py" pass 1 --network none
  has "unknown option '--network'"
  run_case "$py" pass 1 --runner nose
  has "runner must be pytest or unittest (got 'nose')"
  run_case "$py" pass 1 --package py3-foo
  has "reserved"
  run_case "$py" pass 1 --min-run-percent 150
  has "min-run-percent must be a number from 0 to 100 (got '150')"
  run_case "$py" pass 1 --dir nonexistent
  has "dir 'nonexistent' does not exist"
  run_case "$py" pass 1 --bogus x
  has "unknown option '--bogus'"
done

out=$(cd "$tmp" && $TOOL_SH "$tool" --stage build --destdir /nonexistent 2>&1)
rc=$?
fx=usage
if [ "$rc" -eq 1 ]; then pass "no --name: exit 1"; else fail "no --name: exit $rc"; fi
has "--name is required"

echo "tw-upstream-pytest tests: $npass passed, $nfail failed"
[ "$nfail" -eq 0 ]
