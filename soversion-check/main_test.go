package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNameMatches(t *testing.T) {
	tests := []struct {
		name      string
		soversion string
		want      bool
	}{
		{"libjemalloc2", "2", true},
		{"libx11-6", "6", true},
		{"libpng16-16", "16", true},
		{"libskarnet2.16", "2.16", true},
		{"libx265-216", "216", true},
		{"libjemalloc2", "3", false},
		{"libfoo12", "2", false},
		{"libfoo1.2", "2", false},
		{"libskarnet2", "2.16", false},
		{"2", "2", false},
		{"libfoo", "2", false},
	}
	for _, tt := range tests {
		if got := nameMatches(tt.name, tt.soversion); got != tt.want {
			t.Errorf("nameMatches(%q, %q) = %v, want %v", tt.name, tt.soversion, got, tt.want)
		}
	}
}

func TestSoversionOf(t *testing.T) {
	tests := []struct {
		soname string
		want   string
		ok     bool
	}{
		{"libfoo.so.2", "2", true},
		{"libskarnet.so.2.16", "2.16", true},
		{"libX11-xcb.so.1", "1", true},
		{"libfoo.so", "", false},
		{"libfoo.so.", "", false},
	}
	for _, tt := range tests {
		got, ok := soversionOf(tt.soname)
		if got != tt.want || ok != tt.ok {
			t.Errorf("soversionOf(%q) = (%q, %v), want (%q, %v)", tt.soname, got, ok, tt.want, tt.ok)
		}
	}
}

func TestIsLibraryName(t *testing.T) {
	for path, want := range map[string]bool{
		"usr/lib/libfoo.so":         true,
		"usr/lib/libfoo.so.2":       true,
		"usr/lib/libfoo.so.2.0.0":   true,
		"usr/lib/pkgconfig/foo.pc":  false,
		"usr/share/doc/so.txt":      false,
		"usr/lib/python3/foo.so.py": true,
	} {
		if got := isLibraryName(path); got != want {
			t.Errorf("isLibraryName(%q) = %v, want %v", path, got, want)
		}
	}
}

// buildLib compiles a minimal shared object in dir. An empty soname builds one
// without DT_SONAME.
func buildLib(t *testing.T, dir, file, soname string) string {
	t.Helper()
	src := filepath.Join(dir, "lib.c")
	if err := os.WriteFile(src, []byte("int soversion_check_fixture(void) { return 0; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, file)
	args := []string{"-shared", "-fPIC", "-o", out, src}
	if soname != "" {
		args = append(args, "-Wl,-soname,"+soname)
	}
	if b, err := exec.Command("cc", args...).CombinedOutput(); err != nil {
		t.Fatalf("cc %v: %v\n%s", args, err, b)
	}
	return out
}

func TestCheckFile(t *testing.T) {
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skip("cc not available to build fixture libraries")
	}
	dir := t.TempDir()
	libfoo := buildLib(t, dir, "libfoo.so.2.0.0", "libfoo.so.2")
	libbaz := buildLib(t, dir, "libbaz.so.2.16.0", "libbaz.so.2.16")
	libbar := buildLib(t, dir, "libbar.so", "libbar.so")
	plugin := buildLib(t, dir, "plugin.so", "")
	link := filepath.Join(dir, "libfoo.so.2")
	if err := os.Symlink("libfoo.so.2.0.0", link); err != nil {
		t.Fatal(err)
	}
	text := filepath.Join(dir, "notes.so.txt")
	if err := os.WriteFile(text, []byte("not elf"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		desc    string
		file    string
		pkg     string
		insist  bool
		passes  int
		fails   int
		checked int
		fix     string
	}{
		{"matching soversion", libfoo, "libfoo2", true, 1, 0, 1, ""},
		{"matching soversion with hyphen", libfoo, "libfoo-2", true, 1, 0, 1, ""},
		{"matching dotted soversion", libbaz, "libbaz2.16", true, 1, 0, 1, ""},
		{"stale soversion", libfoo, "libfoo3", true, 0, 1, 1, `set vars.soversion to "2"`},
		{"digit boundary", libfoo, "libfoo12", true, 0, 1, 1, `set vars.soversion to "2"`},
		{"dotted name does not match major", libbaz, "libbaz2", true, 0, 1, 1, `set vars.soversion to "2.16"`},
		{"unversioned soname", libbar, "libbar1", true, 0, 1, 1, ""},
		{"no soname insisted", plugin, "plugin1", true, 0, 1, 0, ""},
		{"no soname skipped in packages", plugin, "plugin1", false, 0, 0, 0, ""},
		{"symlink insisted", link, "libfoo2", true, 0, 1, 0, ""},
		{"symlink skipped in packages", link, "libfoo2", false, 0, 0, 0, ""},
		{"not elf skipped in packages", text, "libfoo2", false, 0, 0, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			result := &Result{}
			ch := newChecker(tt.pkg, result)
			ch.checkFile(tt.file, tt.insist)
			if result.Passes != tt.passes || result.Fails != tt.fails || ch.checked != tt.checked {
				t.Errorf("passes/fails/checked = %d/%d/%d, want %d/%d/%d: %v",
					result.Passes, result.Fails, ch.checked, tt.passes, tt.fails, tt.checked, result.FailMessages)
			}
			fix := ch.remedy()
			if tt.fix == "" && fix != "" {
				t.Errorf("unexpected remedy %q", fix)
			}
			if tt.fix != "" && !strings.Contains(fix, tt.fix) {
				t.Errorf("remedy %q does not contain %q", fix, tt.fix)
			}
		})
	}
}

func TestRemedyMultipleSoversions(t *testing.T) {
	ch := newChecker("libfoo2", &Result{})
	ch.mismatch["3"] = true
	ch.mismatch["4"] = true
	if fix := ch.remedy(); !strings.Contains(fix, "more than one soversion (3, 4)") {
		t.Errorf("remedy = %q", fix)
	}
}

func TestParseArgs(t *testing.T) {
	c, err := parseArgs([]string{"--packages=libfoo2 libbar1,libbaz3"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(c.Packages, "|") != "libfoo2|libbar1|libbaz3" {
		t.Errorf("Packages = %v", c.Packages)
	}
	if _, err := parseArgs([]string{"--files=/usr/lib/libfoo.so.2"}); err == nil {
		t.Error("--files without --name: want error")
	}
	if _, err := parseArgs([]string{"--packages=none"}); err == nil {
		t.Error("--packages=none alone: want error")
	}
}
