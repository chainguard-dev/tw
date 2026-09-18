# daemon-check-output

Starts a daemon, waits for expected log output, optionally runs setup/post
scripts, then terminates the daemon and reports the result. This is the tool
behind the `test/tw/daemon-check-output` melange pipeline; it replaces the
~220-line inline shell that pipeline used to carry.

## Usage

```
daemon-check-output \
    --start-file=FILE \
    --expected-file=FILE \
    [--error-strings-file=FILE] \
    [--setup-file=FILE] \
    [--post-file=FILE] \
    [--timeout=SECONDS]
```

Inputs are passed as files rather than command-line strings so the melange
wrapper can hand user-provided content over via quoted heredocs, avoiding shell
expansion/injection of that content.

- `--start-file` (required): the daemon start command, run via `/bin/sh -c`.
- `--expected-file` (required): newline-separated regex patterns; **all** must
  appear in the daemon's combined stdout/stderr within `--timeout`.
- `--error-strings-file`: newline-separated regex patterns; **any** match fails
  the check. Empty/absent disables error checking.
- `--setup-file`: commands run before the daemon starts. A failure aborts before
  the daemon is started. `#!/bin/sh -ex` is prepended if there is no shebang.
- `--post-file`: commands run after the daemon is ready (same shebang handling).
- `--check-url-file`: YAML declarative HTTP check(s) run after the daemon is
  ready (see below).
- `--timeout`: seconds to wait for all expected patterns (default 30); also the
  ceiling for retrying each URL check.

## Declarative URL checks

`--check-url-file` replaces the common "curl in a post script" pattern. It is
YAML describing one check or a list of them:

```yaml
- url: http://localhost:8080/health
  method: GET                     # optional, default GET
  headers:                        # optional
    Authorization: "Bearer TOKEN"
  expected:
    code: 200                     # optional; if omitted, any status < 400 passes
    body-contains: "hello world"  # optional; a string or a list of strings
- url: http://localhost:8080/metrics
```

Each check is retried once per second until it passes or `--timeout` elapses, so
a connection refused while the port is still coming up is tolerated (like
`curl --retry`). Only `http`/`https` URLs are allowed. Advanced cases (JSON
assertions with jq, POST bodies, digest auth, dynamic tokens) still belong in a
`post` script.

## Behavior notes

- The daemon runs in its own process group; on cleanup the whole group receives
  SIGTERM, then SIGKILL after 30s, so forked workers are not left running.
- Exit code is `1` if any expected pattern is missing or any error string
  matched; otherwise the post script's exit code; otherwise `0`.

## Testing

```
make test   # hermetic go tests using small /bin/sh fake daemons
```
