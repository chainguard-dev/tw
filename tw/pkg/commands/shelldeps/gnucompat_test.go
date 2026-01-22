package shelldeps

import (
	"strings"
	"testing"

	"mvdan.cc/sh/v3/syntax"
)

func parseScript(t *testing.T, script string) *syntax.File {
	t.Helper()
	parser := syntax.NewParser(syntax.Variant(syntax.LangBash))
	file, err := parser.Parse(strings.NewReader(script), "test.sh")
	if err != nil {
		t.Fatalf("failed to parse script: %v", err)
	}
	return file
}

func TestCheckGNUCompatibilityAST(t *testing.T) {
	tests := []struct {
		name         string
		script       string
		wantIssues   int
		wantCommands []string // Commands expected to be flagged
		wantFlags    []string // Flags expected to be flagged
	}{
		{
			name: "realpath --no-symlinks",
			script: `#!/bin/sh
path=$(realpath --no-symlinks /some/path)
echo $path
`,
			wantIssues:   1,
			wantCommands: []string{"realpath"},
			wantFlags:    []string{"--no-symlinks"},
		},
		{
			name: "realpath --relative-base",
			script: `#!/bin/bash
path=$(realpath --relative-base=/opt /opt/foo/bar)
`,
			wantIssues:   1,
			wantCommands: []string{"realpath"},
			wantFlags:    []string{"--relative-base"},
		},
		{
			name: "stat --format",
			script: `#!/bin/sh
size=$(stat --format=%s file.txt)
`,
			wantIssues:   1,
			wantCommands: []string{"stat"},
			wantFlags:    []string{"--format"},
		},
		{
			name: "cp --reflink",
			script: `#!/bin/sh
cp --reflink=auto src dest
`,
			wantIssues:   1,
			wantCommands: []string{"cp"},
			wantFlags:    []string{"--reflink"},
		},
		{
			name: "date --iso-8601",
			script: `#!/bin/bash
today=$(date --iso-8601)
`,
			wantIssues:   1,
			wantCommands: []string{"date"},
			wantFlags:    []string{"--iso-8601"},
		},
		{
			name: "date -I shorthand",
			script: `#!/bin/sh
today=$(date -I)
`,
			wantIssues:   1,
			wantCommands: []string{"date"},
			wantFlags:    []string{"-I"},
		},
		{
			name: "chmod --reference",
			script: `#!/bin/bash
chmod --reference=file1 file2
`,
			wantIssues:   1,
			wantCommands: []string{"chmod"},
			wantFlags:    []string{"--reference"},
		},
		{
			name: "install -D",
			script: `#!/bin/sh
install -D binary /usr/local/bin/
`,
			wantIssues:   1,
			wantCommands: []string{"install"},
			wantFlags:    []string{"-D"},
		},
		{
			name: "install -Dm755 combined flags",
			script: `#!/bin/sh
install -Dm755 binary /usr/local/bin/
`,
			wantIssues:   1,
			wantCommands: []string{"install"},
			wantFlags:    []string{"-D"},
		},
		{
			name: "multiple issues",
			script: `#!/bin/bash
path=$(realpath --no-symlinks /opt)
size=$(stat --format=%s file)
today=$(date --iso-8601)
`,
			wantIssues:   3,
			wantCommands: []string{"realpath", "stat", "date"},
		},
		{
			name: "no issues - busybox compatible",
			script: `#!/bin/sh
path=$(realpath /some/path)
size=$(stat -c %s file.txt)
target=$(readlink -f symlink)
ls -la /tmp
`,
			wantIssues: 0,
		},
		{
			name: "multiline command handled correctly",
			script: `#!/bin/sh
chmod \
  --reference=foo \
  bar
`,
			wantIssues:   1,
			wantCommands: []string{"chmod"},
			wantFlags:    []string{"--reference"},
		},
		{
			name: "no false positive - different command uses --reference",
			script: `#!/bin/sh
chmod 644 foo && some-command --reference foo
`,
			wantIssues: 0, // chmod doesn't use --reference here
		},
		{
			name: "readlink -f is ok",
			script: `#!/bin/sh
target=$(readlink -f symlink)
`,
			wantIssues: 0,
		},
		{
			name: "readlink -e is GNU only",
			script: `#!/bin/sh
target=$(readlink -e symlink)
`,
			wantIssues:   1,
			wantCommands: []string{"readlink"},
			wantFlags:    []string{"-e"},
		},
		{
			name: "sort -h is GNU only",
			script: `#!/bin/sh
du -h | sort -h
`,
			wantIssues:   1,
			wantCommands: []string{"sort"},
			wantFlags:    []string{"-h"},
		},
		{
			name: "mktemp --suffix",
			script: `#!/bin/bash
tmpfile=$(mktemp --suffix=.txt)
`,
			wantIssues:   1,
			wantCommands: []string{"mktemp"},
			wantFlags:    []string{"--suffix"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := parseScript(t, tt.script)
			issues := CheckGNUCompatibilityAST(file, "test.sh")

			if len(issues) != tt.wantIssues {
				t.Errorf("CheckGNUCompatibilityAST() found %d issues, want %d", len(issues), tt.wantIssues)
				for _, issue := range issues {
					t.Logf("  Issue: %s %s (line %d)", issue.Command, issue.Flag, issue.Line)
				}
				return
			}

			// Check that expected commands were flagged
			if len(tt.wantCommands) > 0 {
				foundCmds := make(map[string]bool)
				for _, issue := range issues {
					foundCmds[issue.Command] = true
				}

				for _, wantCmd := range tt.wantCommands {
					if !foundCmds[wantCmd] {
						t.Errorf("expected command %q to be flagged", wantCmd)
					}
				}
			}

			// Check that expected flags were flagged
			if len(tt.wantFlags) > 0 {
				foundFlags := make(map[string]bool)
				for _, issue := range issues {
					foundFlags[issue.Flag] = true
				}

				for _, wantFlag := range tt.wantFlags {
					if !foundFlags[wantFlag] {
						t.Errorf("expected flag %q to be flagged", wantFlag)
					}
				}
			}
		})
	}
}

