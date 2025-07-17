# symlink-check

This is a script that checks for broken/dangling symlinks in the filesystem.

It will find all symlinks in specified paths or packages and verify that they point to existing, readable targets. The script excludes virtual filesystems like `/proc`, `/sys`, and `/dev` to avoid false positives.

## Usage

```bash
# Check symlinks in a specific path
symlink-check --paths=/usr/bin

# Check symlinks in a package
symlink-check --packages=bash

# Check with verbose output
symlink-check --paths=/usr/bin --verbose=true
```