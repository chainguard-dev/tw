#!/bin/sh
# Sync check against stereo: render the inline `runs:` block of a
# pytest-core.yaml (inputs substituted the way melange does) and run it and
# tw-upstream-pytest on every fixture with the same inputs. `same` cases
# must give the same output and exit status, apart from durations, scratch
# paths and the plugin's line numbers in tracebacks. `differs` cases are the documented, intended differences
# from stereo 87239accdf8 (see the SYNC POINT comment in
# tw-upstream-pytest): their diff is shown, and one that stops differing is
# an error, so that this list stays accurate.
#
# Usage: tests/equivalence.sh path/to/pytest-core.yaml python
#
# Given a slim core that calls tw-upstream-pytest (with it on PATH), it
# checks the slim core against the tool run directly instead.

set -u

if [ $# -ne 2 ]; then
  echo "Usage: $0 path/to/pytest-core.yaml python" >&2
  exit 1
fi
core=$1 py=$2
here=$(cd "$(dirname "$0")" && pwd)
tool="$here/../tw-upstream-pytest"
export UT_LIB="$here/../lib"

tmp=$(mktemp -d "${TMPDIR:-/tmp}/tw-upstream-pytest-equiv.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
export UT_SCRATCH_BASE="$tmp/scratch"

# The runs: block, dedented.
awk '/^    runs: \|$/ { on = 1; next } on { sub(/^      /, ""); print }' "$core" > "$tmp/core.sh"
[ -s "$tmp/core.sh" ] || { echo "ERROR: no runs: block in $core" >&2; exit 1; }

# render NAME=VALUE...: substitute ${{inputs.NAME}} (defaults below) and
# ${{context.name}} into the runs: block, as melange does.
render() {
  "$py" - "$tmp/core.sh" "$@" <<'EOF'
import sys
s = open(sys.argv[1]).read()
inputs = {"stage": "build", "caller": "upstream-tests/pytest", "python": "DEFAULT", "dir": ".",
          "paths": "", "args": "", "markers": "", "ignore": "", "ignore-files": "",
          "import-mode": "auto", "clear-addopts": "false", "jobs": "1", "timeout": "600",
          "overall-timeout": "3600", "runner": "pytest", "extensions": "auto",
          "min-run-percent": "5", "package": "", "test-tree": ""}
for a in sys.argv[2:]:
    k, v = a.split("=", 1)
    inputs[k] = v
for k, v in inputs.items():
    s = s.replace("${{inputs.%s}}" % k, v)
s = s.replace("${{context.name}}", "demo")
s = s.replace("${{package.name}}", "demo")
s = s.replace("/var/cache/melange/upstream-tests-pytest", "$UT_SCRATCH_BASE")
if "${{" in s:
    sys.exit("unsubstituted: " + s[s.index("${{"):][:60])
print(s)
EOF
}

npass=0 nfail=0 nintended=0
norm() {
  sed -e 's/duration=[0-9]*s/duration=Ns/' \
      -e "s#$tmp/[^ :'\"]*#TMP#g" \
      -e 's/run\.[A-Za-z0-9]\{6\}/run.X/g' \
      -e 's/ in [0-9.]*s / in Ns /' -e 's/ in [0-9.]*s$/ in Ns/' \
      -e 's/zz-upstream-tests-pytest-[0-9]*\.pth/zz.pth/' \
      -e 's/upstream_tests_pytest\.py:[0-9]*/upstream_tests_pytest.py:N/g'
}

# setup_ws: a fresh workspace with $fx's tree, its demo/ installed into
# the destdir's site-packages (the same paths for both runs).
setup_ws() {
  ws="$tmp/ws"
  rm -rf "$ws"
  mkdir -p "$ws"
  cp -R "$here/fixtures/$fx/." "$ws/"
  dest="$ws/melange-out/demo"
  sp=$("$py" -c 'import site; print(site.getsitepackages()[0])')
  mkdir -p "$dest$sp"
  inst="$here/fixtures/$fx/demo"
  [ -d "$here/fixtures/$fx/.installed/demo" ] && inst="$here/fixtures/$fx/.installed/demo"
  cp -R "$inst" "$dest$sp/"
}

# run_both FIXTURE [input=value ...]: run the inline block and the tool;
# sets irc, trc and the normalized outputs in $tmp/{inline,tool}.norm.
# EQ_ENV (one NAME=VALUE, or empty) is set for both runs.
run_both() {
  fx=$1
  shift
  setup_ws
  render destdir="$dest" python="$py" "$@" > "$tmp/inline.sh" || return 1
  (cd "$ws" && env ${EQ_ENV:+"$EQ_ENV"} sh -e "$tmp/inline.sh") > "$tmp/inline.out" 2>&1
  irc=$?
  setup_ws
  set -- destdir="$dest" python="$py" "$@"
  for kv; do
    shift
    set -- "$@" "--${kv%%=*}" "${kv#*=}"
  done
  (cd "$ws" && env ${EQ_ENV:+"$EQ_ENV"} "$tool" --name demo --stage build --caller upstream-tests/pytest "$@") > "$tmp/tool.out" 2>&1
  trc=$?
  norm < "$tmp/inline.out" > "$tmp/inline.norm"
  norm < "$tmp/tool.out" > "$tmp/tool.norm"
}

alike() { [ "$irc" -eq "$trc" ] && cmp -s "$tmp/inline.norm" "$tmp/tool.norm"; }

# same FIXTURE [input=value ...]: the outputs must match.
same() {
  run_both "$@" || { nfail=$((nfail + 1)); return; }
  if alike; then
    echo "  [SAME] $fx: same output, exit $trc"
    npass=$((npass + 1))
  else
    echo "  [FAIL] $fx: exit inline=$irc tool=$trc"
    diff "$tmp/inline.norm" "$tmp/tool.norm" | sed 's/^/      | /'
    nfail=$((nfail + 1))
  fi
}

# differs WHY FIXTURE [input=value ...]: an intended difference; the
# outputs must differ.
differs() {
  why=$1
  shift
  run_both "$@" || { nfail=$((nfail + 1)); return; }
  if alike; then
    echo "  [FAIL] $fx: listed as an intended difference ($why), but the outputs match; drop it from the list"
    nfail=$((nfail + 1))
  else
    echo "  [INTENDED] $fx: $why (exit inline=$irc tool=$trc)"
    diff "$tmp/inline.norm" "$tmp/tool.norm" | sed 's/^/      | /'
    nintended=$((nintended + 1))
  fi
}

EQ_ENV=""
same pass
differs "ignore: no --deselect in the echoed command" \
  neg "ignore=tests/test_neg.py::test_fail tests/test_neg.py::test_nonexistent"
same neg
same shadow
same netprobe
same allskip
same mostlyskip
same mostlyskip min-run-percent=0
same empty
same collect
differs "the echoed command line shows the parsed args words, shell-quoted" \
  select 'args=-k "alpha or beta"'
same unittest runner=unittest
same pass dir=nonexistent
differs "ignore deselects test_x only, not test_x_regression (pytest --deselect matched the prefix)" \
  ignoreprefix "ignore=tests/test_prefix.py::test_x"
differs "runner unittest rejects ignore, ignore-files, markers, jobs and timeout" \
  unittest runner=unittest "ignore=tests/test_ut.py::DemoTest::test_add"
differs "extensions is validated" \
  pass extensions=requried
differs "the plugin takes src/demo, not docs/demo, as the source root" \
  srclayout
mkdir -p "$tmp/no-pytest-timeout"
echo 'raise ImportError("hidden by tests/equivalence.sh")' > "$tmp/no-pytest-timeout/pytest_timeout.py"
EQ_ENV="PYTHONPATH=$tmp/no-pytest-timeout"
differs "a non-zero timeout needs pytest-timeout (no faulthandler_timeout fallback)" \
  pass "args=-p no:timeout"
EQ_ENV=""
# Not exercised here: the tool has no --network option (the pipeline has
# no such input), and also removes its scratch space on INT/TERM.

echo "equivalence: $npass same, $nintended intended differences, $nfail unexpected"
[ "$nfail" -eq 0 ]
