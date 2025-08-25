// Package checkers provides various checkers for different package types in Wolfi
package checkers

import (
	"fmt"
	"strings"

	"github.com/chainguard-dev/cg-tw/package-type-check/pkg/utils"
)

func CheckDocsPackage(pkg string, pathPrefix string) error {
	fmt.Printf("Checking if package %s is a valid documentation package\n", pkg)

	if pathPrefix == "" {
		pathPrefix = "usr/share"
	}

	// Check 1: if the package is empty
	isEmpty, err := utils.IsEmptyPackage(pkg)
	if err != nil {
		return err
	}
	if isEmpty {
		return fmt.Errorf("FAIL [1/2]: Documentation package [%s] is completely empty (i.e. installs no files).\n"+
			"Please check the package build for proper docs installation", pkg)
	}
	fmt.Printf("PASS [1/2]: Documentation package [%s] is not empty\n", pkg)

	// Check 2: File content is a valid documentation file
	files, err := utils.GetPackageFiles(pkg)
	if err != nil {
		return err
	}

	hasDocFiles := false
	for _, file := range files {
		if strings.HasPrefix(file, pathPrefix+"/man/") && !strings.Contains(file, "usr/share/man/db/") {
			if utils.FileExists("/" + file) {
				if utils.TestManPage("/" + file) {
					hasDocFiles = true
				}
			}
		} else if strings.HasPrefix(file, pathPrefix+"/info/") {
			if utils.FileExists("/" + file) {
				if utils.TestInfoPage("/" + file) {
					hasDocFiles = true
				}
			}
		} else if strings.HasPrefix(file, pathPrefix+"/") {
			if utils.FileExists("/" + file) {
				if utils.TestReadableFile("/" + file) {
					hasDocFiles = true
				}
			}
		}
	}

	if !hasDocFiles {
		return fmt.Errorf("FAIL [2/2]: Documentation package [%s] does not contain any valid usable documentation files\n"+
			"Please check the package build for proper docs installation", pkg)
	}
	fmt.Printf("PASS [2/2]: Documentation package [%s] contains valid documentation files\n", pkg)
	return nil
}
