package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultAPKDatabase = "/lib/apk/db/installed"
)

// Package represents an installed APK package
type Package struct {
	Name        string
	Version     string
	Arch        string
	Description string
	Files       []string
}

// APKDatabase represents the APK installed packages database
type APKDatabase struct {
	packages map[string]*Package
}

// NewAPKDatabase creates a new APK database reader
func NewAPKDatabase() (*APKDatabase, error) {
	return NewAPKDatabaseFromFile(DefaultAPKDatabase)
}

// NewAPKDatabaseFromDir creates a new APK database reader from a directory
func NewAPKDatabaseFromDir(dir string) (*APKDatabase, error) {
	installedPath := filepath.Join(dir, "installed")
	return NewAPKDatabaseFromFile(installedPath)
}

// NewAPKDatabaseFromFile creates a new APK database reader from a specific file
func NewAPKDatabaseFromFile(path string) (*APKDatabase, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database file %s: %w", path, err)
	}
	defer file.Close()

	db := &APKDatabase{
		packages: make(map[string]*Package),
	}

	scanner := bufio.NewScanner(file)
	var currentPkg *Package
	var currentDir string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		
		// Empty line indicates end of package record
		if line == "" {
			if currentPkg != nil && currentPkg.Name != "" {
				db.packages[currentPkg.Name] = currentPkg
			}
			currentPkg = nil
			currentDir = ""
			continue
		}

		// Start of new package or field
		if len(line) < 2 || line[1] != ':' {
			continue
		}

		field := line[0:1]
		value := strings.TrimSpace(line[2:])

		switch field {
		case "P": // Package name
			currentPkg = &Package{Name: value}
			currentDir = ""
		case "V": // Version
			if currentPkg != nil {
				currentPkg.Version = value
			}
		case "A": // Architecture
			if currentPkg != nil {
				currentPkg.Arch = value
			}
		case "T": // Description
			if currentPkg != nil {
				currentPkg.Description = value
			}
		case "F": // Directory
			currentDir = value
			if currentDir != "" && !strings.HasSuffix(currentDir, "/") {
				currentDir += "/"
			}
		case "R": // Files (relative to current directory)
			if currentPkg != nil {
				var fullPath string
				if currentDir != "" {
					fullPath = currentDir + value
				} else {
					fullPath = value
				}
				currentPkg.Files = append(currentPkg.Files, fullPath)
			}
		}
	}

	// Handle last package if file doesn't end with empty line
	if currentPkg != nil && currentPkg.Name != "" {
		db.packages[currentPkg.Name] = currentPkg
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading database: %w", err)
	}

	return db, nil
}

// GetPackage returns a package by name if it exists
func (db *APKDatabase) GetPackage(name string) (*Package, bool) {
	pkg, exists := db.packages[name]
	return pkg, exists
}

// IsInstalled checks if a package is installed
func (db *APKDatabase) IsInstalled(name string) bool {
	_, exists := db.packages[name]
	return exists
}