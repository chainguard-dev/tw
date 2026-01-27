package shelldeps

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// GNUIncompatibility represents a GNU-specific feature that doesn't work with busybox
type GNUIncompatibility struct {
	Command     string // The command (e.g., "realpath")
	Flag        string // The specific flag that's GNU-only
	Line        int    // Line number where found
	Description string // Human-readable description
	Fix         string // Suggested fix
}

// gnuOnlyFlags maps commands to their GNU-only flags/options
// These flags work with GNU coreutils but NOT with busybox
var gnuOnlyFlags = map[string]map[string]string{
	"realpath": {
		"--no-symlinks":   "realpath --no-symlinks (GNU only)",
		"--relative-base": "realpath --relative-base (GNU only)",
		"--relative-to":   "realpath --relative-to (GNU only)",
		"-q":              "realpath -q/--quiet (GNU only)",
		"--quiet":         "realpath --quiet (GNU only)",
	},
	"stat": {
		"--format": "stat --format (GNU only, use -c for busybox)",
		"--printf": "stat --printf (GNU only)",
	},
	"cp": {
		"--reflink": "cp --reflink (GNU only)",
		"--sparse":  "cp --sparse (GNU only)",
	},
	"date": {
		"--iso-8601": "date --iso-8601 (GNU only)",
		"-I":         "date -I (GNU only, short for --iso-8601)",
	},
	"mktemp": {
		"--suffix": "mktemp --suffix (GNU only)",
	},
	"sort": {
		"-h":                   "sort -h (GNU only, human-numeric-sort)",
		"--human-numeric-sort": "sort --human-numeric-sort (GNU only)",
	},
	"ls": {
		"--time-style": "ls --time-style (GNU only)",
	},
	"df": {
		"--output": "df --output (GNU only)",
	},
	"readlink": {
		"-e":           "readlink -e (GNU only, use -f for busybox)",
		"--canonicalize-existing": "readlink --canonicalize-existing (GNU only)",
		"-m":           "readlink -m (GNU only)",
		"--canonicalize-missing": "readlink --canonicalize-missing (GNU only)",
	},
	"tail": {
		"--pid": "tail --pid (GNU only)",
	},
	"touch": {
		"--date": "touch --date (GNU only)",
	},
	"head": {
		"--bytes": "head --bytes (GNU only, use -c)",
	},
	"du": {
		"--apparent-size": "du --apparent-size (GNU only)",
	},
	"chmod": {
		"--reference": "chmod --reference (GNU only)",
	},
	"chown": {
		"--reference": "chown --reference (GNU only)",
	},
	"install": {
		"-D": "install -D (GNU only, creates parent directories)",
	},
	"tr": {
		"--complement": "tr --complement (GNU only, use -c)",
	},
	"wc": {
		"--total": "wc --total (GNU only)",
	},
	"seq": {
		"--equal-width": "seq --equal-width (GNU only, use -w)",
	},
}

// CheckGNUCompatibilityAST parses a shell script and finds GNU-specific flag usage
// using AST analysis. This correctly handles multiline commands and avoids false positives.
func CheckGNUCompatibilityAST(file *syntax.File, filename string) []GNUIncompatibility {
	var incompatibilities []GNUIncompatibility

	syntax.Walk(file, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}

		// Get the command name
		cmdName := wordToString(call.Args[0])

		// Handle absolute paths - extract just the command name
		if strings.HasPrefix(cmdName, "/") {
			cmdName = filepath.Base(cmdName)
		}

		// Check if this command has known GNU-only flags
		gnuFlags, hasGNUFlags := gnuOnlyFlags[cmdName]
		if !hasGNUFlags {
			return true
		}

		// Check each argument for GNU-only flags
		for _, arg := range call.Args[1:] {
			argStr := wordToString(arg)

			// Check against known GNU-only flags
			for flag, description := range gnuFlags {
				if matchesFlag(argStr, flag) {
					incompatibilities = append(incompatibilities, GNUIncompatibility{
						Command:     cmdName,
						Flag:        flag,
						Line:        int(call.Pos().Line()),
						Description: description,
						Fix:         fmt.Sprintf("Add 'coreutils' to runtime dependencies, or modify script to avoid %s", flag),
					})
				}
			}
		}

		return true
	})

	return incompatibilities
}

