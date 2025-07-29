package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewAPKDatabaseFromFile(t *testing.T) {
	// Create a temporary test database file
	testData := `P:busybox
V:1.37.0-r47
A:aarch64
T:Size optimized toolbox of many common UNIX utilities
F:etc
R:securetty
F:etc/busybox-paths.d
R:busybox
F:usr/bin
R:busybox
F:var/lib/db/sbom
R:busybox-1.37.0-r47.spdx.json

P:tree
V:2.2.1-r0
A:aarch64
T:A recursive directory listing program
F:usr/bin
R:tree
F:var/lib/db/sbom
R:tree-2.2.1-r0.spdx.json

P:minimal-pkg
V:1.0.0-r0
A:aarch64
T:Minimal package for testing
R:simple-file
`

	tmpFile, err := os.CreateTemp("", "apk_test_*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(testData); err != nil {
		t.Fatalf("Failed to write test data: %v", err)
	}
	tmpFile.Close()

	// Test loading the database
	db, err := NewAPKDatabaseFromFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load database: %v", err)
	}

	// Test busybox package
	busybox, exists := db.GetPackage("busybox")
	if !exists {
		t.Error("busybox package should exist")
	}
	if busybox.Name != "busybox" {
		t.Errorf("Expected name 'busybox', got '%s'", busybox.Name)
	}
	if busybox.Version != "1.37.0-r47" {
		t.Errorf("Expected version '1.37.0-r47', got '%s'", busybox.Version)
	}
	if busybox.Arch != "aarch64" {
		t.Errorf("Expected arch 'aarch64', got '%s'", busybox.Arch)
	}

	expectedFiles := []string{
		"etc/securetty",
		"etc/busybox-paths.d/busybox",
		"usr/bin/busybox",
		"var/lib/db/sbom/busybox-1.37.0-r47.spdx.json",
	}
	if len(busybox.Files) != len(expectedFiles) {
		t.Errorf("Expected %d files, got %d", len(expectedFiles), len(busybox.Files))
	}
	for i, expected := range expectedFiles {
		if i >= len(busybox.Files) || busybox.Files[i] != expected {
			t.Errorf("Expected file '%s' at index %d, got '%s'", expected, i, busybox.Files[i])
		}
	}

	// Test tree package
	tree, exists := db.GetPackage("tree")
	if !exists {
		t.Error("tree package should exist")
	}
	if tree.Version != "2.2.1-r0" {
		t.Errorf("Expected version '2.2.1-r0', got '%s'", tree.Version)
	}

	// Test minimal package (no F: records, just R:)
	minimal, exists := db.GetPackage("minimal-pkg")
	if !exists {
		t.Error("minimal-pkg package should exist")
	}
	if len(minimal.Files) != 1 || minimal.Files[0] != "simple-file" {
		t.Errorf("Expected file 'simple-file', got %v", minimal.Files)
	}

	// Test non-existent package
	_, exists = db.GetPackage("non-existent")
	if exists {
		t.Error("non-existent package should not exist")
	}
}

