package checkers

import (
	"fmt"
	"strings"

	"github.com/chainguard-dev/cg-tw/package-type-check/pkg/utils"
)

func CheckMetaPackage(pkg string) error {
	fmt.Printf("Checking if package %s is a valid meta package\n", pkg)

	// Check 1: if the package is empty
	isEmpty, err := utils.IsEmptyPackage(pkg)
	if err != nil {
		return err
	}
	if !isEmpty {
		return fmt.Errorf("FAIL [1/3]: Meta package [%s] is not empty (i.e. installs files).\n"+
			"A metapackage is an empty package that only declares dependencies on other packages", pkg)
	}
	fmt.Printf("PASS [1/3]: This package [%s] is empty (i.e. installs no files)\n", pkg)

	// Check 2: Description contains 'meta' keyword
	description, err := utils.GetPackageDescription(pkg)
	if err != nil {
		return err
	}
	if !strings.Contains(strings.ToLower(description), "meta") {
		return fmt.Errorf("FAIL [2/3]: Meta package [%s] does not contain 'meta' in its description.\n"+
			"A metapackage must have 'meta' in its description", pkg)
	}
	fmt.Printf("PASS [2/3]: This package [%s] contains 'meta' in its description\n", pkg)

	// Check 3: Package must has dependencies
	depCount, err := utils.GetPackageDependencyCount(pkg)
	if err != nil {
		return err
	}
	if depCount == 0 {
		return fmt.Errorf("FAIL [3/3]: Meta package [%s] has no dependencies.\n"+
			"A metapackage must have at least one dependency", pkg)
	}
	fmt.Printf("PASS [3/3]: This package [%s] has %d runtime dependencies\n", pkg, depCount)
	return nil
}
