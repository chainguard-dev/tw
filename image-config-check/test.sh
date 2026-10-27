#!/bin/sh
# Tests for image-config-check. A fake crane on PATH serves tag lists and
# image configs from fixtures, so no registry is contacted.

set -u

TOOL="$(pwd)/image-config-check"
TMP="$(mktemp -d)"
LOG="$TMP/log"
trap 'rm -rf "$TMP"' EXIT

pass() { echo "  [PASS] $*"; }
fail() { echo "  [FAIL] $*"; cat "$LOG"; exit_code=1; }

exit_code=0
FIXTURES="$TMP/fixtures"
mkdir -p "$TMP/bin" "$FIXTURES/ls" "$FIXTURES/config"

cat > "$TMP/bin/crane" <<'EOF'
#!/bin/sh
key="$(printf '%s' "$2" | tr '/:@' '___')"
case "$1" in
  ls) cat "$FIXTURES/ls/$key" 2>/dev/null ;;
  config) cat "$FIXTURES/config/$key" 2>/dev/null ;;
  *) exit 2 ;;
esac
EOF
chmod +x "$TMP/bin/crane"
export FIXTURES
PATH="$TMP/bin:$PATH"

key() { printf '%s' "$1" | tr '/:@' '___'; }

# tags REPO TAG...: the tags crane ls reports for a repository.
tags() {
  repo="$1"; shift
  printf '%s\n' "$@" > "$FIXTURES/ls/$(key "$repo")"
}

# image REF CREATED CONFIG: the config crane config reports for an image.
image() {
  printf '{"created":"%s","config":%s}\n' "$2" "$3" > "$FIXTURES/config/$(key "$1")"
}

# run ARGS...: runs the tool, recording its output and exit status.
run() {
  "$TOOL" "$@" > "$LOG" 2>&1
  status=$?
}

contains() { grep -qF -- "$1" "$LOG"; }

ENTRY='{"Entrypoint":["/usr/bin/foo"],"WorkingDir":"/data"}'
OLD="2020-01-01T00:00:00Z"
NEW="2026-01-01T00:00:00Z"

# 1) Matching image passes, against the newest release rather than the alias.
tags up/foo 1.0.0 1.2.0 latest
image up/foo:1.2.0 "$NEW" "$ENTRY"
image up/foo:latest "$OLD" '{"Entrypoint":["/stale"]}'
image ours/foo:latest "$NEW" "$ENTRY"
run --image ours/foo:latest --alias up/foo:latest
if [ "$status" -eq 0 ] && contains "PASS[image-config-check]: ours/foo:latest matches up/foo:1.2.0"; then
  pass "match against the newest release, not a stale latest"
else
  fail "expected a match against up/foo:1.2.0 (status $status)"
fi

# 2) A differing field fails and both configs are printed.
image ours/bar:latest "$NEW" '{"Entrypoint":["/usr/local/bin/foo"],"WorkingDir":"/data"}'
run --image ours/bar:latest --alias up/foo:latest
if [ "$status" -eq 1 ] && contains "does not match up/foo:1.2.0" && contains '"/usr/local/bin/foo"' && contains '"/usr/bin/foo"'; then
  pass "mismatch fails and prints both configs"
else
  fail "expected a mismatch (status $status)"
fi

# 3) Missing Entrypoint, Cmd and WorkingDir equal [], [] and /.
tags up/norm 2.0
image up/norm:2.0 "$NEW" '{"Entrypoint":[],"Cmd":[],"WorkingDir":"/"}'
image ours/norm:latest "$NEW" '{}'
run --image ours/norm:latest --alias up/norm
if [ "$status" -eq 0 ]; then
  pass "missing fields normalize to [], [] and /"
else
  fail "expected normalized fields to match (status $status)"
fi

