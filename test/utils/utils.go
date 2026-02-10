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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Run executes the provided command and returns its combined output.
// If the command fails, the error includes stderr for debugging.
func Run(cmd *exec.Cmd) ([]byte, error) {
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("%s failed with error: (%v) %s", cmd.Path, err, string(output))
	}
	return output, nil
}

// LoadImageToKindCluster loads a Docker image into a Kind cluster.
func LoadImageToKindCluster(name, clusterName string) error {
	cmd := exec.Command("kind", "load", "docker-image", name, "--name", clusterName)
	if _, err := Run(cmd); err != nil {
		return err
	}
	return nil
}

// GetNonEmptyLines splits the input string by newlines and returns
// only the non-empty lines.
func GetNonEmptyLines(input string) []string {
	var result []string
	for _, line := range strings.Split(input, "\n") {
		if strings.TrimSpace(line) != "" {
			result = append(result, line)
		}
	}
	return result
}

// GetProjectDir returns the root directory of the project by
// looking for the go.mod file.
func GetProjectDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return wd, fmt.Errorf("failed to get working directory: %w", err)
	}

	// Walk up the directory tree looking for go.mod
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return wd, fmt.Errorf("unable to find go.mod in any parent directory of %s", wd)
}
