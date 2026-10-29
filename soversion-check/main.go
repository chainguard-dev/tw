package main

import (
	"context"
	"debug/elf"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"chainguard.dev/apko/pkg/apk/apk"
)

const progName = "soversion-check"

// Config holds the command-line configuration for soversion checking
type Config struct {
	Packages []string // APK packages whose own names are the expected suffix
	Files    []string // Files to check against Name
	Name     string   // Package name to check Files against
}

// Result tracks the outcomes of soversion checks
type Result struct {
	Passes       int
	Fails        int
	FailMessages []string
}

func (r *Result) AddPass(msg string) {
	r.Passes++
	fmt.Printf("PASS[%s]: %s\n", progName, msg)
}

func (r *Result) AddFail(msg string) {
	r.Fails++
	r.FailMessages = append(r.FailMessages, fmt.Sprintf("FAIL[%s]: %s", progName, msg))
}

func info(msg string) {
	fmt.Printf("INFO[%s]: %s\n", progName, msg)
}

// readSoname returns the DT_SONAME of the ELF file at path. It returns an
// empty string and no error for an ELF file without one, and an error for a
// file that is not ELF.
func readSoname(path string) (string, error) {
	f, err := elf.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	names, err := f.DynString(elf.DT_SONAME)
	if err != nil || len(names) == 0 {
		return "", nil
	}
	return names[0], nil
}

// soversionOf returns the part of soname after ".so.", for example "2.16"
// for libfoo.so.2.16, and false for an unversioned soname such as libfoo.so.
func soversionOf(soname string) (string, bool) {
	_, ver, ok := strings.Cut(soname, ".so.")
	if !ok || ver == "" {
		return "", false
	}
	return ver, true
}

// nameMatches reports whether name ends in soversion on a boundary, so that
// libfoo2 and libfoo-2 match "2" but libfoo12 and libfoo1.2 do not.
func nameMatches(name, soversion string) bool {
	prefix, ok := strings.CutSuffix(name, soversion)
	if !ok || prefix == "" {
		return false
	}
	last := prefix[len(prefix)-1]
	return last != '.' && (last < '0' || last > '9')
}

// isLibraryName reports whether a file name looks like a shared object.
func isLibraryName(path string) bool {
	base := filepath.Base(path)
	return strings.HasSuffix(base, ".so") || strings.Contains(base, ".so.")
}

// soProvides returns the so: entries of an APK provides list as a map from
// soname to provided version, for example "so:libfoo.so.2=2" gives
// libfoo.so.2 -> "2". Other provides, such as so-ver: and cmd:, are ignored.
func soProvides(provides []string) map[string]string {
	sonames := map[string]string{}
	for _, p := range provides {
		rest, ok := strings.CutPrefix(p, "so:")
		if !ok {
			continue
		}
		soname, ver, _ := strings.Cut(rest, "=")
		sonames[soname] = ver
	}
	return sonames
}

// checker checks the files of one package name and remembers the soversions
// that did not match, so it can suggest a remedy. With provides set, it also
// checks the so: provides that melange SCA generated for the package.
type checker struct {
	name     string
	result   *Result
	checked  int
	mismatch map[string]bool
	provides map[string]string
	seen     map[string]bool
}

// checkFile checks one file. With insist set, files that are not regular or
// have no SONAME fail; otherwise they are skipped.
func (c *checker) checkFile(path string, insist bool) {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() {
		if insist {
			c.result.AddFail(fmt.Sprintf("%s: not a regular file", path))
		}
		return
	}

	soname, err := readSoname(path)
	if err != nil || soname == "" {
		if insist {
			c.result.AddFail(fmt.Sprintf("%s: has no SONAME", path))
		}
		return
	}
	c.checked++
	c.seen[soname] = true

	ver, ok := soversionOf(soname)
	if !ok {
		c.result.AddFail(fmt.Sprintf("%s: SONAME %s is unversioned, so it cannot match %s", path, soname, c.name))
		return
	}
	if nameMatches(c.name, ver) {
		c.result.AddPass(fmt.Sprintf("%s: SONAME %s matches %s", path, soname, c.name))
	} else {
		c.result.AddFail(fmt.Sprintf("%s: SONAME %s has soversion %s, but the package name %s does not end in %s",
			path, soname, ver, c.name, ver))
		c.mismatch[ver] = true
	}
	if c.provides != nil {
		c.checkProvide(soname, ver)
	}
}

