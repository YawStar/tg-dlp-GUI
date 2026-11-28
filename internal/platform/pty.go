package platform

import (
	"io"
)

// PTYSession is a running tg-dlp process attached to a pseudo terminal.
// Implemented per-OS in platform_windows.go (ConPTY) and platform_unix.go (creack/pty).
type PTYSession interface {
	io.Reader
	Write(p []byte) (int, error)
	Wait() (int, error) // blocks until the process exits, returns exit code
	Interrupt()         // graceful stop (Ctrl+C / SIGINT)
	Kill()              // force kill (whole process tree)
	Close() error
}
