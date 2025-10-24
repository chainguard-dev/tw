package utils

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// IsPackageInstalled checks if the package is installed
func IsPackageInstalled(pkg string) error {
	cmd := exec.Command("apk", "info", "-eq", pkg)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("package %q is not installed: %w", pkg, err)
	}
	return nil
}

// GetTotalApkCount retrieves the total count of installed APK packages in the environment
func GetTotalApkCount() int {
	cmd := exec.Command("apk", "info", "-L")
	output, err := cmd.Output()
	if err != nil {
		return 0
	}

	// Split the output by lines and count the number of lines
	lines := strings.Split(string(output), "\n")
	count := 0
	for _, line := range lines {
		if line != "" {
			count++
		}
	}
	return count
}

// GetPackageFiles retrieves the list of files installed by the package
func GetPackageFiles(pkg string) ([]string, error) {
	if err := IsPackageInstalled(pkg); err != nil {
		return nil, err
	}

	cmd := exec.Command("apk", "info", "-qL", pkg)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get files for package %q: %w", pkg, err)
	}
	// Look Again
	return strings.Split(string(output), "\n"), nil
}

// IsEmptyPackage checks if the package is empty and only contains SBOM Files
func IsEmptyPackage(pkg string) (bool, error) {
	files, err := GetPackageFiles(pkg)
	if err != nil {
		return false, err
	}

	nonSBOMFileCount := 0
	for _, file := range files {
		if !strings.Contains(file, "var/lib/db/sbom") && !strings.HasSuffix(file, ".spdx.json") {
			nonSBOMFileCount++
		}
	}

	return nonSBOMFileCount == 0, nil
}

// GetPackageDescription retrieves the package description
func GetPackageDescription(pkg string) (string, error) {
	if err := IsPackageInstalled(pkg); err != nil {
		return "", err
	}

	cmd := exec.Command("apk", "info", "--installed", "--description", pkg)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get description for package %q: %w", pkg, err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) < 2 {
		return "", fmt.Errorf("unexpected description format for package %s", pkg)
	}
	return strings.TrimSpace(lines[1]), nil
}

func GetPackageDependency(pkg string) ([]string, error) {
	if err := IsPackageInstalled(pkg); err != nil {
		return nil, err
	}

	cmd := exec.Command("apk", "info", "--installed", "--depends", pkg)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get dependencies for package %q: %w", pkg, err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	dependencies := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			dependencies = append(dependencies, strings.TrimSpace(line))
		}
	}
	return dependencies, nil
}

func GetPackageProvides(pkg string) ([]string, error) {
	if err := IsPackageInstalled(pkg); err != nil {
		return nil, err
	}

	cmd := exec.Command("apk", "info", "--installed", "--provides", pkg)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get provides for package %q: %w", pkg, err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	provides := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			provides = append(provides, strings.TrimSpace(line))
		}
	}
	return provides, nil
}

// GetPackageDependencyCount retrieves the package runtime dependency count
func GetPackageDependencyCount(pkg string) (int, error) {
	count, err := GetPackageDependency(pkg)
	if err != nil {
		return 0, err
	}
	return len(count), nil
}

// FileExists checks if a file exists at the given path
func FileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return err == nil
}

// TestManPage tests if a man page is readable
func TestManPage(path string) bool {
	cmd := exec.Command("man", "-l", path)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

// TestInfoPage tests if an info page is readable
func TestInfoPage(path string) bool {
	cmd := exec.Command("info", "-f", path, "-o", "-")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return len(strings.Split(string(output), "\n")) > 0
}

// TestReadableFile tests if a file is readable
func TestReadableFile(path string) bool {
	cmd := exec.Command("cat", path)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}
