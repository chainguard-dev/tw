package checkers

import (
	"fmt"
	"strings"

	"github.com/chainguard-dev/cg-tw/package-type-check/pkg/utils"
)

func CheckEmptyPackage(pkg string) error {
	fmt.Printf("Checking if package %s is an empty package\n", pkg)

	empty, err := utils.IsEmptyPackage(pkg)
	if err != nil {
		return err
	}

	if !empty {
		files, err := utils.GetPackageFiles(pkg)
		if err != nil {
			return err
		}
		fileList := strings.Join(files, "\n  ")
		return fmt.Errorf("FAIL: Package [%s] is not empty: %s", pkg, fileList)
	}

	fmt.Printf("PASS: Package [%s] is empty", pkg)

    return nil
}
