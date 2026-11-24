#!/bin/sh
# Tests for tw-upstream-pytest: run it against the projects in fixtures/
# (and mostlymodskip, asyncioscopetoml and asyncioscopedottoml, generated
# below), each "installed" into a fake destdir, and check the exit status
# and the lines stereo's logs and parsers rely on. The negative controls are the ones the stereo pilot
# used (py3-six neg/bneg, shadow2, netprobe, mostly-skip).
#
# Usage: tests/run.sh [python ...]
# Default: every python3.X on PATH that can import pytest and pytest-timeout
# (the default --timeout needs it); none is an error.
#
# Stage test (contract 5) cases install the fixture's demo/ into a venv
# (--system-site-packages, so pytest stays importable) and describe it in
# a fake apk database (UT_APK_DB); the source tree is a separate copy
# passed as --test-tree.
#
# UT_BASELINE=DIR (a released tw-upstream-pytest directory: the script
# and lib/, e.g. from `git archive v0.0.58 tw-upstream-pytest`, or
# `make test-baseline`): every stage build case also runs that tool on the
# same workspace and compares the two outputs (durations, pytest timings
# and the scratch directory's name normalized), so the build stage's
# output is shown to be unchanged. Cases whose usage message changed on
# purpose say so and are not compared.

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
# The fixtures: a copy of fixtures/, plus mostlymodskip, generated here
# because it is 20 near-identical modules (tw#504 negative control: one
# test runs, the 20 other modules are skipped at collection by
# importorskip, so 1 of 21 ran (4.8%), below the default min-run-percent
# of 5).
fixtures="$tmp/fixtures"
cp -R "$here/fixtures" "$fixtures"
d="$fixtures/mostlymodskip"
mkdir -p "$d/demo" "$d/tests"
printf 'def add(a, b):\n    return a + b\n' > "$d/demo/__init__.py"
printf 'import demo\n\n\ndef test_runs():\n    assert demo.add(1, 1) == 2\n' > "$d/tests/test_runs.py"
i=1
while [ "$i" -le 20 ]; do
  n=$(printf '%02d' "$i")
  printf 'import pytest\n\npytest.importorskip("nonexistent_mod_%s")\n\n\ndef test_needs_mod():\n    pass\n' "$n" > "$d/tests/test_opt$n.py"
  i=$((i + 1))
done
# asyncioscopetoml and asyncioscopedottoml: asyncioscope with its config
# in pytest.toml and .pytest.toml (pytest 9's [pytest] table, native TOML
# types; dannf's review of tw#507).
for f in pytest.toml .pytest.toml; do
  case "$f" in pytest.toml) d="$fixtures/asyncioscopetoml" ;; *) d="$fixtures/asyncioscopedottoml" ;; esac
  cp -R "$fixtures/asyncioscope" "$d"
  rm "$d/pytest.ini"
  printf '[pytest]\nfilterwarnings = ["error"]\nasyncio_default_fixture_loop_scope = "module"\n' > "$d/$f"
done

# noimport: a suite that passes without importing the package (stage
# test negative control for the import-origin check).
d="$fixtures/noimport"
mkdir -p "$d/demo" "$d/tests"
printf 'def add(a, b):\n    return a + b\n' > "$d/demo/__init__.py"
printf 'def test_nothing_imported():\n    assert 1 + 1 == 2\n' > "$d/tests/test_noimport.py"

npass=0 nfail=0
pass() { echo "  [PASS] $*"; npass=$((npass + 1)); }
fail() { echo "  [FAIL] $*"; nfail=$((nfail + 1)); }