# 4) Release selection ignores a leading v, plain numbers and suffixed tags.
tags up/rel v4.9.1 8.2.1 94079563 9.0.0-alpine 9.0.0-rc1 latest
image up/rel:8.2.1 "$NEW" "$ENTRY"
image ours/rel:latest "$NEW" "$ENTRY"
run --image ours/rel:latest --alias up/rel:latest
if [ "$status" -eq 0 ] && contains "matches up/rel:8.2.1"; then
  pass "release selection picks 8.2.1 over v4.9.1, 94079563 and suffixed tags"
else
  fail "expected up/rel:8.2.1 (status $status)"
fi

# 5) An alias newer than the newest plain release is used instead.
tags up/suffix v1.5.0 v1.34.0-abc latest
image up/suffix:v1.5.0 "$OLD" '{"Entrypoint":["/old"]}'
image up/suffix:latest "$NEW" "$ENTRY"
image ours/suffix:latest "$NEW" "$ENTRY"
run --image ours/suffix:latest --alias up/suffix:latest
if [ "$status" -eq 0 ] && contains "matches up/suffix:latest"; then
  pass "alias newer than the release wins"
else
  fail "expected up/suffix:latest (status $status)"
fi

# 6) A repository without release tags falls back to the alias.
tags up/dated 2026-43 latest
image up/dated:latest "$NEW" "$ENTRY"
image ours/dated:latest "$NEW" "$ENTRY"
run --image ours/dated:latest --alias up/dated:latest
if [ "$status" -eq 0 ] && contains "matches up/dated:latest"; then
  pass "no release tags falls back to the alias"
else
  fail "expected up/dated:latest (status $status)"
fi

# 7) Several images pair with the alias of the same name, ignoring -fips.
tags reg/cni 1.0
tags reg/cni-init 1.0
image reg/cni:1.0 "$NEW" '{"Entrypoint":["/app/cni"]}'
image reg/cni-init:1.0 "$NEW" '{"Cmd":["/init/cni-init"]}'
image ours/cni-fips:latest "$NEW" '{"Entrypoint":["/app/cni"]}'
image ours/cni-init:latest "$NEW" '{"Cmd":["/init/cni-init"]}'
image ours/kubectl-cni:latest "$NEW" '{"Entrypoint":["/kubectl"]}'
run --image ours/cni-fips:latest --image ours/cni-init:latest --image ours/kubectl-cni:latest \
  --alias reg/cni:latest --alias reg/cni-init:latest
if [ "$status" -eq 0 ] && contains "ours/cni-fips:latest matches reg/cni:1.0" \
  && contains "ours/cni-init:latest matches reg/cni-init:1.0" \
  && contains "no alias for ours/kubectl-cni:latest; not compared"; then
  pass "several images pair by repository name"
else
  fail "expected per-component pairing (status $status)"
fi

# 8) A single image always uses the first alias, whatever its name.
run --image ours/foo:latest --alias up/foo:latest --alias up/other:latest
if [ "$status" -eq 0 ] && contains "matches up/foo:1.2.0"; then
  pass "single image uses the first alias"
else
  fail "expected the first alias (status $status)"
fi

# 9) An upstream image that cannot be read fails.
run --image ours/foo:latest --alias up/missing:latest
if [ "$status" -eq 1 ] && contains "cannot read the config of up/missing:latest"; then
  pass "unreadable upstream fails"
else
  fail "expected a read failure (status $status)"
fi

# 10) Usage errors exit 2.
run --alias up/foo:latest
s1=$status
run --image ours/foo:latest
s2=$status
run --image ours/foo:latest --alias up/foo:latest --bogus
s3=$status
if [ "$s1" -eq 2 ] && [ "$s2" -eq 2 ] && [ "$s3" -eq 2 ]; then
  pass "usage errors exit 2"
else
  fail "expected exit 2 for usage errors (got $s1 $s2 $s3)"
fi

# 11) --help exits 0 and documents the options.
run --help
if [ "$status" -eq 0 ] && contains "--image" && contains "--alias"; then
  pass "--help"
else
  fail "expected --help to exit 0 (status $status)"
fi

exit "$exit_code"
