/*
Copyright 2024 ayoy.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name        string
		cmd         *exec.Cmd
		wantErr     bool
		errContains string
	}{
		{
			name:    "successful command",
			cmd:     exec.Command("echo", "hello"),
			wantErr: false,
		},
		{
			name:        "failing command",
			cmd:         exec.Command("sh", "-c", "echo error >&2; exit 1"),
			wantErr:     true,
			errContains: "failed with error",
		},
		{
			name:        "nonexistent command",
			cmd:         exec.Command("nonexistent-command-12345"),
			wantErr:     true,
			errContains: "failed with error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := Run(tt.cmd)
			if (err != nil) != tt.wantErr {
				t.Errorf("Run() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Run() error = %v, should contain %q", err, tt.errContains)
				}
			}
			if !tt.wantErr && len(output) == 0 {
				t.Error("Run() returned empty output for successful command")
			}
		})
	}
}

func TestGetNonEmptyLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "empty input",
			input: "",
			want:  []string{},
		},
		{
			name:  "single line",
			input: "single line",
			want:  []string{"single line"},
		},
		{
			name:  "multiple lines",
			input: "line1\nline2\nline3",
			want:  []string{"line1", "line2", "line3"},
		},
		{
			name:  "lines with blank lines",
			input: "line1\n\nline2\n\nline3",
			want:  []string{"line1", "line2", "line3"},
		},
		{
			name:  "lines with whitespace-only lines",
			input: "line1\n   \nline2\n\t\nline3",
			want:  []string{"line1", "line2", "line3"},
		},
		{
			name:  "only blank lines",
			input: "\n\n\n",
			want:  []string{},
		},
		{
			name:  "only whitespace lines",
			input: "   \n\t\n  \t  \n",
			want:  []string{},
		},
		{
			name:  "trailing newline",
			input: "line1\nline2\n",
			want:  []string{"line1", "line2"},
		},
		{
			name:  "leading newline",
			input: "\nline1\nline2",
			want:  []string{"line1", "line2"},
		},
		{
			name:  "mixed content",
			input: "\nline1\n\n  \nline2\n\t\nline3\n",
			want:  []string{"line1", "line2", "line3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetNonEmptyLines(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("GetNonEmptyLines() length = %v, want %v", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("GetNonEmptyLines()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestGetProjectDir(t *testing.T) {
	// Save original working directory
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	defer func() {
		// Restore original working directory
		_ = os.Chdir(origWd)
	}()

	tests := []struct {
		name            string
		setupFunc       func(t *testing.T) string // Returns the directory to change to
		wantErr         bool
		errContains     string
		validateProject bool // If true, verify go.mod exists in result
	}{
		{
			name: "from project root",
			setupFunc: func(t *testing.T) string {
				// Find the project root first
				dir, err := GetProjectDir()
				if err != nil {
					t.Fatalf("failed to find project dir: %v", err)
				}
				return dir
			},
			wantErr:         false,
			validateProject: true,
		},
		{
			name: "from subdirectory",
			setupFunc: func(t *testing.T) string {
				// Find project root, then go into a subdirectory
				dir, err := GetProjectDir()
				if err != nil {
					t.Fatalf("failed to find project dir: %v", err)
				}
				// Navigate to test/utils directory (where this test is running)
				subdir := filepath.Join(dir, "test", "utils")
				return subdir
			},
			wantErr:         false,
			validateProject: true,
		},
		{
			name: "from nested subdirectory",
			setupFunc: func(t *testing.T) string {
				// Find project root, then go into a deeper subdirectory
				dir, err := GetProjectDir()
				if err != nil {
					t.Fatalf("failed to find project dir: %v", err)
				}
				// Navigate to internal/controller directory
				subdir := filepath.Join(dir, "internal", "controller")
				if _, err := os.Stat(subdir); os.IsNotExist(err) {
					// Fallback to test/utils if internal/controller doesn't exist
					subdir = filepath.Join(dir, "test", "utils")
				}
				return subdir
			},
			wantErr:         false,
			validateProject: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup: change to the test directory
			testDir := tt.setupFunc(t)
			if err := os.Chdir(testDir); err != nil {
				t.Fatalf("failed to change directory to %s: %v", testDir, err)
			}

			// Act
			got, err := GetProjectDir()

			// Assert
			if (err != nil) != tt.wantErr {
				t.Errorf("GetProjectDir() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.errContains != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("GetProjectDir() error = %v, should contain %q", err, tt.errContains)
				}
			}

			if !tt.wantErr {
				// Verify the result is an absolute path
				if !filepath.IsAbs(got) {
					t.Errorf("GetProjectDir() = %v, want absolute path", got)
				}

				// Verify go.mod exists if requested
				if tt.validateProject {
					goModPath := filepath.Join(got, "go.mod")
					if _, err := os.Stat(goModPath); os.IsNotExist(err) {
						t.Errorf("GetProjectDir() = %v, but go.mod does not exist at %v", got, goModPath)
					}
				}
			}
		})
	}
}