// checkProvide checks that the package provides so:<soname>=<ver> for a
// library it ships.
func (c *checker) checkProvide(soname, ver string) {
	got, ok := c.provides[soname]
	switch {
	case !ok:
		c.result.AddFail(fmt.Sprintf("%s: ships %s but does not provide so:%s; "+
			"check that the subpackage does not set options.no-provides", c.name, soname, soname))
	case got != ver:
		c.result.AddFail(fmt.Sprintf("%s: provides so:%s=%s, want so:%s=%s", c.name, soname, got, soname, ver))
	default:
		c.result.AddPass(fmt.Sprintf("%s: provides so:%s=%s", c.name, soname, got))
	}
}

// checkOtherProvides checks so: provides for sonames the package does not
// ship, such as hand-written provides entries, against the package name.
func (c *checker) checkOtherProvides() {
	sonames := make([]string, 0, len(c.provides))
	for soname := range c.provides {
		if !c.seen[soname] {
			sonames = append(sonames, soname)
		}
	}
	sort.Strings(sonames)
	for _, soname := range sonames {
		ver := c.provides[soname]
		if ver == "" {
			ver, _ = soversionOf(soname)
		}
		if ver != "" && nameMatches(c.name, ver) {
			c.result.AddPass(fmt.Sprintf("%s: provides so:%s=%s", c.name, soname, c.provides[soname]))
			continue
		}
		c.result.AddFail(fmt.Sprintf("%s: provides so:%s=%s, which does not match the package name",
			c.name, soname, c.provides[soname]))
		if ver != "" {
			c.mismatch[ver] = true
		}
	}
}

// remedy returns the fix for the soversions that did not match, or an empty
// string when everything matched.
func (c *checker) remedy() string {
	if len(c.mismatch) == 0 {
		return ""
	}
	vers := make([]string, 0, len(c.mismatch))
	for v := range c.mismatch {
		vers = append(vers, v)
	}
	sort.Strings(vers)
	if len(vers) == 1 {
		return fmt.Sprintf("FIX[%s]: %s ships soversion %s. If its name comes from vars.soversion, "+
			"set vars.soversion to %q in the package YAML; the subpackage name and its -dev dependency follow it.",
			progName, c.name, vers[0], vers[0])
	}
	return fmt.Sprintf("FIX[%s]: %s ships more than one soversion (%s). Split them into one subpackage per soversion.",
		progName, c.name, strings.Join(vers, ", "))
}

func newChecker(name string, result *Result) *checker {
	return &checker{name: name, result: result, mismatch: map[string]bool{}, seen: map[string]bool{}}
}

// checkFiles checks the files given on the command line against Name.
func (c *Config) checkFiles(result *Result) []string {
	ch := newChecker(c.Name, result)
	for _, f := range c.Files {
		ch.checkFile(f, true)
	}
	if fix := ch.remedy(); fix != "" {
		return []string{fix}
	}
	return nil
}