func TestNewAPKDatabaseFromDir(t *testing.T) {
	// Create a temporary directory with an 'installed' file
	tmpDir, err := os.MkdirTemp("", "apk_test_dir_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testData := `P:test-pkg
V:1.0.0-r0
A:aarch64
T:Test package
F:usr/bin
R:test-binary
`

	installedFile := filepath.Join(tmpDir, "installed")
	if err := os.WriteFile(installedFile, []byte(testData), 0644); err != nil {
		t.Fatalf("Failed to write installed file: %v", err)
	}

	// Test loading from directory
	db, err := NewAPKDatabaseFromDir(tmpDir)
	if err != nil {
		t.Fatalf("Failed to load database from dir: %v", err)
	}

	pkg, exists := db.GetPackage("test-pkg")
	if !exists {
		t.Error("test-pkg should exist")
	}
	if pkg.Version != "1.0.0-r0" {
		t.Errorf("Expected version '1.0.0-r0', got '%s'", pkg.Version)
	}
	if len(pkg.Files) != 1 || pkg.Files[0] != "usr/bin/test-binary" {
		t.Errorf("Expected file 'usr/bin/test-binary', got %v", pkg.Files)
	}
}

func TestIsInstalled(t *testing.T) {
	testData := `P:installed-pkg
V:1.0.0-r0
A:aarch64
T:Test package
F:usr/bin
R:binary
`

	tmpFile, err := os.CreateTemp("", "apk_test_*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(testData); err != nil {
		t.Fatalf("Failed to write test data: %v", err)
	}
	tmpFile.Close()

	db, err := NewAPKDatabaseFromFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load database: %v", err)
	}

	if !db.IsInstalled("installed-pkg") {
		t.Error("installed-pkg should be installed")
	}

	if db.IsInstalled("not-installed-pkg") {
		t.Error("not-installed-pkg should not be installed")
	}
}

func TestEmptyDatabase(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "apk_empty_*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	db, err := NewAPKDatabaseFromFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load empty database: %v", err)
	}

	if db.IsInstalled("any-package") {
		t.Error("Empty database should not contain any packages")
	}

	_, exists := db.GetPackage("any-package")
	if exists {
		t.Error("Empty database should not contain any packages")
	}
}

func TestMalformedDatabase(t *testing.T) {
	testData := `P:valid-pkg
V:1.0.0-r0
A:aarch64

P:incomplete-pkg
V:2.0.0-r0
# Missing A: field but should still work

P:
V:3.0.0-r0
A:aarch64
# Package with empty name should be ignored

some random line without colon
: line with empty field
X:unknown field type
`

	tmpFile, err := os.CreateTemp("", "apk_malformed_*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(testData); err != nil {
		t.Fatalf("Failed to write test data: %v", err)
	}
	tmpFile.Close()

	db, err := NewAPKDatabaseFromFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load malformed database: %v", err)
	}

	// Valid package should exist
	if !db.IsInstalled("valid-pkg") {
		t.Error("valid-pkg should be installed")
	}

	// Incomplete package should still exist (missing fields are okay)
	if !db.IsInstalled("incomplete-pkg") {
		t.Error("incomplete-pkg should be installed")
	}

	// Package with empty name should be ignored
	if db.IsInstalled("") {
		t.Error("Package with empty name should be ignored")
	}
}

func TestRootDirectoryFiles(t *testing.T) {
	// Test the edge case where F: is empty (meaning root directory)
	testData := `P:redis-operator-compat
V:0.21.0-r1
A:x86_64
T:Compat package for redis-operator
F:
R:operator
F:var/lib/db/sbom
R:redis-operator-compat-0.21.0-r1.spdx.json
`

	tmpFile, err := os.CreateTemp("", "apk_root_test_*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(testData); err != nil {
		t.Fatalf("Failed to write test data: %v", err)
	}
	tmpFile.Close()

	db, err := NewAPKDatabaseFromFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load database: %v", err)
	}

	pkg, exists := db.GetPackage("redis-operator-compat")
	if !exists {
		t.Error("redis-operator-compat package should exist")
	}

	expectedFiles := []string{
		"operator",                                          // File in root directory (F: was empty)
		"var/lib/db/sbom/redis-operator-compat-0.21.0-r1.spdx.json", // File in subdirectory
	}

	if len(pkg.Files) != len(expectedFiles) {
		t.Errorf("Expected %d files, got %d", len(expectedFiles), len(pkg.Files))
	}

	for i, expected := range expectedFiles {
		if i >= len(pkg.Files) || pkg.Files[i] != expected {
			t.Errorf("Expected file '%s' at index %d, got '%s'", expected, i, pkg.Files[i])
		}
	}

	// Specifically test that the root directory file is just "operator", not "/operator"
	if pkg.Files[0] != "operator" {
		t.Errorf("Root directory file should be 'operator', got '%s'", pkg.Files[0])
	}
}

func TestRealWorldRedisOperator(t *testing.T) {
	// Test with the actual redis-operator-compat file
	db, err := NewAPKDatabaseFromDir("testdata/redis-operator")
	if err != nil {
		t.Fatalf("Failed to load real redis-operator database: %v", err)
	}

	pkg, exists := db.GetPackage("redis-operator-compat")
	if !exists {
		t.Error("redis-operator-compat package should exist in real data")
	}

	if pkg.Version != "0.21.0-r1" {
		t.Errorf("Expected version '0.21.0-r1', got '%s'", pkg.Version)
	}

	// Check that it contains the root-level 'operator' file
	foundOperator := false
	for _, file := range pkg.Files {
		if file == "operator" {
			foundOperator = true
			break
		}
	}
	if !foundOperator {
		t.Errorf("Should contain root-level 'operator' file, got files: %v", pkg.Files)
	}
}

func TestFileNotFound(t *testing.T) {
	_, err := NewAPKDatabaseFromFile("/non/existent/path")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}

	_, err = NewAPKDatabaseFromDir("/non/existent/dir")
	if err == nil {
		t.Error("Expected error for non-existent directory")
	}
}