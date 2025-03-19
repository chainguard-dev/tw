# `sht` (SHell Test)

A minimal "framework" for authoring shell scripts that produce `go test` output.

## Quickstart

```shell
apk add sht
```

### Create a test file (`test.sh`)

```bash
#!/usr/bin/env sht
# ^ sht "wraps" #!/bin/sh (or its bash equivalent)
# before executing, this will be converted to #!/bin/sh (or its bash equivalent)

# Define test functions with `shtest_`
shtest_success() {
    echo "this is going to pass"
}

# everything else "just runs" as normal
echo "your regularly scheduled commands"

# Test failures occur with any non-zero exit code
shtest_fail() {
    echo "this test is going to fail"
    cat donkey
}
```

### Run the test

```bash
$ chmod +x test.sh

$ ./test.sh
=== RUN   TestScript
=== RUN   TestScript/simple.sh
=== RUN   TestScript/simple.sh/shtest_success
    main_test.go:291: [stdout] this is going to pass
    main_test.go:270: -- [shtest_success] finished successfully
=== RUN   TestScript/simple.sh/shtest_fail
    main_test.go:291: [stdout] this test is going to fail
    main_test.go:291: [stderr] cat: donkey: No such file or directory
    main_test.go:273: -- [shtest_fail] finished with exit code 1
--- FAIL: TestScript (0.61s)
    --- FAIL: TestScript/simple.sh (0.60s)
        --- PASS: TestScript/simple.sh/shtest_success (0.02s)
        --- FAIL: TestScript/simple.sh/shtest_fail (0.01s)
FAIL
```

## How it works

`sht` consists of three components:

- **shtr**: The test runner
- **shtc**: The test runner client
- **sht**: A simple wrapper to support OS's without support for `env -S`

On execution of a shell script using the `sht` interpreter, `shtr` "wraps" the
script with the ["shell framework"](./framework.sh.tpl), and then executes it
with `os/exec` using the framework's shbang (`#!/bin/sh`).

The "framework" is mostly shell functions with a very thin amount of go templating
to ensure all the appropriate pipes are configured.

From there, the final rendered script is written to disk and executed by `shtr`.

Communication between the shell script for stdout/stderr, and progress updates
happen over named pipes, which `shtr` and `shtc` orchestrate.

There are no modifications made to the original script, which means all of its
contents are still executed. The only "trickery" done by `sht` is parsing (via
`shfmt`) and explicitly calling all `shtest_*` functions with the appropriate
`set -e` arguments.

Each `shtest_*` function is run serially, in the order it is defined. Tests
`PASS` when the `shtest_*` function exits with a 0 exit code, and tests `FAIL`
and exit the script with a non-zero exit code (`set -e` is automatically wired
up).

## But why?!?

You may hate shell, but sometimes you just want to execute some third party
programs and check some things. For those use cases, wouldn't it also be nice
if you got structured test output and traces for free?