// checkPackages checks the shared libraries in each installed package against
// that package's own name.
func (c *Config) checkPackages(result *Result) []string {
	a, err := apk.New(context.Background())
	if err != nil {
		result.AddFail(fmt.Sprintf("failed to create apk client: %v", err))
		return nil
	}
	pkgs, err := a.GetInstalled()
	if err != nil {
		result.AddFail(fmt.Sprintf("failed to get installed packages: %v", err))
		return nil
	}
	pkgmap := map[string]*apk.InstalledPackage{}
	for _, pkg := range pkgs {
		pkgmap[pkg.Name] = pkg
	}

	var fixes []string
	for _, name := range c.Packages {
		pkg, ok := pkgmap[name]
		if !ok {
			result.AddFail(fmt.Sprintf("package not installed: %s", name))
			continue
		}
		info(fmt.Sprintf("Testing shared libraries in package %s", name))
		ch := newChecker(name, result)
		ch.provides = soProvides(pkg.Provides)
		for _, f := range pkg.Files {
			if isLibraryName(f.Name) {
				ch.checkFile("/"+f.Name, false)
			}
		}
		if ch.checked == 0 {
			result.AddFail(fmt.Sprintf("%s: contains no shared libraries with a SONAME", name))
		}
		ch.checkOtherProvides()
		if fix := ch.remedy(); fix != "" {
			fixes = append(fixes, fix)
		}
	}
	return fixes
}

func showHelp() {
	fmt.Printf(`Usage: %s [OPTIONS]

Check that the shared libraries in a soname-versioned package carry the
soversion that the package name ends in. For example, libfoo2 and libfoo-2
must only ship libraries whose SONAME is libfoo.so.2. With --packages, the
package must also provide so:libfoo.so.2=2, and every so: provide it has must
match its name.

Options:
  -h, --help                    Show this help message and exit
  --packages=PKG, --packages PKG
                               Specify installed packages to check. Each
                               package's own name is the expected suffix.
  --files=PATH, --files PATH    Specify files to check against --name
  --name=NAME, --name NAME      Package name to check --files against

Lists may be separated by spaces or commas.

Examples:
  %s --packages=libjemalloc2
  %s --files=/usr/lib/libfoo.so.2.0.0 --name=libfoo2
`, progName, progName, progName)
	os.Exit(0)
}

// splitList splits a space- or comma-separated list.
func splitList(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' })
}

func parseArgs(args []string) (*Config, error) {
	config := &Config{}
	fs := flag.NewFlagSet(progName, flag.ContinueOnError)

	var filesFlag, packagesFlag string
	var helpFlag bool
	fs.StringVar(&filesFlag, "files", "", "Specify files to check")
	fs.StringVar(&packagesFlag, "packages", "", "Specify packages to check")
	fs.StringVar(&config.Name, "name", "", "Package name to check --files against")
	fs.BoolVar(&helpFlag, "help", false, "Show help message")
	fs.BoolVar(&helpFlag, "h", false, "Show help message")
	fs.Usage = showHelp

	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if helpFlag {
		showHelp()
	}

	config.Files = splitList(filesFlag)
	if packagesFlag != "none" {
		config.Packages = splitList(packagesFlag)
	}

	if len(config.Files) == 0 && len(config.Packages) == 0 {
		return nil, errors.New("specify --packages or --files")
	}
	if len(config.Files) > 0 && config.Name == "" {
		return nil, errors.New("--files requires --name")
	}
	return config, nil
}

func main() {
	config, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Printf("ERROR[%s]: %v\n", progName, err)
		os.Exit(1)
	}

	result := &Result{}
	var fixes []string
	if len(config.Files) > 0 {
		fixes = append(fixes, config.checkFiles(result)...)
	}
	if len(config.Packages) > 0 {
		fixes = append(fixes, config.checkPackages(result)...)
	}

	total := result.Passes + result.Fails
	info(fmt.Sprintf("Ran [%d] checks with [%s]. [%d/%d] passed.", total, progName, result.Passes, total))

	if result.Fails > 0 {
		fmt.Println()
		fmt.Println("FAILED SONAME CHECKS:")
		for _, msg := range result.FailMessages {
			fmt.Println(msg)
		}
		for _, fix := range fixes {
			fmt.Println(fix)
		}
		os.Exit(1)
	}
}
