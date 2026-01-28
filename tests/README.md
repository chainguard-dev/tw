# Pipeline Test Suite

This directory contains test melange files for validating the pipeline checks located in `../pipelines/test/tw/`. for this test to work we need to run `make build` for getting the latest version of the pipeline and projects locally.

## Purpose

These tests ensure that:
1. Pipeline validators correctly accept valid packages (positive tests)
2. Pipeline validators correctly reject invalid packages (negative tests)
3. Changes to pipelines don't break existing functionality
4. Both synthetic test packages and real Wolfi packages are validated

## Writing Pipeline Tests

### File Structure

Each test file should follow this structure:

```yaml
package:
  name: <pipeline-name>-test    # Must match filename without .yaml
  version: "0.0.0"               # Always use 0.0.0
  epoch: 0
  description: Test for <pipeline-name> pipeline validation

environment:
  contents:
    packages:
      - wolfi-base     # REQUIRED: Provides /bin/sh and basic utilities

pipeline:
  # Main package should NOT be tested - just use a log line
  - runs: |
      echo "Test package for <pipeline-name> validation"

subpackages:
  # All test scenarios go here as subpackages
  - name: test-scenario-1
    # ... test definition
```

### Critical Configuration Rules

#### 1. Always Use Version `0.0.0`
```yaml
package:
  version: "0.0.0"
  epoch: 0
```

**Why:** Using `0.0.0` ensures test packages never conflict with real packages and clearly indicates these are test-only packagese.

#### 3. Don't Test the Main Package
```yaml
pipeline:
  - runs: |
      echo "Test package for docs validation"
```

**Why:** The main package is just a container for subpackages. Put all test scenarios in subpackages to keep tests organized and focused.

#### 4. Use Subpackages for All Test Scenarios
```yaml
subpackages:
  - name: positive-test-1
    # Valid package that should pass
  
  - name: negative-test-1
    # Invalid package that should fail
```

**Why:** Each subpackage tests one specific scenario, making it easy to identify which test failed and why.

### Testing Real Wolfi Packages

You can test real Wolfi packages by using their exact name as a subpackage name:

```yaml
subpackages:
  # This tests the REAL giflib-doc package from Wolfi
  - name: giflib-doc
    description: Test real giflib-doc package from Wolfi
    pipeline:
      - runs: echo "Testing giflib-doc from Wolfi"
    test:
      pipeline:
        - uses: test/tw/docs
```

**How it works:**
1. Your test package declares a subpackage named `giflib-doc`
2. Because of `version: 0.0.0`, Wolfi's `giflib-doc` takes precedence
3. The pipeline test runs against the real Wolfi package
4. Your build step (`echo ...`) is a no-op since Wolfi's package is used

**Benefits:**
- Tests pipeline validators against real-world packages
- Catches issues with actual package structures
- Validates that checkers work with production packages

### Writing Positive Tests

Positive tests validate that valid packages pass the pipeline check:

```yaml
subpackages:
  # positive manual test contains only static libs
  - name: contains-only-static
    description: "Positive test: Valid static package from Wolfi (contains *.a libraries)"
    pipeline:
      - runs: |
          # create a directory for static libraries
          mkdir -p ${{targets.subpkgdir}}/usr/lib/
          # create a static libraries in the lib directory
          touch ${{targets.subpkgdir}}/usr/lib/libexample.a
    test:
      pipeline:
        - uses: test/tw/staticpackage

```

**Key points:**
- Create realistic package content
- Use the pipeline directly with `uses: test/tw/<pipeline-name>`
- No special test logic needed - the pipeline should succeed

### Writing Negative Tests

Negative tests validate that invalid packages are correctly rejected:

```yaml
subpackages:
  # negative manual test contains static + other libs
  - name: contains-static-and-more
    description: "Negative test: Invalid static package from Wolfi (contains *.so libraries)"
    pipeline:
      - runs: |
          # create a directory for static libraries
          mkdir -p ${{targets.subpkgdir}}/usr/lib/
          # create a static libraries in the lib directory
          touch ${{targets.subpkgdir}}/usr/lib/libexample.so
          # create a shared library in the lib directory
          touch ${{targets.subpkgdir}}/usr/lib/libexample.so.1
    test:
      environment:
        contents:
          packages:
            - package-type-check  # Needed for manual invocation
      pipeline:
        - name: Verify pipeline correctly rejects invalid package
          runs: |
            set +e  # CRITICAL: Don't exit on command failure
            output=$(package-type-check static "${{context.name}}" 2>&1)
            result=$?
            echo "=== Output from package-type-check ==="
            echo "$output"
            echo "=== Exit code: $result ==="
            if [ $result -eq 0 ]; then
              echo "FAIL: Pipeline should have rejected non-static package (glibc)" >&2
              exit 1
            fi
            echo "PASS: Pipeline correctly rejected non-static Wolfi package"
```

**Critical requirements for negative tests:**

#### 1. Always Use `set +e`
```bash
set +e  # Allow commands to fail without exiting
```

**Why:** By default, shell scripts exit immediately when a command fails. Negative tests expect the pipeline checker to fail, so `set +e` allows the script to continue and validate the failure.

#### 2. Capture and Display Output
```bash
output=$(package-type-check docs "${{targets.subpkgname}}" 2>&1)
result=$?
echo "=== Output from package-type-check ==="
echo "$output"
echo "=== Exit code: $result ==="
```

**Why:** Displaying the checker's output helps debug when tests fail unexpectedly and documents what the checker reported.

#### 3. Validate the Failure
```bash
if [ $result -eq 0 ]; then
  echo "FAIL: Pipeline should have rejected this package" >&2
  exit 1
fi
echo "PASS: Pipeline correctly rejected invalid package"
```

**Why:** The test succeeds when the pipeline checker fails (non-zero exit code).

#### 4. Add tw bin used to Environment
```yaml
test:
  environment:
    contents:
      packages:
        - package-type-check # or gem-check, header-check etc
```

**Why:** Negative tests invoke the checker manually, so it must be available in the test environment.

## Running Tests

Run all pipeline tests:
```bash
make test-pipelines
```

## Common Mistakes to Avoid

### 1. Forgetting `set +e` in Negative Tests
**Problem:** Script exits immediately when checker fails, test never validates the failure

### 2. Not Capturing Output in Negative Tests
**Problem:** Can't debug why test failed
