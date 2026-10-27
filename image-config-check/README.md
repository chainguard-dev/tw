# image-config-check

Compares the entrypoint, cmd and working directory of images with the newest release of the upstream images they replace, and fails when they differ.

```
image-config-check --image cgr.dev/chainguard/nginx:latest --alias nginx:latest
```

A single `--image` is compared with the first `--alias`. Several images are each compared with the alias of the same repository name, ignoring `-fips`, and images without one are not compared.

The alias's own tag is ignored in favour of the newest release tag in its repository (`X.Y` or `X.Y.Z`, with or without a `v`), since that tag may be a stale `latest`. The alias is used when its image is newer than that release, or when the repository has no release tags.

Entrypoint, Cmd and WorkingDir are read with `crane config` and must be identical. A missing Entrypoint or Cmd counts as empty, and a missing WorkingDir counts as `/`.

The exit status is 0 when every compared image matches, 1 when one differs or cannot be read, and 2 on a usage error. Run `image-config-check --help` for details.
