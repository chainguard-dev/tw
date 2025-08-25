package checkers

import (
	"fmt"
	"os/exec"

	"github.com/debasishbsws/cg-tw/package-type-check/pkg/utils"
)

// Check performs the virtual package checks
func CheckVirtualPackage(pkg string, virtualPkgs []string) error {
	fmt.Printf("Checking if package %v is/are valid Virtual Packages of %s\n", virtualPkgs, pkg)

	if len(virtualPkgs) == 0 {
		return fmt.Errorf("no virtual packages specified")
	}

	provides, err := utils.GetPackageProvides(pkg)
	if err != nil {
		return fmt.Errorf("failed to get provides for package %q: %w", pkg, err)
	}

	providesSet := make(map[string]bool, len(provides))
	for _, p := range provides {
		providesSet[p] = true
	}

	// Check all virtual packages exist in provides (fast map lookups)
	var missingPkgs []string
	for _, vp := range virtualPkgs {
		if !providesSet[vp] {
			missingPkgs = append(missingPkgs, vp)
		}
	}

	if len(missingPkgs) > 0 {
		return fmt.Errorf("FAIL: package %q does not provide virtual packages: %v", pkg, missingPkgs)
	}

	// Get initial package count once
	initialCount := utils.GetTotalApkCount()

	// Test all virtual packages in a single batch operation if possible
	if err := testVirtualPackagesInstallation(virtualPkgs, pkg, initialCount); err != nil {
		return err
	}

	fmt.Printf("PASS: All packages %v are valid virtual packages provided by %s\n", virtualPkgs, pkg)

	return nil
}

func testVirtualPackagesInstallation(virtualPkgs []string, pkg string, initialCount int) error {
	for _, vp := range virtualPkgs {
		if err := testSingleVirtualPackage(vp, pkg, initialCount); err != nil {
			return err
		}
	}
	return nil
}

// testSingleVirtualPackage tests a single virtual package installation
func testSingleVirtualPackage(virtualPkg, pkg string, expectedCount int) error {
	cmd := exec.Command("apk", "add", virtualPkg)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("FAIL: failed to run 'apk add %s': %w", virtualPkg, err)
	}

	// Only check count if dry-run wasn't conclusive
	newCount := utils.GetTotalApkCount()
	if newCount > expectedCount {
		return fmt.Errorf("FAIL: 'apk add %s' installed additional packages (package count: %d -> %d), but should be a no-op as %s provides %s",
			virtualPkg, expectedCount, newCount, pkg, virtualPkg)
	}

	return nil
}