func TestMatchesFlag(t *testing.T) {
	tests := []struct {
		name  string
		arg   string
		flag  string
		want  bool
	}{
		{"exact match long", "--reflink", "--reflink", true},
		{"exact match short", "-D", "-D", true},
		{"long flag with value", "--reflink=auto", "--reflink", true},
		{"long flag no match", "--other", "--reflink", false},
		{"combined short flags", "-Dm755", "-D", true},
		{"combined short flags no match", "-m755", "-D", false},
		{"short flag in arg", "-h", "-h", true},
		{"different short flag", "-v", "-h", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesFlag(tt.arg, tt.flag)
			if got != tt.want {
				t.Errorf("matchesFlag(%q, %q) = %v, want %v", tt.arg, tt.flag, got, tt.want)
			}
		})
	}
}

func TestNeedsGNUCoreutils(t *testing.T) {
	tests := []struct {
		name              string
		incompatibilities []GNUIncompatibility
		want              bool
	}{
		{
			name:              "no incompatibilities",
			incompatibilities: []GNUIncompatibility{},
			want:              false,
		},
		{
			name: "has incompatibilities",
			incompatibilities: []GNUIncompatibility{
				{Command: "realpath", Description: "test"},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NeedsGNUCoreutils(tt.incompatibilities)
			if got != tt.want {
				t.Errorf("NeedsGNUCoreutils() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatIncompatibilities(t *testing.T) {
	tests := []struct {
		name              string
		incompatibilities []GNUIncompatibility
		filename          string
		wantEmpty         bool
		wantContains      []string
	}{
		{
			name:              "empty list",
			incompatibilities: []GNUIncompatibility{},
			filename:          "test.sh",
			wantEmpty:         true,
		},
		{
			name: "single issue",
			incompatibilities: []GNUIncompatibility{
				{
					Command:     "realpath",
					Flag:        "--no-symlinks",
					Line:        5,
					Description: "realpath --no-symlinks (GNU only)",
					Fix:         "Add 'coreutils' to runtime dependencies",
				},
			},
			filename:     "script.sh",
			wantEmpty:    false,
			wantContains: []string{"script.sh", "Line 5", "realpath"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatIncompatibilities(tt.incompatibilities, tt.filename)

			if tt.wantEmpty && got != "" {
				t.Errorf("FormatIncompatibilities() = %q, want empty", got)
			}

			if !tt.wantEmpty && got == "" {
				t.Errorf("FormatIncompatibilities() = empty, want non-empty")
			}

			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("FormatIncompatibilities() output should contain %q, got: %s", want, got)
				}
			}
		})
	}
}
