package main

import (
	"fmt"
	"os"

	"github.com/debasishbsws/cg-tw/package-type-check/pkg/checkers"
	"github.com/spf13/cobra"
)

func main() {
	var rootCmd = &cobra.Command{
		Use:   "package-type-check",
		Short: "A tool to check and verify the type of package in Wolfi",
		Long:  `This tool is used in wolfi melange test configuration to verify the kind of packages: : docs, meta, static, virtual, and biproduct packages.`,
		Run: func(cmd *cobra.Command, args []string) {
			// Default help message if no command is provided
			cmd.Help()
		},
	}

	// Add all subcommands
	// TODO: Add other commands for static, biproduct
	rootCmd.AddCommand(CheckDocsCommand())
	rootCmd.AddCommand(CheckMetaCommand())
	rootCmd.AddCommand(CheckVirtualCommand())

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func CheckDocsCommand() *cobra.Command {
	var pathPrefix string

	cmd := &cobra.Command{
		Use:   "docs <PACKAGE>",
		Short: "Check and verify the package is a documentation package",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return checkers.CheckDocsPackage(args[0], pathPrefix)
		},
	}

	cmd.Flags().StringVar(&pathPrefix, "path-prefix", "usr/share", "Specify the path prefix used for documentation")
	return cmd
}

func CheckMetaCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "meta <PACKAGE>",
		Short: "Check and verify the package is a meta package",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return checkers.CheckMetaPackage(args[0])
		},
	}
}

func CheckStaticCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "static <PACKAGE>",
		Short: "Check and verify the package is a static package",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("Static package check for %s is not implemented yet\n", args[0])
		},
	}
}

func CheckVirtualCommand() *cobra.Command {
	var virtualPkg []string

	cmd := &cobra.Command{
		Use:   "virtual <PACKAGE>",
		Short: "Check and verify the package is a virtual package",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return checkers.CheckVirtualPackage(args[0], virtualPkg)
		},
	}

	cmd.Flags().StringArrayVar(&virtualPkg, "virtual-pkg", []string{}, "The names of the virtual packages")
	cmd.MarkFlagRequired("virtual-pkg")
	return cmd
}

func CheckBiProductCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "biproduct <PACKAGE>",
		Short: "Check and verify the package is a bi-product (can't be installed by the package manager) package",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return checkers.CheckBiProductPackage(args[0])
		},
	}
}
