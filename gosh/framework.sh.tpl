#!/bin/sh

set -u

if command -v gt >/dev/null 2>&1; then
	readonly GT_BIN="gt"
else
	echo "Error: gt command not found in PATH" >&2
	exit 1
fi

# Define no-op functions for hooks that may not be defined by the user
setup() { return 0; }
teardown() { return 0; }
setup_once() { return 0; }
teardown_once() { return 0; }

# Send a signal to the test runner
# Args:
#   $1 - The operation to perform (e.g., start, stop, status)
#   $2 - The name of the test
#   $3 - The exit code of the test (optional, default is 0)
#   $4 - An optional message to include (default is "")
_gosh_signal() {
	local op="${1}"
	local test_name="${2}"
	local exit_code="${3:-0}"
	local message="${4:-}"

	"${GT_BIN}" ctrl \
		--op="$op" \
		--test-name="$test_name" \
		--exit-code="$exit_code" \
		--addr="${GOSH_CONTROL_ADDR}" \
		--message="${message}"
}

# Run a wrapped test with proper pipe redirection. The wrapped test function
# blocks until the test completes and the runner acks the "end" signal.
# Args:
#   $1 - The name of the test
#   $2 - The stdout pipe path
#   $3 - The stderr pipe path
# Returns:
#   The exit code of the test
_gosh_run_test() {
	local test_name="${1}"
	local stdout_pipe="${2}"
	local stderr_pipe="${3}"

	_gosh_signal "start" "${test_name}"

	# User defined test(s) run in this subshell
	(
		set -o errexit -o errtrace

		# Wrap it so we can trap it
		trap '_gosh_teardown' EXIT

		_gosh_teardown() {
			teardown >"${stdout_pipe}" 2>"${stderr_pipe}"
		}

		setup

		"${test_name}"
	) </dev/null >"${stdout_pipe}" 2>"${stderr_pipe}"

	local test_exit_code=$?

	# Signal the test completion with the exit code
	_gosh_signal "end" "${test_name}" "${test_exit_code}"

	return "${test_exit_code}"
}

# Inline the newly rendered user script content, the shbang will be preserved, but will always be treated as a comment by the actual interpreter above
{{loadScript}}

# Run the one-time setup
setup_once

# Run each test function in order they appear
{{ range .OrderedTestFns }}
{{ $tfn := index $.TestFns . }}
_gosh_run_test "{{$tfn.Name}}" "{{$tfn.StdoutPipe.Path}}" "{{$tfn.StderrPipe.Path}}"
{{ end }}

# Run the one-time teardown
teardown_once

exit 0
