package unit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/abcdlsj/gnar/test/common"
)

func BuildGnarBinary() error {
	var once sync.Once
	once.Do(func() {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Printf("failed to get working directory: %v\n", err)
			return
		}

		projectRoot := filepath.Dir(filepath.Dir(wd))
		mainPath := filepath.Join(projectRoot, "cmd/gnar/main.go")

		cmd := exec.Command("go", "build", "-o", common.GnarPath, mainPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Printf("failed to build gnar: %v\n", err)
			return
		}

		if _, err := os.Stat(common.GnarPath); os.IsNotExist(err) {
			fmt.Printf("gnar binary not found at %s after build\n", common.GnarPath)
			return
		}

		fmt.Printf("Gnar binary built successfully at: %s\n", common.GnarPath)
	})
	return nil
}
