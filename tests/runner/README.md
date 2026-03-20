# Pipeline Test Runner

A Go-based test runner that automatically generates Melange test configurations from declarative test case definitions and executes them via the Melange CLI.

## Overview

This tool eliminates the need to manually write duplicate Melange YAML files for testing pipelines. Instead, you define test cases in a simple YAML format, and the runner:

1. Parses test case definitions from YAML files
2. Validates test suites and test cases
3. Generates Melange test configurations (one per package per test)
4. Executes tests by invoking the `melange` CLI binary
5. Validates results against expectations (positive/negative tests)
6. Provides detailed failure reporting

## Building

```bash
go build -o pipeline-runner .
```

Or via Makefile from the repository root:

```bash
make build-test-runner
```

## Usage

### Run All Tests (Normal Mode)

```bash
./pipeline-runner \
  --test-dir ../suites \
  --pipeline-dir ../../pipelines \
  --arch aarch64 \
  --repositories "/path/to/packages,https://packages.wolfi.dev/os" \
  --keyrings "/path/to/key.pub,https://packages.wolfi.dev/os/wolfi-signing.rsa.pub" \
  --out-dir ../.out/generated \
  --append-packages "wolfi-base"
```

**Normal mode output:**
- Shows test suite progress
- Shows test pass/fail results
- Hides melange command output (clean logs)
- Perfect for CI/CD and daily development

### Run with Debug Mode

```bash
./pipeline-runner \
  --test-dir ../suites \
  --pipeline-dir ../../pipelines \
  --out-dir ../.out/generated \
  --debug \
  [other flags...]
```

**Debug mode output:**
- Shows all normal mode output
- Shows [DEBUG] messages with internal details
- Shows full melange command output in real-time
- Essential for troubleshooting test failures

### Run a Specific Test Suite

```bash
./pipeline-runner \
  --test-dir ../suites \
  --pipeline-dir ../../pipelines \
  --out-dir ../.out/generated \
  --test-suite docs \
  [other flags...]
```

The test suite name comes from the filename (e.g., `docs.yaml` → `docs`).

### Generate Configs Only (No Testing)

```bash
./pipeline-runner \
  --test-dir ../suites \
  --pipeline-dir ../../pipelines \
  --out-dir ../.out/generated \
  --generate-only \
  [other flags...]
```

This generates all melange YAML files in the output directory without executing tests. Useful for:
- Inspecting generated configurations
- Debugging test definitions
- Understanding what will be tested
- Manual testing with melange CLI

### Version Information

```bash
./pipeline-runner --version
```

## Command-Line Options

### Required Flags
- `--test-dir`: Directory containing test suite YAML files (the `suites/` directory)
- `--pipeline-dir`: Directory containing pipeline definitions

### Optional Flags
- `--arch`: Architecture to test (default: `x86_64`)
- `--repositories`: Comma-separated list of package repositories
- `--keyrings`: Comma-separated list of signing key paths
- `--out-dir`: Output directory for generated files (default: `{test-dir}/.out/generated`)
- `--append-packages`: Comma-separated list of packages to add to all test environments
- `--test-suite`: Run only a specific test suite (e.g., `docs`)
- `--generate-only`: Only generate configs without running tests
- `--debug`: Enable debug mode (shows melange output and internal details)
- `--melange`: Path to melange binary (default: `melange`)
- `--version`: Show version information

## Directory Structure

The runner operates within the following layout:

```
tests/
├── suites/                    # Declarative test definitions (consumed by runner)
│   ├── docs.yaml
│   ├── contains-files.yaml
│   ├── emptypackage.yaml
│   └── metapackage.yaml
├── manual/                    # Hand-written melange YAML (synthetic packages)
│   └── header-check.yaml
├── runner/                    # This tool (Go source)
│   ├── main.go
│   ├── go.mod
│   └── go.sum
├── .out/                      # All ephemeral artifacts (gitignored)
│   ├── generated/             #   Auto-generated melange configs
│   └── packages/              #   Built test packages
├── README.md
└── .gitignore
```

- **`suites/`** — The primary input. Each YAML file defines a test suite for one pipeline. File names match pipeline names 1:1 (e.g., `docs.yaml` tests `test/tw/docs`).
- **`manual/`** — Hand-written melange YAML files for edge cases that need synthetic packages (e.g., creating fake headers to test `header-check`). These are run directly via `melange build` + `melange test`, not through the runner.
- **`.out/`** — All build artifacts land here. Gitignored. `make clean` removes it entirely.

