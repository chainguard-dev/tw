# shell-deps

The `shell-deps` command analyzes shell scripts (bash, dash, or sh) and lists external programs (dependencies) that the shell script may invoke. It can also detect GNU coreutils-specific flags that don't work with busybox.

## Overview

`shell-deps` uses the [mvdan.cc/sh/v3](https://github.com/mvdan/sh) parser to analyze shell scripts and identify external command dependencies. It correctly excludes:

- Shell built-in commands (e.g., `echo`, `cd`, `test`, `[`)
- Functions defined within the script
- Aliases defined within the script
- Shell control structures (e.g., `if`, `while`, `for`)
- Wrapper functions that execute their arguments (e.g., `vr() { "$@" }`)

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
- `--path=PATH` - PATH-like colon-separated directories to check for missing commands (e.g., `/usr/bin:/usr/local/bin`)

**Examples:**

```bash
# Show dependencies for a single script
tw shell-deps show script.sh

# Show dependencies for multiple scripts
tw shell-deps show install.sh configure.sh

# Check for missing dependencies in PATH
tw shell-deps show --path=/usr/bin:/usr/local/bin script.sh

# JSON output
tw shell-deps show --json script.sh
```

**Example Output:**

```
script.sh:
  deps: awk grep sed
  shell: /bin/sh
```

With `--path=/usr/bin`:
```
script.sh:
  deps: awk bobob grep
  shell: /bin/bash
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

### check

Check shell scripts for missing dependencies and GNU coreutils compatibility issues.

```bash
tw shell-deps check [flags] file [file...]
```

**Flags:**
- `--path=PATH` - PATH-like colon-separated directories to search for commands (default: `/usr/bin:/usr/local/bin`)
- `--strict` - Exit with non-zero status if any issues are found

This command performs two types of checks:
1. **Missing dependencies** - Commands that don't exist in the specified PATH
2. **GNU compatibility** - Detects GNU coreutils-specific flags that won't work with busybox

The GNU compatibility check automatically determines whether commands are provided by busybox or coreutils by examining symlinks in the PATH.

**Examples:**

```bash
# Check specific files against system PATH
tw shell-deps check --path=/usr/bin:/usr/local/bin script.sh

# Check with strict mode (exit 1 if issues found)
tw shell-deps check --path=/usr/bin --strict entrypoint.sh run.sh

# Check files, auto-detect GNU issues based on actual binaries
tw shell-deps check --path=/usr/bin /opt/scripts/*.sh
```

**Example Output:**

```
Checked 2 file(s)

entrypoint.sh:
  shell: /bin/sh
  deps: chmod install realpath
  missing: custom-tool
  gnu-incompatible:
    - line 15: realpath --no-symlinks
      realpath --no-symlinks (GNU only)
    - line 23: install -D
      install -D (GNU only, creates parent directories)

---
Issues found in 1 of 2 file(s)
```

### check-package

Check a melange package for missing shell dependencies and GNU compatibility issues.

```bash
tw shell-deps check-package [flags] <package-name>
```

**Flags:**
- `--path=PATH` - PATH-like colon-separated directories to search for commands (default: `/usr/bin:/bin`)
- `--strict` - Exit with non-zero status if any issues are found
- `--package-dir=DIR` - Directory to search for package YAML files (default: `.`)

This command:
1. Finds the melange YAML file for the given package (supports main packages and subpackages)
2. Extracts shell scripts from the package's pipeline and test sections
3. Analyzes the package's runtime dependencies to determine if it uses busybox or coreutils
4. Reports GNU-specific flags that will fail if busybox is the only provider

**Examples:**

```bash
# Check a package against system PATH
tw shell-deps check-package valkey-8.1-iamguarded-compat

# Check with strict mode (exit 1 if issues found)
tw shell-deps check-package --strict valkey-8.1

# Check against custom PATH and package directory
tw shell-deps check-package --path=/custom/bin --package-dir=/path/to/packages mypackage
```

**Example Output:**

```
Found package: ./valkey-8.1.yaml
Runtime dependencies: [busybox valkey-8.1]
Note: Package has busybox but NOT coreutils - GNU-specific flags will fail
Found 3 script(s) to check

Checked 3 script(s)

subpackage:valkey-8.1-iamguarded-compat/pipeline[0].runs:
  gnu-incompatible (busybox cannot handle these):
    - line 5: install -D
      install -D (GNU only, creates parent directories)
  ⚠ MISSING RUNTIME DEPENDENCY: coreutils
    Package declares 'busybox' but scripts use GNU-specific flags.
    Add 'coreutils' to dependencies.runtime in the package YAML.

---
Issues found in 1 of 3 script(s)
```

## Dependency Detection

### What is Detected

The parser identifies external commands from:
- Direct command invocations: `grep pattern file.txt`
- Command substitutions: `out=$(awk '{print $1}' file)`
- Pipes: `cat file | grep pattern | awk '{print $1}'`
- Conditionals: `if command; then ... fi`
- Absolute paths: `/usr/bin/sudo`, `/sbin/modprobe`
- Wrapper function calls: Commands passed to functions that execute `$@` or `$*`

### Wrapper Function Detection

The parser automatically identifies "wrapper functions" - functions that execute their arguments. This is a common pattern for logging or error handling:

```bash
#!/bin/sh
vr() {
    echo "running:" "$@" 1>&2
    "$@" || { echo "failed" 1>&2; return 1; }
}

vr ls /etc        # 'ls' is detected as a dependency
vr grep foo bar   # 'grep' is detected as a dependency
```

A function is identified as a wrapper if it contains `"$@"` or `$@` in command position. The first argument passed to such functions is analyzed as a potential external command.

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

## GNU Coreutils Compatibility

The `check` and `check-package` commands detect GNU coreutils-specific flags that don't work with busybox. This is critical for Wolfi/Chainguard packages where busybox is often used instead of full coreutils.

### Detected GNU-only Flags

| Command    | GNU-only Flags                                           |
|------------|----------------------------------------------------------|
| `realpath` | `--no-symlinks`, `--relative-base`, `--relative-to`, `-q`, `--quiet` |
| `stat`     | `--format`, `--printf`                                   |
| `cp`       | `--reflink`, `--sparse`                                  |
| `date`     | `--iso-8601`, `-I`                                       |
| `mktemp`   | `--suffix`                                               |
| `sort`     | `-h`, `--human-numeric-sort`                             |
| `ls`       | `--time-style`                                           |
| `df`       | `--output`                                               |
| `readlink` | `-e`, `--canonicalize-existing`, `-m`, `--canonicalize-missing` |
| `tail`     | `--pid`                                                  |
| `touch`    | `--date`                                                 |
| `head`     | `--bytes`                                                |
| `du`       | `--apparent-size`                                        |
| `chmod`    | `--reference`                                            |
| `chown`    | `--reference`                                            |
| `install`  | `-D` (creates parent directories)                        |
| `tr`       | `--complement`                                           |
| `wc`       | `--total`                                                |
| `seq`      | `--equal-width`                                          |

### Auto-detection of Providers

The `check` command automatically determines whether a command is provided by busybox or coreutils by examining symlinks in the PATH. If a command (e.g., `/usr/bin/chmod`) is a symlink to busybox, GNU-specific flags will be flagged. If it points to a real coreutils binary, no warning is issued.

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
  shell: /bin/sh
```

**Note:** `stderr` is excluded (it's a function), `echo`, `test`, and `[` are excluded (built-ins), but `grep`, `awk`, `bobob`, and `/sbin/sudo` are included as external dependencies.

## JSON Output Format

When using `--json`, the output is structured as follows:

**For `show` and `scan` commands:**

```json
[
  {
    "file": "/path/to/script.sh",
    "deps": ["awk", "grep", "sed"],
    "shell": "/bin/bash",
    "missing": ["custom-tool"]
  }
]
```

**For `check` command:**

```json
[
  {
    "file": "/path/to/script.sh",
    "shell": "/bin/sh",
    "deps": ["chmod", "install", "realpath"],
    "missing": ["custom-tool"],
    "gnu_incompatible": [
      {
        "command": "realpath",
        "flag": "--no-symlinks",
        "line": 15,
        "description": "realpath --no-symlinks (GNU only)",
        "fix": "Add 'coreutils' to runtime dependencies, or modify script to avoid --no-symlinks"
      }
    ]
  }
]
```

Fields:
- `file` - Path to the script
- `deps` - List of external dependencies (sorted alphabetically)
- `shell` - The shell interpreter from the shebang (e.g., `/bin/bash`, `bash`)
- `missing` - List of missing dependencies (only present if `--path` or `--missing` flag is used)
- `gnu_incompatible` - List of GNU-specific flag usages (only in `check` command)
- `error` - Error message (only present if parsing failed)

## Exit Codes

- `0` - Success (all scripts parsed successfully, no issues in strict mode)
- `1` - Errors occurred while processing one or more files, or issues found in `--strict` mode

When errors occur, the error messages are included in the output, and the command exits with code 1 after processing all files.

## Use Cases

1. **Build System Validation** - Ensure all required tools are available before running build scripts
2. **Container Image Optimization** - Identify minimal set of packages needed for scripts
3. **Wolfi/Chainguard Package Validation** - Detect GNU-specific flags in packages that only have busybox
4. **Documentation** - Generate documentation of script dependencies
5. **CI/CD Checks** - Verify that CI environment has all necessary tools installed
6. **Security Audits** - Identify external commands invoked by scripts

## Implementation Details

- **Parser:** Uses `mvdan.cc/sh/v3` for robust shell script parsing
- **Language Support:** Supports POSIX sh, bash, and dash syntax
- **Performance:** Scripts are parsed once; dependencies are extracted in two passes (first to identify functions/aliases/wrappers, second to identify commands)
- **GNU Detection:** Uses symlink analysis to determine if commands are provided by busybox or coreutils
