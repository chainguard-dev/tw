package main

import (
	"fmt"
)

// handleInstalledCommand handles the --installed flag functionality
func handleInstalledCommand(db *APKDatabase, packages []string, verbose bool) bool {
	success := true

	for _, pkgName := range packages {
		pkg, exists := db.GetPackage(pkgName)
		if exists {
			if verbose {
				fmt.Printf("%s-%s\n", pkg.Name, pkg.Version)
			} else {
				fmt.Printf("%s\n", pkg.Name)
			}
		} else {
			success = false
		}
	}

	return success
}

// handleContentsCommand handles the --contents flag functionality
func handleContentsCommand(db *APKDatabase, packages []string, quiet bool) bool {
	success := true

	for _, pkgName := range packages {
		pkg, exists := db.GetPackage(pkgName)
		if exists {
			if !quiet {
				fmt.Printf("%s-%s contains:\n", pkg.Name, pkg.Version)
			}
			for _, file := range pkg.Files {
				fmt.Printf("%s\n", file)
			}
			// Add empty line between packages if not quiet and multiple packages
			if !quiet && len(packages) > 1 {
				fmt.Println()
			}
		} else {
			success = false
		}
	}

	return success
}