# ldd-check

This is called to run `ldd` on package contents or files.

Use this to check binaries and `.so` files to ensure all of their requirements
are met in the package.

## Usage

In the package `.yaml`:

```yaml
test:
  pipeline:
    - uses: test/tw/ldd-check
```
