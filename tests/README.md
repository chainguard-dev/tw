# Pipeline Test Suite

This directory contains all pipeline validation tests for the `test/tw/` pipelines defined in `../pipelines/test/tw/`.

## Directory Structure

```
tests/
├── suites/                    # Declarative test definitions (consumed by runner)
│   ├── docs.yaml
│   ├── contains-files.yaml
│   ├── emptypackage.yaml
│   └── metapackage.yaml
├── manual/                    # Hand-written melange YAML (synthetic packages)
│   └── header-check.yaml
├── runner/                    # Test runner Go source (infrastructure)
│   ├── main.go
│   ├── go.mod
│   └── go.sum
├── .out/                      # ALL ephemeral artifacts (gitignored)
│   ├── generated/             #   auto-generated melange configs
│   └── packages/              #   built test packages
├── README.md
└── .gitignore
```

## Two Flavors of Test

### 1. Suite Tests (declarative, auto-generated)

Suite tests live in `suites/` and use a simple declarative YAML format. The test runner reads these definitions, auto-generates melange configs, executes them, and validates results. This is the **default and preferred way** to test pipelines.

Each file maps 1:1 to a pipeline under `pipelines/test/tw/`. For example, `suites/docs.yaml` tests the `test/tw/docs` pipeline.

```yaml
name: Docs pipeline validation tests
description: Test suite for test/tw/docs pipeline

testcases:
  - name: Valid docs package giflib-doc
    package: giflib-doc
    pipelines:
      - uses: test/tw/docs
    expect_pass: true

  - name: Invalid docs package bash (binary package)
    package: bash
    pipelines:
      - uses: test/tw/docs
    expect_pass: false
```

**When to use:** For any test that validates a pipeline against a real Wolfi package. This covers the vast majority of cases.

See the [runner README](runner/README.md) for full details on the test case format, CLI options, and how the runner works.

### 2. Manual Tests (hand-written melange YAML)

Manual tests live in `manual/` and are full melange YAML files with subpackages that create synthetic package content. These are built with `melange build` and then tested with `melange test`.

**When to use:** Only when you need to create synthetic packages with specific file layouts that don't exist in Wolfi. For example, testing `header-check` with deliberately malformed headers, or testing edge cases that require precise control over package contents.

See [Writing Manual Tests](#writing-manual-tests) below for the format.

## Running Tests

```bash
# Run all pipeline tests (both suite and manual)
make test-pipelines

# Run only suite tests (declarative)
make test-suite

# Run only manual tests (hand-written)
make test-manual

# Run all tests (melange + projects + pipelines)
make test-all
```

### Prerequisites

Tests require:
- `melange` binary in your PATH
- A signing key (auto-generated via `make build` if missing)
- A built `tw` package (`make build` handles this)

## Adding a New Suite Test

1. Create `suites/<pipeline-name>.yaml` matching the pipeline you're testing:

```yaml
name: <Pipeline> pipeline validation tests
description: Test suite for test/tw/<pipeline-name> pipeline

testcases:
  - name: Positive test - <describe what should pass>
    description: Verify pipeline passes for <reason>
    package: <real-wolfi-package>
    pipelines:
      - uses: test/tw/<pipeline-name>
    expect_pass: true

  - name: Negative test - <describe what should fail>
    description: Verify pipeline fails for <reason>
    package: <real-wolfi-package>
    pipelines:
      - uses: test/tw/<pipeline-name>
    expect_pass: false
```

2. Run `make test-suite` to verify.

### Test Case Fields

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Descriptive name for the test case |
| `description` | No | Detailed explanation |
| `package` | Yes | Real Wolfi package to test against |
| `pipelines` | Yes | List of pipelines to apply (`uses` + optional `with`) |
| `expect_pass` | Yes | `true` for positive tests, `false` for negative tests |
| `test-dependencies` | No | Additional packages needed at test time |

### Guidelines

- **Use real Wolfi packages** — tests run against actual packages, not synthetic ones
- **One package per test case** — keeps the 1:1 mapping clear for debugging
- **Always include both positive and negative cases** — verify accept and reject behavior
- **Name files after the pipeline** — `suites/docs.yaml` for `test/tw/docs`

## Adding a New Manual Test

Only add manual tests when suite tests can't cover the scenario (e.g., you need synthetic package content).

1. Create `manual/<tool-name>.yaml` as a full melange YAML file:

```yaml
package:
  name: <tool-name>-test
  version: "0.0.0"
  epoch: 0
  description: Manual tests for <tool-name> pipeline edge cases

environment:
  contents:
    packages:
      - busybox
      - wolfi-base

pipeline:
  - runs: |
      echo "Manual edge case tests for <tool-name> pipeline"

subpackages:
  - name: <tool-name>-test-<scenario>
    description: "<Positive/Negative> test: <describe scenario>"
    pipeline:
      - runs: |
          # Create synthetic package content
          mkdir -p ${{targets.contextdir}}/usr/...
    test:
      pipeline:
        - uses: test/tw/<pipeline-name>
```

2. Run `make test-manual` to verify.

### Writing Manual Tests

#### Critical Rules

1. **Always use version `0.0.0`** — ensures test packages never conflict with real packages.

2. **Use subpackages for all scenarios** — the main package is just a container. Each subpackage tests one scenario.

3. **For negative tests, use `set +e`** — prevents the script from exiting when the checker command fails:

```bash
set +e
output=$(some-check --packages="${{context.name}}" 2>&1)
result=$?
echo "$output"
if [ $result -eq 0 ]; then
  echo "FAIL: Should have been rejected" >&2
  exit 1
fi
echo "PASS: Correctly rejected"
```

4. **Add tool binaries to test environment** — negative tests invoke checkers manually:

```yaml
test:
  environment:
    contents:
      packages:
        - header-check  # or gem-check, package-type-check, etc.
```

## Build Artifacts

All build artifacts are written to `tests/.out/` (gitignored):

- `.out/generated/` — auto-generated melange configs from suite tests
- `.out/packages/` — built packages from manual tests

Run `make clean` to remove all artifacts.