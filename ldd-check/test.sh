#!/bin/sh

set -u

FILES="/bin/sh /usr/sbin/test"
EXCLUDED_FILES="/bin/sh /usr/sbin/test"
LOG="test.log"
LDD_CHECK="./ldd-check"

pass() {
  echo "  [PASS] ${*}"
}

fail() {
  echo "  [FAIL] ${*}"
}

info() {
  echo "  ${*}"
}

file_contains() {
  string=$1
  file=$2
  while IFS= read -r line; do
    if [ "${line#*"${string}"}" != "$line" ]; then
      return 0
    fi
  done < "$file"
  return 1
}

cleanup() {
  rm $LOG
}

test_exclusion() {
  fail=0

  $LDD_CHECK --files="${FILES}" --exclude-files="${EXCLUDED_FILES}" >$LOG

  # Assert one case per excluded file.
  for file in $EXCLUDED_FILES; do
    expected="${file}: excluded"
    if file_contains "${expected}" "${LOG}"; then
      pass "${expected}"
    else
      fail "${expected}: not found"
      fail=1
    fi
  done

  return $fail
}

main() {
  failed_tests=0

  echo "Test exclusion"
  test_exclusion

  # TODO: other tests here.

  failed_tests=$((failed_tests + $?))
  if [ "$failed_tests" -eq 0 ]; then
    echo "[PASS] All tests passed."
  else
    echo "[FAIL] ${failed_tests} test(s) failed."
    exit 1
  fi
  cleanup
}

main ${@+"$@"}
