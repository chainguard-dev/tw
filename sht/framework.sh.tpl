#!/bin/sh

set -e
set -u

if command -v shtc >/dev/null 2>&1; then
    readonly SHT_BIN="shtc"
else
    echo "Error: shtc command not found in PATH" >&2
    exit 1
fi

if command -v otel-cli >/dev/null 2>&1; then
    OTEL_ENABLED=1

    _sht_random_hex() {
        tr -dc 'a-f0-9' < /dev/urandom | head -c "$1"
    }

    # Extract trace ID from TRACEPARENT or generate a new one
    _sht_get_trace_id() {
        local trace_id="$(echo "${TRACEPARENT:-}" | grep -o '[0-9a-f]\{32\}' || true)"
        echo "${trace_id:-$(_sht_random_hex 32)}"
    }

    sht_span_start() {
        local span_name="$1"
        local parent_traceparent="${TRACEPARENT:-}"
        local trace_id="$(_sht_get_trace_id)"
        local span_id="$(_sht_random_hex 16)"
    }

    sht_span_end() {
    }

    sht_with_span() {
        local span_name="$1"
        local attributes="${2:-}"
        shift; [ -n "$attributes" ] && shift

        local span_id="$(sht_span_start "$span_name")"

        local result=0
        "$@" || result=$?

        local status_attr="status.code=$result"
        [ -n "attributes" ] && status_attr="$attributes,$status_attr"
        sht_span_end "$span_id" "$status_attr"

        return $result
    }


else
    OTEL_ENABLED=0

    # define no-op versions for the helper tracing functions
    sht_span_start() { :; }
    sht_span_end() { :; }
    sht_with_span() {
        shift; [ -n "${2:-}" ] && shift
        "$@"
    }
fi

# Send a signal to the test runner
# Args:
#   $1 - The operation to perform (e.g., start, stop, status)
#   $2 - The name of the test
#   $3 - The exit code of the test (optional, default is 0)
_sht_signal() {
    local op="${1}"
    local test_name="${2}"
    local exit_code="${3:-0}"

    "${SHT_BIN}" \
        --op "$op" \
        --test-name "$test_name" \
        --exit-code "$exit_code" \
        --addr "${SHT_CONTROL_ADDR}"
}

# Run a wrapped test with proper pipe redirection. The wrapped test function
# blocks until the test completes and the runner acks the "end" signal.
# Args:
#   $1 - The name of the test
#   $2 - The stdout pipe path
#   $3 - The stderr pipe path
# Returns:
#   The exit code of the test
_sht_run_test() {
    local test_name="${1}"
    local stdout_pipe="${2}"
    local stderr_pipe="${3}"
    local test_exit_code=0

    _sht_signal "start" "${test_name}"

    {
        set -e
        "${test_name}" > "${stdout_pipe}" 2> "${stderr_pipe}"
    }

    local test_exit_code=$?

    # Signal the test completion with the exit code
    _sht_signal "end" "${test_name}" "${test_exit_code}"

    return "${test_exit_code}"
}

# Verbatim load the user's test script content (without any shbangs)
{{loadScript}}

# Run the one-time setup if defined
if command -v oneTimeSetup >/dev/null 2>&1; then
    oneTimeSetup
fi

# Run each test function in order they appear
{{ range .OrderedTestFns }}
{{ $tfn := index $.TestFns . }}
_sht_run_test "{{$tfn.Name}}" "{{$tfn.StdoutPipePath}}" "{{$tfn.StderrPipePath}}" || exit $?
{{ end }}

# Run the one-time teardown if defined
if command -v oneTimeTeardown >/dev/null 2>&1; then
    oneTimeTeardown
fi

exit 0
