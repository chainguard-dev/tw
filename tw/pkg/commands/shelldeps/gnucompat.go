package shelldeps

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// GNUIncompatibility represents a GNU-specific feature that doesn't work with busybox
type GNUIncompatibility struct {
	Command     string // The command (e.g., "realpath")
	Pattern     string // The flag/option pattern found
	Line        int    // Line number where found
	LineContent string // The actual line content
	Description string // Human-readable description
	Fix         string // Suggested fix
}

// gnuPattern defines a pattern to match and its metadata
type gnuPattern struct {
	command     string
	regex       *regexp.Regexp
	description string
	fix         string
}

// gnuPatterns contains all the GNU-specific patterns we check for
var gnuPatterns = []gnuPattern{
	// realpath
	{
		command:     "realpath",
		regex:       regexp.MustCompile(`realpath\s+[^|;&]*--no-symlinks`),
		description: "realpath --no-symlinks (GNU only)",
		fix:         "Add 'coreutils' to runtime dependencies",
	},
	{
		command:     "realpath",
		regex:       regexp.MustCompile(`realpath\s+[^|;&]*--relative-base`),
		description: "realpath --relative-base (GNU only)",
		fix:         "Add 'coreutils' to runtime dependencies",
	},
	{
		command:     "realpath",
		regex:       regexp.MustCompile(`realpath\s+-q\b`),
		description: "realpath -q (GNU only, busybox doesn't support quiet mode)",
		fix:         "Add 'coreutils' to runtime dependencies, or redirect stderr",
	},
	{
		command:     "realpath",
		regex:       regexp.MustCompile(`realpath\s+[^|;&]*--quiet`),
		description: "realpath --quiet (GNU only)",
		fix:         "Add 'coreutils' to runtime dependencies",
	},

	// stat
	{
		command:     "stat",
		regex:       regexp.MustCompile(`stat\s+[^|;&]*--format`),
		description: "stat --format (GNU only, use -c for busybox)",
		fix:         "Use 'stat -c' instead, or add 'coreutils' to runtime dependencies",
	},
	{
		command:     "stat",
		regex:       regexp.MustCompile(`stat\s+[^|;&]*--printf`),
		description: "stat --printf (GNU only)",
		fix:         "Use 'stat -c' instead, or add 'coreutils' to runtime dependencies",
	},

	// cp
	{
		command:     "cp",
		regex:       regexp.MustCompile(`cp\s+[^|;&]*--reflink`),
		description: "cp --reflink (GNU only)",
		fix:         "Add 'coreutils' to runtime dependencies, or remove --reflink",
	},
	{
		command:     "cp",
		regex:       regexp.MustCompile(`cp\s+[^|;&]*--sparse`),
		description: "cp --sparse (GNU only)",
		fix:         "Add 'coreutils' to runtime dependencies",
	},

	// date
	{
		command:     "date",
		regex:       regexp.MustCompile(`date\s+[^|;&]*--iso-8601`),
		description: "date --iso-8601 (GNU only)",
		fix:         "Use 'date +%Y-%m-%d' format instead, or add 'coreutils'",
	},
	{
		command:     "date",
		regex:       regexp.MustCompile(`date\s+-I\b`),
		description: "date -I (GNU only, short for --iso-8601)",
		fix:         "Use 'date +%Y-%m-%d' format instead, or add 'coreutils'",
	},

	// mktemp
	{
		command:     "mktemp",
		regex:       regexp.MustCompile(`mktemp\s+[^|;&]*--suffix`),
		description: "mktemp --suffix (GNU only)",
		fix:         "Add 'coreutils' to runtime dependencies",
	},

	// sort
	{
		command:     "sort",
		regex:       regexp.MustCompile(`sort\s+[^|;&]*-h\b`),
		description: "sort -h/--human-numeric-sort (GNU only)",
		fix:         "Add 'coreutils' to runtime dependencies",
	},
	{
		command:     "sort",
		regex:       regexp.MustCompile(`sort\s+[^|;&]*--human-numeric`),
		description: "sort --human-numeric-sort (GNU only)",
		fix:         "Add 'coreutils' to runtime dependencies",
	},

	// ls
	{
		command:     "ls",
		regex:       regexp.MustCompile(`ls\s+[^|;&]*--time-style`),
		description: "ls --time-style (GNU only)",
		fix:         "Add 'coreutils' to runtime dependencies",
	},

	// df
	{
		command:     "df",
		regex:       regexp.MustCompile(`df\s+[^|;&]*--output`),
		description: "df --output (GNU only)",
		fix:         "Add 'coreutils' to runtime dependencies",
	},

	// readlink
	{
		command:     "readlink",
		regex:       regexp.MustCompile(`readlink\s+-e\b`),
		description: "readlink -e (GNU only, use -f for busybox)",
		fix:         "Use 'readlink -f' instead (works on both), or add 'coreutils'",
	},
	{
		command:     "readlink",
		regex:       regexp.MustCompile(`readlink\s+-m\b`),
		description: "readlink -m (GNU only)",
		fix:         "Add 'coreutils' to runtime dependencies",
	},

	// tail
	{
		command:     "tail",
		regex:       regexp.MustCompile(`tail\s+[^|;&]*--pid`),
		description: "tail --pid (GNU only)",
		fix:         "Add 'coreutils' to runtime dependencies",
	},

	// touch
	{
		command:     "touch",
		regex:       regexp.MustCompile(`touch\s+[^|;&]*--date`),
		description: "touch --date (GNU only)",
		fix:         "Use 'touch -d' instead, or add 'coreutils'",
	},

	// head
	{
		command:     "head",
		regex:       regexp.MustCompile(`head\s+[^|;&]*--bytes`),
		description: "head --bytes (GNU only, use -c for busybox)",
		fix:         "Use 'head -c' instead",
	},

	// du
	{
		command:     "du",
		regex:       regexp.MustCompile(`du\s+[^|;&]*--apparent-size`),
		description: "du --apparent-size (GNU only)",
		fix:         "Add 'coreutils' to runtime dependencies",
	},

	// chmod/chown with --reference
	{
		command:     "chmod",
		regex:       regexp.MustCompile(`chmod\s+[^|;&]*--reference`),
		description: "chmod --reference (GNU only)",
		fix:         "Add 'coreutils' to runtime dependencies",
	},
	{
		command:     "chown",
		regex:       regexp.MustCompile(`chown\s+[^|;&]*--reference`),
		description: "chown --reference (GNU only)",
		fix:         "Add 'coreutils' to runtime dependencies",
	},

	// install
	{
		command:     "install",
		regex:       regexp.MustCompile(`install\s+[^|;&]*-D\b`),
		description: "install -D (GNU only, creates parent directories)",
		fix:         "Use 'mkdir -p' before install, or add 'coreutils'",
	},

	// tr
	{
		command:     "tr",
		regex:       regexp.MustCompile(`tr\s+[^|;&]*--complement`),
		description: "tr --complement (GNU only, use -c for busybox)",
		fix:         "Use 'tr -c' instead",
	},

	// wc
	{
		command:     "wc",
		regex:       regexp.MustCompile(`wc\s+[^|;&]*--total`),
		description: "wc --total (GNU only)",
		fix:         "Add 'coreutils' to runtime dependencies",
	},

	// seq
	{
		command:     "seq",
		regex:       regexp.MustCompile(`seq\s+[^|;&]*--equal-width`),
		description: "seq --equal-width (GNU only, use -w for busybox)",
		fix:         "Use 'seq -w' instead",
	},
}

