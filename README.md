# tw (tee-dub)
tw (pronounced tee-dub) is a centralized repository for testing and building
tools or helpers.

## Release to stereo
To release a version of tw to stereo, run tools/release-to-stereo.

    $ git tag vX.Y.Z
    $ git push origin vX.Y.Z
    $ ./tools/release-to-stereo vX.Y.Z ~/git/cg/chainguard-dev/stereo/

This takes care of updating the `tw.yaml` file from `melange.yaml`,
and syncs the pipeline files for other dirs.

That will do a commit and you just need to push and do a PR.

## Testing

This repository contains three types of tests to ensure quality and correctness:

### 1. Main Package Tests (`test-melange`)
Tests the main `tw` package defined in `melange.yaml`.

```bash
make test-melange
```

This validates that the tw tools package builds and functions correctly.

### 2. Project Tests (`test-projects`)
Tests individual project subdirectories (e.g., `ldd-check`, `package-type-check`, `gosh`, etc.).

```bash
# Run all project tests
make test-projects

# Run a specific project test
make test-project/package-type-check
```

Each project directory contains its own test suite specific to that tool.

### 3. Pipeline Validation Tests (`test-pipelines`)
Tests the pipeline validators located in `pipelines/test/tw/` using test packages in `tests/`.

```bash
make test-pipelines
```

This runs a complete test suite that:
3. Builds all test packages in `tests/*-test.yaml`
4. Runs pipeline validation tests against those packages

**Test files structure:**
- `tests/docs-test.yaml` - Tests the `pipelines/test/tw/docs.yaml` pipeline
- `tests/staticpackage-test.yaml` - Tests the `pipelines/test/tw/staticpackage.yaml` pipeline
- `tests/emptypackage-test.yaml` - Tests the `pipelines/test/tw/emptypackage.yaml` pipeline

Each test file contains:
- **Positive tests**: Valid packages that should pass the pipeline check
- **Negative tests**: Invalid packages that should be rejected by the pipeline

### 4. Run All Tests
Run the complete test suite (all three test types):

```bash
make test-all
```

This executes all tests in sequence: `test-melange` → `test-projects` → `test-pipelines`

### 5. Individual Test Targets
For more granular control:

```bash
make build-pipeline-tests    # Only build test packages
make run-pipeline-tests       # Only run pipeline tests (assumes already built)
```

## Testing locally

To test a tw pipeline in a local stereo repository, you need the following steps:

* Build the tw tools package in this repository, `make build`.
* If required, sync the pipeline yaml to stereo by hand.
* If required, build the melange package with the new pipeline, in the stereo repository.
* If required, test the melange package with the new pipeline, in the stereo repository.

Most likely, you need to tell the melange build in the stereo repository to use the tw index.

A complete example:

```
user@debian:~git/tw $ make build
user@debian:~git/tw $ cp pipelines/test/tw/something.yaml ~/git/stereo/os/pipelines/test/tw/
user@debian:~git/tw $ cd ~/git/stereo/enterprise-packages/
user@debian:~git/stereo/os $ make debug/somepackage
user@debian:~git/stereo/os $ MELANGE_DEBUG_TEST_OPTS="--ignore-signatures --repository-append ~/git/tw/packages" make test-debug/somepackage
```
