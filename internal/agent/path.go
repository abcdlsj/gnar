package agent

import (
	"os"
	"path/filepath"
)

func defaultStatePath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ".gnar-agent-state.json"
	}
	return filepath.Join(home, ".gnar", "agent-state.json")
}
