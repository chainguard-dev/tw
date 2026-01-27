package cmd

import (
	"encoding/json"
	"os"

	"chainguard.dev/tw/chelm/internal/chelm"
	"github.com/spf13/cobra"
)

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Validate and format cg.json",
	Long: `Read Chainguard chart metadata as JSON from stdin, validate markers and test cases, and write formatted JSON.

Example:
  yq -n '.images.nginx.values.image = "${ref}"' -o=json | chelm generate -o cg.json`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		output, _ := cmd.Flags().GetString("output")

		w := cmd.OutOrStdout()
		if output != "-" {
			f, err := os.Create(output)
			if err != nil {
				return err
			}
			defer f.Close()
			w = f
		}

		meta, err := chelm.Parse(cmd.InOrStdin())
		if err != nil {
			return err
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(meta)
	},
}

func init() {
	generateCmd.Flags().StringP("output", "o", "-", "Output file (- for stdout)")
}
