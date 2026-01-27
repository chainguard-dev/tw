package shelldeps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chainguard-dev/clog"
	"github.com/spf13/cobra"
)

type showCfg struct {
	parent     *cfg
	searchPath string
}

func (c *cfg) showCommand() *cobra.Command {
	showCfg := &showCfg{parent: c}
	cmd := &cobra.Command{
		Use:   "show [flags] file [file...]",
		Short: "Show dependencies for one or more shell scripts",
		Long: `Analyze shell scripts and display their external command dependencies.

This command parses shell scripts and extracts all external commands they invoke.
It excludes shell builtins, functions defined in the script, and aliases.

Optionally, use --path to check which dependencies are missing from a PATH.

Example usage:
  # Show dependencies for a script
  tw shell-deps show script.sh

  # Show dependencies and check against PATH
  tw shell-deps show --path=/usr/bin:/usr/local/bin script.sh

  # JSON output
  tw shell-deps show --json script.sh`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return showCfg.Run(cmd.Context(), cmd, args)
		},
	}

	cmd.Flags().StringVar(&showCfg.searchPath, "path", "",
		"PATH-like colon-separated directories to check for missing commands")

	return cmd
}

func (s *showCfg) Run(ctx context.Context, cmd *cobra.Command, args []string) error {
	var results []scriptResult
	hadErrors := false

	for _, file := range args {
		result := scriptResult{File: file}

		// Open and parse the file
		f, err := os.Open(file)
		if err != nil {
			result.Error = err.Error()
			hadErrors = true
			results = append(results, result)
			if s.parent.verbose {
				clog.ErrorContext(ctx, "failed to open file", "file", file, "error", err)
			}
			continue
		}

		// Extract shell from shebang
		shell, err := extractShebang(f)
		if err != nil {
			f.Close()
			result.Error = fmt.Sprintf("failed to extract shebang: %v", err)
			hadErrors = true
			results = append(results, result)
			if s.parent.verbose {
				clog.ErrorContext(ctx, "failed to extract shebang", "file", file, "error", err)
			}
			continue
		}
		result.Shell = shell

		// Reset file pointer to beginning for extractDeps
		if _, err := f.Seek(0, 0); err != nil {
			f.Close()
			result.Error = fmt.Sprintf("failed to seek to beginning: %v", err)
			hadErrors = true
			results = append(results, result)
			if s.parent.verbose {
				clog.ErrorContext(ctx, "failed to seek", "file", file, "error", err)
			}
			continue
		}

		deps, err := extractDeps(ctx, f, file)
		f.Close()

		if err != nil {
			result.Error = err.Error()
			hadErrors = true
			results = append(results, result)
			if s.parent.verbose {
				clog.ErrorContext(ctx, "failed to parse file", "file", file, "error", err)
			}
			continue
		}

		result.Deps = deps

		// Find missing dependencies if path provided
		if s.searchPath != "" {
			result.Missing = findMissingInPath(deps, s.searchPath)
		}

		results = append(results, result)

		if s.parent.verbose {
			clog.InfoContext(ctx, "processed file", "file", file, "deps", len(deps))
		}
	}

	// Output results
	if err := outputResults(cmd.OutOrStdout(), results, s.parent.jsonOut); err != nil {
		return err
	}

	if hadErrors {
		return fmt.Errorf("errors occurred while processing files")
	}

	return nil
}

// findMissingInPath checks which commands are not found in the search PATH
func findMissingInPath(deps []string, searchPath string) []string {
	var missing []string

	dirs := filepath.SplitList(searchPath)

	for _, dep := range deps {
		// Skip absolute paths - check them directly
		if strings.HasPrefix(dep, "/") {
			if _, err := os.Stat(dep); err != nil {
				missing = append(missing, dep)
			}
			continue
		}

		// Search in PATH directories
		found := false
		for _, dir := range dirs {
			cmdPath := filepath.Join(dir, dep)
			if _, err := os.Stat(cmdPath); err == nil {
				found = true
				break
			}
		}

		if !found {
			missing = append(missing, dep)
		}
	}

	return missing
}
