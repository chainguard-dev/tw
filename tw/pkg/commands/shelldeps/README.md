# shell-deps

The `shell-deps` command analyzes shell scripts (bash, dash, or sh) and lists external programs (dependencies) that the shell script may invoke.

## Overview

`shell-deps` uses the [mvdan.cc/sh/v3](https://github.com/mvdan/sh) parser to analyze shell scripts and identify external command dependencies. It correctly excludes:

- Shell built-in commands (e.g., `echo`, `cd`, `test`, `[`)
- Functions defined within the script
- Aliases defined within the script
- Shell control structures (e.g., `if`, `while`, `for`)

## Usage

```bash
tw shell-deps [command] [flags]
```

### Global Flags

- `--json` - Output results in JSON format
- `-v, --verbose` - Increase verbosity (logs detailed information)

## Subcommands

### show

Analyze one or more specific shell script files.

```bash
tw shell-deps show [flags] file [file...]
```

**Flags:**
- `--missing=path/` - Path to directory containing available executables. If provided, the output will include a list of dependencies that are not found in the specified directory.

**Examples:**

```bash
# Show dependencies for a single script
tw shell-deps show script.sh

# Show dependencies for multiple scripts
tw shell-deps show install.sh configure.sh

# Check for missing dependencies
tw shell-deps show --missing=/usr/bin script.sh

# JSON output
tw shell-deps show --json script.sh
```

**Example Output:**

```
script.sh:
  deps: awk grep sed
```

With `--missing=/usr/bin`:
```
script.sh:
  deps: awk bobob grep
  missing: bobob
```

### scan

Recursively scan a directory for shell scripts and analyze their dependencies.

```bash
tw shell-deps scan [flags] search-dir
```

**Flags:**
- `--missing=path/` - Path to directory containing available executables
- `--match=regex` - Regular expression pattern to match additional files as shell scripts (e.g., `\.makefile$` to include files ending in `.makefile`)
- `-x, --executable` - Only consider executable files as shell scripts

**Shell Script Detection:**

By default, `scan` identifies shell scripts by checking for these shebangs:
- `#!/bin/sh`
- `#!/bin/dash`
- `#!/bin/bash`
- `#!/usr/bin/env sh`
- `#!/usr/bin/env dash`
- `#!/usr/bin/env bash`

Both `#!` and `#! ` (with space) variations are supported.

**Examples:**

```bash
# Scan a directory for shell scripts
tw shell-deps scan /path/to/scripts

# Scan only executable scripts
tw shell-deps scan --executable /path/to/scripts
tw shell-deps scan -x /path/to/scripts

# Include files matching a pattern (e.g., makefiles)
tw shell-deps scan --match='\.makefile$' /path/to/scripts

# Combine --executable and --match
tw shell-deps scan -x --match='\.sh$' /path/to/scripts

# Check for missing dependencies
tw shell-deps scan --missing=/usr/bin /path/to/scripts

# Verbose output with JSON formatting
tw shell-deps scan -v --json /path/to/scripts
```

## Dependency Detection

### What is Detected

The parser identifies external commands from:
- Direct command invocations: `grep pattern file.txt`
- Command substitutions: `out=$(awk '{print $1}' file)`
- Pipes: `cat file | grep pattern | awk '{print $1}'`
- Conditionals: `if command; then ... fi`
- Absolute paths: `/usr/bin/sudo`, `/sbin/modprobe`

### What is Excluded

The following are **not** considered external dependencies:

**Shell Built-ins:**
- POSIX special built-ins: `break`, `:`, `continue`, `.`, `eval`, `exec`, `exit`, `export`, `readonly`, `return`, `set`, `shift`, `times`, `trap`, `unset`
- POSIX regular built-ins: `alias`, `bg`, `cd`, `command`, `false`, `fc`, `fg`, `getopts`, `jobs`, `kill`, `pwd`, `read`, `true`, `umask`, `unalias`, `wait`, `hash`, `type`, `ulimit`, `[`, `test`, `echo`, `printf`
- Bash/dash additional built-ins: `source`, `local`, `declare`, `typeset`, `let`, `enable`, `builtin`, and others

**Script-defined entities:**
- Functions defined in the script
- Aliases defined in the script

**Control structures:**
- `if`, `then`, `else`, `elif`, `fi`, `while`, `do`, `done`, `for`, `in`, `case`, `esac`, `until`, `select`

## Example Script Analysis

Given this script:

```bash
#!/bin/sh
stderr() { echo "this is a thing:" "$@" 1>&2; }

out=$(grep stuff /etc/passwd)
out2=$(echo "$out" | awk -F: '{print $3}')
if [ -n "$out2" ]; then
    bobob --check thing
elif test -s /tt; then
    /sbin/sudo ls -l
    stderr "Oh no, tt is not there"
fi
```

Output:
```
script.sh:
  deps: /sbin/sudo awk bobob grep
```

**Note:** `stderr` is excluded (it's a function), `echo`, `test`, and `[` are excluded (built-ins), but `grep`, `awk`, `bobob`, and `/sbin/sudo` are included as external dependencies.

## JSON Output Format

When using `--json`, the output is structured as follows:

```json
[
  {
    "file": "/path/to/script.sh",
    "deps": ["awk", "grep", "sed"],
    "missing": ["custom-tool"]
  }
]
```

Fields:
- `file` - Path to the script
- `deps` - List of external dependencies (sorted alphabetically)
- `missing` - List of missing dependencies (only present if `--missing` flag is used)
- `error` - Error message (only present if parsing failed)

## Exit Codes

- `0` - Success (all scripts parsed successfully)
- `1` - Errors occurred while processing one or more files

When errors occur, the error messages are included in the output, and the command exits with code 1 after processing all files.

## Use Cases

1. **Build System Validation** - Ensure all required tools are available before running build scripts
2. **Container Image Optimization** - Identify minimal set of packages needed for scripts
3. **Documentation** - Generate documentation of script dependencies
4. **CI/CD Checks** - Verify that CI environment has all necessary tools installed
5. **Security Audits** - Identify external commands invoked by scripts

## Implementation Details

- **Parser:** Uses `mvdan.cc/sh/v3` for robust shell script parsing
- **Language Support:** Supports POSIX sh, bash, and dash syntax
- **Performance:** Scripts are parsed once; dependencies are extracted in two passes (first to identify functions/aliases, second to identify commands)
