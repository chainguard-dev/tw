package shelldeps

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/chainguard-dev/clog"
	"github.com/spf13/cobra"
)

type checkCfg struct {
	parent         *cfg
	packages       string   // Comma-separated list of available packages
	packageList    []string // Parsed package list
	checkGNUCompat bool     // Check for GNU coreutils incompatibilities
	strict         bool     // Exit non-zero if issues found
	matchRegex     string   // Regex to match additional files
	executable     bool     // Only check executable files
}

// checkResult contains the results for a single script
type checkResult struct {
	File              string              `json:"file"`
	Shell             string              `json:"shell,omitempty"`
	Deps              []string            `json:"deps"`
	Missing           []string            `json:"missing,omitempty"`
	GNUIncompatible   []gnuIncompatResult `json:"gnu_incompatible,omitempty"`
	Error             string              `json:"error,omitempty"`
}

type gnuIncompatResult struct {
	Command     string `json:"command"`
	Line        int    `json:"line"`
	Description string `json:"description"`
	Fix         string `json:"fix"`
}

func (c *cfg) checkCommand() *cobra.Command {
	checkCfg := &checkCfg{
		parent:         c,
		checkGNUCompat: true, // Enable by default
	}
	cmd := &cobra.Command{
		Use:   "check [flags] search-dir",
		Short: "Check shell scripts for missing dependencies",
		Long: `Scan a directory for shell scripts and check if their dependencies
are satisfied by the specified packages.

This command combines dependency extraction with package resolution
to identify:
  - Missing commands (not provided by any specified package)
  - GNU coreutils incompatibilities (when using busybox)

Example usage:
  # Check scripts against specific packages
  tw shell-deps check --packages=busybox,bash,curl /opt/scripts

  # Check with strict mode (exit 1 if issues found)
  tw shell-deps check --packages=busybox,bash --strict /opt/scripts

  # Skip GNU compatibility check (if you know coreutils is available)
  tw shell-deps check --packages=busybox,coreutils --no-gnu-compat /opt/scripts

For pipeline usage, this integrates with melange test environments where
the runtime dependencies are already installed.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return checkCfg.Run(cmd.Context(), cmd, args)
		},
	}

	cmd.Flags().StringVar(&checkCfg.packages, "packages", "",
		"comma-separated list of available packages (e.g., busybox,bash,curl)")
	cmd.Flags().BoolVar(&checkCfg.checkGNUCompat, "gnu-compat", true,
		"check for GNU coreutils incompatibilities with busybox")
	cmd.Flags().BoolVar(&checkCfg.strict, "strict", false,
		"exit with non-zero status if any issues are found")
	cmd.Flags().StringVar(&checkCfg.matchRegex, "match", "",
		"regex pattern to match additional files as shell scripts")
	cmd.Flags().BoolVarP(&checkCfg.executable, "executable", "x", false,
		"only consider executable files as shell scripts")

	return cmd
}

func (c *checkCfg) Run(ctx context.Context, cmd *cobra.Command, args []string) error {
	searchDir := args[0]

	// Parse package list
	if c.packages != "" {
		c.packageList = strings.Split(c.packages, ",")
		for i, pkg := range c.packageList {
			c.packageList[i] = strings.TrimSpace(pkg)
		}
	}

	// Validate search directory
	info, err := os.Stat(searchDir)
	if err != nil {
		return fmt.Errorf("search directory error: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("search path %s is not a directory", searchDir)
	}

	// Compile match regex if provided
	var matchPattern *regexp.Regexp
	if c.matchRegex != "" {
		matchPattern, err = regexp.Compile(c.matchRegex)
		if err != nil {
			return fmt.Errorf("invalid --match regex: %w", err)
		}
	}

	// Find all shell scripts
	var shellScripts []string
	err = filepath.Walk(searchDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if c.parent.verbose {
				clog.WarnContext(ctx, "failed to access path", "path", path, "error", err)
			}
			return nil
		}

		if info.IsDir() {
			return nil
		}

		if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			return nil
		}

		isExecutable := info.Mode()&0111 != 0
		if c.executable && !isExecutable {
			return nil
		}

		matchedByRegex := matchPattern != nil && matchPattern.MatchString(filepath.Base(path))
		if matchedByRegex {
			shellScripts = append(shellScripts, path)
			return nil
		}

		isShell, err := isShellScript(path)
		if err != nil {
			if c.parent.verbose {
				clog.WarnContext(ctx, "failed to check shebang", "path", path, "error", err)
			}
			return nil
		}

		if isShell {
			shellScripts = append(shellScripts, path)
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to walk directory: %w", err)
	}

	if len(shellScripts) == 0 {
		if c.parent.verbose {
			clog.InfoContext(ctx, "no shell scripts found", "dir", searchDir)
		}
		fmt.Fprintln(cmd.OutOrStdout(), "No shell scripts found.")
		return nil
	}

	// Process each script
	var results []checkResult
	hasIssues := false

	// Determine if we should skip GNU compat check
	hasCoreutils := HasGNUCoreutils(c.packageList)
	shouldCheckGNU := c.checkGNUCompat && !hasCoreutils

	for _, file := range shellScripts {
		result := c.processScript(ctx, file, shouldCheckGNU)
		results = append(results, result)

		if len(result.Missing) > 0 || len(result.GNUIncompatible) > 0 || result.Error != "" {
			hasIssues = true
		}
	}

	// Output results
	if err := c.outputResults(cmd.OutOrStdout(), results); err != nil {
		return err
	}

	// Exit with error if strict mode and issues found
	if c.strict && hasIssues {
		return fmt.Errorf("shell dependency issues found")
	}

	return nil
}

func (c *checkCfg) processScript(ctx context.Context, file string, checkGNU bool) checkResult {
	result := checkResult{File: file}

	f, err := os.Open(file)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer f.Close()

	// Extract shell
	shell, err := extractShebang(f)
	if err != nil {
		result.Error = fmt.Sprintf("failed to extract shebang: %v", err)
		return result
	}
	result.Shell = shell

	// Reset for dep extraction
	if _, err := f.Seek(0, 0); err != nil {
		result.Error = fmt.Sprintf("failed to seek: %v", err)
		return result
	}

	// Extract dependencies
	deps, err := extractDeps(ctx, f, file)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Deps = deps

	// Find missing commands if packages were specified
	if len(c.packageList) > 0 {
		result.Missing = FindMissingCommands(deps, c.packageList)
	}

	// Check GNU compatibility
	if checkGNU {
		if _, err := f.Seek(0, 0); err != nil {
			result.Error = fmt.Sprintf("failed to seek for GNU check: %v", err)
			return result
		}

		incompatibilities, err := CheckGNUCompatibility(f, file)
		if err != nil {
			if c.parent.verbose {
				clog.WarnContext(ctx, "failed to check GNU compat", "file", file, "error", err)
			}
		} else {
			for _, inc := range incompatibilities {
				result.GNUIncompatible = append(result.GNUIncompatible, gnuIncompatResult{
					Command:     inc.Command,
					Line:        inc.Line,
					Description: inc.Description,
					Fix:         inc.Fix,
				})
			}
		}
	}

	return result
}

func (c *checkCfg) outputResults(w io.Writer, results []checkResult) error {
	if c.parent.jsonOut {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(results)
	}

	// Text output
	var issueCount int
	var scriptWithIssues []checkResult

	for _, result := range results {
		if len(result.Missing) > 0 || len(result.GNUIncompatible) > 0 || result.Error != "" {
			scriptWithIssues = append(scriptWithIssues, result)
			issueCount++
		}
	}

	// Summary header
	fmt.Fprintf(w, "Checked %d shell scripts\n", len(results))

	if len(scriptWithIssues) == 0 {
		fmt.Fprintln(w, "✓ No issues found")
		return nil
	}

	fmt.Fprintf(w, "\n")

	// Report issues
	for _, result := range scriptWithIssues {
		fmt.Fprintf(w, "%s:\n", result.File)

		if result.Error != "" {
			fmt.Fprintf(w, "  error: %s\n", result.Error)
			continue
		}

		if result.Shell != "" {
			fmt.Fprintf(w, "  shell: %s\n", result.Shell)
		}

		if len(result.Deps) > 0 {
			fmt.Fprintf(w, "  deps: %s\n", strings.Join(result.Deps, " "))
		}

		if len(result.Missing) > 0 {
			sort.Strings(result.Missing)
			fmt.Fprintf(w, "  missing: %s\n", strings.Join(result.Missing, " "))
		}

		if len(result.GNUIncompatible) > 0 {
			fmt.Fprintf(w, "  gnu-incompatible:\n")
			for _, inc := range result.GNUIncompatible {
				fmt.Fprintf(w, "    - line %d: %s\n", inc.Line, inc.Description)
				fmt.Fprintf(w, "      fix: %s\n", inc.Fix)
			}
		}

		fmt.Fprintln(w)
	}

	// Summary footer
	fmt.Fprintf(w, "---\n")
	fmt.Fprintf(w, "Issues found in %d of %d scripts\n", len(scriptWithIssues), len(results))

	// Collect unique suggestions
	suggestions := make(map[string]bool)
	for _, result := range scriptWithIssues {
		if len(result.Missing) > 0 {
			suggestions["Missing commands: consider adding packages that provide them"] = true
		}
		if len(result.GNUIncompatible) > 0 {
			suggestions["GNU compatibility: add 'coreutils' to runtime dependencies"] = true
		}
	}

	if len(suggestions) > 0 {
		fmt.Fprintln(w, "\nSuggestions:")
		for suggestion := range suggestions {
			fmt.Fprintf(w, "  - %s\n", suggestion)
		}
	}

	return nil
}
