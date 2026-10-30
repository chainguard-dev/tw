#!/bin/sh
# Sync check against stereo: render the inline `runs:` block of a
# pytest-core.yaml (inputs substituted the way melange does) and run it and
# tw-upstream-pytest on every fixture with the same inputs; the outputs and
# exit statuses must match, apart from durations and scratch paths.
#
# Usage: tests/equivalence.sh path/to/pytest-core.yaml python

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
          "overall-timeout": "3600", "runner": "pytest", "extensions": "auto", "network": "none"}
for a in sys.argv[2:]:
    k, v = a.split("=", 1)
    inputs[k] = v
for k, v in inputs.items():
    s = s.replace("${{inputs.%s}}" % k, v)
s = s.replace("${{context.name}}", "demo")
s = s.replace("/var/cache/melange/upstream-tests-pytest", "$UT_SCRATCH_BASE")
if "${{" in s:
    sys.exit("unsubstituted: " + s[s.index("${{"):][:60])
print(s)
EOF
}

npass=0 nfail=0
norm() {
  sed -e 's/duration=[0-9]*s/duration=Ns/' \
      -e "s#$tmp/[^ :'\"]*#TMP#g" \
      -e 's/run\.[A-Za-z0-9]\{6\}/run.X/g' \
      -e 's/ in [0-9.]*s / in Ns /' -e 's/ in [0-9.]*s$/ in Ns/' \
      -e 's/zz-upstream-tests-pytest-[0-9]*\.pth/zz.pth/'
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
  cp -R "$here/fixtures/$fx/demo" "$dest$sp/"
}

# compare FIXTURE [input=value ...]
compare() {
  fx=$1
  shift
  setup_ws
  render destdir="$dest" python="$py" "$@" > "$tmp/inline.sh" || { nfail=$((nfail + 1)); return; }
  (cd "$ws" && sh -e "$tmp/inline.sh") > "$tmp/inline.out" 2>&1
  irc=$?
  setup_ws
  set -- destdir="$dest" python="$py" "$@"
  for kv; do
    shift
    set -- "$@" "--${kv%%=*}" "${kv#*=}"
  done
  (cd "$ws" && "$tool" --name demo --stage build --caller upstream-tests/pytest "$@") > "$tmp/tool.out" 2>&1
  trc=$?
  norm < "$tmp/inline.out" > "$tmp/inline.norm"
  norm < "$tmp/tool.out" > "$tmp/tool.norm"
  if [ "$irc" -eq "$trc" ] && cmp -s "$tmp/inline.norm" "$tmp/tool.norm"; then
    echo "  [PASS] $fx: same output, exit $trc"
    npass=$((npass + 1))
  else
    echo "  [FAIL] $fx: exit inline=$irc tool=$trc"
    diff "$tmp/inline.norm" "$tmp/tool.norm" | sed 's/^/      | /'
    nfail=$((nfail + 1))
  fi
}

compare pass
compare neg "ignore=tests/test_neg.py::test_fail tests/test_neg.py::test_nonexistent"
compare neg
compare shadow
compare netprobe
compare allskip
compare empty
compare collect
compare select 'args=-k "alpha or beta"'
compare unittest runner=unittest
compare pass dir=nonexistent

echo "equivalence: $npass same, $nfail different"
[ "$nfail" -eq 0 ]