## Test Case Format

Test cases are defined in YAML files under `suites/`:

```yaml
name: Test suite name
description: Optional description of the test suite

testcases:
  - name: Test case name
    description: Optional description
    package: package-name
    pipelines:
      - uses: test/tw/docs
      - uses: test/tw/contains-files
        with:
          files: |
            /usr/share/doc
    expect_pass: true
    test-dependencies:
      - build-base
      - git
```

### Test Case Fields

- **name** (required): Descriptive name for the test case
- **description** (optional): Detailed explanation
- **package** (required): Single Wolfi package to test (1:1 mapping)
- **pipelines** (required): List of pipelines to apply
- **expect_pass** (required): Boolean - `true` for positive tests, `false` for negative tests
- **test-dependencies** (optional): Additional packages needed at test time

### Pipeline Configuration

Each pipeline can have:
- **uses** (required): Path to the pipeline (e.g., `test/tw/docs`)
- **with** (optional): Parameters to pass to the pipeline

## Generated Directory Structure

The runner generates configs in separate directories for positive and negative tests:

```
.out/generated/
├── pass-{sanitized-suite-name}/
│   ├── package1.yaml
│   └── package2.yaml
└── fail-{sanitized-suite-name}/
    ├── package3.yaml
    └── package4.yaml
```

Suite names are sanitized for directory names:
- "Docs Pipeline Tests" → `docs-pipeline-tests`
- Lowercase, spaces to hyphens, special chars removed

## Generated Melange Configurations

For each test case + package combination, the runner generates:

```yaml
# Auto-generated melange test file
# DO NOT EDIT - Generated by pipeline-runner

package:
  name: package-name
  version: 0.0.0
  epoch: 0
  description: Test case description

environment:
  contents:
    packages:
      - wolfi-base

test:
  environment:
    contents:
      packages:
        - wolfi-base
        # Packages from --append-packages
        # Packages from test-dependencies
  pipeline:
    - uses: test/tw/docs
    - uses: test/tw/contains-files
      with:
        files: |
          /usr/share/doc
```

Key features:
- **Auto-generated header**: Clearly marked as generated
- **Package name**: Matches the package being tested (1:1 mapping)
- **Version**: Always `0.0.0` (not built, only tested)
- **Base environment**: Includes `wolfi-base` by default
- **Test dependencies**: Combines global `--append-packages` with test-specific dependencies
- **Deterministic output**: Packages sorted alphabetically for reproducible builds

## Logging Modes

### Normal Mode (Default)

Shows clean, focused output:

```
Found 4 test suite files
Processing test suite: tests/suites/docs.yaml
Test Suite: Docs pipeline validation tests
  ✓ Generated positive test: tests/.out/generated/pass-docs-pipeline/giflib-doc.yaml
    ✓ PASS: Valid docs package giflib-doc (correctly accepted giflib-doc)
  ✓ Generated positive test: tests/.out/generated/pass-docs-pipeline/curl-doc.yaml
    ✓ PASS: Valid docs package curl-doc (correctly accepted curl-doc)
  ✓ Generated negative test: tests/.out/generated/fail-docs-pipeline/bash.yaml
    ✓ PASS: Invalid docs package bash (correctly rejected bash)

========================================
Test Results:
  Passed: 15
  Failed: 0
  Total:  15
========================================
```

### Debug Mode (--debug)

Shows everything including melange output:

```
Processing test suite: tests/suites/docs.yaml
[DEBUG] Loading test suite from: tests/suites/docs.yaml
Test Suite: Docs pipeline validation tests
[DEBUG] Test suite has 4 test cases
[DEBUG] Processing 2 positive test cases
  ✓ Generated positive test: tests/.out/generated/pass-docs-pipeline/giflib-doc.yaml
[DEBUG]     Package: giflib-doc, Expect pass: true
[DEBUG] Running melange command: melange test --runner docker ...
2026/01/27 17:48:15 INFO melange 0.39.0 with runner docker is testing:
2026/01/27 17:48:15 INFO image configuration:
[... full melange output ...]
    ✓ PASS: Valid docs package giflib-doc (correctly accepted giflib-doc)
```