// CheckGNUCompatibility scans content for GNU-specific patterns that won't work with busybox.
// It returns a list of incompatibilities found.
func CheckGNUCompatibility(r io.Reader, filename string) ([]GNUIncompatibility, error) {
	var incompatibilities []GNUIncompatibility

	scanner := bufio.NewScanner(r)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Skip comments (but not shebangs on line 1)
		trimmed := strings.TrimSpace(line)
		if lineNum > 1 && strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Check each pattern
		for _, pattern := range gnuPatterns {
			if pattern.regex.MatchString(line) {
				incompatibilities = append(incompatibilities, GNUIncompatibility{
					Command:     pattern.command,
					Pattern:     pattern.regex.FindString(line),
					Line:        lineNum,
					LineContent: strings.TrimSpace(line),
					Description: pattern.description,
					Fix:         pattern.fix,
				})
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading file: %w", err)
	}

	return incompatibilities, nil
}

// HasGNUCoreutils checks if a list of packages includes coreutils
func HasGNUCoreutils(packages []string) bool {
	for _, pkg := range packages {
		if pkg == "coreutils" {
			return true
		}
	}
	return false
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
		sb.WriteString(fmt.Sprintf("      %s\n", truncateLine(inc.LineContent, 60)))
		sb.WriteString(fmt.Sprintf("      Fix: %s\n", inc.Fix))
	}

	return sb.String()
}

// truncateLine truncates a line to maxLen characters, adding ... if truncated
func truncateLine(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
