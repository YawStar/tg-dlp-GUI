package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ============================================================
// App data directory
//   Windows : %AppData%\tg-dlp-gui\
//   macOS   : ~/Library/Application Support/tg-dlp-gui/
//   Linux   : ~/.config/tg-dlp-gui/
//
//   config.json   last used form values, theme, window size
//   history.json  task history (newest last)
//   logs/         one text log per task
// ============================================================

const (
	appDataDirName    = "tg-dlp-gui"
	maxHistoryEntries = 500
	maxTaskLogBytes   = 8 << 20 // per task log file
)

func appDataDir() string {
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

func logsDir() string {
	dir := filepath.Join(appDataDir(), "logs")
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

// ============================================================
// Config (last used form values)
// ============================================================

// formProfile holds the form values of ONE command (the entries are shared
// between commands in the UI, so each command remembers its own values).
type formProfile struct {
	Primary     string          `json:"primary"`
	Secondary   string          `json:"secondary"`
	Tertiary    string          `json:"tertiary"`
	Quaternary  string          `json:"quaternary"`
	Concurrency string          `json:"concurrency"`
	Extra       string          `json:"extra"`
	Checks      map[string]bool `json:"checks,omitempty"`
}

type appConfig struct {
	Version  int                    `json:"version"`
	Command  string                 `json:"command"`
	Theme    string                 `json:"theme"` // "dark" | "light"
	Proxy    string                 `json:"proxy"`
	Global   map[string]bool        `json:"global,omitempty"` // noProxy, skipFlood, debug
	Profiles map[string]formProfile `json:"profiles"`
	WinW     float32                `json:"win_w,omitempty"`
	WinH     float32                `json:"win_h,omitempty"`
}

func configPath() string { return filepath.Join(appDataDir(), "config.json") }

// loadConfig returns ok=false when there is no (valid) config yet.
func loadConfig() (appConfig, bool) {
	var c appConfig
	b, err := os.ReadFile(configPath())
	if err != nil {
		return appConfig{Profiles: map[string]formProfile{}}, false
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return appConfig{Profiles: map[string]formProfile{}}, false
	}
	if c.Profiles == nil {
		c.Profiles = map[string]formProfile{}
	}
	return c, true
}

func saveConfig(c appConfig) error {
	c.Version = 1
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(configPath(), b)
}

// ============================================================
// History
// ============================================================

type historyEntry struct {
	ID       string       `json:"id"`
	Command  string       `json:"command"` // e.g. "Download (dl)"
	Target   string       `json:"target"`  // URL / path / channel typed in the first field
	CmdLine  string       `json:"cmdline"` // passwords masked
	Started  time.Time    `json:"started"`
	Ended    time.Time    `json:"ended,omitempty"`
	Status   string       `json:"status"` // running | finished | failed | cancelled | interrupted
	ExitCode int          `json:"exit_code"`
	Summary  string       `json:"summary,omitempty"` // e.g. "68 / 210744 files"
	LogFile  string       `json:"log_file,omitempty"`
	Form     *formProfile `json:"form,omitempty"` // lets the user load the settings back
}

type historyStore struct {
	mu       sync.Mutex
	path     string
	entries  []historyEntry // oldest first
	onChange func()         // optional, called after every change (any goroutine)
}

func openHistory() *historyStore {
	h := &historyStore{path: filepath.Join(appDataDir(), "history.json")}
	if b, err := os.ReadFile(h.path); err == nil {
		_ = json.Unmarshal(b, &h.entries)
	}
	// anything still "running" at startup belongs to a previous run that died
	dirty := false
	for i := range h.entries {
		if h.entries[i].Status == "running" {
			h.entries[i].Status = "interrupted"
			dirty = true
		}
	}
	if dirty {
		h.persistLocked()
	}
	return h
}

func (h *historyStore) persistLocked() {
	b, err := json.MarshalIndent(h.entries, "", "  ")
	if err != nil {
		return
	}
	_ = writeFileAtomic(h.path, b)
}

func (h *historyStore) notify() {
	h.mu.Lock()
	cb := h.onChange
	h.mu.Unlock()
	if cb != nil {
		cb()
	}
}

func (h *historyStore) SetOnChange(cb func()) {
	h.mu.Lock()
	h.onChange = cb
	h.mu.Unlock()
}

func (h *historyStore) Add(e historyEntry) {
	h.mu.Lock()
	h.entries = append(h.entries, e)
	for len(h.entries) > maxHistoryEntries {
		if old := h.entries[0]; old.LogFile != "" {
			_ = os.Remove(filepath.Join(logsDir(), old.LogFile))
		}
		h.entries = h.entries[1:]
	}
	h.persistLocked()
	h.mu.Unlock()
	h.notify()
}

func (h *historyStore) Update(e historyEntry) {
	h.mu.Lock()
	for i := range h.entries {
		if h.entries[i].ID == e.ID {
			h.entries[i] = e
			break
		}
	}
	h.persistLocked()
	h.mu.Unlock()
	h.notify()
}

func (h *historyStore) Delete(id string) {
	h.mu.Lock()
	for i := range h.entries {
		if h.entries[i].ID == id {
			if lf := h.entries[i].LogFile; lf != "" {
				_ = os.Remove(filepath.Join(logsDir(), lf))
			}
			h.entries = append(h.entries[:i], h.entries[i+1:]...)
			break
		}
	}
	h.persistLocked()
	h.mu.Unlock()
	h.notify()
}

func (h *historyStore) Clear() {
	h.mu.Lock()
	for _, e := range h.entries {
		if e.LogFile != "" {
			_ = os.Remove(filepath.Join(logsDir(), e.LogFile))
		}
	}
	h.entries = nil
	h.persistLocked()
	h.mu.Unlock()
	h.notify()
}

// List returns a copy, newest first.
func (h *historyStore) List() []historyEntry {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]historyEntry, len(h.entries))
	for i, e := range h.entries {
		out[len(h.entries)-1-i] = e
	}
	return out
}

