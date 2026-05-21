#!/bin/sh
# Smoke tests for ver-check.
# Builds a tiny fake binary that reports a fixed version via various
# flag patterns, then asserts ver-check behaves correctly.

set -u

VER_CHECK="./ver-check"
LOG="test.log"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP" "$LOG"' EXIT

pass() { echo "  [PASS] $*"; }
fail() { echo "  [FAIL] $*"; exit_code=1; }
info() { echo "  $*"; }

file_contains() {
  needle="$1"; file="$2"
  while IFS= read -r line; do
    if [ "${line#*"$needle"}" != "$line" ]; then return 0; fi
  done < "$file"
  return 1
}

exit_code=0

# ---- helper: write a fake bin that emits $1 only for the `$2` flag/arg ----
make_fake_bin() {
  name="$1"; emit="$2"; trigger="$3"
  cat > "$TMP/$name" <<EOF
#!/bin/sh
case "\$1" in
  $trigger) printf '%s\n' "$emit" ;;
  *) printf 'unknown flag: %s\n' "\$1" >&2; exit 2 ;;
esac
EOF
  chmod +x "$TMP/$name"
}

PATH="$TMP:$PATH"

# 1) auto-detect picks up `version` subcommand when binary doesn't support --version
make_fake_bin foo-sub "foo 1.2.3" "version"
$VER_CHECK --bins=foo-sub --version=1.2.3 >"$LOG" 2>&1 || true
if file_contains "PASS[ver-check]: 'foo-sub" "$LOG" && file_contains "found: 1.2.3" "$LOG"; then
  pass "auto-detect picks 'version' subcommand"
else
  fail "auto-detect did not pick 'version' subcommand for foo-sub"
  cat "$LOG"
fi

# 2) drift hint fires when binary emits a version-shaped string that doesn't match expected
make_fake_bin foo-drift "foo v1.2.4" "version"
$VER_CHECK --bins=foo-drift --version=1.2.3 >"$LOG" 2>&1 || true
if file_contains "Could not auto-detect" "$LOG" \
   && file_contains "version-shaped" "$LOG" \
   && file_contains "package.version" "$LOG"; then
  pass "drift hint fires when binary reports different version"
else
  fail "drift hint not emitted; log:"
  cat "$LOG"
fi

# 3) no drift hint when no flag produced version-shaped output
make_fake_bin foo-noversion "garbage" "version"
$VER_CHECK --bins=foo-noversion --version=1.2.3 >"$LOG" 2>&1 || true
if file_contains "Could not auto-detect" "$LOG" \
   && ! file_contains "version-shaped" "$LOG"; then
  pass "drift hint correctly absent when no version-shaped output"
else
  fail "drift hint fired incorrectly for non-version output; log:"
  cat "$LOG"
fi

# 4) explicit --version-flag still works (no regression)
make_fake_bin foo-explicit "1.2.3" "version"
$VER_CHECK --bins=foo-explicit --version=1.2.3 --version-flag=version >"$LOG" 2>&1 || true
if file_contains "PASS[ver-check]" "$LOG"; then
  pass "explicit --version-flag still works"
else
  fail "explicit --version-flag regressed; log:"
  cat "$LOG"
fi

exit $exit_code
