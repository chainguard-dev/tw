package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"chainguard.dev/apko/pkg/apk/apk"
)

const progName = "no-docs-check"

// Config holds the command-line configuration for documentation checking
type Config struct {
	Package string // Package name to check for documentation files
}

func main() {
	config := parseArgs()

	if err := checkNoDocsViolations(config.Package); err != nil {
		fmt.Printf("FAIL[%s]: %v\n", progName, err)
		os.Exit(1)
	}

	fmt.Printf("PASS[%s]: Package [%s] does not contain documentation files\n", progName, config.Package)
}

func parseArgs() *Config {
	config := &Config{}

	var helpFlag bool
	flag.StringVar(&config.Package, "package", "", "Package name to check for documentation files")
	flag.BoolVar(&helpFlag, "help", false, "Show help message")

	flag.Usage = showHelp
	flag.Parse()

	if helpFlag {
		showHelp()
	}

	if config.Package == "" {
		fmt.Printf("FAIL[%s]: Package name is required\n", progName)
		os.Exit(1)
	}

	return config
}

func showHelp() {
	fmt.Printf(`Usage: %s [OPTIONS]

Tool to check that packages do not contain documentation files.

Options:
  -h, --help                    Show this help message and exit
  --package=PKG                 Package name to check

Examples:
  %s --package=nginx
`, progName, progName)
	os.Exit(0)
}

func checkNoDocsViolations(packageName string) error {
	ctx := context.Background()
	a, err := apk.New(ctx)
	if err != nil {
		return fmt.Errorf("failed to create apk client: %v", err)
	}

	// Get all installed packages
	pkgs, err := a.GetInstalled()
	if err != nil {
		return fmt.Errorf("failed to get installed packages: %v", err)
	}

	var pkg *apk.InstalledPackage
	for _, p := range pkgs {
		if p.Name == packageName {
			pkg = p
			break
		}
	}

	if pkg == nil {
		return fmt.Errorf("package not installed: %s", packageName)
	}

	docFiles := checkPackageFiles(pkg)

	if len(docFiles) > 0 {
		fmt.Printf("Package [%s] contains documentation files:\n", packageName)
		for _, file := range docFiles {
			fmt.Printf("  /%s\n", file)
		}
		fmt.Println()
		fmt.Println("These files should be moved to a -doc subpackage.")
		fmt.Println("Please add the split/alldocs pipeline.")
		fmt.Println()
		fmt.Printf("Total documentation files found: %d\n", len(docFiles))
		return fmt.Errorf("documentation files found in package")
	}

	return nil
}

func checkPackageFiles(pkg *apk.InstalledPackage) []string {
	var docFiles []string
	docPaths := getDocumentationPaths()

	for _, f := range pkg.Files {
		filePath := f.Name
		fullPath := "/" + filePath

		if isDocumentationFile(filePath, docPaths) {
			if _, err := os.Stat(fullPath); err == nil {
				docFiles = append(docFiles, filePath)
			}
		}
	}

	return docFiles
}

func getDocumentationPaths() []string {
	return []string{
		"usr/share/man/",
		"usr/local/share/man/",
		"usr/man/",
		"usr/share/info/",
		"usr/local/share/info/",
        "usr/share/doc",
        "usr/share/local/doc",
        "usr/local/share/doc",
	}
}

func isDocumentationFile(filePath string, docPaths []string) bool {
	for _, docPath := range docPaths {
		if strings.HasPrefix(filePath, docPath) {
    		if filepath.Base(filePath) == "dir" {
				continue
			}
			return true
		}
	}
	return false
}

func info(msg string) {
	fmt.Printf("INFO[%s]: %s\n", progName, msg)
}