// matchesFlag checks if an argument matches a flag pattern
// Handles: --flag, --flag=value, -f, -fvalue
func matchesFlag(arg, flag string) bool {
	if arg == flag {
		return true
	}
	// Handle --flag=value
	if strings.HasPrefix(flag, "--") && strings.HasPrefix(arg, flag+"=") {
		return true
	}
	// Handle combined short flags like -Dm (matches -D)
	if len(flag) == 2 && flag[0] == '-' && flag[1] != '-' {
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") {
			// Check if the flag letter is in the combined flags
			return strings.Contains(arg, string(flag[1]))
		}
	}
	return false
}

// ProviderInfo contains information about what provides a command
type ProviderInfo struct {
	Command  string // The command name
	Path     string // Full path to the binary
	Provider string // "busybox", "coreutils", or "unknown"
}

// DetectCommandProvider determines if a command is provided by busybox or coreutils
// by examining symlinks and binary names in the given PATH
func DetectCommandProvider(cmd string, searchPath string) ProviderInfo {
	info := ProviderInfo{
		Command:  cmd,
		Provider: "unknown",
	}

	// Search through PATH directories
	dirs := filepath.SplitList(searchPath)
	for _, dir := range dirs {
		cmdPath := filepath.Join(dir, cmd)

		// Check if file exists
		fi, err := os.Lstat(cmdPath)
		if err != nil {
			continue
		}

		info.Path = cmdPath

		// If it's a symlink, check where it points
		if fi.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(cmdPath)
			if err != nil {
				continue
			}

			// Resolve relative symlinks
			if !filepath.IsAbs(target) {
				target = filepath.Join(dir, target)
			}

			// Check if target is busybox
			targetBase := filepath.Base(target)
			if targetBase == "busybox" || strings.Contains(target, "busybox") {
				info.Provider = "busybox"
				return info
			}

			// Check if target points to coreutils (could be in /usr/bin/coreutils or similar)
			if strings.Contains(target, "coreutils") {
				info.Provider = "coreutils"
				return info
			}

			// Follow the symlink to get more info
			realPath, err := filepath.EvalSymlinks(cmdPath)
			if err == nil {
				realBase := filepath.Base(realPath)
				if realBase == "busybox" {
					info.Provider = "busybox"
					return info
				}
			}
		}

		// If it's a regular file, check the binary name/path
		// Assume non-symlinked binaries in standard paths are coreutils
		if fi.Mode().IsRegular() {
			// If it's a real binary (not busybox symlink), likely coreutils
			info.Provider = "coreutils"
			return info
		}

		// Found the command, but can't determine provider
		return info
	}

	return info
}

// CheckGNUCompatWithPath checks GNU compatibility considering the actual binaries in PATH
// Returns only incompatibilities where the command is provided by busybox
func CheckGNUCompatWithPath(file *syntax.File, filename string, searchPath string) []GNUIncompatibility {
	allIncompat := CheckGNUCompatibilityAST(file, filename)

	if searchPath == "" {
		// No PATH provided, return all potential incompatibilities
		return allIncompat
	}

	var filtered []GNUIncompatibility
	providerCache := make(map[string]ProviderInfo)

	for _, incompat := range allIncompat {
		// Check cached provider info
		provider, ok := providerCache[incompat.Command]
		if !ok {
			provider = DetectCommandProvider(incompat.Command, searchPath)
			providerCache[incompat.Command] = provider
		}

		// Only report if the command is provided by busybox (or unknown)
		// If coreutils provides it, the GNU flags will work
		if provider.Provider != "coreutils" {
			filtered = append(filtered, incompat)
		}
	}

	return filtered
}

// NeedsGNUCoreutils returns true if any of the incompatibilities require coreutils
func NeedsGNUCoreutils(incompatibilities []GNUIncompatibility) bool {
	return len(incompatibilities) > 0
}

// FormatIncompatibilities formats the incompatibilities for display
func FormatIncompatibilities(incompatibilities []GNUIncompatibility, filename string) string {
	if len(incompatibilities) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("  GNU coreutils incompatibilities in %s:\n", filename))

	for _, inc := range incompatibilities {
		sb.WriteString(fmt.Sprintf("    Line %d: %s\n", inc.Line, inc.Description))
		sb.WriteString(fmt.Sprintf("      Fix: %s\n", inc.Fix))
	}

	return sb.String()
}
