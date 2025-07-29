package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var dbDir string

func main() {
	rootCmd := &cobra.Command{
		Use:   "apkq",
		Short: "Query APK database",
		Long:  "A lightweight tool to query APK V2 database for package information",
	}

	rootCmd.PersistentFlags().StringVar(&dbDir, "db-dir", "/lib/apk/db", "Path to APK database directory")
	rootCmd.AddCommand(infoCommand())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func infoCommand() *cobra.Command {
	var (
		installed bool
		contents  bool
		verbose   bool
		quiet     bool
	)

	cmd := &cobra.Command{
		Use:   "info [packages...]",
		Short: "Show package information",
		Long:  "Display information about installed packages",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("at least one package name is required")
			}

			db, err := NewAPKDatabaseFromDir(dbDir)
			if err != nil {
				return fmt.Errorf("failed to open APK database: %w", err)
			}

			var hasError bool

			if installed {
				hasError = !handleInstalledCommand(db, args, verbose)
			} else if contents {
				hasError = !handleContentsCommand(db, args, quiet)
			} else {
				return fmt.Errorf("must specify either --installed or --contents")
			}

			if hasError {
				os.Exit(1)
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&installed, "installed", "e", false, "Show installed packages")
	cmd.Flags().BoolVarP(&contents, "contents", "L", false, "Show package contents")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "Quiet output")

	return cmd
}