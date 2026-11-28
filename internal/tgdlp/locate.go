package tgdlp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func tgDlpNames() []string {
	if runtime.GOOS == "windows" {
		return []string{"tg-dlp.exe", "tg-dlp"}
	}
	return []string{"tg-dlp"}
}

func ResolvePath() (string, error) {
	names := tgDlpNames()
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			return filepath.Abs(p)
		}
	}

	var dirs []string
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	if cwd, err := os.Getwd(); err == nil {
		dirs = append(dirs, cwd)
	}
	for _, d := range dirs {
		for _, n := range names {
			c := filepath.Join(d, n)
			if info, err := os.Stat(c); err == nil && !info.IsDir() {
				return c, nil
			}
		}
	}

	return "", fmt.Errorf("tg-dlp not found in PATH or application directory")
}
