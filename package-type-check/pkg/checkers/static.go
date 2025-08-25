package checkers

import (
	"fmt"
	"strings"

	"github.com/debasishbsws/cg-tw/package-type-check/pkg/utils"
)

func CheckStaticPackage(pkg string) error {
	fmt.Printf("Checking if package %s is a valid static package\n", pkg)

	// Check 1: Package is not empty
	isEmpty, err := utils.IsEmptyPackage(pkg)
	if err != nil {
		return err
	}
	if isEmpty {
		return fmt.Errorf("FAIL [1/3]: Static package [%s] is empty (i.e. installs no files).\n"+
			"A static package must not be empty, and should have at least one static library", pkg)
	}
	fmt.Printf("PASS [1/3]: Static package [%s] is not empty\n", pkg)

	// Retrive package files excluding SBOM

	files, err := utils.GetPackageFiles(pkg)
	if err != nil {
		return err
	}

	var nonSBOMFiles []string
	for _, file := range files {
		if !strings.Contains(file, "var/lib/db/sbom") && !strings.HasSuffix(file, ".spdx.json") {
			nonSBOMFiles = append(nonSBOMFiles, file)
		}
	}

	// Check 2: Contains .a files
	staticLibcount := 0
	for _, file := range nonSBOMFiles {
		if strings.HasSuffix(file, ".a") {
			staticLibcount++
		}
	}
	if staticLibcount == 0 {
		return fmt.Errorf("FAIL [2/3]: Static package [%s] does not contain any .a files.\n"+
			"A static package must contain at least one static library (.a file)", pkg)
	}
	fmt.Printf("PASS [2/3]: Static package [%s] contains static library(.a) files\n", pkg)

	// Check 3: Contains only .a files
	if len(nonSBOMFiles) > staticLibcount {
		return fmt.Errorf("FAIL [3/3]: Static package [%s] also contains %d non-static files.\n"+
			"A static package must contain only static library (.a) files", pkg, len(nonSBOMFiles)-staticLibcount)
	}
	fmt.Printf("PASS [3/3]: Static package [%s] contains only static library(.a) files\n", pkg)
	return nil
}
