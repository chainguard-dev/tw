// chelm is a CLI for validating Helm chart image mappings.
// It is designed to complement melange pipelines for chart packaging and testing.
package main

import (
	"fmt"
	"os"

	"chainguard.dev/tw/chelm/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
