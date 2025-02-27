package shu

import (
	"github.com/spf13/cobra"
)

type cfg struct{}

func Command() *cobra.Command {
	cfg := &cfg{}

	cmd := &cobra.Command{
		Use:   "shu",
		Short: "A collection of SHell Utilities useful for testing things",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cfg.Run(cmd, args)
		},
	}

	cmd.AddCommand(
		retryCommand(),
		waitCommand(),
	)

	return cmd
}

func (c *cfg) Run(cmd *cobra.Command, args []string) error {
	return nil
}
