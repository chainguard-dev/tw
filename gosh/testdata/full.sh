#!/usr/bin/env gosh

this="works"

setup() {
  echo "setup runs before each test"
}

setup_once() {
  echo "this happens before all the tests"
}

teardown() {
  echo "teardown happens after each test, even on failures. errors in teardown are not caught"
}

teardown_once() {
  echo "this happens after all the tests"
}

# Define test functions with `gt_*`
# @gt:echo "this is a test"
gt_success() {
  echo "this is going to pass"

  printf "tests run with the same shell context as the original script: %s\nand multilines are printed as expected\n" "$this"

  # will won't persist across test runs
  foo="bar"

  # the "gosh" cli (gt) comes with some common assertions so you don't need to
  # remember how to shell properly
  gt assert eq "foo" "$(echo "foo")"
}

# Test failures occur with any non-zero exit code
gt_fail() {
  echo "this test is going to fail"
  echo "${foo:-each test run in different subshells}"

  # exit code will be captured and test will immediately fail
  cat donkey

  echo "this should never be printed"
}

gt_still_run() {
  echo "test execution is serial but independent, failures in the predecessor (gt_fail) will not impact subsequent tests"

  echo "this test will pass"
}
