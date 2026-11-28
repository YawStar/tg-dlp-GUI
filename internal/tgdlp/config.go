package tgdlp

import (
	"fmt"
	"os"
	"path/filepath"
)

// ConfigDir is the folder tg-dlp keeps its data in (same rule as the CLI).
func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot find the user home directory: %w", err)
	}
	return filepath.Join(home, ".tg-dlp"), nil
}
