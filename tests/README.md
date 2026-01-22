# Pipeline Test Suite

This directory contains test melange files for validating the pipeline checks located in `../pipelines/test/tw/`.

## Purpose

These tests ensure that:
1. The local pipeline implementations work correctly with the built packages
2. Both positive tests (should pass) and negative tests (should fail) are validated
3. Changes to pipelines don't break existing functionality

## Structure

Each test file follows this pattern:
- **Filename**: `<pipeline-name>-test.yaml` (must match main package name)
- **Main package**: Tests the primary use case for the pipeline
- **Subpackages**: Test various scenarios including:
  - Valid positive cases (should pass the pipeline check)
  - Invalid negative cases (should fail the pipeline check)

## Key Design Principles

### 1. Version and Priority
```yaml
package:
  name: pipeline-test
  version: "0.0.1"
  epoch: 0
  dependencies:
    provider-priority: 0  # Ensures Wolfi packages take precedence
```

Using `provider-priority: 0` ensures that when runtime dependencies are needed from Wolfi, those packages are preferred over our test packages.

### 2. Local Pipeline Resolution

Pipelines are referenced as:
```yaml
test:
  pipeline:
    - uses: test/tw/staticpackage
```

Melange resolves these from the local `pipelines/test/tw/` directory first, ensuring you're testing the latest changes before they're released.

### 3. Positive and Negative Tests

**Positive tests** validate that valid packages pass:
```yaml
subpackages:
  - name: staticpackage-test-valid
    pipeline:
      - runs: |
          # Create valid static library
          ar rcs ${{targets.contextdir}}/usr/lib/libtest.a /tmp/test.o
    test:
      pipeline:
        - uses: test/tw/staticpackage
```

**Negative tests** validate that invalid packages are correctly rejected:
```yaml
subpackages:
  - name: staticpackage-test-invalid
    pipeline:
      - runs: |
          # Create invalid content (e.g., .so file in static package)
          touch ${{targets.contextdir}}/usr/lib/libtest.so
    test:
      pipeline:
        - name: Verify pipeline correctly rejects invalid package
          runs: |
            set +e
            package-type-check static "${{targets.subpkgname}}" 2>&1
            result=$?
            if [ $result -eq 0 ]; then
              echo "FAIL: Pipeline should have rejected invalid package" >&2
              exit 1
            fi
            echo "PASS: Pipeline correctly rejected invalid package"
```

## Test Files

| Test File | Pipeline Tested | Description |
|-----------|----------------|-------------|
| `staticpackage-test.yaml` | `test/tw/staticpackage` | Validates static library packages (.a files only) |
| `docs-test.yaml` | `test/tw/docs` | Validates documentation packages (man pages, info files, etc.) |
| `emptypackage-test.yaml` | `test/tw/emptypackage` | Validates empty packages (no files installed) |
| `metapackage-test.yaml` | `test/tw/metapackage` | Validates meta packages (dependencies only, no files) |
| `devpackage-test.yaml` | `test/tw/devpackage` | Validates dev packages (*-dev, *-devel with headers) |
| `debugpackage-test.yaml` | `test/tw/debugpackage` | Validates debug packages (*-dbg, *-debug with debug symbols) |
| `virtualpackage-test.yaml` | `test/tw/virtualpackage` | Validates virtual package provides |
| `byproductpackage-test.yaml` | `test/tw/byproductpackage` | Validates byproduct/split packages |
| `contains-files-test.yaml` | `test/tw/contains-files` | Validates file existence checks |

## Running Tests

### Run all pipeline tests:
```bash
make test-pipelines
```

### Run a specific test:
```bash
melange build --runner docker test/staticpackage-test.yaml \
  --debug \
  --arch=$(uname -m) \
  --keyring-append=local-melange.rsa.pub \
  --repository-append=./packages \
  --signing-key=local-melange.rsa \
  --out-dir=./packages

melange test --runner docker test/staticpackage-test.yaml \
  --debug \
  --arch=$(uname -m) \
  --keyring-append=local-melange.rsa.pub \
  --repository-append=./packages \
  --test-package-append=wolfi-base
```

### CI Integration

Pipeline tests are automatically run in CI after the main build and test steps:
```yaml
- name: Test pipeline validations
  run: |
    make test-pipelines
```

## Adding New Pipeline Tests

When adding a new pipeline to `pipelines/test/tw/`, create a corresponding test file:

1. Create `test/<pipeline-name>-test.yaml`
2. Set `package.name` to match the filename (without `.yaml`)
3. Set `version: "0.0.1"` and `provider-priority: 0`
4. Create the main package to test the primary use case
5. Add subpackages for edge cases:
   - At least one positive test (should pass)
   - At least one negative test (should fail, with explicit validation)
6. Run `make test-pipelines` locally to verify

## Troubleshooting

### Test builds but pipeline doesn't use local version
- Ensure you've run `make build` first to build the latest `tw` package
- The pipelines reference `needs.packages: package-type-check` which must be available

### Negative test passes when it should fail
- Make sure you're using `set +e` to continue on error
- Check that you're testing the exit code correctly
- Verify the package-type-check command is being called with the right arguments

### Permission errors in Docker
- Ensure Docker has access to the repository path
- The `--runner docker` flag should handle isolation

## Best Practices

1. **Keep tests focused**: Each subpackage should test one specific scenario
2. **Use descriptive names**: `*-valid`, `*-invalid-*` clearly indicate intent
3. **Test both success and failure**: Don't just test the happy path
4. **Document why tests exist**: Add comments for non-obvious test cases
5. **Minimize dependencies**: Only include runtime dependencies actually needed for the test
