#!/bin/sh
# Tests for tw-upstream-pytest: run it against the projects in fixtures/,
# each "installed" into a fake destdir, and check the exit status and the
# lines stereo's logs and parsers rely on. The negative controls are the
# ones the stereo pilot used (py3-six neg/bneg, shadow2, netprobe,
# mostly-skip).
#
# Usage: tests/run.sh [python ...]
# Default: every python3.X on PATH that can import pytest; none is an error.

set -u

here=$(cd "$(dirname "$0")" && pwd)
tool="$here/../tw-upstream-pytest"
# TOOL_SH='busybox sh' (for example) runs the tool under another shell.
TOOL_SH=${TOOL_SH:-}
export UT_LIB="$here/../lib"

if [ $# -eq 0 ]; then
  for p in python3.10 python3.11 python3.12 python3.13 python3.14 python3.15; do
    command -v "$p" >/dev/null 2>&1 || continue
    "$p" -c 'import pytest' 2>/dev/null || continue
    set -- "$@" "$p"
  done
  if [ $# -eq 0 ]; then
    echo "ERROR: no python3.X with pytest found; pass one as an argument" >&2
    exit 1
  fi
fi

tmp=$(mktemp -d "${TMPDIR:-/tmp}/tw-upstream-pytest-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
export UT_SCRATCH_BASE="$tmp/scratch"

npass=0 nfail=0
pass() { echo "  [PASS] $*"; npass=$((npass + 1)); }
fail() { echo "  [FAIL] $*"; nfail=$((nfail + 1)); }

# run_case PY FIXTURE WANT_RC [tool options...]: lay out a workspace with
# the fixture's tree, install its demo/ into destdir's site-packages, run
# the tool from the workspace. Output in $out, status in $rc.
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
  cp -R "$here/fixtures/$fx/demo" "$dest$sp/"
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
  has "ignore entry 'tests/test_neg.py::test_nonexistent' matches no collected test"
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
  has "-k alpha or beta tests"

  run_case "$py" unittest 0 --runner unittest
  has "python -m unittest discover -s tests"
  has "UPSTREAM-TESTS demo: pass=1 fail=0 skip=0"

  # Usage errors: exit 1 before running anything.
  run_case "$py" pass 1 --stage test
  has "stage must be build (got 'test')"
  run_case "$py" pass 1 --extensions requried
  has "extensions must be auto, required or none (got 'requried')"
  run_case "$py" pass 1 --network allow
  has "network must be none; the network guard cannot be turned off (got 'allow')"
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
