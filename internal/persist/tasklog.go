package persist

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type TaskLogger struct {
	mu        sync.Mutex
	f         *os.File
	w         *bufio.Writer
	n         int64
	truncated bool
	lastFlush time.Time
}

// Line appends one line (nil-safe). Output stops at maxTaskLogBytes.
func (t *TaskLogger) Line(s string) {
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

func (t *TaskLogger) Close() {
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

// TaskLog is the log of the task that is running right now (nil if none).
var TaskLog atomic.Pointer[TaskLogger]

func NewTaskLogger(path string) *TaskLogger {
	f, err := os.Create(path)
	if err != nil {
		return nil
	}
	return &TaskLogger{f: f, w: bufio.NewWriterSize(f, 64*1024), lastFlush: time.Now()}
}

// ReadLogTail returns at most maxBytes from the end of a task log.
func ReadLogTail(name string, maxBytes int64) (string, error) {
	f, err := os.Open(filepath.Join(LogsDir(), name))
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

// RedactArgs masks secrets before a command line is written to history:
// passwords inside URLs (http://user:pass@host) and the value after --password.
func RedactArgs(args []string) []string {
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
			out[i] = RedactURLPassword(a)
		default:
			out[i] = a
		}
	}
	return out
}

var urlPasswordRe = regexp.MustCompile(`(://[^/:@\s]+:)[^@/\s]+@`)

func RedactURLPassword(s string) string {
	return urlPasswordRe.ReplaceAllString(s, "${1}***@")
}

// CmdShortName: "Download (dl)" -> "dl"
func CmdShortName(title string) string {
	if i := strings.LastIndex(title, "("); i >= 0 {
		if j := strings.Index(title[i:], ")"); j > 0 {
			return title[i+1 : i+j]
		}
	}
	return "task"
}
