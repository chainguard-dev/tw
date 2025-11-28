package checkers

import (
	"fmt"

	"github.com/chainguard-dev/cg-tw/package-type-check/pkg/utils"
)

func CheckDevPackage(pkg string) error {
	fmt.Printf("Checking if package %s is a valid dev package\n", pkg)

	// Check 1: Package name must end with -dev or -devel
	if !utils.IsDevPackage(pkg) {
		return fmt.Errorf("FAIL [1/3]: Dev package [%s] name does not end with -dev or -devel", pkg)
	}
	fmt.Printf("PASS [1/3]: Dev package [%s] has correct naming convention\n", pkg)

	// Check 2: Package should not be empty
	isEmpty, err := utils.IsEmptyPackage(pkg)
	if err != nil {
		return err
	}
	if isEmpty {
		return fmt.Errorf("FAIL [2/3]: Dev package [%s] is completely empty (installs no files)", pkg)
	}
	fmt.Printf("PASS [2/3]: Dev package [%s] is not empty\n", pkg)

	// Check 3: Package should contain .h files under /usr
	hasHeaders, err := utils.HasHeaderFiles(pkg)
	if err != nil {
		return err
	}
	if !hasHeaders {
		return fmt.Errorf("FAIL [3/3]: Dev package [%s] does not contain any .h files under /usr", pkg)
	}
	fmt.Printf("PASS [3/3]: Dev package [%s] contains header files under /usr\n", pkg)

	return nil
}