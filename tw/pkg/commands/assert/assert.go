package assert

import (
	"strings"

	"github.com/spf13/cobra"
)

type cfg struct{}

func Command() *cobra.Command {
	cfg := &cfg{}
	_ = cfg

	cmd := &cobra.Command{
		Use:   "assert",
		Short: "Helper cli for making assertions about an environment.",
	}

	cmd.AddCommand(fileCommand())

	return cmd
}

func lineMatches(line string, expects ...string) bool {
	for _, expect := range expects {
		if strings.Contains(line, expect) {
			return true
		}
	}
	return false
}
