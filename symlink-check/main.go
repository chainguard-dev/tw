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

type Config struct {
	Paths    []string
	Packages []string
}

type Result struct {
	Passes       int64
	Fails        int64
	FailMessages []string
	mu           sync.Mutex
}

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

func main() {
	config := parseArgs()
	result := &Result{
		FailMessages: make([]string, 0),
	}

	if len(config.Packages) > 0 {
		for _, pkg := range config.Packages {
			checkPackage(pkg, config.Paths, result)
		}
	} else {
		for _, path := range config.Paths {
			checkPath(path, result)
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

func isInPaths(filePath string, paths []string) bool {
	for _, path := range paths {
		cleanPath := filepath.Clean(path)
		if strings.HasPrefix(filePath, cleanPath+"/") || filePath == cleanPath {
			return true
		}
	}
	return false
}

func checkPath(path string, result *Result) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			result.AddFail(fmt.Sprintf("%s: Path does not exist", path))
		} else {
			result.AddFail(fmt.Sprintf("%s: Cannot access path: %v", path, err))
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
				checkSymlink(link, result)
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
		result.AddFail(fmt.Sprintf("Error walking %s: %v", path, err))
	}
}

func checkSymlink(link string, result *Result) {
	target, err := os.Readlink(link)
	if err != nil {
		result.AddFail(fmt.Sprintf("%s: Cannot read symlink target", link))
		return
	}

	if _, err := os.Stat(link); err != nil {
		if os.IsNotExist(err) {
			if target == "" {
				result.AddFail(fmt.Sprintf("%s: Points to empty target", link))
			} else {
				result.AddFail(fmt.Sprintf("%s: Points to non-existent target '%s'", link, target))
			}
		} else {
			result.AddFail(fmt.Sprintf("%s: Cannot access target '%s': %v", link, target, err))
		}
		return
	}

	if file, err := os.Open(link); err != nil {
		result.AddFail(fmt.Sprintf("%s: Target '%s' exists but is not readable: %v", link, target, err))
		return
	} else {
		file.Close()
	}

	result.AddPass(fmt.Sprintf("%s -> %s", link, target))
}

func checkPackage(pkg string, filterPaths []string, result *Result) {
	cmd := exec.Command("apk", "info", "-eq", pkg)
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Run(); err != nil {
		result.AddFail(fmt.Sprintf("Package '%s' is not installed or apk command failed: %v", pkg, err))
		return
	}

	cmd = exec.Command("apk", "info", "-Lq", pkg)
	output, err := cmd.Output()
	if err != nil {
		result.AddFail(fmt.Sprintf("Failed to list files in package '%s': %v", pkg, err))
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
				checkSymlink(link, result)
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
		result.AddFail(fmt.Sprintf("Error reading package file list for %s: %v", pkg, err))
	}
}