## Exit Codes

- `0`: All tests passed
- `1`: One or more tests failed or an error occurred

## Test Execution Flow

1. **Discovery**: Scan `suites/` directory for YAML files
2. **Loading**: Parse and validate test suites
3. **Validation**: Check test cases for required fields and pipeline definitions
4. **Separation**: Split tests into positive (expect pass) and negative (expect fail)
5. **Generation**: Create Melange configs in `.out/generated/pass-*/` and `.out/generated/fail-*/` directories
6. **Execution**: Run `melange test` for each generated config
7. **Validation**: Compare results to expectations
8. **Reporting**: Display results and detailed failure information

## Integration with Melange

This runner executes the `melange` CLI binary as a subprocess. This approach:
- Uses the same melange binary as the rest of the build system
- Ensures consistent behavior with manual melange invocations
- Avoids dependency management complexity
- Works with any melange version in PATH
- Supports different runners (docker, bubblewrap, etc.)

In normal mode, the runner captures melange output. In debug mode, stdout/stderr are connected directly for real-time output.

## Failure Reporting

When tests fail, the runner provides detailed information:

```
    ✗ FAIL: Invalid docs package bash
      Package: bash
      Expected: fail
      Actual:   pass
      Config:   tests/.out/generated/fail-docs-pipeline/bash.yaml
      Run:      make test-pipelines-autogen/docs-pipeline

========================================
Test Results:
  Passed: 14
  Failed: 1
  Total:  15

Failing Test Details:
----------------------------------------

1. Invalid docs package bash
   Package:  bash
   Expected: fail
   Actual:   pass
   Config:   tests/.out/generated/fail-docs-pipeline/bash.yaml
   Run:      make test-pipelines-autogen/docs-pipeline

To debug failures:
  1. Check the generated config file listed above
  2. Run the specific test suite with the make command shown
  3. Run melange test directly with --debug for more details
========================================
```

## Context Cancellation

The runner supports graceful cancellation:
- Responds to `Ctrl+C` interrupts
- Checks for context cancellation in loops
- Cleans up properly on interruption

## Validation

The runner validates:
- **Test suites**: Must have name and test cases
- **Test cases**: Must have name, package, and pipelines
- **Pipelines**: Must have `uses` field defined
- **YAML syntax**: Proper structure and types

Validation errors include file paths and specific issues for easy debugging.

## Development

### Building with Version Info

```bash
go build -ldflags="-X main.commit=$(git rev-parse HEAD) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o pipeline-runner .
```

### Running Tests

```bash
# Generate configs to verify correctness
./pipeline-runner --generate-only --test-dir ../suites --pipeline-dir ../../pipelines --out-dir ../.out/generated

# Run a single test suite
./pipeline-runner --test-suite docs --test-dir ../suites --pipeline-dir ../../pipelines --out-dir ../.out/generated

# Run with debug mode to troubleshoot
./pipeline-runner --debug --test-dir ../suites --pipeline-dir ../../pipelines --out-dir ../.out/generated
```

## Best Practices

1. **Use descriptive test names** - Include package name for clarity
2. **One package per test** - Maintains 1:1 mapping for easy debugging
3. **Test both positive and negative cases** - Ensures pipelines correctly accept and reject
4. **Use real Wolfi packages** - Tests actual behavior, not hypothetical scenarios
5. **Add test-dependencies when needed** - Some tests require specific tools
6. **Run in debug mode for troubleshooting** - Shows full melange output
7. **Use `--generate-only` to inspect configs** - Verify generated files before running

## Troubleshooting

### Test fails unexpectedly

1. Generate and inspect the config:
   ```bash
   ./pipeline-runner --generate-only --test-dir ../suites --pipeline-dir ../../pipelines --out-dir ../.out/generated
   cat ../.out/generated/pass-suite-name/package.yaml
   ```

2. Run in debug mode:
   ```bash
   ./pipeline-runner --debug --test-dir ../suites --pipeline-dir ../../pipelines --out-dir ../.out/generated
   ```

3. Run melange directly:
   ```bash
   melange test --debug ../.out/generated/pass-suite-name/package.yaml
   ```

## See Also

- [tests/README.md](../README.md) - Overview of the entire test infrastructure
- Test suite definitions in `tests/suites/`
- Manual test definitions in `tests/manual/`
