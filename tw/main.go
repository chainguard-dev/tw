package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chainguard-dev/tw/pkg/commands/assert"
	"github.com/chainguard-dev/tw/pkg/commands/kgrep"
	"github.com/chainguard-dev/tw/pkg/commands/kimages"
	"github.com/chainguard-dev/tw/pkg/commands/sfuzz"
	"github.com/chainguard-dev/tw/pkg/commands/shu"
	"github.com/spf13/cobra"
)

var cmds = map[string]*cobra.Command{
	"sfuzz":   sfuzz.Command(),
	"kgrep":   kgrep.Command(),
	"kimages": kimages.Command(),
	"wassert": assert.Command(),
	"shu":     shu.Command(),
}

func main() {
	// First check if we're being called as a multicall
	ename := filepath.Base(os.Args[0])
	if cmd, ok := cmds[ename]; ok {
		if err := cmd.Execute(); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}

	// If not a multicall, then build the regular command hierarchy
	cmd := &cobra.Command{
		Use:          "tw",
		SilenceUsage: true,
	}

	for _, c := range cmds {
		cmd.AddCommand(c)
	}

	// Add a helper command for creating the multicall symlinks
	cmd.AddCommand(
		&cobra.Command{
			Use:    "list-multicalls",
			Hidden: true,
			Run: func(cmd *cobra.Command, args []string) {
				names := make([]string, 0)
				for _, c := range cmds {
					names = append(names, c.Name())
				}
				sort.Strings(names)
				fmt.Fprint(cmd.OutOrStdout(), strings.Join(names, " "))
			},
		},
	)

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}
