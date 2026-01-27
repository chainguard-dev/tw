package cmd

import "github.com/spf13/cobra"

var rootCmd = &cobra.Command{
	Use:   "chelm",
	Short: "Validate Helm chart image mappings",
	Long: `chelm validates Helm chart image mappings for Chainguard packaging.

It complements melange pipelines, composing with helm template via stdin/stdout.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(generateCmd)
	rootCmd.AddCommand(testCmd)
}
