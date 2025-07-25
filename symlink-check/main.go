package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"chainguard.dev/apko/pkg/apk/apk"
)

const progName = "symlink-check"

// Config holds the command-line configuration for symlink checking
type Config struct {
	Paths         []string // Paths to check for symlinks
	Packages      []string // APK packages to check for symlinks
	AllowDangling bool
	AllowAbsolute bool
}

func (c *Config) checkPackages(result *Result) {
	ctx := context.Background()
	// Create APK instance
	a, err := apk.New(ctx)
	if err != nil {
		result.AddFail(fmt.Sprintf("failed to create apk client: %v", err))
		return
	}

	pkgmap := map[string](*apk.InstalledPackage){}
	// Get all installed packages
	pkgs, err := a.GetInstalled()
	if err != nil {
		result.AddFail(fmt.Sprintf("failed to get installed packages: %v", err))
		return
	}

	for _, pkg := range pkgs {
		pkgmap[pkg.Name] = pkg
	}

	symlinks := make(chan string, 100)
	var wg sync.WaitGroup

	numWorkers := runtime.NumCPU()
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for link := range symlinks {
				checkSymlink(link, result, c.AllowDangling, c.AllowAbsolute)
			}
		}()
	}

	for _, pkgName := range c.Packages {
		fmt.Printf("checking %s\n", pkgName)
		pkg, ok := pkgmap[pkgName]
		if !ok {
			result.AddFail(fmt.Sprintf("package not installed: %s", pkgName))
			continue
		}

		// Process package files
		for _, f := range pkg.Files {
			fullPath := "/" + f.Name
			fmt.Printf("%s\n", fullPath)

			if info, err := os.Lstat(fullPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
				symlinks <- fullPath
			}
		}
	}

	close(symlinks)
	wg.Wait()
}

func checkPath(path string, result *Result, allowDangling, allowAbsolute bool) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			result.AddFail(fmt.Sprintf("path does not exist: %s", path))
		} else {
			result.AddFail(fmt.Sprintf("cannot access path: %s (%v)", path, err))
		}
		return
	}

	symlinks := make(chan string, 100)
	var wg sync.WaitGroup

	numWorkers := runtime.NumCPU()
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for link := range symlinks {
				checkSymlink(link, result, allowDangling, allowAbsolute)
			}
		}()
	}

	err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if strings.HasPrefix(filePath, "/proc/") ||
			strings.HasPrefix(filePath, "/sys/") ||
			strings.HasPrefix(filePath, "/dev/") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if info.Mode()&os.ModeSymlink != 0 {
			symlinks <- filePath
		}

		return nil
	})

	close(symlinks)
	wg.Wait()

	if err != nil {
		result.AddFail(fmt.Sprintf("error walking path: %s (%v)", path, err))
	}
}

// Result tracks the outcomes of symlink checking operations
type Result struct {
	Passes       int64      // Number of valid symlinks found
	Fails        int64      // Number of broken symlinks found
	FailMessages []string   // Error messages for failed symlinks
	mu           sync.Mutex // Protects FailMessages for concurrent access
}

// Result methods for tracking symlink check outcomes

func (r *Result) AddPass(msg string) {
	atomic.AddInt64(&r.Passes, 1)
	fmt.Printf("PASS[%s]: %s\n", progName, msg)
}

func (r *Result) AddFail(msg string) {
	atomic.AddInt64(&r.Fails, 1)
	r.mu.Lock()
	r.FailMessages = append(r.FailMessages, fmt.Sprintf("FAIL[%s]: %s", progName, msg))
	r.mu.Unlock()
}

// Utility functions

func info(msg string) {
	fmt.Printf("INFO[%s]: %s\n", progName, msg)
}

func showHelp() {
	fmt.Printf(`Usage: %s [OPTIONS]

Tool to check for broken/dangling symlinks in the filesystem.

Options:
  -h, --help                    Show this help message and exit
  --paths=PATH, --paths PATH    Specify paths to check (default: /)
  --packages=PKG, --packages PKG
                               Specify packages to check
  --allow-dangling             Allow dangling symlinks
  --allow-absolute             Allow absolute symlinks

Examples:
  %s --paths=/usr/bin
  %s --packages=bash
  %s --allow-dangling --paths=/usr/bin
`, progName, progName, progName, progName)
	os.Exit(0)
}

