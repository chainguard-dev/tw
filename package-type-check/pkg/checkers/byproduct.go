package checkers

import (
	"fmt"
	"os/exec"
	"strings"
)

// IsSameNamePackageInstalled checks if a package is really installed
func IsSameNamePackageInstalled(pkg string) (bool, error) {
	cmd := exec.Command("apk", "list", "--installed", pkg)
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to get installed version for package %q: %w", pkg, err)
	}

	// Split the output by lines and get the first line
	lines := strings.Split(string(output), "\n")
	if len(lines) == 0 || lines[0] == "" || strings.Compare(lines[0], pkg) != 0 {
		return false, nil // Package not installed
	}

	return true, nil
}

func CheckByProductPackage(pkg string) error {
	fmt.Printf("Checking if package %s is a valid by-product (can't be installed by the package manager) package\n", pkg)

	// Try to install the package
	cmd := exec.Command("apk", "add", pkg)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		// If installation fails, that no package even provides this package which is not good
		fmt.Printf("FAIL: package %q could not be installed, it might not be provided by any package: %v\n", pkg, err)
		return nil
	}

	installed, err := IsSameNamePackageInstalled(pkg)
	if err != nil {
		return fmt.Errorf("failed to check if package %q is installed: %w", pkg, err)
	}
	if installed {
		return fmt.Errorf("FAIL: package %q is installed, but it is a by-product package which should not be installed by the package manager", pkg)
	}

	fmt.Printf("PASS: package %q can't be installed by the package manager, it is a valid by-product package\n", pkg)

	return nil
}