// ============================================================
// Per-task log file
// ============================================================

type taskLogger struct {
	mu        sync.Mutex
	f         *os.File
	w         *bufio.Writer
	n         int64
	truncated bool
	lastFlush time.Time
}

// taskLog is the log of the task that is running right now (nil if none).
var taskLog atomic.Pointer[taskLogger]

func newTaskLogger(path string) *taskLogger {
	f, err := os.Create(path)
	if err != nil {
		return nil
	}
	return &taskLogger{f: f, w: bufio.NewWriterSize(f, 64*1024), lastFlush: time.Now()}
}

// Line appends one line (nil-safe). Output stops at maxTaskLogBytes.
func (t *taskLogger) Line(s string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.f == nil || t.truncated {
		return
	}
	if t.n > maxTaskLogBytes {
		t.truncated = true
		_, _ = t.w.WriteString("[log truncated: size limit reached]\n")
		_ = t.w.Flush()
		return
	}
	line := time.Now().Format("15:04:05") + "  " + s + "\n"
	n, _ := t.w.WriteString(line)
	t.n += int64(n)
	if time.Since(t.lastFlush) > time.Second {
		_ = t.w.Flush()
		t.lastFlush = time.Now()
	}
}

func (t *taskLogger) Close() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.f == nil {
		return
	}
	_ = t.w.Flush()
	_ = t.f.Close()
	t.f = nil
}

// readLogTail returns at most maxBytes from the end of a task log.
func readLogTail(name string, maxBytes int64) (string, error) {
	f, err := os.Open(filepath.Join(logsDir(), name))
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	prefix := ""
	if st.Size() > maxBytes {
		if _, err := f.Seek(st.Size()-maxBytes, 0); err != nil {
			return "", err
		}
		prefix = fmt.Sprintf("... (showing the last %d KiB, open the log file to see everything)\n", maxBytes/1024)
	}
	b := make([]byte, maxBytes)
	n, _ := f.Read(b)
	s := string(b[:n])
	if prefix != "" {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:] // drop the partial first line
		}
	}
	return prefix + s, nil
}

// ============================================================
// Helpers
// ============================================================

// redactArgs masks secrets before a command line is written to history:
// passwords inside URLs (http://user:pass@host) and the value after --password.
func redactArgs(args []string) []string {
	out := make([]string, len(args))
	maskNext := false
	for i, a := range args {
		switch {
		case i == 1 && args[0] == "config-pw": // config-pw <password> [--profile name]
			out[i] = "***"
		case maskNext:
			out[i] = "***"
			maskNext = false
		case a == "--password" || a == "--pass":
			out[i] = a
			maskNext = true
		case strings.Contains(a, "://"):
			out[i] = redactURLPassword(a)
		default:
			out[i] = a
		}
	}
	return out
}

var urlPasswordRe = regexp.MustCompile(`(://[^/:@\s]+:)[^@/\s]+@`)

func redactURLPassword(s string) string {
	return urlPasswordRe.ReplaceAllString(s, "${1}***@")
}

// cmdShortName: "Download (dl)" -> "dl"
func cmdShortName(title string) string {
	if i := strings.LastIndex(title, "("); i >= 0 {
		if j := strings.Index(title[i:], ")"); j > 0 {
			return title[i+1 : i+j]
		}
	}
	return "task"
}

// histSummary holds the last "N / M files" (or size) text of the running task.
var histSummary atomic.Value // string
