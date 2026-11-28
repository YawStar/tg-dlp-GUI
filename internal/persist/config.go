// Package persist stores the GUI's own data on disk: last used form values
// (config.json), the task history (history.json) and one log file per task.

package persist

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// FormProfile holds the form values of ONE command (the entries are shared
// between commands in the UI, so each command remembers its own values).
type FormProfile struct {
	Primary     string          `json:"primary"`
	Secondary   string          `json:"secondary"`
	Tertiary    string          `json:"tertiary"`
	Quaternary  string          `json:"quaternary"`
	Template    string          `json:"template,omitempty"`
	Concurrency string          `json:"concurrency"`
	Extra       string          `json:"extra"`
	Checks      map[string]bool `json:"checks,omitempty"`
}

type AppConfig struct {
	Version  int                    `json:"version"`
	Command  string                 `json:"command"`
	Theme    string                 `json:"theme"` // "dark" | "light"
	Proxy    string                 `json:"proxy"`
	Global   map[string]bool        `json:"global,omitempty"` // noProxy, skipFlood, debug
	Profiles map[string]FormProfile `json:"profiles"`
	WinW     float32                `json:"win_w,omitempty"`
	WinH     float32                `json:"win_h,omitempty"`
}

func configPath() string { return filepath.Join(AppDataDir(), "config.json") }

// LoadConfig returns ok=false when there is no (valid) config yet.
func LoadConfig() (AppConfig, bool) {
	var c AppConfig
	b, err := os.ReadFile(configPath())
	if err != nil {
		return AppConfig{Profiles: map[string]FormProfile{}}, false
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return AppConfig{Profiles: map[string]FormProfile{}}, false
	}
	if c.Profiles == nil {
		c.Profiles = map[string]FormProfile{}
	}
	return c, true
}

func SaveConfig(c AppConfig) error {
	c.Version = 1
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(configPath(), b)
}
