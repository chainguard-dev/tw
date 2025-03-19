#!/usr/bin/env sht

# Define test functions with `shtest_`
shtest_success() {
    echo "this is going to pass"
}

# Test failures occur with any non-zero exit code
shtest_fail() {
    echo "this test is going to fail"
    cat donkey
}
