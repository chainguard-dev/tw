# AGENTS.md

tw is Chainguard's package testing toolkit. It builds one melange package,
`tw`, whose subpackages are the tools, and it holds the melange pipelines
under `pipelines/*/tw/` that call them. Releases copy both into stereo,
where package tests in `os`, `enterprise-packages` and `extra-packages` use
them.

## Layout

- `melange.yaml`: the `tw` package and one subpackage per tool.
- `<tool>/`: one directory per tool, each with a `Makefile` providing
  `build`, `test` and `melange-install`. Go tools carry a `go.mod` and are
  listed in `go.work`; the rest are POSIX shell scripts.
- `tw/`: the `tw` binary, installed with a symlink per multi-call command,
  and its commands under `tw/pkg/commands/`. `twt` is its compiled test
  binary, shipped as its own subpackage.
- `pipelines/test/tw/`: test pipelines, indexed in `pipelines/test/tw/INDEX.md`.
  `pipelines/py/tw/` holds Python build pipelines.
- `tests/suites/`: declarative pipeline tests run against real Wolfi packages
  by `tests/runner/`. `tests/manual/`: full melange files for cases that need
  synthetic package contents. See `tests/README.md`.
- `tools/release-to-stereo`: the release script.
- `.chainguard/ci/`: Chainguard Checks CI. `.chainguard/deps.yaml`: the deps
  agent. `.chainguard/reviewers.yaml`: the code reviewer.

## Commands

```sh
make lint            # yam over every YAML file, shellcheck over every tracked shell script
make build           # melange build of melange.yaml into ./packages
make test-projects   # each tool's `make test`
make test-melange    # melange test of melange.yaml
make test-pipelines  # tests/manual and tests/suites
make yam             # reformat YAML in place
```

`make build` and the melange tests use melange's bubblewrap runner and build
for the host architecture. Run `make lint` and the test targets your change
reaches before opening a pull request; Checks runs all of them on amd64 and
arm64.

## Conventions

- Shell scripts start `#!/bin/sh` and must run under busybox ash: no
  bashisms, no GNU-only flags. They run in package test environments that may
  have nothing but busybox and the declared dependencies. A script that needs
  bash says `#!/usr/bin/env bash`, as `syspeek-tool/syspeek` does.
- Every command a tool or pipeline runs is declared: in the subpackage's
  `dependencies.runtime` in `melange.yaml`, or the pipeline's
  `needs.packages`.
- A check fails by exit status. An empty input, a missing file or a missing
  command is an error, never a pass.
- Pipeline inputs are an interface stereo depends on. Add inputs as optional
  with a default that keeps today's behaviour; don't rename or remove one.
- Commits follow Conventional Commits, scoped by tool:
  `feat(soversion-check): ...`, `fix(ci): ...`, `build(deps): ...`.

## Adding a tool or pipeline

1. Create `<tool>/` with its `Makefile`, and for Go add it to `go.work`.
2. Add a subpackage to `melange.yaml` that runs
   `make -C <tool> MELANGE_CONTEXTDIR=${{targets.contextdir}} melange-install`,
   with its runtime dependencies and a test.
3. Add the pipeline under `pipelines/test/tw/`, with `needs.packages` naming
   the subpackage, and a row in `INDEX.md`.
4. Add tests: the tool's own `make test`, and a suite in `tests/suites/` with
   at least one package that should pass and one that should fail. Use
   `tests/manual/` only when no real Wolfi package exercises the case.

## Releasing

Tag `main` with a lightweight tag and push it, then run
`./tools/release-to-stereo vX.Y.Z <stereo checkout>`, which commits the
release into the stereo checkout for a pull request there. The script rewrites
`melange.yaml` as it copies it: `version` and `epoch` must stay in the first
10 lines, the `#wolfi#`-prefixed `git-checkout` block is uncommented, and
`expected-commit` is set to the tag. A signed or annotated tag gives
`expected-commit` the tag object's SHA instead of the commit's, so tag with
`git tag --no-sign`.

## CI and dependencies

Chainguard Checks runs `.chainguard/ci/c-i.yaml` on pull requests and pushes
to main; `Chainguard Checks (presubmit)` is required. A test that needs a new
host or package needs it added there: checks run with allowlisted egress and
only the packages the environment lists. The deps agent opens Go module
updates on `chainguard/deps/go`.
