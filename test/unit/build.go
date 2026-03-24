package unit

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

var (
	buildOnce sync.Once
	buildPath string
	buildErr  error
)

func BinaryPath(t *testing.T) string {
	t.Helper()

	buildOnce.Do(func() {
		buildPath, buildErr = buildBinary("./cmd/gnar", "gnar-build-*", "gnar-test")
	})

	if buildErr != nil {
		t.Fatalf("build failed: %v", buildErr)
	}

	return buildPath
}

func buildBinary(pkg, dirPattern, outputName string) (string, error) {
	dir, err := os.MkdirTemp("", dirPattern)
	if err != nil {
		return "", err
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	root := filepath.Clean(filepath.Join(wd, "../.."))
	outputPath := filepath.Join(dir, outputName)
	cmd := exec.Command("go", "build", "-o", outputPath, pkg)
	cmd.Dir = root
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return outputPath, nil
}