# run_case PY FIXTURE WANT_RC [tool options...]: lay out a workspace with
# the fixture's tree, install its demo/ (or .installed/demo/, when the
# installed package differs from the source tree) into destdir's
# site-packages, run the tool from the workspace. Output in $out, status
# in $rc. With UT_BASELINE, run the baseline tool the same way and
# compare (unless nobase is set: an intended difference).
nobase=""
setup_ws() {
  ws="$tmp/ws-$fx"
  rm -rf "$ws"
  mkdir -p "$ws"
  cp -R "$fixtures/$fx/." "$ws/"
  dest="$ws/melange-out/demo"
  sp=$("$py" -c 'import site; print(site.getsitepackages()[0])')
  mkdir -p "$dest$sp"
  inst="$fixtures/$fx/demo"
  [ -d "$fixtures/$fx/.installed/demo" ] && inst="$fixtures/$fx/.installed/demo"
  cp -R "$inst" "$dest$sp/"
}
# normalize: what differs between two runs of the same case.
normalize() {
  sed -e 's/duration=[0-9]*s/duration=Ns/' -e 's/ in [0-9.]*s\b/ in Ns/g' \
    -e 's/run\.[A-Za-z0-9]\{6\}/run.XXXXXX/g' -e 's/zz-upstream-tests-pytest-[0-9]*\.pth/zz-upstream-tests-pytest-PID.pth/g'
}
run_case() {
  py=$1 fx=$2 want=$3
  shift 3
  setup_ws
  out=$(cd "$ws" && $TOOL_SH "$tool" --name demo --stage build --caller upstream-tests/pytest \
    --destdir "$dest" --python "$py" "$@" 2>&1)
  rc=$?
  if [ "$rc" -eq "$want" ]; then
    pass "$fx ($py): exit $rc"
  else
    fail "$fx ($py): exit $rc, want $want"
    printf '%s\n' "$out" | sed 's/^/      | /'
  fi
  if [ -n "${UT_BASELINE:-}" ] && [ -n "$nobase" ]; then
    echo "  [SKIP] baseline: $fx ($py): intended difference ($nobase)"
  elif [ -n "${UT_BASELINE:-}" ]; then
    new_out=$out new_rc=$rc
    setup_ws
    base_out=$(cd "$ws" && UT_LIB="$UT_BASELINE/lib" $TOOL_SH "$UT_BASELINE/tw-upstream-pytest" --name demo --stage build --caller upstream-tests/pytest \
      --destdir "$dest" --python "$py" "$@" 2>&1)
    base_rc=$?
    a=$(printf '%s\n' "$base_out" | normalize)
    b=$(printf '%s\n' "$new_out" | normalize)
    if [ "$base_rc" -eq "$new_rc" ] && [ "$a" = "$b" ]; then
      pass "$fx ($py): same output and exit status as the baseline"
    else
      fail "$fx ($py): differs from the baseline (exit $base_rc -> $new_rc)"
      printf '%s\n' "$a" > "$tmp/base.out"
      printf '%s\n' "$b" > "$tmp/new.out"
      diff -u "$tmp/base.out" "$tmp/new.out" | sed 's/^/      | /'
    fi
    out=$new_out rc=$new_rc
  fi
}

# mkapkdb DB DIR...: a fake apk database (the format apk-tools writes to
# /usr/lib/apk/db/installed) in which py3-demo owns every file under the
# DIRs, with Q1 (SHA-1) checksums; plus py3-supported-demo, a metapackage
# without files, py3.99-demo, which installs for another python, and an
# unrelated package after them.
mkapkdb() {
  _db=$1
  shift
  mkdir -p "$tmp/other/usr/lib/python3.99/site-packages/demo"
  : > "$tmp/other/usr/lib/python3.99/site-packages/demo/__init__.py"
  "$py" -I - "$_db" "$tmp/other/usr/lib/python3.99/site-packages/demo" "$@" <<'PYEOF'
import base64, hashlib, os, sys
db, other, dirs = sys.argv[1], sys.argv[2], sys.argv[3:]
def block(name, roots):
    out = ["P:" + name, "V:1.0-r0", "A:x86_64", "o:py3-demo"]
    for root in roots:
        for d, dn, fs in os.walk(root):
            dn.sort()
            out.append("F:" + d.lstrip("/"))
            for f in sorted(fs):
                p = os.path.join(d, f)
                data = os.readlink(p).encode() if os.path.islink(p) else open(p, "rb").read()
                out += ["R:" + f, "Z:Q1" + base64.b64encode(hashlib.sha1(data).digest()).decode()]
    return "\n".join(out) + "\n\n"
with open(db, "w") as fh:
    fh.write("P:py3-supported-demo\nV:1.0-r0\nD:py3.13-demo\n\n")
    fh.write(block("py3.99-demo", [other]))
    fh.write(block("py3-demo", dirs))
    fh.write("P:unrelated\nV:1.0-r0\nF:usr/bin\nR:unrelated\nZ:Q1AAAAAAAAAAAAAAAAAAAAAAAAAAA=\n\n")
PYEOF
}

