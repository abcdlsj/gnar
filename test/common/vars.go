package common

import (
	"os"
	"path/filepath"
)

var GnarPath string

func init() {
	wd, _ := os.Getwd()
	GnarPath = filepath.Join(filepath.Dir(filepath.Dir(wd)), "gnar")
}
