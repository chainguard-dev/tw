package shelldeps

import (
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestResolveCommands(t *testing.T) {
	tests := []struct {
		name     string
		packages []string
		want     map[string]bool
	}{
		{
			name:     "single package",
			packages: []string{"bash"},
			want:     map[string]bool{"bash": true},
		},
		{
			name:     "multiple packages",
			packages: []string{"bash", "curl"},
			want:     map[string]bool{"bash": true, "curl": true},
		},
		{
			name:     "unknown package",
			packages: []string{"nonexistent-package"},
			want:     map[string]bool{},
		},
		{
			name:     "busybox provides many commands",
			packages: []string{"busybox"},
			want: func() map[string]bool {
				// We just check that busybox provides some expected commands
				m := make(map[string]bool)
				for _, cmd := range PackageProvides["busybox"] {
					m[cmd] = true
				}
				return m
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveCommands(tt.packages)

			// For the busybox test, we just verify it contains expected commands
			if tt.name == "busybox provides many commands" {
				expectedCmds := []string{"grep", "awk", "sed", "cat", "ls"}
				for _, cmd := range expectedCmds {
					if !got[cmd] {
						t.Errorf("expected busybox to provide %q", cmd)
					}
				}
				return
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ResolveCommands() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestFindMissingCommands(t *testing.T) {
	tests := []struct {
		name     string
		required []string
		packages []string
		want     []string
	}{
		{
			name:     "no missing - all from busybox",
			required: []string{"grep", "awk", "sed"},
			packages: []string{"busybox"},
			want:     nil,
		},
		{
			name:     "missing curl",
			required: []string{"grep", "curl", "jq"},
			packages: []string{"busybox"},
			want:     []string{"curl", "jq"},
		},
		{
			name:     "no missing with curl package",
			required: []string{"grep", "curl"},
			packages: []string{"busybox", "curl"},
			want:     nil,
		},
		{
			name:     "empty required",
			required: []string{},
			packages: []string{"busybox"},
			want:     nil,
		},
		{
			name:     "empty packages",
			required: []string{"grep", "curl"},
			packages: []string{},
			want:     []string{"grep", "curl"},
		},
		{
			name:     "jq package provides jq",
			required: []string{"jq", "grep"},
			packages: []string{"busybox", "jq"},
			want:     nil,
		},
		{
			name:     "custom command missing",
			required: []string{"my-custom-tool", "grep"},
			packages: []string{"busybox"},
			want:     []string{"my-custom-tool"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FindMissingCommands(tt.required, tt.packages)
			sort.Strings(got)
			sort.Strings(tt.want)

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("FindMissingCommands() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPackageProvidesContainsExpectedPackages(t *testing.T) {
	// Verify that common packages we expect are in the map
	expectedPackages := []string{
		"busybox",
		"coreutils",
		"bash",
		"curl",
		"jq",
		"git",
		"openssh",
		"grep",
		"sed",
	}

	for _, pkg := range expectedPackages {
		if _, ok := PackageProvides[pkg]; !ok {
			t.Errorf("expected PackageProvides to contain %q", pkg)
		}
	}
}

func TestBusyboxProvidesCommonCommands(t *testing.T) {
	busyboxCmds := PackageProvides["busybox"]
	cmdsMap := make(map[string]bool)
	for _, cmd := range busyboxCmds {
		cmdsMap[cmd] = true
	}

	expectedCmds := []string{
		"grep", "awk", "sed", "cat", "ls", "cp", "mv", "rm",
		"mkdir", "tar", "gzip", "wget", "ps", "kill", "env",
		"basename", "dirname", "sort", "uniq", "head", "tail",
	}

	for _, cmd := range expectedCmds {
		if !cmdsMap[cmd] {
			t.Errorf("expected busybox to provide %q", cmd)
		}
	}
}
