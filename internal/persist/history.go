package persist

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

type HistoryEntry struct {
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
	Form     *FormProfile `json:"form,omitempty"` // lets the user load the settings back
}

type HistoryStore struct {
	mu       sync.Mutex
	path     string
	entries  []HistoryEntry // oldest first
	onChange func()         // optional, called after every change (any goroutine)
}

func (h *HistoryStore) persistLocked() {
	b, err := json.MarshalIndent(h.entries, "", "  ")
	if err != nil {
		return
	}
	_ = writeFileAtomic(h.path, b)
}

func (h *HistoryStore) notify() {
	h.mu.Lock()
	cb := h.onChange
	h.mu.Unlock()
	if cb != nil {
		cb()
	}
}

func (h *HistoryStore) SetOnChange(cb func()) {
	h.mu.Lock()
	h.onChange = cb
	h.mu.Unlock()
}

func (h *HistoryStore) Add(e HistoryEntry) {
	h.mu.Lock()
	h.entries = append(h.entries, e)
	for len(h.entries) > maxHistoryEntries {
		if old := h.entries[0]; old.LogFile != "" {
			_ = os.Remove(filepath.Join(LogsDir(), old.LogFile))
		}
		h.entries = h.entries[1:]
	}
	h.persistLocked()
	h.mu.Unlock()
	h.notify()
}

func (h *HistoryStore) Update(e HistoryEntry) {
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

func (h *HistoryStore) Delete(id string) {
	h.mu.Lock()
	for i := range h.entries {
		if h.entries[i].ID == id {
			if lf := h.entries[i].LogFile; lf != "" {
				_ = os.Remove(filepath.Join(LogsDir(), lf))
			}
			h.entries = append(h.entries[:i], h.entries[i+1:]...)
			break
		}
	}
	h.persistLocked()
	h.mu.Unlock()
	h.notify()
}

func (h *HistoryStore) Clear() {
	h.mu.Lock()
	for _, e := range h.entries {
		if e.LogFile != "" {
			_ = os.Remove(filepath.Join(LogsDir(), e.LogFile))
		}
	}
	h.entries = nil
	h.persistLocked()
	h.mu.Unlock()
	h.notify()
}

// List returns a copy, newest first.
func (h *HistoryStore) List() []HistoryEntry {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]HistoryEntry, len(h.entries))
	for i, e := range h.entries {
		out[len(h.entries)-1-i] = e
	}
	return out
}

func OpenHistory() *HistoryStore {
	h := &HistoryStore{path: filepath.Join(AppDataDir(), "history.json")}
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

// HistSummary holds the last "N / M files" (or size) text of the running task.
var HistSummary atomic.Value // string
