//go:build !windows

package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/creack/pty"
)

type unixPTY struct {
	cmd *exec.Cmd
	f   *os.File
}

func startPTY(exePath string, args []string) (ptySession, error) {
	cmd := exec.Command(exePath, args...)
	cmd.Env = os.Environ()
	if os.Getenv("TERM") == "" {
		cmd.Env = append(cmd.Env, "TERM=xterm-256color")
	}
	// pty.StartWithSize makes the child a session leader (Setsid), so pgid == pid
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 160})
	if err != nil {
		return nil, err
	}
	return &unixPTY{cmd: cmd, f: f}, nil
}

func (p *unixPTY) Read(b []byte) (int, error) {
	n, err := p.f.Read(b)
	if err != nil && errors.Is(err, syscall.EIO) {
		err = io.EOF // Linux returns EIO when the child side is closed
	}
	return n, err
}

func (p *unixPTY) Write(b []byte) (int, error) { return p.f.Write(b) }

func (p *unixPTY) Wait() (int, error) {
	err := p.cmd.Wait()
	time.Sleep(200 * time.Millisecond) // let the reader drain the last output
	if err == nil {
		return 0, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	return -1, err
}

// Interrupt sends SIGINT to the whole process group.
func (p *unixPTY) Interrupt() {
	if p.cmd.Process != nil {
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGINT)
	}
}

// Kill sends SIGKILL to the whole process group.
func (p *unixPTY) Kill() {
	if p.cmd.Process != nil {
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
		_ = p.cmd.Process.Kill()
	}
}

func (p *unixPTY) Close() error { return p.f.Close() }

// ------------------------------------------------------------
// Interactive terminal (login / accounts)
// ------------------------------------------------------------

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func runInTerminal(exePath string, args []string) error {
	parts := []string{shQuote(exePath)}
	for _, a := range args {
		parts = append(parts, shQuote(a))
	}
	script := strings.Join(parts, " ") + `; echo; echo 'Press Enter to close...'; read _`

	if runtime.GOOS == "darwin" {
		esc := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(script)
		as := `tell application "Terminal"
activate
set t to do script "` + esc + `"
repeat
delay 1
try
if not (busy of t) then exit repeat
on error
exit repeat
end try
end repeat
end tell`
		return exec.Command("osascript", "-e", as).Run()
	}

	type term struct {
		bin  string
		args []string
	}
	candidates := []term{
		{"gnome-terminal", []string{"--wait", "--", "bash", "-c", script}},
		{"konsole", []string{"--nofork", "-e", "bash", "-c", script}},
		{"xfce4-terminal", []string{"--disable-server", "-x", "bash", "-c", script}},
		{"kitty", []string{"bash", "-c", script}},
		{"alacritty", []string{"-e", "bash", "-c", script}},
		{"x-terminal-emulator", []string{"-e", "bash", "-c", script}},
		{"xterm", []string{"-e", "bash", "-c", script}},
	}
	for _, t := range candidates {
		p, err := exec.LookPath(t.bin)
		if err != nil {
			continue
		}
		cmd := exec.Command(p, t.args...)
		if err := cmd.Start(); err != nil {
			return err
		}
		_ = cmd.Wait()
		return nil
	}
	return errors.New("no supported terminal emulator found (tried gnome-terminal, konsole, xfce4-terminal, kitty, alacritty, x-terminal-emulator, xterm)")
}