func parseArgs() *Config {
	config := &Config{}

	var pathsFlag, packagesFlag string
	var helpFlag bool

	flag.StringVar(&pathsFlag, "paths", "", "Specify paths to check (comma-separated)")
	flag.StringVar(&packagesFlag, "packages", "", "Specify packages to check (comma-separated)")
	flag.BoolVar(&helpFlag, "help", false, "Show help message")
	flag.BoolVar(&config.AllowDangling, "allow-dangling", false, "Allow dangling symlinks")
	flag.BoolVar(&config.AllowAbsolute, "allow-absolute", false, "Allow absolute symlinks")

	flag.Usage = showHelp
	flag.Parse()

	if helpFlag {
		showHelp()
	}

	if pathsFlag != "" {
		config.Paths = strings.Split(pathsFlag, ",")
		for i, path := range config.Paths {
			config.Paths[i] = strings.TrimSpace(path)
		}
	}

	if packagesFlag != "" && packagesFlag != "none" {
		config.Packages = strings.Split(packagesFlag, ",")
		for i, pkg := range config.Packages {
			config.Packages[i] = strings.TrimSpace(pkg)
		}
	}

	if len(config.Paths) == 0 && len(config.Packages) == 0 {
		config.Paths = []string{"/"}
	}

	return config
}

// Main application logic

func main() {
	config := parseArgs()
	result := &Result{
		FailMessages: make([]string, 0),
	}

	if len(config.Packages) > 0 {
		config.checkPackages(result)
	}
	if len(config.Paths) > 0 {
		for _, path := range config.Paths {
			checkPath(path, result, config.AllowDangling, config.AllowAbsolute)
		}
	}

	total := result.Passes + result.Fails
	if total == 0 {
		info("No symlinks found to check")
	} else {
		info(fmt.Sprintf("Tested [%d] symlinks with [%s]. [%d/%d] passed.", total, progName, result.Passes, total))
	}

	if result.Fails > 0 {
		fmt.Println()
		fmt.Println("FAILED SYMLINKS:")
		for _, msg := range result.FailMessages {
			fmt.Println(msg)
		}
		os.Exit(1)
	}

	os.Exit(0)
}

// Core symlink checking functions

func checkSymlink(link string, result *Result, allowDangling, allowAbsolute bool) {
	target, err := os.Readlink(link)
	if err != nil {
		result.AddFail(fmt.Sprintf("cannot read symlink target: %s", link))
		return
	}

	// Check if symlink target exists and is accessible first
	if _, err := os.Stat(link); err != nil {
		if !os.IsNotExist(err) {
			result.AddFail(fmt.Sprintf("cannot access target: %s -> %s (%v)", link, target, err))
			return
		}

		// Handle dangling symlinks (target doesn't exist)
		if allowDangling {
			result.AddPass(fmt.Sprintf("dangling symlink (allowed): %s -> %s", link, target))
			return
		}

		if target == "" {
			result.AddFail(fmt.Sprintf("points to empty target: %s", link))
		} else {
			errorMsg := fmt.Sprintf("points to non-existent target: %s -> %s", link, target)
			if filepath.IsAbs(target) && !allowAbsolute {
				errorMsg = fmt.Sprintf("absolute symlink points to non-existent target: %s -> %s", link, target)
			}
			result.AddFail(errorMsg)
		}
		return
	}

	if filepath.IsAbs(target) {
		if !allowAbsolute {
			result.AddFail(fmt.Sprintf("absolute symlink: %s -> %s", link, target))
			return
		}
	} else if targetEscapesTree(target, link) {
		result.AddFail(fmt.Sprintf("relative symlink escapes root: %s -> %s", link, target))
		return
	}

	// Verify target is readable
	if file, err := os.Open(link); err != nil {
		result.AddFail(fmt.Sprintf("target exists but not readable: %s -> %s (%v)", link, target, err))
		return
	} else {
		file.Close()
	}

	result.AddPass(fmt.Sprintf("%s -> %s", link, target))
}

// targetEscapesTree returns true when opening the relative symlink `link` with the
// destination `target` would result in reading a file filesystem tree rooted at '.'
//
// target should be a relative symlink (not starting with a '/').
// link should not start with a slash or contain any '.' or '..' components.
//
//		Examples:
//		 -  targetEscapesTree("../../usr/bin/bar", "usr/bin/foo") -> false
//	  -  targetEscapesTree("../../../../../../etc/passwd", "etc/passwd") -> true
func targetEscapesTree(target, link string) bool {
	// If target is absolute, it escapes the tree
	if filepath.IsAbs(target) {
		return true
	}

	// Get the directory of the source symlink
	linkDir := filepath.Dir(link)

	// Join the source directory with the target to get the absolute path
	absTarget := filepath.Join(linkDir, target)

	// Clean the path to resolve all ".." and "." components
	cleanTarget := filepath.Clean(absTarget)

	// If the cleaned path starts with ".." or equals "..", it escapes the tree
	return strings.HasPrefix(cleanTarget, "../") || cleanTarget == ".."
}
