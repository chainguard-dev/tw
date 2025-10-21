package shelldeps

import (
	"context"
	"fmt"
	"os"

	"github.com/chainguard-dev/clog"
	"github.com/spf13/cobra"
)

type showCfg struct {
	parent      *cfg
	missingPath string
}

func (c *cfg) showCommand() *cobra.Command {
	showCfg := &showCfg{parent: c}
	cmd := &cobra.Command{
		Use:   "show [flags] file [file...]",
		Short: "Show dependencies for one or more shell scripts",
		Long:  "Analyze shell scripts and display their external command dependencies.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return showCfg.Run(cmd.Context(), cmd, args)
		},
	}

	cmd.Flags().StringVar(&showCfg.missingPath, "missing", "", "path to directory containing available executables")

	return cmd
}

func (s *showCfg) Run(ctx context.Context, cmd *cobra.Command, args []string) error {
	// Validate missing path if provided
	if s.missingPath != "" {
		info, err := os.Stat(s.missingPath)
		if err != nil {
			return fmt.Errorf("--missing path error: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("--missing path %s is not a directory", s.missingPath)
		}
	}

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

		// Find missing dependencies if requested
		if s.missingPath != "" {
			missing, err := findMissing(deps, s.missingPath)
			if err != nil {
				result.Error = err.Error()
				hadErrors = true
				results = append(results, result)
				if s.parent.verbose {
					clog.ErrorContext(ctx, "failed to find missing deps", "file", file, "error", err)
				}
				continue
			}
			result.Missing = missing
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
