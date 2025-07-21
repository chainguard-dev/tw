package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

const progName = "symlink-check"

// Config holds the command-line configuration for symlink checking
type Config struct {
	Paths        []string // Paths to check for symlinks
	Packages     []string // APK packages to check for symlinks
	RelativeOnly bool     // Only check relative symlinks, skip absolute ones
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
  --relative-only              Only check relative symlinks, ignore absolute ones

Examples:
  %s --paths=/usr/bin
  %s --packages=bash
`, progName, progName, progName)
	os.Exit(0)
}

func parseArgs() *Config {
	config := &Config{}

	var pathsFlag, packagesFlag string
	var helpFlag bool

	flag.StringVar(&pathsFlag, "paths", "", "Specify paths to check (comma-separated)")
	flag.StringVar(&packagesFlag, "packages", "", "Specify packages to check (comma-separated)")
	flag.BoolVar(&helpFlag, "help", false, "Show help message")
	flag.BoolVar(&config.RelativeOnly, "relative-only", false, "Only check relative symlinks, ignore absolute ones")

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
		for _, pkg := range config.Packages {
			checkPackage(pkg, config.Paths, result, config.RelativeOnly)
		}
	} else {
		for _, path := range config.Paths {
			checkPath(path, result, config.RelativeOnly)
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

func isInPaths(filePath string, paths []string) bool {
	for _, path := range paths {
		cleanPath := filepath.Clean(path)
		if strings.HasPrefix(filePath, cleanPath+"/") || filePath == cleanPath {
			return true
		}
	}
	return false
}

func checkPath(path string, result *Result, relativeOnly bool) {
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
				checkSymlink(link, result, relativeOnly)
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

func checkSymlink(link string, result *Result, relativeOnly bool) {
	target, err := os.Readlink(link)
	if err != nil {
		result.AddFail(fmt.Sprintf("cannot read symlink target: %s", link))
		return
	}

	// Handle relative-only mode
	if relativeOnly {
		if filepath.IsAbs(target) {
			// Skip absolute symlinks in relative-only mode
			return
		}

		// Check if relative target escapes root filesystem
		if err := checkEscapeRoot(link, target); err != nil {
			result.AddFail(fmt.Sprintf("relative symlink escapes root: %s -> %s", link, target))
			return
		}
	}

	// Check if symlink target exists and is accessible
	if _, err := os.Stat(link); err != nil {
		if os.IsNotExist(err) {
			if target == "" {
				result.AddFail(fmt.Sprintf("points to empty target: %s", link))
			} else {
				result.AddFail(fmt.Sprintf("points to non-existent target: %s -> %s", link, target))
			}
		} else {
			result.AddFail(fmt.Sprintf("cannot access target: %s -> %s (%v)", link, target, err))
		}
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

// checkEscapeRoot verifies if a relative symlink target would escape the root filesystem
func checkEscapeRoot(link, target string) error {
	linkDir := filepath.Dir(link)
	resolvedPath := filepath.Join(linkDir, target)
	cleanPath := filepath.Clean(resolvedPath)

	relToRoot, err := filepath.Rel("/", cleanPath)
	if err != nil {
		return fmt.Errorf("error computing relative path: %v", err)
	}

	if strings.HasPrefix(relToRoot, "..") {
		return fmt.Errorf("path escapes root")
	}

	return nil
}

func checkPackage(pkg string, filterPaths []string, result *Result, relativeOnly bool) {
	cmd := exec.Command("apk", "info", "-eq", pkg)
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Run(); err != nil {
		result.AddFail(fmt.Sprintf("package not installed or apk failed: %s (%v)", pkg, err))
		return
	}

	cmd = exec.Command("apk", "info", "-Lq", pkg)
	output, err := cmd.Output()
	if err != nil {
		result.AddFail(fmt.Sprintf("failed to list package files: %s (%v)", pkg, err))
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
				checkSymlink(link, result, relativeOnly)
			}
		}()
	}

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		file := strings.TrimSpace(scanner.Text())
		if file == "" {
			continue
		}

		fullPath := "/" + file

		if len(filterPaths) > 0 && !isInPaths(fullPath, filterPaths) {
			continue
		}

		if info, err := os.Lstat(fullPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
			symlinks <- fullPath
		}
	}

	close(symlinks)
	wg.Wait()

	if err := scanner.Err(); err != nil {
		result.AddFail(fmt.Sprintf("error reading package file list: %s (%v)", pkg, err))
	}
}
