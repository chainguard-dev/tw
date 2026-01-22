package shelldeps

import (
	"strings"
	"testing"
)

func TestCheckGNUCompatibility(t *testing.T) {
	tests := []struct {
		name          string
		script        string
		wantIssues    int
		wantCommands  []string // Commands expected to be flagged
		wantNoIssues  []string // Commands that should NOT be flagged
	}{
		{
			name: "realpath --no-symlinks",
			script: `#!/bin/sh
path=$(realpath --no-symlinks /some/path)
echo $path
`,
			wantIssues:   1,
			wantCommands: []string{"realpath"},
		},
		{
			name: "realpath --relative-base",
			script: `#!/bin/bash
path=$(realpath --relative-base=/opt /opt/foo/bar)
`,
			wantIssues:   1,
			wantCommands: []string{"realpath"},
		},
		{
			name: "realpath -q quiet mode",
			script: `#!/bin/sh
path=$(realpath -q /some/path)
`,
			wantIssues:   1,
			wantCommands: []string{"realpath"},
		},
		{
			name: "stat --format",
			script: `#!/bin/sh
size=$(stat --format=%s file.txt)
`,
			wantIssues:   1,
			wantCommands: []string{"stat"},
		},
		{
			name: "stat --printf",
			script: `#!/bin/bash
stat --printf='%s' file.txt
`,
			wantIssues:   1,
			wantCommands: []string{"stat"},
		},
		{
			name: "cp --reflink",
			script: `#!/bin/sh
cp --reflink=auto src dest
`,
			wantIssues:   1,
			wantCommands: []string{"cp"},
		},
		{
			name: "date --iso-8601",
			script: `#!/bin/bash
today=$(date --iso-8601)
`,
			wantIssues:   1,
			wantCommands: []string{"date"},
		},
		{
			name: "date -I shorthand",
			script: `#!/bin/sh
today=$(date -I)
`,
			wantIssues:   1,
			wantCommands: []string{"date"},
		},
		{
			name: "mktemp --suffix",
			script: `#!/bin/bash
tmpfile=$(mktemp --suffix=.txt)
`,
			wantIssues:   1,
			wantCommands: []string{"mktemp"},
		},
		{
			name: "sort -h human numeric",
			script: `#!/bin/sh
du -h | sort -h
`,
			wantIssues:   1,
			wantCommands: []string{"sort"},
		},
		{
			name: "ls --time-style",
			script: `#!/bin/bash
ls -l --time-style=long-iso
`,
			wantIssues:   1,
			wantCommands: []string{"ls"},
		},
		{
			name: "df --output",
			script: `#!/bin/sh
df --output=source,target
`,
			wantIssues:   1,
			wantCommands: []string{"df"},
		},
		{
			name: "readlink -e",
			script: `#!/bin/bash
target=$(readlink -e symlink)
`,
			wantIssues:   1,
			wantCommands: []string{"readlink"},
		},
		{
			name: "readlink -m",
			script: `#!/bin/sh
target=$(readlink -m path)
`,
			wantIssues:   1,
			wantCommands: []string{"readlink"},
		},
		{
			name: "readlink -f is ok",
			script: `#!/bin/sh
target=$(readlink -f symlink)
`,
			wantIssues:   0,
			wantNoIssues: []string{"readlink"},
		},
		{
			name: "tail --pid",
			script: `#!/bin/bash
tail --pid=$$ -f logfile
`,
			wantIssues:   1,
			wantCommands: []string{"tail"},
		},
		{
			name: "touch --date",
			script: `#!/bin/sh
touch --date="2024-01-01" file
`,
			wantIssues:   1,
			wantCommands: []string{"touch"},
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
			name: "comments are skipped",
			script: `#!/bin/sh
# realpath --no-symlinks would be nice
# stat --format is also useful
echo "hello"
`,
			wantIssues: 0,
		},
		{
			name: "install -D",
			script: `#!/bin/sh
install -D binary /usr/local/bin/
`,
			wantIssues:   1,
			wantCommands: []string{"install"},
		},
		{
			name: "chmod --reference",
			script: `#!/bin/bash
chmod --reference=file1 file2
`,
			wantIssues:   1,
			wantCommands: []string{"chmod"},
		},
		{
			name: "chown --reference",
			script: `#!/bin/sh
chown --reference=file1 file2
`,
			wantIssues:   1,
			wantCommands: []string{"chown"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.script)
			issues, err := CheckGNUCompatibility(r, "test.sh")

			if err != nil {
				t.Fatalf("CheckGNUCompatibility() error = %v", err)
			}

			if len(issues) != tt.wantIssues {
				t.Errorf("CheckGNUCompatibility() found %d issues, want %d", len(issues), tt.wantIssues)
				for _, issue := range issues {
					t.Logf("  Issue: %s (line %d)", issue.Description, issue.Line)
				}
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

			// Check that commands that should NOT be flagged aren't
			if len(tt.wantNoIssues) > 0 {
				foundCmds := make(map[string]bool)
				for _, issue := range issues {
					foundCmds[issue.Command] = true
				}

				for _, noIssue := range tt.wantNoIssues {
					if foundCmds[noIssue] {
						t.Errorf("command %q should NOT have been flagged", noIssue)
					}
				}
			}
		})
	}
}

func TestHasGNUCoreutils(t *testing.T) {
	tests := []struct {
		name     string
		packages []string
		want     bool
	}{
		{
			name:     "has coreutils",
			packages: []string{"busybox", "coreutils", "bash"},
			want:     true,
		},
		{
			name:     "no coreutils",
			packages: []string{"busybox", "bash"},
			want:     false,
		},
		{
			name:     "only coreutils",
			packages: []string{"coreutils"},
			want:     true,
		},
		{
			name:     "empty list",
			packages: []string{},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasGNUCoreutils(tt.packages)
			if got != tt.want {
				t.Errorf("HasGNUCoreutils() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNeedsGNUCoreutils(t *testing.T) {
	tests := []struct {
		name             string
		incompatibilities []GNUIncompatibility
		want             bool
	}{
		{
			name:             "no incompatibilities",
			incompatibilities: []GNUIncompatibility{},
			want:             false,
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
		name             string
		incompatibilities []GNUIncompatibility
		filename         string
		wantEmpty        bool
		wantContains     []string
	}{
		{
			name:             "empty list",
			incompatibilities: []GNUIncompatibility{},
			filename:         "test.sh",
			wantEmpty:        true,
		},
		{
			name: "single issue",
			incompatibilities: []GNUIncompatibility{
				{
					Command:     "realpath",
					Line:        5,
					Description: "realpath --no-symlinks (GNU only)",
					Fix:         "Add 'coreutils' to runtime dependencies",
				},
			},
			filename:     "script.sh",
			wantEmpty:    false,
			wantContains: []string{"script.sh", "Line 5", "realpath", "coreutils"},
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

func TestTruncateLine(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		maxLen int
		want   string
	}{
		{
			name:   "short string",
			s:      "hello",
			maxLen: 10,
			want:   "hello",
		},
		{
			name:   "exact length",
			s:      "hello",
			maxLen: 5,
			want:   "hello",
		},
		{
			name:   "needs truncation",
			s:      "hello world this is a long string",
			maxLen: 15,
			want:   "hello world ...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateLine(tt.s, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateLine() = %q, want %q", got, tt.want)
			}
		})
	}
}
