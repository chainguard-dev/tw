# soversion-check

Checks that a soname-versioned library package ships only shared libraries
whose SONAME version matches the soversion the package name ends in.
`libfoo2` and `libfoo-2` must ship `libfoo.so.2`; `libskarnet2.16` must ship
`libskarnet.so.2.16`.

Use it on library subpackages named from a fixed `vars.soversion`. When an
upstream release changes the SONAME, the check fails and prints the
`vars.soversion` value to set.

## Usage

In the package `.yaml`:

```yaml
subpackages:
  - name: libfoo${{vars.soversion}}
    pipeline:
      - uses: split/lib
    test:
      pipeline:
        - uses: test/tw/soversion-check
```

Run directly:

```shell
soversion-check --packages=libjemalloc2
soversion-check --files=/usr/lib/libfoo.so.2.0.0 --name=libfoo2
```

In `--packages` mode, files that are symlinks, are not ELF, or have no SONAME
are skipped, but a package with no shared library that has a SONAME fails.