# run_test_case PY FIXTURE WANT_RC [tool options...]: stage test. The
# fixture's demo/ (or .installed/demo/) and a dist-info are installed into
# a venv's site-packages and listed in a fake apk database as py3-demo;
# the fixture's tree is the test tree (as git-checkout would fetch it),
# outside the (empty) workspace. $before_run, if set, runs after the
# install (to tamper with it). Output in $out, status in $rc.
before_run=""
run_test_case() {
  py=$1 fx=$2 want=$3
  shift 3
  venv="$tmp/venv-${py##*/}"
  [ -x "$venv/bin/python" ] || "$py" -m venv --without-pip --system-site-packages "$venv" || fail "venv for $py"
  vpy="$venv/bin/python"
  vsp=$("$vpy" -c 'import site; print(site.getsitepackages()[0])')
  rm -rf "$vsp/demo" "$vsp/demo-1.0.dist-info"
  inst="$fixtures/$fx/demo"
  [ -d "$fixtures/$fx/.installed/demo" ] && inst="$fixtures/$fx/.installed/demo"
  cp -R "$inst" "$vsp/"
  mkdir -p "$vsp/demo-1.0.dist-info"
  printf 'Metadata-Version: 2.1\nName: demo\nVersion: 1.0\n' > "$vsp/demo-1.0.dist-info/METADATA"
  mkapkdb "$tmp/apkdb" "$vsp/demo" "$vsp/demo-1.0.dist-info"
  tt="$tmp/tt-$fx"
  rm -rf "$tt" "$tmp/tws"
  mkdir -p "$tt" "$tmp/tws"
  cp -R "$fixtures/$fx/." "$tt/"
  [ -n "$before_run" ] && eval "$before_run"
  out=$(cd "$tmp/tws" && UT_APK_DB="$tmp/apkdb" $TOOL_SH "$tool" --name demo --stage test --caller test/upstream-tests/pytest \
    --package py3-demo --test-tree "$tt" --python "$vpy" "$@" 2>&1)
  rc=$?
  if [ "$rc" -eq "$want" ]; then
    pass "$fx ($py, stage test): exit $rc"
  else
    fail "$fx ($py, stage test): exit $rc, want $want"
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
  # (with UT_BASELINE, the baseline tool ran it once too)
  want=1
  [ -n "${UT_BASELINE:-}" ] && want=2
  if [ "$(wc -l < "$tmp/argcount")" -eq "$want" ]; then pass "$fx: args expanded once"; else fail "$fx: args expanded $(wc -l < "$tmp/argcount") times"; fi

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
  # Ids that exist only on some builds (test_future: a python that is not
  # out yet; test_codec[no_such_backend]: a backend that is not
  # importable). As ignore entries they fail the gate where absent, with
  # a hint to use --args "--deselect ID", which pytest skips silently;
  # where the id exists, --deselect drops it (no IGNORE line, not in
  # ignored=, a note with the count, not in the min-run denominator).
  g=tests/test_gated.py
  run_case "$py" buildgate 1 --ignore "$g::test_slow $g::test_codec[no_such_backend] $g::test_future"
  has "IGNORE demo: $g::test_slow (deselected 1)"
  has "ERROR: ignore entry '$g::test_codec[no_such_backend]' matches no collected test: no such test"
  has "ERROR: ignore entry '$g::test_future' matches no collected test"
  has "use --args \"--deselect ID\" instead: pytest skips it silently where it is absent"
  has "FAILED: 2 ignore entries match no collected test ($g::test_codec[no_such_backend] $g::test_future)"
  has "ids that exist only on some builds in --args \"--deselect ID\""
  has "UPSTREAM-TESTS demo: pass=3 fail=0 skip=0 ignored=1"
  run_case "$py" buildgate 0 --ignore "$g::test_slow" --args "--deselect $g::test_codec[no_such_backend] --deselect $g::test_future"
  has "UPSTREAM-TESTS demo: pass=3 fail=0 skip=0 ignored=1"
  hasnt "matches no collected test"
  hasnt "deselected by markers"
  run_case "$py" buildgate 0 --min-run-percent 100 --args "--deselect $g::test_slow"
  has "3 passed, 1 deselected"
  has "note: 1 test(s) deselected by markers, -m, -k or --deselect (not in ignored=, not counted towards min-run-percent)"
  has "UPSTREAM-TESTS demo: pass=3 fail=0 skip=0"
  hasnt "IGNORE demo:"
  hasnt "skip=0 ignored="
  hasnt "UPSTREAM-TESTS-FAIL"
  # --deselect is a prefix match: test_codec takes test_codec_roundtrip.
  run_case "$py" buildgate 0 --ignore "$g::test_slow" --args "--deselect $g::test_codec"
  has "note: 2 test(s) deselected by markers"
  has "UPSTREAM-TESTS demo: pass=1 fail=0 skip=0 ignored=1"
  # Both an ignore entry and a --deselect: counted once, as ignored.
  run_case "$py" buildgate 0 --ignore "$g::test_slow" --args "--deselect $g::test_slow"
  has "IGNORE demo: $g::test_slow (deselected 1)"
  has "UPSTREAM-TESTS demo: pass=3 fail=0 skip=0 ignored=1"
  hasnt "deselected by markers"
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
    # --deselect under xdist: pytest's summary has no "deselected"; the
    # note (from the workers' records) still counts it.
    run_case "$py" buildgate 0 --jobs 2 --args "--deselect $g::test_slow --deselect $g::test_future"
    has "note: 1 test(s) deselected by markers"
    has "UPSTREAM-TESTS demo: pass=3 fail=0 skip=0"
    hasnt "matches no collected test"
    run_case "$py" buildgate 0 --jobs 2 --ignore "$g::test_slow" --args "--deselect $g::test_slow --deselect $g::test_codec[no_such_backend]"
    has "IGNORE demo: $g::test_slow (deselected 1)"
    has "UPSTREAM-TESTS demo: pass=3 fail=0 skip=0 ignored=1"
    hasnt "deselected by markers"
    run_case "$py" buildgate 1 --jobs 2 --ignore "$g::test_slow $g::test_future"
    has "ERROR: ignore entry '$g::test_future' matches no collected test"
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
    echo "  [SKIP] ignoreprefix, buildgate, modskip, modskipshared, mostlymodskip, asyncio --jobs 2 ($py): no pytest-xdist"
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
  for cfg in pytest.ini pyproject.toml setup.cfg tox.ini addopts none empty pytest.toml .pytest.toml toml-over-ini ini-under-toml toml-notable; do
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
      pytest.toml|.pytest.toml) printf '[pytest]\nasyncio_default_fixture_loop_scope = "module"\n' > "$d/$cfg" ;;
      # pytest 9 reads pytest.toml before pytest.ini (the first one wins),
      # and pytest.toml is the config even without a [pytest] table.
      toml-over-ini)
        printf '[pytest]\nasyncio_default_fixture_loop_scope = "module"\n' > "$d/.pytest.toml"
        printf '[pytest]\nfilterwarnings = error\n' > "$d/pytest.ini" ;;
      ini-under-toml)
        printf '[pytest]\nfilterwarnings = ["error"]\n' > "$d/pytest.toml"
        printf '[pytest]\nasyncio_default_fixture_loop_scope = module\n' > "$d/pytest.ini"; want="unset unset" ;;
      toml-notable)
        printf '[tool.other]\nx = 1\n' > "$d/pytest.toml"
        printf '[tool.pytest.ini_options]\nasyncio_default_fixture_loop_scope = "session"\n' > "$d/pyproject.toml"; want="unset unset" ;;
    esac
    got=$(cd "$d" && "$py" -I "$UT_LIB/ini.py" | sed -n '3,4p' | tr '\n' ' ')
    fx=ini.py
    if [ "$got" = "$want " ]; then pass "ini.py loop scope ($cfg): $got"; else fail "ini.py loop scope ($cfg): got '$got', want '$want '"; fi
  done
  # ini.py: pytest-asyncio disabled in addopts.
  for cfg in ini toml none pytest.toml pytest.toml-str; do
    d="$tmp/ini-noaio-$cfg"
    rm -rf "$d"
    mkdir -p "$d"
    want="set"
    case "$cfg" in
      ini) printf '[pytest]\naddopts = -ra -p no:asyncio\n' > "$d/pytest.ini" ;;
      toml) printf '[tool.pytest.ini_options]\naddopts = ["-p", "no:asyncio"]\n' > "$d/pyproject.toml" ;;
      none) printf '[pytest]\naddopts = -ra\n' > "$d/pytest.ini"; want="unset" ;;
      pytest.toml) printf '[pytest]\naddopts = ["-ra", "-p", "no:asyncio"]\n' > "$d/pytest.toml" ;;
      pytest.toml-str) printf '[pytest]\naddopts = "-ra -p no:asyncio"\n' > "$d/pytest.toml" ;;
    esac
    got=$(cd "$d" && "$py" -I "$UT_LIB/ini.py" | sed -n 5p)
    fx=ini.py
    if [ "$got" = "$want" ]; then pass "ini.py no:asyncio ($cfg): $got"; else fail "ini.py no:asyncio ($cfg): got '$got', want '$want'"; fi
  done
  # ini.py: testpaths and python_files from pytest.toml (native lists).
  d="$tmp/ini-testpaths-toml"
  rm -rf "$d"
  mkdir -p "$d"
  printf '[pytest]\ntestpaths = ["tests", "more"]\npython_files = ["check_*.py"]\n' > "$d/pytest.toml"
  got=$(cd "$d" && "$py" -I "$UT_LIB/ini.py" | sed -n '1,2p' | tr '\n' ' ')
  fx=ini.py
  if [ "$got" = "tests more set " ]; then pass "ini.py testpaths (pytest.toml): $got"; else fail "ini.py testpaths (pytest.toml): got '$got', want 'tests more set '"; fi

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
    cp -R "$fixtures/asyncioscoped" "$tmp/plain"
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
    # The same scope in pytest.toml or .pytest.toml (pytest 9): no -W, no
    # note, and pytest sees the scope (test_loop_scope).
    for fx in asyncioscopetoml asyncioscopedottoml; do
      run_case "$py" "$fx" 0
      hasnt "is unset"
      hasnt "no asyncio_default_fixture_loop_scope"
      hasnt " -W "
      has "UPSTREAM-TESTS demo: pass=2 fail=0 skip=0"
    done
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
    echo "  [SKIP] asyncio, asyncioscoped, asyncioscope, asyncioscopetoml, asyncioscopedottoml, asyncionoplugin ($py): no pytest-asyncio (the absent case runs instead)"
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

  # A hung test: overall-timeout stops the run, which fails.
  run_case "$py" hang 1 --timeout 0 --overall-timeout 5
  has "UPSTREAM-TESTS demo: FAILED: pytest timed out or was killed (exit "
  has "overall-timeout=5s) while running tests/test_hang.py::test_hang"

  # ---- Stage test (contract 5): the installed package, in place. ----
  run_test_case "$py" pass 0
  has "test/upstream-tests/pytest: stage test: testing the installed package py3-demo: 2 files from $tmp/apkdb (2 in $vsp), 2 checksum(s) match the database"
  has "UPSTREAM-TESTS demo: pass=2 fail=0 skip=1"
  has "cwd=$UT_SCRATCH_BASE/run."
  hasnt "FAILED"
  hasnt "melange-out"
  # The test tree defaults to the workspace (git-checkout's default
  # destination), and the run leaves the test tree as it was.
  out=$(cd "$tt" && UT_APK_DB="$tmp/apkdb" $TOOL_SH "$tool" --name demo --stage test --package py3-demo --python "$vpy" 2>&1)
  rc=$?
  fx=pass-default-tree
  if [ "$rc" -eq 0 ]; then pass "$fx: exit 0"; else fail "$fx: exit $rc"; printf '%s\n' "$out" | sed 's/^/      | /'; fi
  has "UPSTREAM-TESTS demo: pass=2 fail=0 skip=1"
  if diff -r "$fixtures/pass" "$tt" >/dev/null; then pass "$fx: test tree unchanged"; else fail "$fx: test tree changed"; fi
  # A real failure, and a test that writes a file next to the package's.
  run_test_case "$py" neg 1
  has "UPSTREAM-TESTS-FAIL demo: tests/test_neg.py::test_fail"
  has "UPSTREAM-TESTS demo: pass=3 fail=1 skip=0"
  has "1 failing test(s)/collection error(s)"
  has "the run changed the installed package py3-demo (files written, removed or modified by tests): added: $vsp/demo/leak.txt"
  # A test that rewrites an installed file, keeping its size and mtime.
  run_test_case "$py" tamper 1
  has "UPSTREAM-TESTS demo: pass=1 fail=0 skip=0"
  has "the run changed the installed package py3-demo (files written, removed or modified by tests): changed: $vsp/demo/__init__.py"
  # The source tree's copy stands in for the installed package.
  run_test_case "$py" shadow 1
  has "the package under test was imported from the source tree, not the installed package: demo"
  # The suite passes without importing the package's files.
  run_test_case "$py" noimport 1
  has "UPSTREAM-TESTS demo: pass=1 fail=0 skip=0"
  has "none of the installed modules (demo) was imported from the files of py3-demo in $vsp: the suite did not test the installed package"
  # Zero tests, everything skipped, a missing test dependency (the HINT
  # names the test environment), the network, a stale ignore entry, a hang.
  run_test_case "$py" empty 1
  has "no tests collected; the suite did not run"
  run_test_case "$py" allskip 1
  has "no test passed (everything skipped, xfailed or deselected)"
  run_test_case "$py" collect 1
  has "UPSTREAM-TESTS-FAIL demo: tests/test_missing.py [collection]"
  has "to test.environment.contents.packages, or name the files in ignore-files"
  hasnt " to environment.contents.packages"
  run_test_case "$py" netprobe 1
  has "UPSTREAM-TESTS-FAIL demo: tests/test_net.py::test_needs_network"
  has "UPSTREAM-TESTS demo: pass=1 fail=2 skip=0"
  if [ "$isolated" = yes ]; then
    run_test_case "$py" netchild 0
    has "netns=isolated"
    has "UPSTREAM-TESTS demo: pass=1 fail=0 skip=0"
  fi
  run_test_case "$py" ignoreprefix 1 --ignore "tests/test_prefix.py::test_x tests/test_prefix.py::test_gone"
  has "IGNORE demo: tests/test_prefix.py::test_x (deselected 1)"
  has "ERROR: ignore entry 'tests/test_prefix.py::test_gone' matches no collected test"
  has "UPSTREAM-TESTS demo: pass=4 fail=0 skip=0 ignored=1"
  run_test_case "$py" ignoreprefix 0 --ignore "tests/test_prefix.py::test_x"
  has "UPSTREAM-TESTS demo: pass=4 fail=0 skip=0 ignored=1"
  run_test_case "$py" hang 1 --timeout 0 --overall-timeout 5
  has "UPSTREAM-TESTS demo: FAILED: pytest timed out or was killed (exit "
  has "overall-timeout=5s) while running tests/test_hang.py::test_hang"
  run_test_case "$py" srclayout 0
  has "UPSTREAM-TESTS demo: pass=1 fail=0 skip=0"
  run_test_case "$py" unittest 0 --runner unittest
  has "UPSTREAM-TESTS demo: pass=1 fail=0 skip=0"
  if "$py" -c 'import xdist' 2>/dev/null; then
    run_test_case "$py" neg 1 --jobs 2
    has "UPSTREAM-TESTS demo: pass=3 fail=1 skip=0"
    has "added: $vsp/demo/leak.txt"
  fi
  export PYTHONPATH="$tmp/no-pytest-timeout"
  run_test_case "$py" pass 1
  has "HINT: add py3-supported-pytest-timeout (or py3.X-pytest-timeout) to test.environment.contents.packages, or set timeout: 0"
  unset PYTHONPATH
  # The package under test: installed, with files, for this python, and
  # as apk installed it.
  run_test_case "$py" pass 1 --package py3-missing
  has "package 'py3-missing' is not installed (not in $tmp/apkdb)"
  hasnt "UPSTREAM-TESTS demo: pass="
  run_test_case "$py" pass 1 --package py3-supported-demo
  has "apk lists no files for 'py3-supported-demo' (a metapackage?)"
  run_test_case "$py" pass 1 --package py3.99-demo
  has "'py3.99-demo' installs nothing in $vsp (it installs into $tmp/other/usr/lib/python3.99/site-packages); set python"
  before_run='printf "# edited\n" >> "$vsp/demo/__init__.py"'
  run_test_case "$py" pass 1
  has "1 installed file(s) of 'py3-demo' differ from the apk database (modified before the run): $vsp/demo/__init__.py"
  before_run='rm "$vsp/demo-1.0.dist-info/METADATA"'
  run_test_case "$py" pass 1
  has "1 file(s) of 'py3-demo' listed in $tmp/apkdb are missing: $vsp/demo-1.0.dist-info/METADATA"
  before_run=""
  run_test_case "$py" pass 1 --test-tree "$tmp/nonexistent"
  has "test-tree '$tmp/nonexistent' does not exist"
  run_test_case "$py" pass 1 --destdir /tmp
  has "destdir is for stage build"

  # Usage errors: exit 1 before running anything.
  nobase="message names both stages (contract 5)"
  run_case "$py" pass 1 --stage bogus
  has "stage must be build or test (got 'bogus')"
  nobase=""
  run_case "$py" pass 1 --extensions requried
  has "extensions must be auto, required or none (got 'requried')"
  run_case "$py" pass 1 --network none
  has "unknown option '--network'"
  run_case "$py" pass 1 --runner nose
  has "runner must be pytest or unittest (got 'nose')"
  nobase="--package is for stage test (contract 5)"
  run_case "$py" pass 1 --package py3-foo
  has "--package and --test-tree are for stage test"
  run_case "$py" pass 1 --test-tree .
  has "--package and --test-tree are for stage test"
  nobase="message no longer names --package and --test-tree (contract 5)"
  run_case "$py" pass 1 --isolate false
  has "--import-from and --isolate are reserved"
  nobase=""
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
