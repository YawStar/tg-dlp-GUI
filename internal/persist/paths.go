package persist

import (
	"os"
	"path/filepath"
)

// ============================================================
// App data directory
//
//	Windows : %AppData%\tg-dlp-gui\
//	macOS   : ~/Library/Application Support/tg-dlp-gui/
//	Linux   : ~/.config/tg-dlp-gui/
//
//	config.json   last used form values, theme, window size
//	history.json  task history (newest last)
//	logs/         one text log per task
//
// ============================================================
const (
	appDataDirName    = "tg-dlp-gui"
	maxHistoryEntries = 500
	maxTaskLogBytes   = 8 << 20 // per task log file
)

func AppDataDir() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		if exe, e := os.Executable(); e == nil {
			base = filepath.Dir(exe)
		} else {
			base = "."
		}
	}
	dir := filepath.Join(base, appDataDirName)
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func LogsDir() string {
	dir := filepath.Join(AppDataDir(), "logs")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// writeFileAtomic writes to a temp file first, then renames it over the target,
// so a crash can never leave a half-written config/history file.
func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
